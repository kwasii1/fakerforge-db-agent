package cmd

import (
	"flag"
	"fmt"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/db"
)

// RunStatus implements: fakerforge status
// Checks API reachable+auth, each saved connection ping, CLI version vs latest.
func RunStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	apiURL := fs.String("api-url", "", "API base URL")
	apiKey := fs.String("api-key", "", "API key override")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	failed := false

	key, err := config.ResolveAPIKey(*apiKey)
	if err != nil || key == "" {
		fmt.Println("API auth:   FAIL (not logged in)")
		failed = true
	} else {
		base := *apiURL
		if base == "" {
			base = apiURLFromEnv()
		}
		c := api.New(base, key)
		u, err := c.Me()
		if err != nil {
			fmt.Printf("API auth:   FAIL (%v)\n", err)
			failed = true
		} else {
			fmt.Printf("API auth:   OK (%s @ %s)\n", u.Email, c.BaseURL)
			if latest, err := c.LatestVersion(); err == nil && latest != "" {
				if latest == api.Version {
					fmt.Printf("CLI version: OK (%s, latest)\n", api.Version)
				} else {
					fmt.Printf("CLI version: UPDATE AVAILABLE (local %s, latest %s)\n", api.Version, latest)
				}
			} else {
				fmt.Printf("CLI version: %s (latest check skipped)\n", api.Version)
			}
		}
	}

	store, err := config.Default()
	if err != nil {
		fmt.Printf("connections: FAIL (%v)\n", err)
		return 1
	}
	conns, err := store.List()
	if err != nil {
		fmt.Printf("connections: FAIL (%v)\n", err)
		return 1
	}
	if len(conns) == 0 {
		fmt.Println("connections: none saved")
	} else {
		def, _ := store.GetDefaultName()
		for _, conn := range conns {
			pw, err := config.GetConnPassword(conn.Name)
			if err != nil {
				fmt.Printf("  %s: FAIL (no password: %v)\n", conn.Name, err)
				failed = true
				continue
			}
			d, err := db.Open(conn, pw)
			if err != nil {
				fmt.Printf("  %s: FAIL (%v)\n", conn.Name, err)
				failed = true
				continue
			}
			_ = d.Close()
			mark := ""
			if conn.Name == def {
				mark = " [default]"
			}
			fmt.Printf("  %s: OK (%s:%d/%s)%s\n", conn.Name, conn.Host, conn.Port, conn.Database, mark)
		}
	}

	if failed {
		return 1
	}
	return 0
}
