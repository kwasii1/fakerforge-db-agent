package db

import (
	"fmt"
	"strconv"
	"strings"
)

// CheckConstraint is one introspected CHECK constraint. Expr is the
// boolean expression as the database renders it, without the CHECK
// keyword. Cols are the table columns the expression references.
type CheckConstraint struct {
	Name string
	Expr string
	Cols []string
}

// NormalizeCheckExpr strips the dialect wrapping around a CHECK clause:
// Postgres' "CHECK (...)" prefix and "NOT VALID" suffix.
func NormalizeCheckExpr(def string) string {
	expr := strings.TrimSpace(def)
	if len(expr) >= 5 && strings.EqualFold(expr[:5], "CHECK") {
		expr = strings.TrimSpace(expr[5:])
	}
	if n := len(expr); n >= 9 && strings.EqualFold(expr[n-9:], "NOT VALID") {
		expr = strings.TrimSpace(expr[:n-9])
	}
	return expr
}

// buildChecks normalizes driver rows into CheckConstraints, dropping
// empty expressions.
func buildChecks[T any](rows []T, fields func(T) (string, string), cols []Column) []CheckConstraint {
	out := make([]CheckConstraint, 0, len(rows))
	for _, r := range rows {
		name, def := fields(r)
		expr := NormalizeCheckExpr(def)
		if expr == "" {
			continue
		}
		out = append(out, CheckConstraint{Name: name, Expr: expr, Cols: CheckColumns(expr, cols)})
	}
	return out
}

// CheckColumns returns the columns (from cols, in table order) that the
// expression references. Matching is case-insensitive.
func CheckColumns(expr string, cols []Column) []string {
	toks, err := tokenizeCheck(expr)
	if err != nil {
		return nil
	}
	refs := map[string]bool{}
	for _, t := range toks {
		if t.kind == tokIdent || t.kind == tokQuotedIdent {
			refs[strings.ToLower(t.text)] = true
		}
	}
	var out []string
	for _, c := range cols {
		if refs[strings.ToLower(c.Name)] {
			out = append(out, c.Name)
		}
	}
	return out
}

// compiledCheck is a CHECK constraint the validator can evaluate.
type compiledCheck struct {
	CheckConstraint
	root checkNode
}

// compileCheck parses a constraint expression. Expressions using syntax
// outside the supported subset (functions, arithmetic, LIKE, ...) return
// an error and are left to the database to enforce.
func compileCheck(c CheckConstraint) (*compiledCheck, error) {
	toks, err := tokenizeCheck(c.Expr)
	if err != nil {
		return nil, err
	}
	p := &checkParser{toks: toks}
	root, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if !p.done() {
		return nil, fmt.Errorf("unexpected %q", p.peek().text)
	}
	return &compiledCheck{CheckConstraint: c, root: root}, nil
}

// passes reports whether the row satisfies the check. Following SQL
// semantics, an UNKNOWN result (a NULL or absent operand) passes.
func (c *compiledCheck) passes(row map[string]any) bool {
	v := c.root.eval(row)
	b, ok := v.(bool)
	return !ok || b
}

// ---- tokenizer ----

type tokKind int

const (
	tokIdent tokKind = iota
	tokQuotedIdent
	tokString
	tokNumber
	tokOp
	tokEOF
)

type checkToken struct {
	kind tokKind
	text string
}

