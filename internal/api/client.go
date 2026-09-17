package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Version is the CLI version, checked against GET /api/cli/latest.
const Version = "0.1.0"

// DefaultBaseURL is used when FAKERFORGE_API_URL is unset.
const DefaultBaseURL = "http://127.0.0.1:8001"

// Client talks to the FakerForge Laravel API.
// Auth: Authorization: Bearer {api_key} on every request.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// New builds a client. baseURL may be "" (env/default resolution applies).
func New(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = os.Getenv("FAKERFORGE_API_URL")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) req(method, path string, body io.Reader) (*http.Request, error) {
	r, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+c.APIKey)
	r.Header.Set("Accept", "application/json")
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("User-Agent", "fakerforge-cli/"+Version)
	return r, nil
}

func decodeErr(resp *http.Response) error {
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err == nil {
		if e, ok := m["error"].(string); ok && e != "" {
			return fmt.Errorf("api %d: %s", resp.StatusCode, e)
		}
		if msg, ok := m["message"].(string); ok && msg != "" {
			return fmt.Errorf("api %d: %s", resp.StatusCode, msg)
		}
	}
	return fmt.Errorf("api %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
}

// ── GET /api/me ──────────────────────────────────────────────────────────────

// MeUser is the authenticated user returned by GET /api/me.
type MeUser struct {
	ID    any    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Plan  string `json:"plan"`
}

// Me validates the key and returns the logged-in user.
func (c *Client) Me() (MeUser, error) {
	r, err := c.req("GET", "/api/me", nil)
	if err != nil {
		return MeUser{}, err
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return MeUser{}, fmt.Errorf("reach api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return MeUser{}, decodeErr(resp)
	}
	// Accept {user:{...}} or flat {...}.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return MeUser{}, fmt.Errorf("decode /api/me: %w", err)
	}
	payload := raw
	if u, ok := raw["user"]; ok {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(u, &inner); err == nil {
			payload = inner
		}
	}
	var u MeUser
	get := func(k string) string {
		var s string
		if v, ok := payload[k]; ok {
			_ = json.Unmarshal(v, &s)
		}
		return s
	}
	u.Name, u.Email, u.Plan = get("name"), get("email"), get("plan")
	if v, ok := payload["id"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			u.ID = s
		} else {
			var n float64
			if err := json.Unmarshal(v, &n); err == nil {
				u.ID = n
			}
		}
	}
	return u, nil
}

// ── POST /api/schemas ────────────────────────────────────────────────────────

type SchemaColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	IsPK     bool   `json:"is_pk,omitempty"`
	Unique   bool   `json:"unique,omitempty"`
	FKRef    string `json:"fk_ref,omitempty"` // "table.column"
}

// ParsedField/ParsedIndexCol/ParsedIndex/ParsedTable mirror the server's
// parsed-tables contract (the same shape ParseSchemaLocalJob caches).
// Index types must be UPPERCASE: the server compares them strictly.
type ParsedField struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Null      bool     `json:"null"`
	Length    int      `json:"length,omitempty"`
	Unsigned  bool     `json:"unsigned,omitempty"`
	Precision int      `json:"precision,omitempty"`
	Scale     int      `json:"scale,omitempty"`
	Values    []string `json:"values,omitempty"`
}

type ParsedIndexCol struct {
	Name string `json:"name"`
}

type ParsedIndex struct {
	Type     string           `json:"type"`
	Cols     []ParsedIndexCol `json:"cols"`
	RefTable string           `json:"ref_table,omitempty"`
	RefCols  []ParsedIndexCol `json:"ref_cols,omitempty"`
}

type ParsedTable struct {
	Name     string        `json:"name"`
	Database string        `json:"database"`
	Fields   []ParsedField `json:"fields"`
	Indexes  []ParsedIndex `json:"indexes"`
	SQL      string        `json:"sql"`
}

type CreateSchemaRequest struct {
	Table   string         `json:"table"`
	Columns []SchemaColumn `json:"columns"`
}

type CreateSchemaResponse struct {
	SchemaID string `json:"schema_id"`
}

