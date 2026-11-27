package migrations_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/NikitaPash/expence-log/internal/testdb"
	"github.com/NikitaPash/expence-log/migrations"
)

// TestMigration002PreservesExistingExpenses applies 001, inserts expenses,
// applies 002, and checks the backfill and the new constraints.
func TestMigration002PreservesExistingExpenses(t *testing.T) {
	ctx := context.Background()
	db := testdb.Empty(t)

	applied, err := migrations.ApplyUpTo(ctx, db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(applied, []string{"001_create_expenses.sql"}) {
		t.Fatalf("applied %v, want only 001", applied)
	}

	type row struct {
		id    int64
		cents int64
	}
	var before []row
	for _, in := range []struct {
		date, desc string
		cents      int64
	}{
		{"2026-10-01", "Coffee beans", 1299},
		{"2026-10-15", "Train ticket", 4550},
		{"2026-10-31", "Groceries", 8734},
	} {
		var r row
		err := db.QueryRow(ctx,
			`INSERT INTO expenses (expense_date, description, amount_cents) VALUES ($1, $2, $3) RETURNING id, amount_cents`,
			in.date, in.desc, in.cents).Scan(&r.id, &r.cents)
		if err != nil {
			t.Fatal(err)
		}
		before = append(before, r)
	}

	applied, err = migrations.ApplyUpTo(ctx, db, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(applied, []string{"002_add_categories.sql"}) {
		t.Fatalf("applied %v, want only 002", applied)
	}

	rows, err := db.Query(ctx, `
		SELECT e.id, e.amount_cents, c.name
		FROM expenses e JOIN categories c ON c.id = e.category_id
		ORDER BY e.id`)
	if err != nil {
		t.Fatal(err)
	}
	var after []row
	for rows.Next() {
		var r row
		var cat string
		if err := rows.Scan(&r.id, &r.cents, &cat); err != nil {
			t.Fatal(err)
		}
		if cat != "Uncategorized" {
			t.Errorf("expense %d has category %q, want Uncategorized", r.id, cat)
		}
		after = append(after, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(before, after) {
		t.Fatalf("ids/amounts changed:\nbefore %v\nafter  %v", before, after)
	}

	// Unknown category: foreign key violation.
	_, err = db.Exec(ctx, `INSERT INTO expenses (expense_date, description, amount_cents, category_id) VALUES ('2026-10-02', 'x', 100, 999999)`)
	wantPgCode(t, err, "23503")

	// Missing category: NOT NULL violation.
	_, err = db.Exec(ctx, `INSERT INTO expenses (expense_date, description, amount_cents) VALUES ('2026-10-02', 'x', 100)`)
	wantPgCode(t, err, "23502")

	// Non-positive amount: CHECK violation (from 001).
	_, err = db.Exec(ctx, `INSERT INTO expenses (expense_date, description, amount_cents, category_id) VALUES ('2026-10-02', 'x', 0, 1)`)
	wantPgCode(t, err, "23514")

	var hasIndex bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'expenses_date_category_idx')`).Scan(&hasIndex); err != nil || !hasIndex {
		t.Errorf("month index missing (err=%v)", err)
	}

	// Re-running is a no-op.
	applied, err = migrations.ApplyAll(ctx, db)
	if err != nil || len(applied) != 0 {
		t.Fatalf("second ApplyAll applied %v, err %v", applied, err)
	}
}

func wantPgCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("want PostgreSQL error %s, got %v", code, err)
	}
	if pgErr.Code != code {
		t.Fatalf("want PostgreSQL error %s, got %s: %s", code, pgErr.Code, pgErr.Message)
	}
}
