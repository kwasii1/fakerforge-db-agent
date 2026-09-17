package db

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "github.com/go-sql-driver/mysql"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
)

type myDriver struct {
	db *sqlx.DB
	c  config.Connection
}

func openMySQL(c config.Connection, password string) (Driver, error) {
	port := c.Port
	if port == 0 {
		port = 3306
	}
	// parseTime so DATETIME scans sanely; multiStatements off.
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4",
		c.User, password, c.Host, port, c.Database)
	db, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	d := &myDriver{db: db, c: c}
	if err := d.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return d, nil
}

func (d *myDriver) DB() *sqlx.DB { return d.db }
func (d *myDriver) Close() error  { return d.db.Close() }

func (d *myDriver) Ping() error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("ping %s:%d/%s: %w", d.c.Host, d.c.Port, d.c.Database, err)
	}
	return nil
}

func (d *myDriver) TableExists(table string) (bool, error) {
	var n int
	q := `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?`
	if err := d.db.Get(&n, q, table); err != nil {
		return false, fmt.Errorf("table exists check: %w", err)
	}
	return n > 0, nil
}

func (d *myDriver) Introspect(table string) ([]Column, error) {
	type row struct {
		ColName  string `db:"col_name"`
		DataType string `db:"data_type"`
		Nullable string `db:"nullable"`
		ColKey   string `db:"col_key"`
	}
	var rows []row
	q := `
SELECT column_name AS col_name, data_type AS data_type, is_nullable AS nullable, column_key AS col_key
FROM information_schema.columns
WHERE table_schema=DATABASE() AND table_name=?
ORDER BY ordinal_position`
	if err := d.db.Select(&rows, q, table); err != nil {
		return nil, fmt.Errorf("introspect %s: %w", table, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("table %q not found", table)
	}
	fks, _ := d.fkRefs(table)
	out := make([]Column, 0, len(rows))
	for _, r := range rows {
		out = append(out, Column{
			Name:     r.ColName,
			Type:     normalizeType(r.DataType),
			Nullable: strings.EqualFold(r.Nullable, "YES"),
			IsPK:     r.ColKey == "PRI",
			Unique:   r.ColKey == "UNI",
			FKRef:    fks[r.ColName],
		})
	}
	return out, nil
}

func (d *myDriver) fkRefs(table string) (map[string]string, error) {
	m := map[string]string{}
	type fk struct {
		Col  string `db:"col"`
		FTab string `db:"ftab"`
		FCol string `db:"fcol"`
	}
	var rows []fk
	q := `
SELECT kcu.column_name AS col, kcu.referenced_table_name AS ftab, kcu.referenced_column_name AS fcol
FROM information_schema.key_column_usage kcu
WHERE kcu.table_schema=DATABASE() AND kcu.table_name=? AND kcu.referenced_table_name IS NOT NULL`
	if err := d.db.Select(&rows, q, table); err != nil {
		return m, err
	}
	for _, r := range rows {
		m[r.Col] = r.FTab + "." + r.FCol
	}
	return m, nil
}