// CreateSchema uploads schema shape only (never row data).
func (c *Client) CreateSchema(table string, cols []SchemaColumn) (CreateSchemaResponse, error) {
	var out CreateSchemaResponse
	body, _ := json.Marshal(CreateSchemaRequest{Table: table, Columns: cols})
	r, err := c.req("POST", "/api/schemas", strings.NewReader(string(body)))
	if err != nil {
		return out, err
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return out, fmt.Errorf("reach api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return out, decodeErr(resp)
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return out, fmt.Errorf("decode POST /api/schemas: %w", err)
	}
	for _, k := range []string{"schema_id", "id"} {
		if v, ok := raw[k].(string); ok && v != "" {
			out.SchemaID = v
			return out, nil
		}
	}
	return out, fmt.Errorf("POST /api/schemas: response missing schema_id")
}

// ── POST /api/schemas (multi-table pull) ───────────────────────────────────

type CreatePullRequest struct {
	Name        string                 `json:"name"`
	Fingerprint string                 `json:"fingerprint"`
	Rows        int                    `json:"rows"`
	Force       bool                   `json:"force,omitempty"`
	Tables      map[string]ParsedTable `json:"tables"`
}

type CreatePullResponse struct {
	SchemaID     string         `json:"schema_id"`
	Reused       bool           `json:"reused"`
	TablesQueued []string       `json:"tables_queued"`
	RowCounts    map[string]int `json:"row_counts"`
	DashboardURL string         `json:"dashboard_url"`
}

// CreatePull uploads CLI-parsed tables (shape only, never row data).
// The server validates, writes cache, and creates the schema without
// any parsing of its own.
func (c *Client) CreatePull(req CreatePullRequest) (CreatePullResponse, error) {
	var out CreatePullResponse
	if err := c.post("/api/schemas", req, &out); err != nil {
		return out, err
	}
	if out.SchemaID == "" {
		return out, fmt.Errorf("POST /api/schemas: response missing schema_id")
	}
	return out, nil
}

// ── POST /api/schemas/{id}/generate ─────────────────────────────────────────

type GenerateRequest struct {
	Rows int `json:"rows"`
}

type GenerateResponse struct {
	SchemaID     string         `json:"schema_id"`
	TablesQueued []string       `json:"tables_queued"`
	RowCounts    map[string]int `json:"row_counts"`
}

// GenerateSchema kicks off data generation for an already-parsed schema.
func (c *Client) GenerateSchema(id string, rows int) (GenerateResponse, error) {
	var out GenerateResponse
	path := "/api/schemas/" + url.PathEscape(id) + "/generate"
	if err := c.post(path, GenerateRequest{Rows: rows}, &out); err != nil {
		return out, err
	}
	if out.SchemaID == "" {
		out.SchemaID = id
	}
	return out, nil
}

// ── GET /api/schemas/{id}/progress ──────────────────────────────────────────

type StageStatus struct {
	Status  string `json:"status"`
	Count   int    `json:"count,omitempty"`
	Message string `json:"message,omitempty"`
}

type ProgressTable struct {
	Table     string `json:"table"`
	Requested int    `json:"requested"`
	Generated int    `json:"generated"`
	Status    string `json:"status"`
}

type GenerationProgress struct {
	Status string          `json:"status"`
	Tables []ProgressTable `json:"tables"`
}

type SchemaProgress struct {
	SchemaID      string             `json:"schema_id"`
	Parsing       StageStatus        `json:"parsing"`
	Relationships StageStatus        `json:"relationships"`
	Generation    GenerationProgress `json:"generation"`
	Overall       string             `json:"overall"`
	Error         string             `json:"error,omitempty"`
}

// GetProgress polls the pipeline state for a schema.
func (c *Client) GetProgress(id string) (SchemaProgress, error) {
	var out SchemaProgress
	path := "/api/schemas/" + url.PathEscape(id) + "/progress"
	if err := c.get(path, &out); err != nil {
		return out, err
	}
	if out.SchemaID == "" {
		out.SchemaID = id
	}
	return out, nil
}

func (c *Client) post(path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode POST %s: %w", path, err)
	}
	r, err := c.req("POST", path, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return fmt.Errorf("reach api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 202 {
		return decodeErr(resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode POST %s: %w", path, err)
		}
	}
	return nil
}

// ── GET /api/schemas (list) ──────────────────────────────────────────────────

// Schema is one row of GET /api/schemas: one row per schema.
type Schema struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tables  int    `json:"tables"` // number of tables in the schema
	Created string `json:"created_at"`
}

