package db

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/kwasii1/fakerforge-db-agent/internal/config"
)

type pgDriver struct {
	db *sqlx.DB
	c  config.Connection
}

func openPostgres(c config.Connection, password string) (Driver, error) {
	port := c.Port
	if port == 0 {
		port = 5432
	}
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		c.Host, port, c.Database, c.User, password)
	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	d := &pgDriver{db: db, c: c}
	if err := d.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return d, nil
}

func (d *pgDriver) DB() *sqlx.DB { return d.db }
func (d *pgDriver) Close() error  { return d.db.Close() }

func (d *pgDriver) Ping() error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("ping %s:%d/%s: %w", d.c.Host, d.c.Port, d.c.Database, err)
	}
	return nil
}

func (d *pgDriver) TableExists(table string) (bool, error) {
	var exists bool
	q := `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`
	if err := d.db.Get(&exists, q, table); err != nil {
		return false, fmt.Errorf("table exists check: %w", err)
	}
	return exists, nil
}

func (d *pgDriver) ListTables() ([]string, error) {
	var tables []string
	q := `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY table_name`
	if err := d.db.Select(&tables, q); err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	return tables, nil
}

func (d *pgDriver) Introspect(table string) ([]Column, error) {
	type row struct {
		ColName  string `db:"col_name"`
		DataType string `db:"data_type"`
		Nullable string `db:"nullable"`
	}
	var rows []row
	q := `
SELECT c.column_name AS col_name, c.data_type AS data_type, c.is_nullable AS nullable
FROM information_schema.columns c
WHERE c.table_schema='public' AND c.table_name=$1
ORDER BY c.ordinal_position`
	if err := d.db.Select(&rows, q, table); err != nil {
		return nil, fmt.Errorf("introspect %s: %w", table, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("table %q not found", table)
	}
	pks, _ := d.pkCols(table)
	uniqs, _ := d.uniqueCols(table)
	fks, _ := d.fkRefs(table)

	out := make([]Column, 0, len(rows))
	for _, r := range rows {
		col := Column{
			Name:     r.ColName,
			Type:     normalizeType(r.DataType),
			Nullable: strings.EqualFold(r.Nullable, "YES"),
			IsPK:     pks[r.ColName],
			Unique:   uniqs[r.ColName],
			FKRef:    fks[r.ColName],
		}
		out = append(out, col)
	}
	return out, nil
}

func (d *pgDriver) pkCols(table string) (map[string]bool, error) {
	m := map[string]bool{}
	var cols []string
	q := `
SELECT kcu.column_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name=kcu.constraint_name AND tc.table_schema=kcu.table_schema
WHERE tc.table_schema='public' AND tc.table_name=$1 AND tc.constraint_type='PRIMARY KEY'`
	if err := d.db.Select(&cols, q, table); err != nil {
		return m, err
	}
	for _, c := range cols {
		m[c] = true
	}
	return m, nil
}

func (d *pgDriver) uniqueCols(table string) (map[string]bool, error) {
	m := map[string]bool{}
	var cols []string
	q := `
SELECT kcu.column_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name=kcu.constraint_name AND tc.table_schema=kcu.table_schema
WHERE tc.table_schema='public' AND tc.table_name=$1 AND tc.constraint_type='UNIQUE'`
	if err := d.db.Select(&cols, q, table); err != nil {
		return m, err
	}
	for _, c := range cols {
		m[c] = true
	}
	return m, nil
}

func (d *pgDriver) fkRefs(table string) (map[string]string, error) {
	m := map[string]string{}
	type fk struct {
		Col    string `db:"col"`
		FTab   string `db:"ftab"`
		FCol   string `db:"fcol"`
	}
	var rows []fk
	q := `
SELECT kcu.column_name AS col, ccu.table_name AS ftab, ccu.column_name AS fcol
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name=kcu.constraint_name AND tc.table_schema=kcu.table_schema
JOIN information_schema.constraint_column_usage ccu
  ON ccu.constraint_name=tc.constraint_name AND ccu.table_schema=tc.table_schema
WHERE tc.table_schema='public' AND tc.table_name=$1 AND tc.constraint_type='FOREIGN KEY'`
	if err := d.db.Select(&rows, q, table); err != nil {
		return m, err
	}
	for _, r := range rows {
		m[r.Col] = r.FTab + "." + r.FCol
	}
	return m, nil
}
