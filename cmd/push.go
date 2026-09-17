package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
	"github.com/kwasii1/fakerforge-db-agent/internal/insert"
)

// RunPush implements:
//
//	fakerforge push --schema ID --connection NAME [--table TABLE]
//	  [--batch-size N] [--dry-run] [--yes] [--append]
//
// With --table: single-table append (prod hosts confirm).
// Without --table: every ready table, TRUNCATE + INSERT parents-first
// (always confirmed), unless --append.
func RunPush(args []string) int {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	schemaID := fs.String("schema", "", "Schema ID")
	connName := fs.String("connection", "", "Saved connection name")
	table := fs.String("table", "", "Single target table (default: all ready tables)")
	batchSize := fs.Int("batch-size", 500, "Rows per transaction (1-5000)")
	dryRun := fs.Bool("dry-run", false, "Show what would happen, write nothing")
	yes := fs.Bool("yes", false, "Confirm writes without prompting")
	append := fs.Bool("append", false, "Insert without truncating first (all-tables mode)")
	apiURL := fs.String("api-url", "", "API base URL")
	apiKey := fs.String("api-key", "", "API key override")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *schemaID == "" {
		fmt.Println("push requires --schema ID")
		return 2
	}
	if *connName == "" {
		fmt.Println("push requires --connection NAME")
		return 2
	}
	if *batchSize < 1 || *batchSize > 5000 {
		fmt.Println("--batch-size must be 1-5000")
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

	if *table != "" {
		return pushSingleTable(client, d, conn, *schemaID, *table, *batchSize, *dryRun, *yes)
	}
	return pushAllTables(client, d, conn, *schemaID, *batchSize, *dryRun, *yes, *append)
}

// confirmWrite returns true when --yes was passed or the user types YES.
func confirmWrite(yes bool, format string, a ...any) bool {
	if yes {
		return true
	}
	fmt.Printf(format, a...)
	ans, err := promptLine("Type YES to continue")
	if err != nil || strings.TrimSpace(ans) != "YES" {
		fmt.Println("aborted.")
		return false
	}
	return true
}

type tablePlan struct {
	table   string
	dbCols  []db.Column
	rules   []api.SchemaColumn
	rows    int
	parents []string
}

// pushSingleTable pushes one table (append; prod hosts confirm).
func pushSingleTable(client *api.Client, d db.Driver, conn config.Connection, schemaID, table string, batchSize int, dryRun, yes bool) int {
	exists, err := d.TableExists(table)
	if err != nil {
		fmt.Printf("table check failed: %v\n", err)
		return 1
	}
	if !exists {
		fmt.Printf("target table %q does not exist in %s\n", table, conn.Database)
		return 1
	}

	detail, err := client.ShowTable(schemaID, table)
	if err != nil {
		fmt.Printf("schema fetch failed: %v\n", err)
		return 1
	}
	if detail.Status != "" && detail.Status != "ready" {
		fmt.Printf("schema %s table %q is not ready (status=%q): refusing to push\n", schemaID, table, detail.Status)
		return 1
	}

	dbCols, err := d.Introspect(table)
	if err != nil {
		fmt.Printf("introspect failed: %v\n", err)
		return 1
	}

	if isProdHost(conn.Host) && !dryRun {
		if !confirmWrite(yes, "WARNING: target host %q looks like production.\n", conn.Host) {
			return 1
		}
	}

	if dryRun {
		return pushDryRun(client, schemaID, table, dbCols)
	}

	n, err := streamInsert(client, d.DB(), conn.Driver, schemaID, table, dbCols, detailCols(detail), batchSize)
	if err != nil {
		fmt.Printf("\n%s\n", err)
		return 1
	}
	fmt.Printf("\n✓ Pushed %d rows into %s.%s\n", n, conn.Database, table)
	return 0
}

// pushAllTables pushes every ready table: validate all, truncate, insert
// parents-first. Aborts on the first failure with per-table tallies.
func pushAllTables(client *api.Client, d db.Driver, conn config.Connection, schemaID string, batchSize int, dryRun, yes, appendMode bool) int {
	detail, err := client.ShowSchema(schemaID)
	if err != nil {
		fmt.Printf("schema fetch failed: %v\n", err)
		return 1
	}
	if len(detail.Tables) == 0 {
		fmt.Printf("schema %s has no tables\n", schemaID)
		return 1
	}

	// Phase 0: validate everything before touching any data.
	var plans []tablePlan
	for _, t := range detail.Tables {
		if t.Status != "" && t.Status != "ready" {
			fmt.Printf("WARNING: skipping %q (status=%q) — dependents may fail on FKs\n", t.Table, t.Status)
			continue
		}
		exists, err := d.TableExists(t.Table)
		if err != nil {
			fmt.Printf("table check failed for %q: %v\n", t.Table, err)
			return 1
		}
		if !exists {
			fmt.Printf("target table %q does not exist in %s\n", t.Table, conn.Database)
			return 1
		}
		dbCols, err := d.Introspect(t.Table)
		if err != nil {
			fmt.Printf("introspect %q failed: %v\n", t.Table, err)
			return 1
		}
		td, err := client.ShowTable(schemaID, t.Table)
		if err != nil {
			fmt.Printf("schema fetch failed for %q: %v\n", t.Table, err)
			return 1
		}
		rules := detailCols(td)
		if len(rules) > 0 {
			if ok, diff := db.CheckSubset(rules, dbCols); !ok {
				fmt.Printf("schema mismatch for %q — refusing to push:\n%s\n", t.Table, diff)
				return 1
			}
		}
		plans = append(plans, tablePlan{table: t.Table, dbCols: dbCols, rules: rules, rows: t.Rows})
	}
	if len(plans) == 0 {
		fmt.Printf("schema %s has no ready tables to push\n", schemaID)
		return 1
	}

	// Order parents-first from the target DB's own FK graph, pruned to
	// the pushed set (external refs must already hold in the target).
	inSet := make(map[string]bool, len(plans))
	for _, p := range plans {
		inSet[p.table] = true
	}
	parents := make(map[string][]string, len(plans))
	for _, p := range plans {
		parents[p.table] = tableParentsOf(p.dbCols, inSet)
	}

	names := make([]string, 0, len(plans))
	for _, p := range plans {
		names = append(names, p.table)
	}
	ordered, err := orderTables(names, parents)
	if err != nil {
		fmt.Printf("cannot order tables: %v\n", err)
		return 1
	}
	byName := make(map[string]tablePlan, len(plans))
	for _, p := range plans {
		byName[p.table] = p
	}

	// Plan print.
	verb := "TRUNCATE + INSERT"
	if appendMode {
		verb = "INSERT"
	}
	fmt.Printf("%s %d table(s) in %s:\n", verb, len(ordered), conn.Database)
	for _, t := range ordered {
		fmt.Printf("  - %s (%d rows)\n", t, byName[t].rows)
	}

	if dryRun {
		fmt.Println("(no writes performed)")
		return 0
	}
	if !appendMode {
		if !confirmWrite(yes, "WARNING: this will DELETE all existing rows in the %d table(s) above.\n", len(ordered)) {
			return 1
		}
		if err := insert.TruncateTables(d.DB(), conn.Driver, ordered); err != nil {
			fmt.Printf("truncate failed: %v\n", err)
			return 1
		}
	} else if isProdHost(conn.Host) {
		if !confirmWrite(yes, "WARNING: target host %q looks like production.\n", conn.Host) {
			return 1
		}
	}

	total := 0
	for _, t := range ordered {
		p := byName[t]
		n, err := streamInsert(client, d.DB(), conn.Driver, schemaID, t, p.dbCols, p.rules, batchSize)
		if err != nil {
			fmt.Printf("\n%s\naborted after %d total rows (%s: %d rows not completed)\n", err, total, t, p.rows)
			return 1
		}
		total += n
		fmt.Printf("\n✓ %s: %d rows\n", t, n)
	}
	fmt.Printf("✓ Pushed %d rows into %d table(s) in %s\n", total, len(ordered), conn.Database)
	return 0
}

// tableParentsOf extracts parent table names, optionally pruned to inSet
// (nil inSet keeps every well-formed ref).
func tableParentsOf(dbCols []db.Column, inSet map[string]bool) []string {
	var out []string
	seen := make(map[string]bool)
	for _, c := range dbCols {
		parts := strings.SplitN(c.FKRef, ".", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		parent := parts[0]
		if inSet != nil && !inSet[parent] {
			continue
		}
		if !seen[parent] {
			seen[parent] = true
			out = append(out, parent)
		}
	}
	return out
}

// streamInsert streams rows and batch-inserts them, printing running
// progress. Returns rows inserted or a descriptive error.
func streamInsert(client *api.Client, sqldb *sqlx.DB, driver, schemaID, table string, dbCols []db.Column, rules []api.SchemaColumn, batchSize int) (int, error) {
	batch := make([]map[string]any, 0, batchSize)
	var columns []string
	succeeded := 0
	checkedSubset := len(rules) > 0
	var abortErr error

	flush := func() int {
		if len(batch) == 0 {
			return 0
		}
		n, err := insert.Batch(sqldb, driver, table, columns, batch)
		if err != nil {
			fmt.Printf("\nbatch failed after %d succeeded rows: %v\n", succeeded, err)
			return -1
		}
		succeeded += n
		fmt.Fprintf(os.Stderr, "\r%d rows inserted", succeeded)
		batch = batch[:0]
		return n
	}

	_, streamErr := client.StreamRows(schemaID, table, func(row map[string]any) bool {
		if !checkedSubset {
			cols := rowKeysAsSchema(row)
			if ok, diff := db.CheckSubset(cols, dbCols); !ok {
				fmt.Printf("schema mismatch — refusing to push:\n%s\n", diff)
				checkedSubset = true // prevent repeat
				abortErr = fmt.Errorf("schema mismatch")
				return false
			}
			checkedSubset = true
		}
		if columns == nil {
			columns = sortedKeys(row)
		}
		batch = append(batch, row)
		if len(batch) >= batchSize {
			if flush() < 0 {
				abortErr = fmt.Errorf("batch insert failed")
				return false
			}
		}
		return true
	})
	if abortErr != nil {
		return succeeded, abortErr
	}
	if streamErr != nil {
		return succeeded, fmt.Errorf("stream failed after %d rows: %w", succeeded, streamErr)
	}
	if flush() < 0 {
		return succeeded, fmt.Errorf("batch insert failed")
	}
	return succeeded, nil
}

func pushDryRun(client *api.Client, id, table string, dbCols []db.Column) int {
	var sample []map[string]any
	count := 0
	_, err := client.StreamRows(id, table, func(row map[string]any) bool {
		if len(sample) < 5 {
			sample = append(sample, row)
		}
		count++
		return true
	})
	if err != nil {
		fmt.Printf("dry-run stream failed: %v\n", err)
		return 1
	}
	fmt.Printf("dry-run: %d rows would be inserted into %s\n", count, table)
	if len(sample) > 0 {
		if ok, diff := db.CheckSubsetMaps(sample[0], dbCols); !ok {
			fmt.Printf("WARNING: %s\n", diff)
		}
		fmt.Println("sample:")
		for _, r := range sample {
			b, _ := json.Marshal(r)
			fmt.Printf("  %s\n", string(b))
		}
	}
	fmt.Println("(no writes performed)")
	return 0
}

func detailCols(d api.TableDetail) []api.SchemaColumn {
	// TableDetail.Rules may carry columns; accept []any of {name,type}.
	if d.Rules == nil {
		return nil
	}
	b, err := json.Marshal(d.Rules)
	if err != nil {
		return nil
	}
	var holder struct {
		Columns []api.SchemaColumn `json:"columns"`
	}
	if err := json.Unmarshal(b, &holder); err == nil && len(holder.Columns) > 0 {
		return holder.Columns
	}
	var direct []api.SchemaColumn
	if err := json.Unmarshal(b, &direct); err == nil && len(direct) > 0 {
		return direct
	}
	return nil
}

func rowKeysAsSchema(row map[string]any) []api.SchemaColumn {
	out := make([]api.SchemaColumn, 0, len(row))
	for k, v := range row {
		out = append(out, api.SchemaColumn{Name: k, Type: inferType(v)})
	}
	return out
}

func inferType(v any) string {
	switch v.(type) {
	case bool:
		return "boolean"
	case float64:
		return "numeric"
	case map[string]any, []any:
		return "json"
	default:
		return "text"
	}
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
