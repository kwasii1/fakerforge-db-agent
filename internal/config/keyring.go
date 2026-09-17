package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const (
	// Service is the OS keychain service name for all fakerforge secrets.
	Service = "fakerforge-cli"
	// APIKeyName is the keychain key for the FakerForge API key.
	APIKeyName = "api_key"
)

// connKey returns the keychain key for a connection password.
func connKey(name string) string { return "conn:" + name }

// secretsFile is the fallback store when no keychain is available
// (headless Linux, CI, missing dbus). Mode 0600, same dir as connections.yaml.
func secretsFile() (string, error) {
	base := os.Getenv("FAKERFORGE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".fakerforge")
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(base, ".secrets"), nil
}

func fallbackSet(key, value string) error {
	p, err := secretsFile()
	if err != nil {
		return err
	}
	// Best-effort simple format: KEY=VALUE lines. Secrets stay 0600.
	existing := map[string]string{}
	if data, err := os.ReadFile(p); err == nil {
		for _, line := range splitLines(string(data)) {
			k, v, ok := splitKV(line)
			if ok {
				existing[k] = v
			}
		}
	}
	existing[key] = value
	out := ""
	for k, v := range existing {
		out += k + "=" + v + "\n"
	}
	return os.WriteFile(p, []byte(out), 0o600)
}

func fallbackGet(key string) (string, error) {
	p, err := secretsFile()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("secret %q not found", key)
	}
	for _, line := range splitLines(string(data)) {
		k, v, ok := splitKV(line)
		if ok && k == key {
			return v, nil
		}
	}
	return "", fmt.Errorf("secret %q not found", key)
}

func fallbackDelete(key string) error {
	p, err := secretsFile()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil // nothing to delete
	}
	out := ""
	for _, line := range splitLines(string(data)) {
		k, _, ok := splitKV(line)
		if ok && k == key {
			continue
		}
		if line != "" {
			out += line + "\n"
		}
	}
	return os.WriteFile(p, []byte(out), 0o600)
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func splitKV(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

// SetAPIKey stores the API key, preferring the OS keychain.
func SetAPIKey(key string) error {
	if err := keyring.Set(Service, APIKeyName, key); err == nil {
		return nil
	}
	return fallbackSet(APIKeyName, key)
}

// GetAPIKey reads the API key. FAKERFORGE_API_KEY env takes precedence
// (handled by callers via ResolveAPIKey), this reads persisted storage.
func GetAPIKey() (string, error) {
	if v, err := keyring.Get(Service, APIKeyName); err == nil && v != "" {
		return v, nil
	}
	return fallbackGet(APIKeyName)
}

// DeleteAPIKey removes the stored API key from keychain and fallback.
func DeleteAPIKey() error {
	_ = keyring.Delete(Service, APIKeyName)
	return fallbackDelete(APIKeyName)
}

// ResolveAPIKey returns --api-key flag > env > stored key.
func ResolveAPIKey(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if v := os.Getenv("FAKERFORGE_API_KEY"); v != "" {
		return v, nil
	}
	return GetAPIKey()
}

// SetConnPassword stores a DB password for a connection.
func SetConnPassword(name, password string) error {
	if err := keyring.Set(Service, connKey(name), password); err == nil {
		return nil
	}
	return fallbackSet(connKey(name), password)
}

// GetConnPassword reads a DB password. FAKERFORGE_DB_PASSWORD env takes
// precedence when set (single-connection scripting convenience).
func GetConnPassword(name string) (string, error) {
	if v := os.Getenv("FAKERFORGE_DB_PASSWORD"); v != "" {
		return v, nil
	}
	if v, err := keyring.Get(Service, connKey(name)); err == nil && v != "" {
		return v, nil
	}
	return fallbackGet(connKey(name))
}

// DeleteConnPassword removes a connection password from all stores.
func DeleteConnPassword(name string) error {
	_ = keyring.Delete(Service, connKey(name))
	return fallbackDelete(connKey(name))
}
