// Package parse builds the parsed-tables structure (the exact shape
// ParseSchemaLocalJob stores in cache on the FakerForge server) from
// live-database introspection, plus a synthesized CREATE TABLE statement
// per table for the `sql` context field.
//
// No SQL parsing is involved: introspection via information_schema is
// exact, so no AI fallback is ever needed on this path.
package parse

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
)

// Field is one column in the parsed shape. Null=true means nullable.
// Only Name/Type/Null are read downstream; keep it minimal.
type Field = api.ParsedField

// IndexCol is a single column reference inside an index.
type IndexCol = api.ParsedIndexCol

// Index types must be UPPERCASE: the server compares them strictly
// ($index['type'] === 'FOREIGN' / 'UNIQUE').
type Index = api.ParsedIndex

// Table is the parsed shape of one table, keyed by table name in the
// upload payload.
type Table = api.ParsedTable

// BuildParsedTable converts introspected columns into the parsed shape.
// inSet holds the pulled table names: FKs pointing outside it are pruned
// (the server drops out-of-set refs the same way).
func BuildParsedTable(table, database string, cols []db.Column, inSet map[string]bool) Table {
	t := Table{
		Name:     table,
		Database: database,
		Fields:   make([]Field, 0, len(cols)),
		Indexes:  make([]Index, 0, 4),
	}

	var pkCols []IndexCol
	for _, c := range cols {
		// Length is only a real constraint for declared-limit types.
		// TEXT/BLOB families report their inherent storage cap (up to 4GB
		// for longtext), which is not a constraint: drop it so prompts
		// stay clean and server validation passes.
		length := c.Length
		if !sizedType(c.Type) {
			length = 0
		}
		t.Fields = append(t.Fields, Field{
			Name: c.Name, Type: c.Type, Null: c.Nullable,
			Length: length, Unsigned: c.Unsigned,
			Precision: c.Precision, Scale: c.Scale, Values: c.Values,
		})
		if c.IsPK {
			pkCols = append(pkCols, IndexCol{Name: c.Name})
		}
	}
	if len(pkCols) > 0 {
		t.Indexes = append(t.Indexes, Index{Type: "PRIMARY", Cols: pkCols})
	}
	for _, c := range cols {
		if c.Unique && !c.IsPK {
			t.Indexes = append(t.Indexes, Index{Type: "UNIQUE", Cols: []IndexCol{{Name: c.Name}}})
		}
	}
	for _, c := range cols {
		refTable, refCol, ok := splitFKRef(c.FKRef)
		if !ok {
			continue
		}
		if inSet != nil && !inSet[refTable] {
			continue
		}
		t.Indexes = append(t.Indexes, Index{
			Type:     "FOREIGN",
			Cols:     []IndexCol{{Name: c.Name}},
			RefTable: refTable,
			RefCols:  []IndexCol{{Name: refCol}},
		})
	}

	t.SQL = SynthesizeDDL(table, t.Fields, t.Indexes)
	return t
}

// sizedType reports whether Length is a declared constraint for the
// base type (as opposed to an inherent storage cap).
func sizedType(t string) bool {
	switch ddlType(t) {
	case "varchar", "char", "binary", "varbinary":
		return true
	}
	return false
}

// splitFKRef splits "table.column" into its parts.
func splitFKRef(ref string) (string, string, bool) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// SynthesizeDDL renders a deterministic CREATE TABLE statement from
// already-pruned fields and indexes. Context only (for the server's AI
// mapping prompts and overview) — never executed.
func SynthesizeDDL(table string, fields []Field, indexes []Index) string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE " + quoteIdent(table) + " (\n")
	defs := make([]string, 0, len(fields)+len(indexes))
	for _, f := range fields {
		null := "NOT NULL"
		if f.Null {
			null = "NULL"
		}
		defs = append(defs, fmt.Sprintf("  %s %s %s", quoteIdent(f.Name), ddlTypeFor(f), null))
	}
	for _, idx := range indexes {
		switch idx.Type {
		case "PRIMARY":
			defs = append(defs, fmt.Sprintf("  PRIMARY KEY (%s)", joinCols(idx.Cols)))
		case "UNIQUE":
			if len(idx.Cols) == 1 {
				defs = append(defs, fmt.Sprintf("  UNIQUE (%s)", joinCols(idx.Cols)))
			}
		case "FOREIGN":
			if idx.RefTable != "" && len(idx.RefCols) > 0 {
				defs = append(defs, fmt.Sprintf("  FOREIGN KEY (%s) REFERENCES %s (%s)",
					joinCols(idx.Cols), quoteIdent(idx.RefTable), joinCols(idx.RefCols)))
			}
		}
	}
	sb.WriteString(strings.Join(defs, ",\n"))
	sb.WriteString("\n);\n")
	return sb.String()
}

func quoteIdent(ident string) string {
	ident = strings.ReplaceAll(ident, `"`, "")
	return `"` + ident + `"`
}

func joinCols(cols []IndexCol) string {
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, quoteIdent(c.Name))
	}
	return strings.Join(names, ", ")
}

// ddlType maps a normalized introspected type to a DDL type name.
// Introspection already yields lowercase base types; fall back to TEXT
// for anything unknown rather than emitting invalid SQL.
func ddlType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "" {
		return "TEXT"
	}
	if i := strings.Index(t, "("); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	return t
}

// ddlTypeFor renders a field's full DDL type with constraints: length,
// unsigned, precision/scale, and inline ENUM/SET values (MySQL-style
// single quotes, embedded quotes doubled).
func ddlTypeFor(f Field) string {
	base := ddlType(f.Type)
	if (base == "enum" || base == "set") && len(f.Values) > 0 {
		quoted := make([]string, 0, len(f.Values))
		for _, v := range f.Values {
			quoted = append(quoted, "'"+strings.ReplaceAll(v, "'", "''")+"'")
		}
		return strings.ToUpper(base) + "(" + strings.Join(quoted, ",") + ")"
	}
	switch base {
	case "varchar", "char", "binary", "varbinary":
		if f.Length > 0 {
			return base + "(" + strconv.Itoa(f.Length) + ")"
		}
	case "decimal", "numeric", "float", "double":
		if f.Precision > 0 {
			if f.Scale > 0 {
				return base + "(" + strconv.Itoa(f.Precision) + "," + strconv.Itoa(f.Scale) + ")"
			}
			return base + "(" + strconv.Itoa(f.Precision) + ")"
		}
	}
	if f.Unsigned {
		return base + " unsigned"
	}
	return base
}

// DisplayType renders a compact human-readable type for the transparency
// print: varchar(255), int unsigned, enum('a','b').
func DisplayType(c db.Column) string {
	f := Field{
		Type: c.Type, Length: c.Length, Unsigned: c.Unsigned,
		Precision: c.Precision, Scale: c.Scale, Values: c.Values,
	}
	rendered := ddlTypeFor(f)
	// DDL renders ENUM in caps; display prefers the base name style.
	if strings.HasPrefix(rendered, "ENUM(") || strings.HasPrefix(rendered, "SET(") {
		return strings.ToLower(rendered[:4]) + rendered[4:]
	}
	return rendered
}

// Fingerprint returns the pull idempotency key: sha256 hex of
// driver|host|port|database|sorted_tables. Same database + same tables
// always hashes identically, so re-pulling reuses the server schema.
func Fingerprint(driver, host string, port int, database string, tables []string) string {
	sorted := make([]string, len(tables))
	copy(sorted, tables)
	sort.Strings(sorted)
	raw := strings.Join([]string{
		driver, host, strconv.Itoa(port), database, strings.Join(sorted, ","),
	}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
