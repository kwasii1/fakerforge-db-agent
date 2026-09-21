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
	"github.com/kwasii1/fakerforge-db-agent/internal/ui"
	"golang.org/x/term"
)

// RunSchemaPull implements:
//
//	fakerforge schema pull --connection NAME [--tables A,B] [--table T]
//	  [--rows N] [--name NAME] [--force] [--regenerate] [--async]
//	  [--timeout D] [--interval D] [--no-progress] [--api-url URL] [--api-key KEY]
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
	noProgress := fs.Bool("no-progress", false, "Suppress live progress, print only the final result")
	apiURL := fs.String("api-url", "", "API base URL")
	apiKey := fs.String("api-key", "", "API key override")
	noColor := fs.Bool("no-color", false, "Disable colored output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *noColor {
		ui.SetNoColor(true)
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
	if ui.Enabled() {
		fmt.Printf("%s Schema uploaded: %s %s\n", ui.Success("✓"),
			ui.Bold("schema_id="+resp.SchemaID),
			ui.Muted(fmt.Sprintf("(%d tables)%s", len(selected), reusedNote)))
		if resp.DashboardURL != "" {
			fmt.Printf("Dashboard: %s\n", ui.Muted(resp.DashboardURL))
		}
	} else {
		fmt.Printf("✓ Schema uploaded: schema_id=%s (%d tables)%s\n", resp.SchemaID, len(selected), reusedNote)
		if resp.DashboardURL != "" {
			fmt.Printf("Dashboard: %s\n", resp.DashboardURL)
		}
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

	final, err := pollProgress(client, resp.SchemaID, *interval, *timeout, os.Stdout, *noProgress)
	if err != nil {
		fmt.Printf("\n%s\n", err)
		return 1
	}
	if ui.Enabled() {
		fmt.Printf("\n%s Ready: %s %s\n", ui.Success("✓"),
			ui.Bold(final.SchemaID), ui.Muted(fmt.Sprintf("(%d tables)", len(final.Generation.Tables))))
		fmt.Printf("Next: %s\n", ui.Muted(fmt.Sprintf("fakerforge push --schema %s --connection %s --table TABLE", final.SchemaID, conn.Name)))
	} else {
		fmt.Printf("\n✓ Ready: %s (%d tables)\n", final.SchemaID, len(final.Generation.Tables))
		fmt.Printf("Next: fakerforge push --schema %s --connection %s --table TABLE\n", final.SchemaID, conn.Name)
	}
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
	if ui.Enabled() {
		fmt.Printf("%s %s\n", ui.Title("Transmitting schema shape only (no row data) for"),
			ui.Bold(table)+ui.Title(":"))
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
			fmt.Printf("  %s %s\n", ui.Muted("-"),
				ui.Muted(fmt.Sprintf("%s %s %s%s", c.Name, parse.DisplayType(c), null, extra)))
		}
		return
	}
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
//
// Rendering (stdlib only, no new dependencies):
//   - TTY (os.Stdout is a terminal): live in-place block — one overall bar
//     plus one bar per table — rewritten via ANSI cursor-up + clear-line.
//   - Piped / non-TTY (tests, CI, logs): single-line summary printed only
//     when the rendered state changes.
//   - noProgress: suppress intermediate output, print only the final state.
func pollProgress(client *api.Client, schemaID string, interval, timeout time.Duration, out io.Writer, noProgress bool) (api.SchemaProgress, error) {
	deadline := time.Now().Add(timeout)
	tty := isTTYWriter(out)
	var last string
	prevLines := 0
	spinIdx := 0
	for {
		p, err := client.GetProgress(schemaID)
		if err != nil {
			if tty && prevLines > 0 {
				fmt.Fprintln(out)
			}
			return p, fmt.Errorf("progress poll failed: %w", err)
		}
		done := p.Overall == "ready" || p.Overall == "failed"

		if !noProgress || done {
			if tty {
				lines := renderPullLines(p, spinnerFrame(spinIdx))
				key := strings.Join(lines, "\n")
				// Spinner is embedded in the lines, so the key changes every
				// poll — this keeps the TTY alive during long parsing stages
				// while identical non-spinner states still dedupe via last.
				if key != last || done {
					if prevLines > 0 {
						fmt.Fprintf(out, "\x1b[%dA", prevLines)
					}
					for _, ln := range lines {
						fmt.Fprintf(out, "\x1b[2K\r%s\n", ln)
					}
					// New block shorter than the previous one: clear leftovers.
					if len(lines) < prevLines {
						fmt.Fprint(out, "\x1b[J")
					}
					last = key
					prevLines = len(lines)
				}
			} else {
				if line := formatProgress(p); line != last {
					fmt.Fprintln(out, line)
					last = line
				}
			}
		}
		spinIdx++

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
			if tty && prevLines > 0 {
				fmt.Fprintln(out)
			}
			return p, fmt.Errorf("timed out after %s waiting for readiness (state: %s)", timeout, p.Overall)
		}
		time.Sleep(interval)
	}
}

