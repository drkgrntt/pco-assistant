package database

import (
	"database/sql"
	"fmt"
	"strings"
)

// Open connects to the database configured by the DB_* env vars without
// running migrations - unlike New() (used by the running server, which
// always wants current schema before serving a single request), a reset
// needs a raw connection first so it can empty the schema before putting it
// back with Migrate. Every call opens its own *sql.DB (no New()-style
// singleton reuse); cmd/dbreset and cmd/dbseed are short-lived processes
// that each want exactly one connection for their one run.
func Open() (*sql.DB, error) {
	return sql.Open("pgx", connString())
}

// Reset drops every table in the configured schema, then runs every
// migration back up from scratch. This is a full wipe, not "undo
// migrations": chaining goose's own Down scripts back to zero would work
// today, but isn't a path production ever exercises and can quietly drift
// as migrations are added, so it's not a trustworthy definition of "back to
// empty." Dropping every table and replaying Up from nothing is exactly
// what a brand new install goes through, which is the only "empty" this
// needs to match.
//
// It does not DROP/CREATE the schema itself (simpler, and would need schema
// ownership the app's own DB role may not have - see dbHost et al.'s owning
// user vs. the schema's actual owner in a typical managed Postgres setup);
// dropping every table it contains, CASCADE, has the same end state for
// everything this app actually reads or writes.
func Reset(db *sql.DB) error {
	schema := dbSchema
	if schema == "" {
		schema = "public"
	}

	rows, err := db.Query(`SELECT tablename FROM pg_tables WHERE schemaname = $1`, schema)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return fmt.Errorf("list tables: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list tables: %w", err)
	}

	for _, table := range tables {
		// quoteIdent, not raw interpolation, so a table named e.g.
		// "weird""name" can't break out of the identifier - not a real risk
		// here (every table this app has is one of its own migrations), but
		// cheap to do right.
		if _, err := db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s.%s CASCADE`, quoteIdent(schema), quoteIdent(table))); err != nil {
			return fmt.Errorf("drop table %s: %w", table, err)
		}
	}

	return Migrate(db)
}

// quoteIdent wraps a Postgres identifier in double quotes, doubling any
// quote characters it contains - the standard SQL escaping for identifiers
// interpolated into a query string (see Reset, which can't use a $N bind
// parameter for a table/schema name).
func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}
