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
		percentage := 0
		switch state {
		case "ready":
			percentage = 100
		case "generating":
			percentage = 50
		case "relationships":
			percentage = 3
		}
		_ = json.NewEncoder(w).Encode(api.SchemaProgress{
			SchemaID:      "s1",
			Parsing:       api.StageStatus{Status: "complete"},
			Relationships: api.StageStatus{Status: "complete", Count: 1},
			Generation: api.GenerationProgress{Status: state, Tables: []api.ProgressTable{
				{Table: "users", Requested: 10, Generated: 10, Status: "ready"},
			}},
			Overall:    state,
			Percentage: percentage,
		})
	})
	s := httptest.NewServer(mux)
	return api.New(s.URL, "good"), s.Close
}

func TestPollProgressReady(t *testing.T) {
	c, done := progressStub(t, "generating", "generating", "ready")
	defer done()

	var out bytes.Buffer
	p, err := pollProgress(c, "s1", time.Millisecond, 5*time.Second, &out, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p.Overall != "ready" {
		t.Fatalf("overall = %q", p.Overall)
	}
	text := out.String()
	if !strings.Contains(text, "generating") || !strings.Contains(text, "50%") {
		t.Fatalf("output:\n%s", text)
	}
	if !strings.Contains(text, "ready") || !strings.Contains(text, "[") || !strings.Contains(text, "100%") {
		t.Fatalf("bar output missing:\n%s", text)
	}
	if strings.Contains(text, "users") {
		t.Fatalf("per-table detail should not be rendered:\n%s", text)
	}
	// Repeated identical states print once.
	if strings.Count(text, "generating") != 1 {
		t.Fatalf("dedupe failed:\n%s", text)
	}
}

func TestPollProgressFailed(t *testing.T) {
	c, done := progressStub(t, "failed")
	defer done()

	var out bytes.Buffer
	if _, err := pollProgress(c, "s1", time.Millisecond, 5*time.Second, &out, false); err == nil {
		t.Fatal("expected failure error")
	}
}

func TestPollProgressTimeout(t *testing.T) {
	c, done := progressStub(t, "parsing")
	defer done()

	var out bytes.Buffer
	start := time.Now()
	_, err := pollProgress(c, "s1", 5*time.Millisecond, 25*time.Millisecond, &out, false)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("poll loop ran far past its timeout")
	}
}

func TestPollProgressNoProgress(t *testing.T) {
	c, done := progressStub(t, "generating", "generating", "ready")
	defer done()

	var out bytes.Buffer
	p, err := pollProgress(c, "s1", time.Millisecond, 5*time.Second, &out, true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p.Overall != "ready" {
		t.Fatalf("overall = %q", p.Overall)
	}
	text := out.String()
	// Intermediate generating state suppressed; only the final ready line.
	if strings.Contains(text, "generating") {
		t.Fatalf("intermediate output not suppressed:\n%s", text)
	}
	if !strings.Contains(text, "ready") {
		t.Fatalf("final state missing:\n%s", text)
	}
}

func TestRenderBar(t *testing.T) {
	if got := renderBar(0, 100, 20); got != "[--------------------]" {
		t.Fatalf("empty bar = %q", got)
	}
	if got := renderBar(100, 100, 20); got != "[####################]" {
		t.Fatalf("full bar = %q", got)
	}
	if got := renderBar(50, 100, 20); got != "[##########----------]" {
		t.Fatalf("half bar = %q", got)
	}
	if got := renderBar(0, 0, 20); got != "[--------------------]" {
		t.Fatalf("zero-request bar = %q", got)
	}
	if got := renderBar(150, 100, 20); got != "[####################]" {
		t.Fatalf("overfull bar not clamped = %q", got)
	}
}

func TestBarPercent(t *testing.T) {
	if got := barPercent(0, 100); got != "  0%" {
		t.Fatalf("pct = %q", got)
	}
	if got := barPercent(50, 100); got != " 50%" {
		t.Fatalf("pct = %q", got)
	}
	if got := barPercent(100, 100); got != "100%" {
		t.Fatalf("pct = %q", got)
	}
	if got := barPercent(0, 0); got != "--%" {
		t.Fatalf("pct = %q", got)
	}
}

func TestRenderPullLinesSingleBar(t *testing.T) {
	p := api.SchemaProgress{
		Overall:    "generating",
		Percentage: 42,
		Generation: api.GenerationProgress{Status: "generating", Tables: []api.ProgressTable{
			{Table: "users", Requested: 100, Generated: 40, Status: "generating"},
			{Table: "orders", Requested: 100, Generated: 44, Status: "generating"},
		}},
	}
	lines := renderPullLines(p, "|")
	if len(lines) != 1 {
		t.Fatalf("expected a single line, got %v", lines)
	}
	if !strings.Contains(lines[0], "generating") || !strings.Contains(lines[0], "42%") {
		t.Fatalf("overall line = %q", lines[0])
	}
	if strings.Contains(lines[0], "users") || strings.Contains(lines[0], "orders") {
		t.Fatalf("per-table detail leaked into line = %q", lines[0])
	}
}

func TestRenderPullLinesRelationships(t *testing.T) {
	p := api.SchemaProgress{
		Overall:       "relationships",
		Percentage:    3,
		Relationships: api.StageStatus{Status: "complete", Count: 4},
	}
	lines := renderPullLines(p, "|")
	if len(lines) != 1 || !strings.Contains(lines[0], "relationships") || !strings.Contains(lines[0], "4 found") {
		t.Fatalf("relationships line = %v", lines)
	}
}

func TestOverallPercentFallsBackToTotals(t *testing.T) {
	p := api.SchemaProgress{
		Overall: "generating",
		Generation: api.GenerationProgress{Status: "generating", Tables: []api.ProgressTable{
			{Table: "users", Requested: 200, Generated: 50, Status: "generating"},
		}},
	}
	if got := overallPercent(p); got != 25 {
		t.Fatalf("percent = %d, want 25", got)
	}
}

func TestFormatProgressHasBar(t *testing.T) {
	p := api.SchemaProgress{
		Overall:    "generating",
		Percentage: 50,
		Generation: api.GenerationProgress{Status: "generating", Tables: []api.ProgressTable{
			{Table: "users", Requested: 10, Generated: 5, Status: "generating"},
		}},
	}
	line := formatProgress(p)
	if !strings.Contains(line, "generating") || !strings.Contains(line, "[") || !strings.Contains(line, "50%") {
		t.Fatalf("line = %q", line)
	}
	if strings.Contains(line, "users") {
		t.Fatalf("per-table detail leaked into line = %q", line)
	}
}
