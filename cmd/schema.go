package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
	"github.com/kwasii1/fakerforge-db-agent/internal/parse"
)

// RunSchemaPull implements:
//
//	fakerforge schema pull --connection NAME [--tables A,B] [--table T]
//	  [--rows N] [--name NAME] [--force] [--regenerate] [--async]
//	  [--timeout D] [--interval D] [--api-url URL] [--api-key KEY]
//
// Introspects the database, builds the parsed-tables structure locally
// (no server-side parsing, no AI), uploads it, then drives the pipeline
// to readiness unless --async is given.
func RunSchemaPull(args []string) int {
	fs := flag.NewFlagSet("schema pull", flag.ContinueOnError)
	connName := fs.String("connection", "", "Saved connection name (default: default connection)")
	table := fs.String("table", "", "Single table (deprecated alias for --tables with one entry)")
	tablesFlag := fs.String("tables", "", "Comma-separated tables to pull (default: all tables)")
	rows := fs.Int("rows", 100, "Rows to generate per table (capped by plan)")
	name := fs.String("name", "", "Schema name (default: database name)")
	force := fs.Bool("force", false, "Delete and recreate an existing schema for this database")
	regenerate := fs.Bool("regenerate", false, "Re-run generation on a reused schema")
	async := fs.Bool("async", false, "Return after upload without waiting for readiness")
	timeout := fs.Duration("timeout", 20*time.Minute, "Max wait for readiness")
	interval := fs.Duration("interval", 3*time.Second, "Progress poll interval")
	apiURL := fs.String("api-url", "", "API base URL")
	apiKey := fs.String("api-key", "", "API key override")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *table != "" && *tablesFlag != "" {
		fmt.Println("schema pull: --table and --tables are mutually exclusive")
		return 2
	}
	if *rows < 1 {
		fmt.Println("schema pull: --rows must be >= 1")
		return 2
	}

	client, err := newClient(*apiKey, *apiURL)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	store, err := config.Default()
	if err != nil {
		fmt.Println(err)
		return 1
	}
	conn, err := store.Resolve(*connName)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	pw, err := config.GetConnPassword(conn.Name)
	if err != nil {
		fmt.Printf("no password for %q: %v\n", conn.Name, err)
		return 1
	}
	d, err := db.Open(conn, pw)
	if err != nil {
		fmt.Printf("connection failed: %v\n", err)
		return 1
	}
	defer d.Close()

	allTables, err := d.ListTables()
	if err != nil {
		fmt.Printf("list tables failed: %v\n", err)
		return 1
	}
	if len(allTables) == 0 {
		fmt.Printf("database %q has no tables\n", conn.Database)
		return 1
	}

	selected, err := selectTables(allTables, *table, *tablesFlag)
	if err != nil {
		fmt.Println(err)
		return 1
	}

	inSet := make(map[string]bool, len(selected))
	for _, t := range selected {
		inSet[t] = true
	}

	parsed := make(map[string]api.ParsedTable, len(selected))
	for _, t := range selected {
		cols, err := d.Introspect(t)
		if err != nil {
			fmt.Printf("introspect %s failed: %v\n", t, err)
			return 1
		}
		printColumns(t, cols)
		parsed[t] = parse.BuildParsedTable(t, conn.Database, cols, inSet)
	}

	schemaName := *name
	if schemaName == "" {
		schemaName = conn.Database
	}
	fp := parse.Fingerprint(conn.Driver, conn.Host, conn.Port, conn.Database, selected)

	resp, err := client.CreatePull(api.CreatePullRequest{
		Name:        schemaName,
		Fingerprint: fp,
		Rows:        *rows,
		Force:       *force,
		Tables:      parsed,
	})
	if err != nil {
		fmt.Printf("upload failed: %v\n", err)
		return 1
	}

	reusedNote := ""
	if resp.Reused {
		reusedNote = " (reused existing schema)"
	}
	fmt.Printf("✓ Schema uploaded: schema_id=%s (%d tables)%s\n", resp.SchemaID, len(selected), reusedNote)
	if resp.DashboardURL != "" {
		fmt.Printf("Dashboard: %s\n", resp.DashboardURL)
	}

	if resp.Reused && !*regenerate {
		fmt.Println("Schema already exists for this database (reused). Pass --regenerate to re-run generation or --force to recreate.")
		return 0
	}
	if *async {
		fmt.Printf("Generation not started. Run: fakerforge schemas show %s\n", resp.SchemaID)
		return 0
	}

	if _, err := client.GenerateSchema(resp.SchemaID, *rows); err != nil {
		fmt.Printf("generate failed to start: %v\n", err)
		return 1
	}

	final, err := pollProgress(client, resp.SchemaID, *interval, *timeout, os.Stdout)
	if err != nil {
		fmt.Printf("\n%s\n", err)
		return 1
	}
	fmt.Printf("\n✓ Ready: %s (%d tables)\n", final.SchemaID, len(final.Generation.Tables))
	fmt.Printf("Next: fakerforge push --schema %s --connection %s --table TABLE\n", final.SchemaID, conn.Name)
	return 0
}

