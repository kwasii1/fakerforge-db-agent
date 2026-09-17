package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
)

// RunLogin implements: fakerforge login [--api-key KEY] [--api-url URL]
func RunLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	apiKey := fs.String("api-key", "", "API key (or FAKERFORGE_API_KEY env, or interactive prompt)")
	apiURL := fs.String("api-url", "", "API base URL (or FAKERFORGE_API_URL env)")
	if err := fs.Parse(args); err != nil {
		return 2
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
		fmt.Printf("login failed: %v\n", err)
		return 1
	}
	if err := config.SetAPIKey(key); err != nil {
		fmt.Printf("login failed to save key: %v\n", err)
		return 1
	}
	fmt.Printf("✓ Logged in as %s\n", u.Email)
	return 0
}

// RunLogout implements: fakerforge logout
func RunLogout(args []string) int {
	if err := config.DeleteAPIKey(); err != nil {
		fmt.Printf("logout: %v\n", err)
		return 1
	}
	fmt.Println("Logged out: API key deleted.")
	return 0
}

// RunWhoami implements: fakerforge whoami [--api-url URL]
func RunWhoami(args []string) int {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	apiURL := fs.String("api-url", "", "API base URL")
	if err := fs.Parse(args); err != nil {
		return 2
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
	fmt.Printf("Name:  %s\nEmail: %s\n", u.Name, u.Email)
	if u.Plan != "" {
		fmt.Printf("Plan:  %s\n", u.Plan)
	}
	fmt.Printf("API:   %s\n", c.BaseURL)
	return 0
}