// barWidth is the fixed width of every ASCII progress bar.
const barWidth = 20

// maxDetailTables caps per-table lines so wide schemas don't flood the terminal.
const maxDetailTables = 10

var spinnerFrames = []string{"|", "/", "-", "\\"}

func spinnerFrame(i int) string {
	return spinnerFrames[i%len(spinnerFrames)]
}

func isTTYWriter(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// renderBar returns e.g. "[######--------------]" for gen/req.
// req <= 0 yields an indeterminate empty bar; gen is clamped to [0, req].
// Delegates to the shared Lip Gloss theme so TTY output is colorized
// while piped output stays plain ASCII.
func renderBar(gen, req, width int) string {
	return ui.ProgressBar(gen, req, width)
}

func barPercent(gen, req int) string {
	return ui.Percent(gen, req)
}

func pullTotals(p api.SchemaProgress) (gen, req int) {
	for _, t := range p.Generation.Tables {
		gen += t.Generated
		req += t.Requested
	}
	return gen, req
}

// renderPullLines builds the TTY block: one overall line plus one line per
// table (capped). Indeterminate stages keep a spinner instead of a bar.
func renderPullLines(p api.SchemaProgress, spin string) []string {
	switch p.Overall {
	case "failed":
		return []string{"failed"}
	case "relationships":
		return []string{fmt.Sprintf("%s relationships (%d found)…", spin, p.Relationships.Count)}
	case "generating", "ready":
		gen, req := pullTotals(p)
		// No per-table detail yet (e.g. queued but not started): fall back
		// to a single overall line instead of an empty block.
		if len(p.Generation.Tables) == 0 {
			if p.Overall == "ready" {
				return []string{"ready"}
			}
			return []string{fmt.Sprintf("%s generating %s %s (%d/%d)", spin, renderBar(gen, req, barWidth), barPercent(gen, req), gen, req)}
		}
		head := fmt.Sprintf("%s generating %s %s (%d/%d)", spin, renderBar(gen, req, barWidth), barPercent(gen, req), gen, req)
		if p.Overall == "ready" {
			head = fmt.Sprintf("ready %s %s (%d/%d)", renderBar(gen, req, barWidth), barPercent(gen, req), gen, req)
		}
		lines := []string{head}
		shown := p.Generation.Tables
		extra := 0
		if len(shown) > maxDetailTables {
			extra = len(shown) - maxDetailTables
			shown = shown[:maxDetailTables]
		}
		for _, t := range shown {
			lines = append(lines, fmt.Sprintf("  %s %s %s (%d/%d)", t.Table, renderBar(t.Generated, t.Requested, barWidth), barPercent(t.Generated, t.Requested), t.Generated, t.Requested))
		}
		if extra > 0 {
			lines = append(lines, fmt.Sprintf("  … +%d more", extra))
		}
		return lines
	default:
		return []string{fmt.Sprintf("%s parsing…", spin)}
	}
}

// formatProgress is the non-TTY single-line summary: overall bar + percent +
// per-table counts. Printed only when changed.
func formatProgress(p api.SchemaProgress) string {
	if p.Overall == "failed" {
		return "failed"
	}
	switch p.Overall {
	case "ready":
		gen, req := pullTotals(p)
		parts := make([]string, 0, len(p.Generation.Tables))
		for _, t := range p.Generation.Tables {
			parts = append(parts, fmt.Sprintf("%s %d/%d", t.Table, t.Generated, t.Requested))
		}
		detail := ""
		if len(parts) > 0 {
			detail = " " + strings.Join(parts, ", ")
		}
		return fmt.Sprintf("ready %s %s (%d/%d)%s", renderBar(gen, req, barWidth), barPercent(gen, req), gen, req, detail)
	case "generating":
		gen, req := pullTotals(p)
		parts := make([]string, 0, len(p.Generation.Tables))
		for _, t := range p.Generation.Tables {
			parts = append(parts, fmt.Sprintf("%s %d/%d", t.Table, t.Generated, t.Requested))
		}
		detail := ""
		if len(parts) > 0 {
			detail = " " + strings.Join(parts, ", ")
		}
		return fmt.Sprintf("generating %s %s (%d/%d)%s", renderBar(gen, req, barWidth), barPercent(gen, req), gen, req, detail)
	case "relationships":
		return fmt.Sprintf("relationships (%d found)…", p.Relationships.Count)
	default:
		return "parsing…"
	}
}
