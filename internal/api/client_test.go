package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func stub(t *testing.T) (*Client, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unauthenticated"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "Kwasi", "email": "k@example.com", "plan": "free"})
	})
	mux.HandleFunc("/api/schemas", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_ = json.NewEncoder(w).Encode(map[string]string{"schema_id": "sch_123"})
			return
		}
		if r.URL.Query().Get("table") == "nope" {
			_ = json.NewEncoder(w).Encode([]Schema{})
			return
		}
		_ = json.NewEncoder(w).Encode([]Schema{{ID: "d1", Name: "shop", Tables: 2}})
	})
	mux.HandleFunc("/api/schemas/d1", func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("table"); t != "" {
			if t != "users" {
				w.WriteHeader(404)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "Table 'x' not found in schema", "tables": []string{"users", "orders"}})
				return
			}
			_ = json.NewEncoder(w).Encode(TableDetail{ID: "d1", Name: "shop", Table: "users",
				Rows: 2, Status: "ready",
				Sample: []map[string]any{{"email": "a@x.com"}, {"email": "b@x.com"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(SchemaDetail{ID: "d1", Name: "shop", Tables: []TableStatus{
			{Table: "users", Rows: 2, Status: "ready"},
			{Table: "orders", Rows: 0, Status: "pending"},
		}})
	})
	mux.HandleFunc("/api/schemas/s1/users/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"email\":\"a@x.com\"}\n{\"email\":\"b@x.com\"}\n"))
	})
	mux.HandleFunc("/api/schemas/d1/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(GenerateResponse{
			SchemaID:     "d1",
			TablesQueued: []string{"users", "orders"},
			RowCounts:    map[string]int{"users": 100, "orders": 100},
		})
	})
	mux.HandleFunc("/api/schemas/d1/progress", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(SchemaProgress{
			SchemaID:      "d1",
			Parsing:       StageStatus{Status: "complete"},
			Relationships: StageStatus{Status: "complete", Count: 1},
			Generation: GenerationProgress{Status: "generating", Tables: []ProgressTable{
				{Table: "users", Requested: 100, Generated: 100, Status: "ready"},
				{Table: "orders", Requested: 100, Generated: 45, Status: "generating"},
			}},
			Overall: "generating",
		})
	})
	mux.HandleFunc("/api/cli/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": Version})
	})
	s := httptest.NewServer(mux)
	return New(s.URL, "good"), s.Close
}

func TestMe(t *testing.T) {
	c, done := stub(t)
	defer done()
	u, err := c.Me()
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "k@example.com" {
		t.Fatalf("got %+v", u)
	}
	bad := New(c.BaseURL, "bad")
	if _, err := bad.Me(); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestPull(t *testing.T) {
	c, done := stub(t)
	defer done()
	out, err := c.CreatePull(CreatePullRequest{
		Name:        "myapp",
		Fingerprint: "fp123",
		Rows:        100,
		Tables: map[string]ParsedTable{
			"users": {Name: "users", Database: "myapp",
				Fields:  []ParsedField{{Name: "id", Type: "integer"}},
				Indexes: []ParsedIndex{{Type: "PRIMARY", Cols: []ParsedIndexCol{{Name: "id"}}}},
				SQL:     "CREATE TABLE \"users\" (\n  \"id\" integer NOT NULL,\n  PRIMARY KEY (\"id\")\n);\n"},
		},
	})
	if err != nil || out.SchemaID != "sch_123" {
		t.Fatalf("pull: %+v %v", out, err)
	}
	gen, err := c.GenerateSchema("d1", 100)
	if err != nil || len(gen.TablesQueued) != 2 || gen.RowCounts["users"] != 100 {
		t.Fatalf("generate: %+v %v", gen, err)
	}
	p, err := c.GetProgress("d1")
	if err != nil || p.Overall != "generating" || p.Parsing.Status != "complete" {
		t.Fatalf("progress: %+v %v", p, err)
	}
	if len(p.Generation.Tables) != 2 || p.Generation.Tables[1].Generated != 45 {
		t.Fatalf("progress tables: %+v", p.Generation.Tables)
	}
}

func TestSchemas(t *testing.T) {
	c, done := stub(t)
	defer done()
	items, err := c.ListSchemas("")
	if err != nil || len(items) != 1 || items[0].ID != "d1" || items[0].Tables != 2 {
		t.Fatalf("list: %+v %v", items, err)
	}
	if got, err := c.ListSchemas("nope"); err != nil || len(got) != 0 {
		t.Fatalf("list filter: %+v %v", got, err)
	}
	d, err := c.ShowSchema("d1")
	if err != nil || len(d.Tables) != 2 || d.Tables[0].Status != "ready" {
		t.Fatalf("show: %+v %v", d, err)
	}
	td, err := c.ShowTable("d1", "users")
	if err != nil || td.Status != "ready" || len(td.Sample) != 2 {
		t.Fatalf("show table: %+v %v", td, err)
	}
	if _, err := c.ShowTable("d1", "nope"); err == nil {
		t.Fatal("expected 404 for unknown table")
	}
	out, err := c.CreateSchema("users", []SchemaColumn{{Name: "email", Type: "varchar"}})
	if err != nil || out.SchemaID != "sch_123" {
		t.Fatalf("create: %+v %v", out, err)
	}
	n, err := c.StreamRows("s1", "users", func(map[string]any) bool { return true })
	if err != nil || n != 2 {
		t.Fatalf("stream: %d %v", n, err)
	}
	v, err := c.LatestVersion()
	if err != nil || v != Version {
		t.Fatalf("latest: %q %v", v, err)
	}
}
