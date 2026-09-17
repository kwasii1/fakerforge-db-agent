package db

import (
	"testing"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
)

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