// selectTables resolves --table/--tables against the database's tables.
// Empty flags select everything. Unknown names fail fast listing available.
func selectTables(all []string, single, multi string) ([]string, error) {
	if single != "" && multi != "" {
		return nil, fmt.Errorf("--table and --tables are mutually exclusive")
	}
	available := make(map[string]bool, len(all))
	for _, t := range all {
		available[t] = true
	}
	check := func(t string) error {
		if !available[t] {
			return fmt.Errorf("table %q not found in database (available: %s)", t, strings.Join(all, ", "))
		}
		return nil
	}

	if single != "" {
		if err := check(single); err != nil {
			return nil, err
		}
		return []string{single}, nil
	}
	if multi == "" {
		out := make([]string, len(all))
		copy(out, all)
		return out, nil
	}

	var selected []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(multi, ",") {
		t := strings.TrimSpace(part)
		if t == "" || seen[t] {
			continue
		}
		if err := check(t); err != nil {
			return nil, err
		}
		seen[t] = true
		selected = append(selected, t)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no tables selected (available: %s)", strings.Join(all, ", "))
	}
	return selected, nil
}

func printColumns(table string, cols []db.Column) {
	// Security transparency: show exactly what leaves the machine.
	fmt.Printf("Transmitting schema shape only (no row data) for %s:\n", table)
	for _, c := range cols {
		null := "NOT NULL"
		if c.Nullable {
			null = "NULL"
		}
		extra := ""
		if c.IsPK {
			extra += " PK"
		}
		if c.Unique {
			extra += " UNIQUE"
		}
		if c.FKRef != "" {
			extra += " FK->" + c.FKRef
		}
		fmt.Printf("  - %s %s %s%s\n", c.Name, parse.DisplayType(c), null, extra)
	}
}

// pollProgress polls GET …/progress until the schema is ready or failed.
// Progress lines print only when the rendered state changes.
func pollProgress(client *api.Client, schemaID string, interval, timeout time.Duration, out io.Writer) (api.SchemaProgress, error) {
	deadline := time.Now().Add(timeout)
	var last string
	for {
		p, err := client.GetProgress(schemaID)
		if err != nil {
			return p, fmt.Errorf("progress poll failed: %w", err)
		}
		if line := formatProgress(p); line != last {
			fmt.Fprintln(out, line)
			last = line
		}
		switch p.Overall {
		case "ready":
			return p, nil
		case "failed":
			if p.Error != "" {
				return p, fmt.Errorf("generation failed: %s", p.Error)
			}
			return p, fmt.Errorf("generation failed (no detail — check the dashboard)")
		}
		if time.Now().After(deadline) {
			return p, fmt.Errorf("timed out after %s waiting for readiness (state: %s)", timeout, p.Overall)
		}
		time.Sleep(interval)
	}
}

func formatProgress(p api.SchemaProgress) string {
	if p.Overall == "failed" {
		return "failed"
	}
	parts := make([]string, 0, len(p.Generation.Tables))
	for _, t := range p.Generation.Tables {
		parts = append(parts, fmt.Sprintf("%s %d/%d", t.Table, t.Generated, t.Requested))
	}
	detail := ""
	if len(parts) > 0 {
		detail = " " + strings.Join(parts, ", ")
	}
	switch p.Overall {
	case "ready":
		return "ready:" + detail
	case "generating":
		return "generating" + detail
	case "relationships":
		return fmt.Sprintf("relationships (%d found)…", p.Relationships.Count)
	default:
		return "parsing…"
	}
}
