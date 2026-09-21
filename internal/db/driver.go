package db

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
)

// Column is the introspected shape of one table column.
// Constraint fields (Unsigned/Length/Precision/Scale/Values) are additive:
// zero values mean "unknown" and are omitted from the upload payload.
type Column struct {
	Name      string
	Type      string // normalized: lowercase base type, e.g. "varchar", "integer", "timestamp"
	Nullable  bool
	IsPK      bool
	Unique    bool
	FKRef     string   // "table.column" or ""
	Unsigned  bool     // MySQL unsigned integer
	Length    int      // char/varchar max length, 0 = unknown/unbounded
	Precision int      // decimal total digits, 0 = unknown
	Scale     int      // decimal fractional digits, 0 = unknown
	Values    []string // enum/set allowed values (MySQL), nil otherwise
}

// Driver abstracts postgres/mysql for connect, introspection, and inserts.
type Driver interface {
	Ping() error
	TableExists(table string) (bool, error)
	ListTables() ([]string, error)
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
