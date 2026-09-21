package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Connection holds non-secret metadata for a saved DB connection.
// Secrets (passwords) live in the OS keychain, never here.
type Connection struct {
	Name     string `yaml:"name"`
	Driver   string `yaml:"driver"` // postgres | mysql
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	User     string `yaml:"user"`
}

type fileShape struct {
	Default     string                `yaml:"default,omitempty"`
	Connections map[string]Connection `yaml:"connections"`
}

// Store persists connection metadata to ~/.fakerforge/connections.yaml.
type Store struct {
	path string
}

// New returns a Store rooted at dir (or ~/.fakerforge when dir == "").
// FAKERFORGE_HOME overrides the base dir (useful for tests).
func New(dir string) (*Store, error) {
	if dir == "" {
		if env := os.Getenv("FAKERFORGE_HOME"); env != "" {
			dir = env
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("resolve home dir: %w", err)
			}
			dir = filepath.Join(home, ".fakerforge")
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	return &Store{path: filepath.Join(dir, "connections.yaml")}, nil
}

// Default returns a Store at the default location.
func Default() (*Store, error) { return New("") }

// Path returns the underlying yaml file path (for debugging).
func (s *Store) Path() string { return s.path }

func (s *Store) load() (fileShape, error) {
	var fs fileShape
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileShape{Connections: map[string]Connection{}}, nil
		}
		return fs, fmt.Errorf("read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return fileShape{Connections: map[string]Connection{}}, nil
	}
	if err := yaml.Unmarshal(data, &fs); err != nil {
		return fs, fmt.Errorf("parse %s: %w", s.path, err)
	}
	if fs.Connections == nil {
		fs.Connections = map[string]Connection{}
	}
	return fs, nil
}

func (s *Store) save(fs fileShape) error {
	if fs.Connections == nil {
		fs.Connections = map[string]Connection{}
	}
	data, err := yaml.Marshal(&fs)
	if err != nil {
		return fmt.Errorf("marshal connections: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	return nil
}

// List returns all saved connections.
func (s *Store) List() ([]Connection, error) {
	fs, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Connection, 0, len(fs.Connections))
	for _, c := range fs.Connections {
		out = append(out, c)
	}
	return out, nil
}

// Get returns the named connection or an error.
func (s *Store) Get(name string) (Connection, error) {
	fs, err := s.load()
	if err != nil {
		return Connection{}, err
	}
	c, ok := fs.Connections[name]
	if !ok {
		return Connection{}, fmt.Errorf("connection %q not found (see `fakerforge connect --list`)", name)
	}
	return c, nil
}

// Put saves (upserts) a connection. First connection saved becomes default.
func (s *Store) Put(c Connection) error {
	if c.Name == "" {
		return fmt.Errorf("connection name is required")
	}
	if c.Driver != "postgres" && c.Driver != "mysql" {
		return fmt.Errorf("unsupported driver %q: want postgres|mysql", c.Driver)
	}
	fs, err := s.load()
	if err != nil {
		return err
	}
	_, exists := fs.Connections[c.Name]
	fs.Connections[c.Name] = c
	if fs.Default == "" || (!exists && len(fs.Connections) == 1) {
		fs.Default = c.Name
	}
	return s.save(fs)
}

// Remove deletes a connection from the yaml file.
func (s *Store) Remove(name string) error {
	fs, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := fs.Connections[name]; !ok {
		return fmt.Errorf("connection %q not found", name)
	}
	delete(fs.Connections, name)
	if fs.Default == name {
		fs.Default = ""
		for k := range fs.Connections {
			fs.Default = k
			break
		}
	}
	return s.save(fs)
}

// GetDefaultName returns the default connection name ("" if none).
func (s *Store) GetDefaultName() (string, error) {
	fs, err := s.load()
	if err != nil {
		return "", err
	}
	return fs.Default, nil
}

// SetDefault marks an existing connection as default.
func (s *Store) SetDefault(name string) error {
	fs, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := fs.Connections[name]; !ok {
		return fmt.Errorf("connection %q not found", name)
	}
	fs.Default = name
	return s.save(fs)
}

// Resolve returns the named connection, or the default when name == "".
func (s *Store) Resolve(name string) (Connection, error) {
	if name != "" {
		return s.Get(name)
	}
	fs, err := s.load()
	if err != nil {
		return Connection{}, err
	}
	if fs.Default == "" {
		return Connection{}, fmt.Errorf("no default connection: pass --connection NAME")
	}
	return s.Get(fs.Default)
}
