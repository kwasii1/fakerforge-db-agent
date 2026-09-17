package db

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
)

// Column is the introspected shape of one table column.
type Column struct {
	Name     string
	Type     string // normalized: lowercase base type, e.g. "varchar", "integer", "timestamp"
	Nullable bool
	IsPK     bool
	Unique   bool
	FKRef    string // "table.column" or ""
}

// Driver abstracts postgres/mysql for connect, introspection, and inserts.
type Driver interface {
	Ping() error
	TableExists(table string) (bool, error)
	Introspect(table string) ([]Column, error)
	DB() *sqlx.DB
	Close() error
}

// Open connects to the given saved connection (password resolved separately).
func Open(c config.Connection, password string) (Driver, error) {
	switch c.Driver {
	case "postgres":
		return openPostgres(c, password)
	case "mysql":
		return openMySQL(c, password)
	default:
		return nil, fmt.Errorf("unsupported driver %q: want postgres|mysql", c.Driver)
	}
}

// DefaultPort returns the conventional port for a driver.
func DefaultPort(driver string) int {
	if driver == "mysql" {
		return 3306
	}
	return 5432
}
