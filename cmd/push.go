package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
	"github.com/kwasii1/fakerforge-db-agent/internal/insert"
)

// RunPush implements:
//   fakerforge push --schema ID --connection NAME --table TABLE [--batch-size N] [--dry-run] [--yes]
func RunPush(args []string) int {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	schemaID := fs.String("schema", "", "Schema ID")
	connName := fs.String("connection", "", "Saved connection name")
	table := fs.String("table", "", "Target table")
	batchSize := fs.Int("batch-size", 500, "Rows per transaction (1-5000)")
	dryRun := fs.Bool("dry-run", false, "Show what would be inserted, write nothing")
	yes := fs.Bool("yes", false, "Confirm writes to prod-like hosts")
	apiURL := fs.String("api-url", "", "API base URL")
	apiKey := fs.String("api-key", "", "API key override")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	id := *schemaID
	if id == "" || *table == "" {
		fmt.Println("push requires --schema ID and --table TABLE")
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

	exists, err := d.TableExists(*table)
	if err != nil {
		fmt.Printf("table check failed: %v\n", err)
		return 1
	}
	if !exists {
		fmt.Printf("target table %q does not exist in %s\n", *table, conn.Database)
		return 1
	}

	detail, err := client.ShowTable(id, *table)
	if err != nil {
		fmt.Printf("schema fetch failed: %v\n", err)
		return 1
	}
	if detail.Status != "" && detail.Status != "ready" {
		fmt.Printf("schema %s is not ready (status=%q): refusing to push\n", id, detail.Status)
		return 1
	}

	dbCols, err := d.Introspect(*table)
	if err != nil {
		fmt.Printf("introspect failed: %v\n", err)
		return 1
	}

	// Prod heuristic: require --yes or interactive confirm.
	if isProdHost(conn.Host) && !*dryRun && !*yes {
		fmt.Printf("WARNING: target host %q looks like production.\n", conn.Host)
		ans, err := promptLine("Type YES to continue")
		if err != nil || strings.TrimSpace(ans) != "YES" {
			fmt.Println("aborted.")
			return 1
		}
	}

	if *dryRun {
		return pushDryRun(client, id, *table, dbCols)
	}

	// Pre-check subset from advertised columns when available.
	if len(detailCols(detail)) > 0 {
		if ok, diff := db.CheckSubset(detailCols(detail), dbCols); !ok {
			fmt.Printf("schema mismatch — refusing to push:\n%s\n", diff)
			return 1
		}
	}

	// Stream + batch. First row also validates subset when columns unknown.
	batch := make([]map[string]any, 0, *batchSize)
	var columns []string
	succeeded := 0
	checkedSubset := len(detailCols(detail)) > 0
	var abortErr error

	flush := func() int {
		if len(batch) == 0 {
			return 0
		}
		n, err := insert.Batch(d.DB(), conn.Driver, *table, columns, batch)
		if err != nil {
			fmt.Printf("\nbatch failed after %d succeeded rows: %v\n", succeeded, err)
			return -1
		}
		succeeded += n
		fmt.Fprintf(os.Stderr, "\r%d rows inserted", succeeded)
		batch = batch[:0]
		return n
	}

	_, streamErr := client.StreamRows(id, *table, func(row map[string]any) bool {
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
		if len(batch) >= *batchSize {
			if flush() < 0 {
				abortErr = fmt.Errorf("batch insert failed")
				return false
			}
		}
		return true
	})
	if abortErr != nil {
		fmt.Printf("\n%s\n", abortErr)
		return 1
	}
	if streamErr != nil {
		fmt.Printf("\nstream failed after %d rows: %v\n", succeeded, streamErr)
		return 1
	}
	if flush() < 0 {
		return 1
	}
	fmt.Printf("\n✓ Pushed %d rows into %s.%s\n", succeeded, conn.Database, *table)
	return 0
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
