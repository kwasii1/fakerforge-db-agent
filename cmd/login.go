package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"github.com/kwasii1/fakerforge-db-agent/internal/ui"
)

// RunLogin implements: fakerforge login [--api-key KEY] [--api-url URL]
func RunLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	apiKey := fs.String("api-key", "", "API key (or FAKERFORGE_API_KEY env, or interactive prompt)")
	apiURL := fs.String("api-url", "", "API base URL (or FAKERFORGE_API_URL env)")
	noColor := fs.Bool("no-color", false, "Disable colored output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *noColor {
		ui.SetNoColor(true)
	}
	if b := ui.Banner(); b != "" {
		fmt.Print(b)
		fmt.Println()
	}
	key := *apiKey
	if key == "" {
		if v := os.Getenv("FAKERFORGE_API_KEY"); v != "" {
			key = v
		} else {
			v, err := promptLine("API key")
			if err != nil || v == "" {
				fmt.Println("login cancelled: no API key provided")
				return 1
			}
			key = v
		}
	}
	base := *apiURL
	if base == "" {
		base = apiURLFromEnv()
	}
	c := api.New(base, key)
	u, err := c.Me()
	if err != nil {
		if ui.Enabled() {
			fmt.Printf("%s login failed: %v\n", ui.Error("✗"), err)
		} else {
			fmt.Printf("login failed: %v\n", err)
		}
		return 1
	}
	if err := config.SetAPIKey(key); err != nil {
		fmt.Printf("login failed to save key: %v\n", err)
		return 1
	}
	if ui.Enabled() {
		body := ui.KV([][2]string{
			{"Email", ui.Bold(u.Email)},
			{"Plan", ui.StatusPill(u.Plan)},
			{"API", ui.Muted(c.BaseURL)},
		})
		fmt.Println(ui.Panel(ui.Success("✓ Logged in"), body))
	} else {
		fmt.Printf("✓ Logged in as %s\n", u.Email)
	}
	return 0
}

// RunLogout implements: fakerforge logout
func RunLogout(args []string) int {
	if b := ui.Banner(); b != "" {
		fmt.Print(b)
		fmt.Println()
	}
	if err := config.DeleteAPIKey(); err != nil {
		fmt.Printf("logout: %v\n", err)
		return 1
	}
	if ui.Enabled() {
		fmt.Printf("%s Logged out — API key deleted.\n", ui.Success("✓"))
	} else {
		fmt.Println("Logged out: API key deleted.")
	}
	return 0
}

// RunWhoami implements: fakerforge whoami [--api-url URL]
func RunWhoami(args []string) int {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	apiURL := fs.String("api-url", "", "API base URL")
	noColor := fs.Bool("no-color", false, "Disable colored output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *noColor {
		ui.SetNoColor(true)
	}
	if b := ui.Banner(); b != "" {
		fmt.Print(b)
		fmt.Println()
	}
	c, err := newClient("", *apiURL)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	u, err := c.Me()
	if err != nil {
		fmt.Printf("whoami failed: %v\n", err)
		return 1
	}
	if ui.Enabled() {
		pairs := [][2]string{
			{"Name", ui.Bold(u.Name)},
			{"Email", ui.Success(u.Email)},
		}
		if u.Plan != "" {
			pairs = append(pairs, [2]string{"Plan", ui.StatusPill(u.Plan)})
		}
		pairs = append(pairs, [2]string{"API", ui.Muted(c.BaseURL)})
		fmt.Println(ui.Panel("", ui.KV(pairs)))
		return 0
	}
	fmt.Printf("Name:  %s\nEmail: %s\n", u.Name, u.Email)
	if u.Plan != "" {
		fmt.Printf("Plan:  %s\n", u.Plan)
	}
	fmt.Printf("API:   %s\n", c.BaseURL)
	return 0
}