func tokenizeCheck(s string) ([]checkToken, error) {
	var out []checkToken
	i := 0
	for i < len(s) {
		ch := s[i]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			i++
		case ch == '\'':
			str, n, err := readQuoted(s[i:], '\'')
			if err != nil {
				return nil, err
			}
			out = append(out, checkToken{tokString, str})
			i += n
		case ch == '`' || ch == '"':
			str, n, err := readQuoted(s[i:], ch)
			if err != nil {
				return nil, err
			}
			out = append(out, checkToken{tokQuotedIdent, str})
			i += n
		case isDigit(ch) || (ch == '.' && i+1 < len(s) && isDigit(s[i+1])):
			j := i
			for j < len(s) && (isDigit(s[j]) || s[j] == '.' || s[j] == 'e' || s[j] == 'E' ||
				((s[j] == '+' || s[j] == '-') && j > i && (s[j-1] == 'e' || s[j-1] == 'E'))) {
				j++
			}
			out = append(out, checkToken{tokNumber, s[i:j]})
			i = j
		case isIdentStart(ch):
			j := i
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			word := s[i:j]
			// MySQL charset introducer (_utf8mb4'x') prefixes a string literal.
			if word[0] == '_' && j < len(s) && s[j] == '\'' {
				i = j
				continue
			}
			out = append(out, checkToken{tokIdent, word})
			i = j
		default:
			if i+1 < len(s) {
				two := s[i : i+2]
				switch two {
				case "<=", ">=", "<>", "!=", "::":
					out = append(out, checkToken{tokOp, two})
					i += 2
					continue
				}
			}
			switch ch {
			case '(', ')', ',', '=', '<', '>', '[', ']', '-', '+', '*', '/', '%':
				out = append(out, checkToken{tokOp, string(ch)})
				i++
			default:
				return nil, fmt.Errorf("unsupported character %q", ch)
			}
		}
	}
	return out, nil
}

// readQuoted reads a quoted run starting at s[0]==q, honoring doubled
// quotes and backslash escapes. Returns the unquoted text and bytes read.
func readQuoted(s string, q byte) (string, int, error) {
	var b strings.Builder
	i := 1
	for i < len(s) {
		ch := s[i]
		if ch == '\\' && q == '\'' && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i += 2
			continue
		}
		if ch == q {
			if i+1 < len(s) && s[i+1] == q {
				b.WriteByte(q)
				i += 2
				continue
			}
			return b.String(), i + 1, nil
		}
		b.WriteByte(ch)
		i++
	}
	return "", 0, fmt.Errorf("unterminated quote")
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) || c == '$' }

// ---- parser ----
//
// Supported subset (both dialects' rendered forms):
//   expr      := and (OR and)*
//   and       := not (AND not)*
//   not       := NOT not | predicate
//   predicate := operand [cmp operand | cmp ANY(ARRAY[...]) | [NOT] BETWEEN operand AND operand
//                | [NOT] IN (operand, ...) | IS [NOT] NULL]
//   operand   := ['-'] (number | string | column | TRUE | FALSE | NULL | '(' expr ')') ['::' type]*

type checkParser struct {
	toks []checkToken
	pos  int
}

func (p *checkParser) peek() checkToken {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return checkToken{kind: tokEOF}
}

func (p *checkParser) next() checkToken {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *checkParser) done() bool { return p.pos >= len(p.toks) }

func (p *checkParser) isKeyword(word string) bool {
	t := p.peek()
	return t.kind == tokIdent && strings.EqualFold(t.text, word)
}

func (p *checkParser) isOp(op string) bool {
	t := p.peek()
	return t.kind == tokOp && t.text == op
}

func (p *checkParser) expectOp(op string) error {
	if !p.isOp(op) {
		return fmt.Errorf("expected %q, got %q", op, p.peek().text)
	}
	p.pos++
	return nil
}

func (p *checkParser) parseOr() (checkNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("OR") {
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orNode{left, right}
	}
	return left, nil
}

func (p *checkParser) parseAnd() (checkNode, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("AND") {
		p.pos++
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = andNode{left, right}
	}
	return left, nil
}

func (p *checkParser) parseNot() (checkNode, error) {
	if p.isKeyword("NOT") {
		p.pos++
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return notNode{inner}, nil
	}
	return p.parsePredicate()
}

