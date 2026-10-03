package db

import (
	"strings"
	"testing"
)

func mustCompile(t *testing.T, expr string) *compiledCheck {
	t.Helper()
	c, err := compileCheck(CheckConstraint{Name: "chk", Expr: NormalizeCheckExpr(expr)})
	if err != nil {
		t.Fatalf("compile %q: %v", expr, err)
	}
	return c
}

func TestCheckEvaluatesDialectRenderings(t *testing.T) {
	cases := []struct {
		name string
		expr string
		pass map[string]any
		fail map[string]any
	}{
		{"mysql comparison", "(`price` > 0)",
			map[string]any{"price": float64(5)}, map[string]any{"price": float64(0)}},
		{"mysql in with introducers", "(`status` in (_utf8mb4'draft',_utf8mb4'published'))",
			map[string]any{"status": "draft"}, map[string]any{"status": "archived"}},
		{"mysql between", "(`qty` between 1 and 10)",
			map[string]any{"qty": float64(10)}, map[string]any{"qty": float64(11)}},
		{"mysql column comparison", "(`ends_at` > `starts_at`)",
			map[string]any{"starts_at": "2024-01-01 10:00:00", "ends_at": "2024-01-02 10:00:00"},
			map[string]any{"starts_at": "2024-01-02 10:00:00", "ends_at": "2024-01-01 10:00:00"}},
		{"postgres numeric cast", "CHECK ((price > (0)::numeric))",
			map[string]any{"price": "12.50"}, map[string]any{"price": "-1.00"}},
		{"postgres any array", "CHECK (((status)::text = ANY ((ARRAY['draft'::character varying, 'published'::character varying])::text[])))",
			map[string]any{"status": "published"}, map[string]any{"status": "deleted"}},
		{"postgres conjunction", "CHECK (((rating >= 1) AND (rating <= 5)))",
			map[string]any{"rating": float64(3)}, map[string]any{"rating": float64(6)}},
		{"negative literal", "(`balance` >= -100)",
			map[string]any{"balance": float64(-100)}, map[string]any{"balance": float64(-101)}},
		{"or with is null", "((discount IS NULL) OR (discount < price))",
			map[string]any{"discount": nil, "price": float64(1)}, map[string]any{"discount": float64(5), "price": float64(1)}},
		{"not in", "(`kind` not in ('x','y'))",
			map[string]any{"kind": "z"}, map[string]any{"kind": "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mustCompile(t, tc.expr)
			if !c.passes(tc.pass) {
				t.Errorf("expected pass for %v", tc.pass)
			}
			if c.passes(tc.fail) {
				t.Errorf("expected fail for %v", tc.fail)
			}
		})
	}
}

func TestCheckTreatsNullAndMissingAsUnknown(t *testing.T) {
	c := mustCompile(t, "(`price` > 0)")
	if !c.passes(map[string]any{"price": nil}) {
		t.Error("NULL operand must pass (UNKNOWN)")
	}
	if !c.passes(map[string]any{}) {
		t.Error("absent column must pass (UNKNOWN)")
	}
}

func TestCheckLeavesCaseOnlyDifferencesToDatabase(t *testing.T) {
	c := mustCompile(t, "(`status` in ('Draft'))")
	if !c.passes(map[string]any{"status": "draft"}) {
		t.Error("case-only mismatch depends on collation and must not be rejected")
	}
}

func TestCheckComparesNumericLookingStringsAsText(t *testing.T) {
	c := mustCompile(t, "(`code` < '9')")
	if !c.passes(map[string]any{"code": "10"}) {
		t.Error("'10' < '9' is true for text comparison")
	}
}

func TestCompileCheckRejectsUnsupportedSyntax(t *testing.T) {
	for _, expr := range []string{
		"(char_length(`name`) > 0)",
		"(`total` = (`price` * `qty`))",
		"(`email` like '%@%')",
	} {
		if _, err := compileCheck(CheckConstraint{Expr: expr}); err == nil {
			t.Errorf("expected %q to be unsupported", expr)
		}
	}
}

func TestCheckColumns(t *testing.T) {
	cols := []Column{{Name: "id"}, {Name: "starts_at"}, {Name: "ends_at"}}
	got := CheckColumns("(`ends_at` > `starts_at`)", cols)
	if strings.Join(got, ",") != "starts_at,ends_at" {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeCheckExpr(t *testing.T) {
	if got := NormalizeCheckExpr("CHECK ((qty > 0)) NOT VALID"); got != "((qty > 0))" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateRowEnforcesChecks(t *testing.T) {
	v := NewRowValidator([]Column{{Name: "price", Type: "decimal"}, {Name: "name", Type: "varchar"}})
	skipped := v.WithChecks([]CheckConstraint{
		{Name: "price_positive", Expr: "(`price` > 0)", Cols: []string{"price"}},
		{Name: "name_len", Expr: "(char_length(`name`) > 0)", Cols: []string{"name"}},
	})
	if len(skipped) != 1 || skipped[0].Name != "name_len" {
		t.Fatalf("expected name_len skipped, got %v", skipped)
	}
	if err := v.ValidateRow(0, map[string]any{"price": float64(1), "name": ""}); err != nil {
		t.Fatalf("valid row rejected: %v", err)
	}
	err := v.ValidateRow(1, map[string]any{"price": float64(-5), "name": "a"})
	if err == nil || !strings.Contains(err.Error(), "price_positive") || !strings.Contains(err.Error(), "price=-5") {
		t.Fatalf("expected CHECK violation naming constraint and value, got %v", err)
	}
}
