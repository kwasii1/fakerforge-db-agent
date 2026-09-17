package insert

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Batch inserts rows in one transaction using parameterized queries only.
// columns is the ordered column list; rows hold values keyed by column name.
// Returns rows inserted or the first error (caller rolls back per batch).
func Batch(db *sqlx.DB, driver, table string, columns []string, rows []map[string]any) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	if len(columns) == 0 {
		// derive sorted columns from first row for determinism
		set := map[string]bool{}
		for _, r := range rows {
			for k := range r {
				set[k] = true
			}
		}
		for k := range set {
			columns = append(columns, k)
		}
		sort.Strings(columns)
	}
	ph := placeholder(driver)
	quoted := make([]string, len(columns))
	for i, c := range columns {
		quoted[i] = quoteIdent(driver, c)
	}
	// INSERT INTO t (a,b) VALUES (...),(...) — one statement per batch.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("INSERT INTO %s (%s) VALUES ",
		quoteIdent(driver, table), strings.Join(quoted, ", ")))
	args := make([]any, 0, len(rows)*len(columns))
	for i, r := range rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(")
		for j, c := range columns {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(ph(i*len(columns) + j + 1))
			args = append(args, r[c])
		}
		sb.WriteString(")")
	}
	tx, err := db.Beginx()
	if err != nil {
		return 0, fmt.Errorf("begin txn: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(sb.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("batch insert (%d rows): %w", len(rows), err)
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	if n == 0 {
		return len(rows), nil
	}
	return int(n), nil
}

func placeholder(driver string) func(int) string {
	if driver == "postgres" {
		return func(i int) string { return fmt.Sprintf("$%d", i) }
	}
	return func(int) string { return "?" }
}

func quoteIdent(driver, ident string) string {
	ident = strings.ReplaceAll(ident, `"`, "")
	if driver == "mysql" {
		ident = strings.ReplaceAll(ident, "`", "")
		return "`" + ident + "`"
	}
	return `"` + ident + `"`
}
