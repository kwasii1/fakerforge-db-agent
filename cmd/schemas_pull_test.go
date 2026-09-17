package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
)

func TestSelectTables(t *testing.T) {
	all := []string{"orders", "users"}

	got, err := selectTables(all, "", "")
	if err != nil || !reflect.DeepEqual(got, all) {
		t.Fatalf("all: %v %v", got, err)
	}

	got, err = selectTables(all, "users", "")
	if err != nil || !reflect.DeepEqual(got, []string{"users"}) {
		t.Fatalf("single: %v %v", got, err)
	}

	got, err = selectTables(all, "", "users, orders,users, ")
	if err != nil || !reflect.DeepEqual(got, []string{"users", "orders"}) {
		t.Fatalf("multi dedupes: %v %v", got, err)
	}

	if _, err := selectTables(all, "nope", ""); err == nil {
		t.Fatal("expected error for unknown single table")
	}
	if _, err := selectTables(all, "", "users,nope"); err == nil {
		t.Fatal("expected error for unknown multi table")
	}
	if _, err := selectTables(all, "users", "orders"); err == nil {
		t.Fatal("expected mutual-exclusion error")
	}
	if _, err := selectTables(all, "", " , "); err == nil {
		t.Fatal("expected error for empty selection")
	}
}

// progressStub serves GET /api/schemas/{id}/progress from a scripted
// sequence of overall states, then repeats the last one.
func progressStub(t *testing.T, states ...string) (*api.Client, func()) {
	t.Helper()
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/schemas/s1/progress", func(w http.ResponseWriter, r *http.Request) {
		state := states[len(states)-1]
		if calls < len(states) {
			state = states[calls]
		}
		calls++
		_ = json.NewEncoder(w).Encode(api.SchemaProgress{
			SchemaID:      "s1",
			Parsing:       api.StageStatus{Status: "complete"},
			Relationships: api.StageStatus{Status: "complete", Count: 1},
			Generation: api.GenerationProgress{Status: state, Tables: []api.ProgressTable{
				{Table: "users", Requested: 10, Generated: 10, Status: "ready"},
			}},
			Overall: state,
		})
	})
	s := httptest.NewServer(mux)
	return api.New(s.URL, "good"), s.Close
}

func TestPollProgressReady(t *testing.T) {
	c, done := progressStub(t, "generating", "generating", "ready")
	defer done()

	var out bytes.Buffer
	p, err := pollProgress(c, "s1", time.Millisecond, 5*time.Second, &out)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p.Overall != "ready" {
		t.Fatalf("overall = %q", p.Overall)
	}
	text := out.String()
	if !strings.Contains(text, "generating users 10/10") || !strings.Contains(text, "ready: users 10/10") {
		t.Fatalf("output:\n%s", text)
	}
	// Repeated identical states print once.
	if strings.Count(text, "generating users 10/10") != 1 {
		t.Fatalf("dedupe failed:\n%s", text)
	}
}

func TestPollProgressFailed(t *testing.T) {
	c, done := progressStub(t, "failed")
	defer done()

	var out bytes.Buffer
	if _, err := pollProgress(c, "s1", time.Millisecond, 5*time.Second, &out); err == nil {
		t.Fatal("expected failure error")
	}
}

func TestPollProgressTimeout(t *testing.T) {
	c, done := progressStub(t, "parsing")
	defer done()

	var out bytes.Buffer
	start := time.Now()
	_, err := pollProgress(c, "s1", 5*time.Millisecond, 25*time.Millisecond, &out)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("poll loop ran far past its timeout")
	}
}
