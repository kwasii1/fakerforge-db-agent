package cmd

import (
	"reflect"
	"testing"

	"github.com/kwasii1/fakerforge-db-agent/internal/db"
)

func TestOrderTablesChain(t *testing.T) {
	tables := []string{"users", "orgs", "audit"}
	parents := map[string][]string{
		"users": {"orgs"},
		"orgs":  {},
		"audit": {"users"},
	}
	got, err := orderTables(tables, parents)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"orgs", "users", "audit"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestOrderTablesDiamond(t *testing.T) {
	tables := []string{"a", "b", "c", "d"}
	parents := map[string][]string{
		"a": {},
		"b": {"a"},
		"c": {"a"},
		"d": {"b", "c"},
	}
	got, err := orderTables(tables, parents)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, tbl := range got {
		pos[tbl] = i
	}
	if !(pos["a"] < pos["b"] && pos["a"] < pos["c"] && pos["b"] < pos["d"] && pos["c"] < pos["d"]) {
		t.Fatalf("bad order: %v", got)
	}
}

func TestOrderTablesSelfRef(t *testing.T) {
	tables := []string{"employees"}
	parents := map[string][]string{"employees": {"employees"}}
	got, err := orderTables(tables, parents)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, tables) {
		t.Fatalf("got %v", got)
	}
}

func TestOrderTablesCycleAborts(t *testing.T) {
	tables := []string{"a", "b"}
	parents := map[string][]string{"a": {"b"}, "b": {"a"}}
	if _, err := orderTables(tables, parents); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestOrderTablesInputOrderStable(t *testing.T) {
	tables := []string{"z", "a", "m"}
	parents := map[string][]string{"z": {}, "a": {}, "m": {}}
	got, err := orderTables(tables, parents)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, tables) {
		t.Fatalf("got %v, want input order", got)
	}
}

func TestTableParentsOf(t *testing.T) {
	cols := []db.Column{
		{Name: "id"},
		{Name: "org_id", FKRef: "orgs.id"},
		{Name: "broken", FKRef: "not-a-ref"},
		{Name: "empty"},
	}
	got := tableParentsOf(cols, nil)
	if !reflect.DeepEqual(got, []string{"orgs"}) {
		t.Fatalf("got %v", got)
	}
	pruned := tableParentsOf(cols, map[string]bool{"users": true})
	if len(pruned) != 0 {
		t.Fatalf("out-of-set refs should be pruned, got %v", pruned)
	}
}
