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
	if b := ui.Brand(); b != "" {
		fmt.Println(b)
		fmt.Println()
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
	if ui.Enabled() {
		fmt.Println(ui.Section(fmt.Sprintf("Introspecting %s", conn.Database)))
		fmt.Println(ui.Muted("Transmitting schema shape only — never row data."))
		fmt.Println()
	}
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
		pairs := [][2]string{
			{"Schema", ui.Bold(resp.SchemaID)},
			{"Tables", fmt.Sprintf("%d%s", len(selected), reusedNote)},
		}
		if resp.DashboardURL != "" {
			pairs = append(pairs, [2]string{"Dashboard", ui.Muted(resp.DashboardURL)})
		}
		fmt.Println(ui.Panel(ui.Success("✓ Schema uploaded"), ui.KV(pairs)))
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
		if ui.Enabled() {
			fmt.Println(ui.Muted(fmt.Sprintf("Generation not started. Run: fakerforge schemas show %s", resp.SchemaID)))
		} else {
			fmt.Printf("Generation not started. Run: fakerforge schemas show %s\n", resp.SchemaID)
		}
		return 0
	}

	if _, err := client.GenerateSchema(resp.SchemaID, *rows); err != nil {
		fmt.Printf("generate failed to start: %v\n", err)
		return 1
	}

	if ui.Enabled() {
		fmt.Println()
		fmt.Println(ui.Section("Generating data"))
	}
	final, err := pollProgress(client, resp.SchemaID, *interval, *timeout, os.Stdout, *noProgress)
	if err != nil {
		fmt.Printf("\n%s\n", err)
		return 1
	}
	if ui.Enabled() {
		body := ui.KV([][2]string{
			{"Schema", ui.Bold(final.SchemaID)},
			{"Tables", fmt.Sprintf("%d", len(final.Generation.Tables))},
			{"Next", ui.Muted(fmt.Sprintf("fakerforge push --schema %s --connection %s --table TABLE", final.SchemaID, conn.Name))},
		})
		fmt.Println(ui.Panel(ui.Success("✓ Ready"), body))
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
		fmt.Printf("%s %s\n", ui.Title("▸ "+table),
			ui.Muted(fmt.Sprintf("(%d columns)", len(cols))))
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
			fmt.Printf("  %s %s\n", ui.Muted("│"),
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

// overallPercent prefers the server-reported percentage and falls back to
// the generated/requested totals for older servers that don't send it.
func overallPercent(p api.SchemaProgress) int {
	if p.Percentage > 0 || p.Overall == "ready" {
		pct := p.Percentage
		if pct > 100 {
			pct = 100
		}
		if pct < 0 {
			pct = 0
		}
		return pct
	}
	gen, req := pullTotals(p)
	if req <= 0 {
		return 0
	}
	pct := gen * 100 / req
	if pct > 100 {
		pct = 100
	}
	return pct
}

// renderPullLines builds the single-line live block: one overall bar with the
// current phase. Indeterminate stages keep a spinner instead of a bar.
func renderPullLines(p api.SchemaProgress, spin string) []string {
	switch p.Overall {
	case "failed":
		return []string{"failed"}
	case "relationships":
		return []string{fmt.Sprintf("%s relationships (%d found)…", spin, p.Relationships.Count)}
	case "generating", "ready":
		pct := overallPercent(p)
		if p.Overall == "ready" {
			pct = 100
		}
		bar := renderBar(pct, 100, barWidth)
		if p.Overall == "ready" {
			return []string{fmt.Sprintf("ready %s %s", bar, barPercent(pct, 100))}
		}
		return []string{fmt.Sprintf("%s generating %s %s", spin, bar, barPercent(pct, 100))}
	default:
		return []string{fmt.Sprintf("%s parsing…", spin)}
	}
}

// formatProgress is the non-TTY single-line summary: overall bar + percent.
// Printed only when changed.
func formatProgress(p api.SchemaProgress) string {
	if p.Overall == "failed" {
		return "failed"
	}
	switch p.Overall {
	case "ready":
		pct := 100
		return fmt.Sprintf("ready %s %s", renderBar(pct, 100, barWidth), barPercent(pct, 100))
	case "generating":
		pct := overallPercent(p)
		return fmt.Sprintf("generating %s %s", renderBar(pct, 100, barWidth), barPercent(pct, 100))
	case "relationships":
		return fmt.Sprintf("relationships (%d found)…", p.Relationships.Count)
	default:
		return "parsing…"
	}
}
