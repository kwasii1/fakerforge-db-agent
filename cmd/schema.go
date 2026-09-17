package cmd

import (
	"flag"
	"fmt"

	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
)

// RunSchemaPull implements: fakerforge schema pull --connection NAME --table TABLE [--api-url URL]
func RunSchemaPull(args []string) int {
	fs := flag.NewFlagSet("schema pull", flag.ContinueOnError)
	connName := fs.String("connection", "", "Saved connection name (default: default connection)")
	table := fs.String("table", "", "Table to introspect")
	apiURL := fs.String("api-url", "", "API base URL")
	apiKey := fs.String("api-key", "", "API key override")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *table == "" {
		fmt.Println("schema pull requires --table TABLE")
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

	cols, err := d.Introspect(*table)
	if err != nil {
		fmt.Printf("introspect failed: %v\n", err)
		return 1
	}

	// Security transparency: show exactly what leaves the machine.
	fmt.Printf("Transmitting schema shape only (no row data) for %s:\n", *table)
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
		fmt.Printf("  - %s %s %s%s\n", c.Name, c.Type, null, extra)
	}

	resp, err := client.CreateSchema(*table, db.ToAPISchema(cols))
	if err != nil {
		fmt.Printf("upload failed: %v\n", err)
		return 1
	}
	fmt.Printf("✓ Schema uploaded: schema_id=%s (%d columns from %s)\n", resp.SchemaID, len(cols), *table)
	return 0
}
