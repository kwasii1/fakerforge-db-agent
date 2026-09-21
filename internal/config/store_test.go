package config

import (
	"os"
	"path/filepath"
	"testing"
)

func tmpHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FAKERFORGE_HOME", dir)
	// also clear keychain fallback isolation: secrets live under FAKERFORGE_HOME
	return dir
}

func TestStorePutGetDefault(t *testing.T) {
	home := tmpHome(t)
	s, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if s.Path() != filepath.Join(home, "connections.yaml") {
		t.Fatalf("unexpected path %s", s.Path())
	}
	c := Connection{Name: "local", Driver: "postgres", Host: "localhost", Port: 5432, Database: "app", User: "u"}
	if err := s.Put(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("local")
	if err != nil {
		t.Fatal(err)
	}
	if got.Database != "app" {
		t.Fatalf("got %+v", got)
	}
	def, _ := s.GetDefaultName()
	if def != "local" {
		t.Fatalf("default = %q", def)
	}
	// second conn doesn't steal default
	if err := s.Put(Connection{Name: "b", Driver: "mysql", Host: "h", Port: 3306, Database: "d", User: "u"}); err != nil {
		t.Fatal(err)
	}
	def, _ = s.GetDefaultName()
	if def != "local" {
		t.Fatalf("default moved: %q", def)
	}
	if err := s.SetDefault("b"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("local"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("local"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestStoreRejectsBadDriver(t *testing.T) {
	tmpHome(t)
	s, _ := Default()
	if err := s.Put(Connection{Name: "x", Driver: "sqlite"}); err == nil {
		t.Fatal("expected driver error")
	}
}

func TestSecretsFallbackRoundtrip(t *testing.T) {
	tmpHome(t)
	// Force fallback path by using keys that keyring may not support in CI;
	// either backend succeeding is fine as long as roundtrip works.
	if err := SetAPIKey("k123"); err != nil {
		t.Fatal(err)
	}
	v, err := GetAPIKey()
	if err != nil || v != "k123" {
		t.Fatalf("got %q %v", v, err)
	}
	if err := SetConnPassword("c1", "pw"); err != nil {
		t.Fatal(err)
	}
	// isolate env override
	os.Unsetenv("FAKERFORGE_DB_PASSWORD")
	pw, err := GetConnPassword("c1")
	if err != nil || pw != "pw" {
		t.Fatalf("got %q %v", pw, err)
	}
	_ = DeleteConnPassword("c1")
	_ = DeleteAPIKey()
}
