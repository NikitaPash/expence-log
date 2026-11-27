// Package seed loads the synthetic demo fixture in demo.sql: six categories
// and 18 expenses in October and November 2026, including expenses on both
// month boundaries. The known totals, which the tests rely on, are listed at
// the top of demo.sql: October 1,289.40 and November 1,321.50.
//
// From the command line: go run ./cmd/seed.
package seed

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed demo.sql
var demoSQL string

// Load replaces all categories and expenses with the demo fixture in one
// transaction. It truncates both tables and resets their IDs, so it deletes
// any existing data. Running it again gives the same rows and the same IDs.
// The migrations must already be applied.
func Load(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, demoSQL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
