package parse

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kwasii1/fakerforge-db-agent/internal/db"
)

func testCols() []db.Column {
	return []db.Column{
		{Name: "id", Type: "integer", Nullable: false, IsPK: true},
		{Name: "email", Type: "varchar", Nullable: false, Unique: true},
		{Name: "nickname", Type: "varchar", Nullable: true},
		{Name: "org_id", Type: "integer", Nullable: true, FKRef: "orgs.id"},
	}
}

func TestBuildParsedTableShape(t *testing.T) {
	inSet := map[string]bool{"users": true, "orgs": true}
	tbl := BuildParsedTable("users", "myapp", testCols(), inSet)

	if tbl.Name != "users" || tbl.Database != "myapp" {
		t.Fatalf("got %+v", tbl)
	}
	if len(tbl.Fields) != 4 {
		t.Fatalf("fields = %d, want 4", len(tbl.Fields))
	}
	// Nullability uses the `null` key (truthy = nullable).
	if tbl.Fields[0].Null {
		t.Fatal("id should not be null")
	}
	if !tbl.Fields[2].Null {
		t.Fatal("nickname should be null")
	}

	// Index types must be UPPERCASE (server compares strictly).
	var types []string
	for _, idx := range tbl.Indexes {
		types = append(types, idx.Type)
	}
	want := []string{"PRIMARY", "UNIQUE", "FOREIGN"}
	if len(types) != len(want) {
		t.Fatalf("index types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("index types = %v, want %v", types, want)
		}
	}

	// Composite grouping: single PRIMARY for the PK column.
	if len(tbl.Indexes[0].Cols) != 1 || tbl.Indexes[0].Cols[0].Name != "id" {
		t.Fatalf("PRIMARY cols = %+v", tbl.Indexes[0].Cols)
	}
	fk := tbl.Indexes[2]
	if fk.RefTable != "orgs" || fk.RefCols[0].Name != "id" || fk.Cols[0].Name != "org_id" {
		t.Fatalf("FOREIGN = %+v", fk)
	}
}

func TestBuildParsedTablePrunesOutOfSetFK(t *testing.T) {
	inSet := map[string]bool{"users": true} // orgs not pulled
	tbl := BuildParsedTable("users", "myapp", testCols(), inSet)
	for _, idx := range tbl.Indexes {
		if idx.Type == "FOREIGN" {
			t.Fatalf("out-of-set FK should be pruned, got %+v", idx)
		}
	}
	if strings.Contains(tbl.SQL, "FOREIGN KEY") {
		t.Fatal("DDL should not reference pruned FK")
	}
}

func TestBuildParsedTableCompositePK(t *testing.T) {
	cols := []db.Column{
		{Name: "a", Type: "integer", IsPK: true},
		{Name: "b", Type: "integer", IsPK: true},
	}
	tbl := BuildParsedTable("t", "d", cols, nil)
	primaries := 0
	for _, idx := range tbl.Indexes {
		if idx.Type == "PRIMARY" {
			primaries++
			if len(idx.Cols) != 2 {
				t.Fatalf("composite PRIMARY cols = %+v", idx.Cols)
			}
		}
	}
	if primaries != 1 {
		t.Fatalf("want exactly one PRIMARY index, got %d", primaries)
	}
}

func TestBuildParsedTableSkipsMalformedFK(t *testing.T) {
	cols := []db.Column{{Name: "x", Type: "integer", FKRef: "not-a-ref"}}
	tbl := BuildParsedTable("t", "d", cols, nil)
	for _, idx := range tbl.Indexes {
		if idx.Type == "FOREIGN" {
			t.Fatal("malformed FKRef should be skipped")
		}
	}
}

func TestJSONContractKeys(t *testing.T) {
	inSet := map[string]bool{"users": true, "orgs": true}
	tbl := BuildParsedTable("users", "myapp", testCols(), inSet)
	b, _ := json.Marshal(tbl)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"name", "database", "fields", "indexes", "sql"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing top-level key %q", k)
		}
	}
	fields := m["fields"].([]any)
	f0 := fields[0].(map[string]any)
	for _, k := range []string{"name", "type", "null"} {
		if _, ok := f0[k]; !ok {
			t.Fatalf("missing field key %q", k)
		}
	}
}

func TestSynthesizeDDL(t *testing.T) {
	inSet := map[string]bool{"users": true, "orgs": true}
	tbl := BuildParsedTable("users", "myapp", testCols(), inSet)
	ddl := tbl.SQL
	for _, want := range []string{
		`CREATE TABLE "users"`,
		`"email" varchar NOT NULL`,
		`"nickname" varchar NULL`,
		`PRIMARY KEY ("id")`,
		`UNIQUE ("email")`,
		`FOREIGN KEY ("org_id") REFERENCES "orgs" ("id")`,
	} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("DDL missing %q:\n%s", want, ddl)
		}
	}
}

func TestFingerprint(t *testing.T) {
	a := Fingerprint("postgres", "localhost", 5432, "myapp", []string{"users", "orders"})
	b := Fingerprint("postgres", "localhost", 5432, "myapp", []string{"orders", "users"})
	if a != b {
		t.Fatal("fingerprint must be order-independent")
	}
	if len(a) != 64 {
		t.Fatalf("fingerprint should be sha256 hex, got %q", a)
	}
	for _, other := range []string{
		Fingerprint("mysql", "localhost", 5432, "myapp", []string{"users", "orders"}),
		Fingerprint("postgres", "other", 5432, "myapp", []string{"users", "orders"}),
		Fingerprint("postgres", "localhost", 5432, "myapp", []string{"users"}),
	} {
		if other == a {
			t.Fatal("fingerprint should differ on driver/host/tables")
		}
	}
}
