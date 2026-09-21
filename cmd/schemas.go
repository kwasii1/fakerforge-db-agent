package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/ui"
)

// RunSchemasList implements: fakerforge schemas list [--table T] [--format table|json]
func RunSchemasList(args []string) int {
	fs := flag.NewFlagSet("schemas list", flag.ContinueOnError)
	table := fs.String("table", "", "Only show schemas containing this table")
	format := fs.String("format", "table", "Output: table|json")
	apiURL := fs.String("api-url", "", "API base URL")
	apiKey := fs.String("api-key", "", "API key override")
	noColor := fs.Bool("no-color", false, "Disable colored output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *noColor {
		ui.SetNoColor(true)
	}
	if *format != "json" {
		if b := ui.Brand(); b != "" {
			fmt.Println(b)
			fmt.Println()
		}
	}
	client, err := newClient(*apiKey, *apiURL)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	items, err := client.ListSchemas(*table)
	if err != nil {
		fmt.Printf("list failed: %v\n", err)
		return 1
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(items)
		return 0
	}
	if len(items) == 0 {
		if ui.Enabled() {
			fmt.Println(ui.Muted("No schemas found — run `fakerforge schema pull` to create one."))
		} else {
			fmt.Println("No schemas found.")
		}
		return 0
	}
	if ui.Enabled() {
		rows := make([][]string, 0, len(items))
		for _, s := range items {
			rows = append(rows, []string{s.ID, s.Name, strconv.Itoa(s.Tables), s.Created})
		}
		fmt.Println(ui.Section("Schemas"))
		fmt.Println(ui.Table([]string{"SCHEMA ID", "NAME", "TABLES", "CREATED"}, rows))
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SCHEMA ID\tNAME\tTABLES\tCREATED")
	for _, s := range items {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", s.ID, s.Name, s.Tables, s.Created)
	}
	_ = w.Flush()
	return 0
}

// RunSchemasShow implements: fakerforge schemas show SCHEMA_ID [--table T]
// Without --table: schema detail with the per-table list (rows + status).
// With --table: detail for just that table (status, columns, sample).
// Flags are accepted on either side of SCHEMA_ID (see extractFlag).
func RunSchemasShow(args []string) int {
	tableVal, args := extractFlag(args, "table")
	apiURLVal, args := extractFlag(args, "api-url")
	apiKeyVal, args := extractFlag(args, "api-key")

	fs := flag.NewFlagSet("schemas show", flag.ContinueOnError)
	table := fs.String("table", "", "Show detail for just this table")
	apiURL := fs.String("api-url", "", "API base URL")
	apiKey := fs.String("api-key", "", "API key override")
	showNoColor := fs.Bool("no-color", false, "Disable colored output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showNoColor {
		ui.SetNoColor(true)
	}
	if b := ui.Brand(); b != "" {
		fmt.Println(b)
		fmt.Println()
	}
	if *table == "" {
		*table = tableVal
	}
	if *apiURL == "" {
		*apiURL = apiURLVal
	}
	if *apiKey == "" {
		*apiKey = apiKeyVal
	}
	rest := fs.Args()
	if len(rest) < 1 {
		fmt.Println("usage: fakerforge schemas show SCHEMA_ID [--table T]")
		return 2
	}
	id := rest[0]
	client, err := newClient(*apiKey, *apiURL)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	if *table != "" {
		return showTable(client, id, *table)
	}
	d, err := client.ShowSchema(id)
	if err != nil {
		fmt.Printf("show failed: %v\n", err)
		return 1
	}
	if ui.Enabled() {
		pairs := [][2]string{
			{"Schema", ui.Bold(d.ID)},
			{"Name", d.Name},
		}
		if d.Created != "" {
			pairs = append(pairs, [2]string{"Created", ui.Muted(d.Created)})
		}
		fmt.Println(ui.Panel("", ui.KV(pairs)))
		if len(d.Tables) == 0 {
			fmt.Println(ui.Muted("No tables."))
			return 0
		}
		fmt.Println()
		fmt.Println(ui.Section("Tables"))
		rows := make([][]string, 0, len(d.Tables))
		for _, t := range d.Tables {
			rows = append(rows, []string{t.Table, strconv.Itoa(t.Rows), ui.StatusPill(t.Status)})
		}
		fmt.Println(ui.Table([]string{"TABLE", "ROWS", "STATUS"}, rows))
		return 0
	}
	fmt.Printf("Schema:  %s\nName:    %s\n", d.ID, d.Name)
	if d.Created != "" {
		fmt.Printf("Created: %s\n", d.Created)
	}
	if len(d.Tables) == 0 {
		fmt.Println("Tables:  (none)")
		return 0
	}
	fmt.Println("Tables:")
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "  TABLE\tROWS\tSTATUS")
	for _, t := range d.Tables {
		fmt.Fprintf(w, "  %s\t%d\t%s\n", t.Table, t.Rows, t.Status)
	}
	_ = w.Flush()
	return 0
}

func showTable(client *api.Client, id, table string) int {
	d, err := client.ShowTable(id, table)
	if err != nil {
		fmt.Printf("show failed: %v\n", err)
		return 1
	}
	if ui.Enabled() {
		pairs := [][2]string{
			{"Schema", ui.Bold(d.ID) + " " + ui.Muted("("+d.Name+")")},
			{"Table", ui.Bold(d.Table)},
			{"Rows", fmt.Sprintf("%d", d.Rows)},
			{"Status", ui.StatusPill(d.Status)},
		}
		if d.Created != "" {
			pairs = append(pairs, [2]string{"Created", ui.Muted(d.Created)})
		}
		fmt.Println(ui.Panel("", ui.KV(pairs)))
		if d.Rules != nil {
			if b, err := json.Marshal(d.Rules); err == nil {
				fmt.Println()
				fmt.Println(ui.Section("Rules"))
				fmt.Println(ui.Muted(string(b)))
			}
		}
		if len(d.Sample) > 0 {
			fmt.Println()
			fmt.Println(ui.Section("Sample (first rows)"))
			n := len(d.Sample)
			if n > 5 {
				n = 5
			}
			for i := 0; i < n; i++ {
				b, _ := json.Marshal(d.Sample[i])
				fmt.Printf("  %s\n", ui.Muted(string(b)))
			}
		}
		return 0
	}
	fmt.Printf("Schema:  %s (%s)\nTable:   %s\nRows:    %d\nStatus:  %s\n",
		d.ID, d.Name, d.Table, d.Rows, d.Status)
	if d.Created != "" {
		fmt.Printf("Created: %s\n", d.Created)
	}
	if d.Rules != nil {
		if b, err := json.Marshal(d.Rules); err == nil {
			fmt.Printf("Rules:   %s\n", string(b))
		}
	}
	if len(d.Sample) > 0 {
		fmt.Println("Sample (first rows):")
		n := len(d.Sample)
		if n > 5 {
			n = 5
		}
		for i := 0; i < n; i++ {
			b, _ := json.Marshal(d.Sample[i])
			fmt.Printf("  %s\n", string(b))
		}
	}
	return 0
}