// ListSchemas hits GET /api/schemas?table= (no status filter).
func (c *Client) ListSchemas(table string) ([]Schema, error) {
	path := "/api/schemas"
	if table != "" {
		path += "?table=" + url.QueryEscape(table)
	}
	r, err := c.req("GET", path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return nil, fmt.Errorf("reach api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, decodeErr(resp)
	}
	var arr []Schema
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		return nil, fmt.Errorf("decode list /api/schemas: %w", err)
	}
	return arr, nil
}

// ── GET /api/schemas/{id} ────────────────────────────────────────────────────

// TableStatus is per-table readiness inside a schema detail.
type TableStatus struct {
	Table  string `json:"table"`
	Rows   int    `json:"rows"`
	Status string `json:"status"` // ready | pending
}

// SchemaDetail is GET /api/schemas/{id} without ?table=.
type SchemaDetail struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Tables   []TableStatus `json:"tables"`
	SchemaID string        `json:"schema_id"`
	Created  string        `json:"created_at"`
}

// TableDetail is GET /api/schemas/{id}?table=T.
type TableDetail struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Table    string           `json:"table"`
	Rows     int              `json:"rows"`
	Status   string           `json:"status"` // ready | pending
	SchemaID string           `json:"schema_id"`
	Rules    any              `json:"rules"`
	Sample   []map[string]any `json:"sample"`
	Created  string           `json:"created_at"`
}

func (c *Client) get(path string, out any) error {
	r, err := c.req("GET", path, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return fmt.Errorf("reach api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return decodeErr(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// ShowSchema fetches schema detail with the per-table list.
func (c *Client) ShowSchema(id string) (SchemaDetail, error) {
	var out SchemaDetail
	path := "/api/schemas/" + url.PathEscape(id)
	if err := c.get(path, &out); err != nil {
		return out, err
	}
	if out.ID == "" {
		out.ID = id
	}
	return out, nil
}

// ShowTable fetches detail for just one table of a schema.
func (c *Client) ShowTable(id, table string) (TableDetail, error) {
	var out TableDetail
	path := "/api/schemas/" + url.PathEscape(id) + "?table=" + url.QueryEscape(table)
	if err := c.get(path, &out); err != nil {
		return out, err
	}
	if out.ID == "" {
		out.ID = id
	}
	return out, nil
}

// ── GET /api/schemas/{id}/{table}/stream (JSONL) ─────────────────────────────

// StreamRows streams generated rows as decoded maps, one per JSONL line.
// The callback returns false to stop early. Response is never fully buffered.
func (c *Client) StreamRows(schemaID, table string, fn func(map[string]any) bool) (int, error) {
	path := "/api/schemas/" + url.PathEscape(schemaID) + "/" + url.PathEscape(table) + "/stream"
	r, err := c.req("GET", path, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return 0, fmt.Errorf("reach api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, decodeErr(resp)
	}
	n := 0
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue // skip malformed lines, keep streaming
		}
		n++
		if !fn(row) {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return n, fmt.Errorf("stream read: %w", err)
	}
	return n, nil
}

// ── GET /api/cli/latest ──────────────────────────────────────────────────────

// LatestVersion returns the newest CLI version string.
func (c *Client) LatestVersion() (string, error) {
	r, err := c.req("GET", "/api/cli/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(r)
	if err != nil {
		return "", fmt.Errorf("reach api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", decodeErr(resp)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return "", fmt.Errorf("decode /api/cli/latest: %w", err)
	}
	if v, ok := m["version"].(string); ok && v != "" {
		return v, nil
	}
	return "", fmt.Errorf("/api/cli/latest: response missing version")
}