func (p *checkParser) parsePredicate() (checkNode, error) {
	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}

	if t := p.peek(); t.kind == tokOp {
		switch t.text {
		case "=", "<>", "!=", "<", "<=", ">", ">=":
			p.pos++
			if p.isKeyword("ANY") {
				p.pos++
				list, err := p.parseAnyArray()
				if err != nil {
					return nil, err
				}
				if t.text != "=" {
					return nil, fmt.Errorf("unsupported %s ANY", t.text)
				}
				return inNode{left, list, false}, nil
			}
			right, err := p.parseOperand()
			if err != nil {
				return nil, err
			}
			return cmpNode{t.text, left, right}, nil
		}
	}

	negate := false
	if p.isKeyword("NOT") {
		p.pos++
		negate = true
	}
	switch {
	case p.isKeyword("BETWEEN"):
		p.pos++
		lo, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		if !p.isKeyword("AND") {
			return nil, fmt.Errorf("BETWEEN without AND")
		}
		p.pos++
		hi, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		var n checkNode = andNode{cmpNode{">=", left, lo}, cmpNode{"<=", left, hi}}
		if negate {
			n = notNode{n}
		}
		return n, nil
	case p.isKeyword("IN"):
		p.pos++
		if err := p.expectOp("("); err != nil {
			return nil, err
		}
		list, err := p.parseList(")")
		if err != nil {
			return nil, err
		}
		return inNode{left, list, negate}, nil
	case p.isKeyword("IS") && !negate:
		p.pos++
		isNot := false
		if p.isKeyword("NOT") {
			p.pos++
			isNot = true
		}
		if !p.isKeyword("NULL") {
			return nil, fmt.Errorf("unsupported IS predicate")
		}
		p.pos++
		return isNullNode{left, isNot}, nil
	}
	if negate {
		return nil, fmt.Errorf("unsupported NOT predicate")
	}
	return left, nil
}

// parseAnyArray parses Postgres' "ANY ((ARRAY[...])::type[])" rendering
// of an IN list.
func (p *checkParser) parseAnyArray() ([]checkNode, error) {
	depth := 0
	for p.isOp("(") {
		p.pos++
		depth++
	}
	if !p.isKeyword("ARRAY") {
		return nil, fmt.Errorf("unsupported ANY operand")
	}
	p.pos++
	if err := p.expectOp("["); err != nil {
		return nil, err
	}
	list, err := p.parseList("]")
	if err != nil {
		return nil, err
	}
	for depth > 0 {
		p.skipCasts()
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		depth--
	}
	p.skipCasts()
	return list, nil
}

func (p *checkParser) parseList(closer string) ([]checkNode, error) {
	var list []checkNode
	for {
		item, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		list = append(list, item)
		if p.isOp(",") {
			p.pos++
			continue
		}
		if err := p.expectOp(closer); err != nil {
			return nil, err
		}
		return list, nil
	}
}

func (p *checkParser) parseOperand() (checkNode, error) {
	neg := false
	if p.isOp("-") {
		p.pos++
		neg = true
	}
	var node checkNode
	t := p.next()
	switch t.kind {
	case tokNumber:
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, err
		}
		if neg {
			f = -f
			neg = false
		}
		node = litNode{f}
	case tokString:
		node = litNode{t.text}
	case tokQuotedIdent:
		node = colNode{strings.ToLower(t.text)}
	case tokIdent:
		switch strings.ToUpper(t.text) {
		case "TRUE":
			node = litNode{true}
		case "FALSE":
			node = litNode{false}
		case "NULL":
			node = litNode{nil}
		case "AND", "OR", "NOT", "IN", "IS", "BETWEEN", "ANY", "ARRAY", "LIKE":
			return nil, fmt.Errorf("unexpected keyword %q", t.text)
		default:
			if p.isOp("(") {
				return nil, fmt.Errorf("unsupported function %q", t.text)
			}
			node = colNode{strings.ToLower(t.text)}
		}
	case tokOp:
		if t.text != "(" {
			return nil, fmt.Errorf("unexpected %q", t.text)
		}
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		node = inner
	default:
		return nil, fmt.Errorf("unexpected end of expression")
	}
	if neg {
		return nil, fmt.Errorf("unsupported unary minus")
	}
	p.skipCasts()
	if t := p.peek(); t.kind == tokOp && len(t.text) == 1 && strings.Contains("+-*/%", t.text) {
		return nil, fmt.Errorf("unsupported arithmetic")
	}
	return node, nil
}

// skipCasts consumes Postgres "::type" suffixes, including multi-word
// types, a "(n[,m])" modifier and "[]" array markers. Casts never change
// how a generated value compares, so they are dropped.
func (p *checkParser) skipCasts() {
	for p.isOp("::") {
		p.pos++
		for p.peek().kind == tokIdent && !p.isTerminatorKeyword() {
			p.pos++
		}
		if p.isOp("(") {
			for !p.done() && !p.isOp(")") {
				p.pos++
			}
			p.pos++
		}
		for p.isOp("[") {
			p.pos++
			_ = p.expectOp("]")
		}
	}
}

