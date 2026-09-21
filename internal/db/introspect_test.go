package db

import (
	"testing"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
)

func TestParseEnumValues(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"enum('a','b','c')", []string{"a", "b", "c"}},
		{"set('x','y')", []string{"x", "y"}},
		{"ENUM('A','B')", []string{"A", "B"}},
		{`enum('it''s','ok')`, []string{"it's", "ok"}},
		{"enum('single')", []string{"single"}},
		{"varchar(255)", nil},
		{"int(11) unsigned", nil},
		{"", nil},
	}
	for _, tc := range cases {
		got := parseEnumValues(tc.in)
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if len(got) != len(tc.want) {
			t.Fatalf("parseEnumValues(%q) = %v, want %v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("parseEnumValues(%q) = %v, want %v", tc.in, got, tc.want)
			}
		}
	}
}

func TestIntValue(t *testing.T) {
	if intValue(nil) != 0 {
		t.Fatal("nil should be 0")
	}
	neg, pos := int64(-1), int64(42)
	if intValue(&neg) != 0 {
		t.Fatal("negative should be 0")
	}
	if intValue(&pos) != 42 {
		t.Fatal("positive should pass through")
	}
}

func TestCheckSubsetOK(t *testing.T) {
	schema := []api.SchemaColumn{
		{Name: "email", Type: "varchar"},
		{Name: "age", Type: "integer"},
	}
	target := []Column{
		{Name: "id", Type: "integer"},
		{Name: "email", Type: "varchar"},
		{Name: "age", Type: "bigint"},
	}
	if ok, diff := CheckSubset(schema, target); !ok {
		t.Fatalf("expected ok: %s", diff)
	}
}

func TestCheckSubsetMissing(t *testing.T) {
	schema := []api.SchemaColumn{{Name: "nope", Type: "text"}}
	target := []Column{{Name: "id", Type: "integer"}}
	if ok, diff := CheckSubset(schema, target); ok || diff == "" {
		t.Fatal("expected mismatch")
	}
}

func TestCheckSubsetTypeMismatch(t *testing.T) {
	schema := []api.SchemaColumn{{Name: "age", Type: "varchar"}}
	target := []Column{{Name: "age", Type: "integer"}}
	if ok, _ := CheckSubset(schema, target); ok {
		t.Fatal("expected type mismatch")
	}
}

func TestCompatibleAliases(t *testing.T) {
	cases := [][2]string{
		{"character varying", "varchar"},
		{"int", "bigint"},
		{"bool", "boolean"},
		{"datetime", "timestamp"},
		{"uuid", "text"},
	}
	for _, c := range cases {
		if !compatibleTypes(c[0], c[1]) {
			t.Fatalf("%s vs %s should be compatible", c[0], c[1])
		}
	}
	if compatibleTypes("varchar", "integer") {
		t.Fatal("varchar vs integer must not be compatible")
	}
}
