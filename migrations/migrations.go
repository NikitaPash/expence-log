// Package migrations embeds the numbered SQL files in this directory and
// applies them in order, recording each applied version in the
// schema_migrations table.
//
//	001_create_expenses.sql  expenses(id, expense_date DATE, description, amount_cents BIGINT > 0)
//	002_add_categories.sql   categories table with "Uncategorized"; adds expenses.category_id,
//	                         backfills existing rows, then sets NOT NULL, the foreign key,
//	                         and the (expense_date, category_id) index
//
// Each migration runs in its own transaction, so a failing file leaves no
// partial changes. Applying again skips versions that are already recorded.
// A new migration is a file named NNN_description.sql. It is embedded
// automatically the next time the module is built.
//
// From the command line: go run ./cmd/migrate [-to N].
package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

// Migration is one numbered SQL file.
type Migration struct {
	// Version is the number before the first underscore in Name.
	Version int
	// Name is the file name, for example "002_add_categories.sql".
	Name string
	// SQL is the file's contents. It may hold several statements.
	SQL string
}

// All returns the embedded migrations sorted by version. It fails if a file
// name does not start with a number and an underscore.
func All() ([]Migration, error) {
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, name := range names {
		num, _, ok := strings.Cut(name, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil {
			return nil, fmt.Errorf("migration %q: name must start with a number and underscore", name)
		}
		body, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: name, SQL: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// ApplyAll applies every pending migration and returns the names it applied,
// which is empty when the database is already up to date.
func ApplyAll(ctx context.Context, db *pgxpool.Pool) ([]string, error) {
	return ApplyUpTo(ctx, db, int(^uint(0)>>1))
}

// ApplyUpTo applies pending migrations with version <= target, each in its
// own transaction, and returns the names it applied. It creates
// schema_migrations if needed. A PostgreSQL advisory lock keeps two
// concurrent runs from applying the same version. On error, earlier
// migrations stay applied and the failing one is rolled back.
//
// Tests use ApplyUpTo(ctx, db, 1) to check the upgrade path from 001 to 002.
func ApplyUpTo(ctx context.Context, db *pgxpool.Pool, target int) ([]string, error) {
	_, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	if err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}
	all, err := All()
	if err != nil {
		return nil, err
	}
	var applied []string
	for _, m := range all {
		if m.Version > target {
			break
		}
		done, err := applyOne(ctx, db, m)
		if err != nil {
			return applied, fmt.Errorf("migration %s: %w", m.Name, err)
		}
		if done {
			applied = append(applied, m.Name)
		}
	}
	return applied, nil
}

func applyOne(ctx context.Context, db *pgxpool.Pool, m Migration) (bool, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// Serialise concurrent migrators; released at commit/rollback.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(72710001)`); err != nil {
		return false, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.Version).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	// No arguments: pgx sends this via the simple protocol, so a file may hold several statements.
	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