func (p *checkParser) isTerminatorKeyword() bool {
	for _, k := range []string{"AND", "OR", "NOT", "IN", "IS", "BETWEEN"} {
		if p.isKeyword(k) {
			return true
		}
	}
	return false
}

// ---- evaluation ----
//
// eval returns bool, float64, string or nil (SQL NULL / UNKNOWN).

type checkNode interface{ eval(row map[string]any) any }

type litNode struct{ v any }

func (n litNode) eval(map[string]any) any { return n.v }

type colNode struct{ name string }

func (n colNode) eval(row map[string]any) any {
	for k, v := range row {
		if strings.ToLower(k) == n.name {
			return v
		}
	}
	return nil
}

type andNode struct{ l, r checkNode }

func (n andNode) eval(row map[string]any) any {
	l, r := truth(n.l.eval(row)), truth(n.r.eval(row))
	if l == 0 || r == 0 {
		return false
	}
	if l < 0 || r < 0 {
		return nil
	}
	return true
}

type orNode struct{ l, r checkNode }

func (n orNode) eval(row map[string]any) any {
	l, r := truth(n.l.eval(row)), truth(n.r.eval(row))
	if l == 1 || r == 1 {
		return true
	}
	if l < 0 || r < 0 {
		return nil
	}
	return false
}

type notNode struct{ inner checkNode }

func (n notNode) eval(row map[string]any) any {
	switch truth(n.inner.eval(row)) {
	case 1:
		return false
	case 0:
		return true
	}
	return nil
}

type isNullNode struct {
	operand checkNode
	not     bool
}

func (n isNullNode) eval(row map[string]any) any {
	isNull := n.operand.eval(row) == nil
	return isNull != n.not
}

type cmpNode struct {
	op   string
	l, r checkNode
}

func (n cmpNode) eval(row map[string]any) any {
	c, ok := compareValues(n.l.eval(row), n.r.eval(row))
	if !ok {
		return nil
	}
	switch n.op {
	case "=":
		return c == 0
	case "<>", "!=":
		return c != 0
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	}
	return nil
}

type inNode struct {
	operand checkNode
	list    []checkNode
	not     bool
}

func (n inNode) eval(row map[string]any) any {
	v := n.operand.eval(row)
	if v == nil {
		return nil
	}
	unknown := false
	for _, item := range n.list {
		c, ok := compareValues(v, item.eval(row))
		if !ok {
			unknown = true
			continue
		}
		if c == 0 {
			return !n.not
		}
	}
	if unknown {
		return nil
	}
	return n.not
}

// truth maps a value to 1 (true), 0 (false) or -1 (unknown).
func truth(v any) int {
	switch b := v.(type) {
	case bool:
		if b {
			return 1
		}
		return 0
	case float64:
		if b != 0 {
			return 1
		}
		return 0
	}
	return -1
}

// compareValues orders two operands. ok=false means the comparison is
// UNKNOWN: a NULL operand, incomparable types, or strings that differ
// only by case (collation-dependent, so the database decides).
func compareValues(a, b any) (int, bool) {
	if a == nil || b == nil {
		return 0, false
	}
	_, aIsString := a.(string)
	_, bIsString := b.(string)
	// Two strings compare as text even when they look numeric: that is
	// how the database orders VARCHAR values.
	if !aIsString || !bIsString {
		fa, okA := asNumber(a)
		fb, okB := asNumber(b)
		if okA && okB {
			switch {
			case fa < fb:
				return -1, true
			case fa > fb:
				return 1, true
			}
			return 0, true
		}
	}
	sa, okA := a.(string)
	sb, okB := b.(string)
	if !okA || !okB {
		return 0, false
	}
	if sa == sb {
		return 0, true
	}
	if strings.EqualFold(sa, sb) {
		return 0, false
	}
	return strings.Compare(sa, sb), true
}

func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}
