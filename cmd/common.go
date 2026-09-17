package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
	"golang.org/x/term"
)

// apiURLFromEnv returns FAKERFORGE_API_URL or the localhost default.
func apiURLFromEnv() string {
	if v := os.Getenv("FAKERFORGE_API_URL"); v != "" {
		return v
	}
	return api.DefaultBaseURL
}

// newClient builds an authenticated API client from flag/env/store.
func newClient(apiKeyFlag, apiURLFlag string) (*api.Client, error) {
	key, err := config.ResolveAPIKey(apiKeyFlag)
	if err != nil || key == "" {
		return nil, fmt.Errorf("not logged in: run `fakerforge login` or set FAKERFORGE_API_KEY")
	}
	base := apiURLFlag
	if base == "" {
		base = apiURLFromEnv()
	}
	return api.New(base, key), nil
}

func promptLine(label string) (string, error) {
	fmt.Printf("%s: ", label)
	r := bufio.NewReader(os.Stdin)
	s, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func promptPassword(label string) (string, error) {
	fmt.Printf("%s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// extractFlag pulls a --name value / --name=value flag out of args (last
// occurrence wins) and returns the value plus the remaining args.
//
// Needed because stdlib flag stops parsing at the first positional
// argument, so `schemas show SCHEMA_ID --table users` would otherwise
// silently ignore --table. Pre-extracting lets flags appear on either
// side of the positional ID.
func extractFlag(args []string, name string) (string, []string) {
	var value string
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--"+name && i+1 < len(args) {
			value = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(a, "--"+name+"=") {
			value = strings.TrimPrefix(a, "--"+name+"=")
			continue
		}
		rest = append(rest, a)
	}
	return value, rest
}

// isProdHost reports whether a host looks like production.
func isProdHost(host string) bool {
	h := strings.ToLower(host)
	return strings.Contains(h, "prod") || strings.Contains(h, "production")
}
