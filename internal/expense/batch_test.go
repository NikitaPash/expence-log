package expense_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/NikitaPash/expence-log/internal/expense"
	"github.com/NikitaPash/expence-log/internal/testdb"
)

func TestCreateBatchCommitsBothRows(t *testing.T) {
	ctx := context.Background()
	db := testdb.Migrated(t)
	s := expense.NewStore(db)
	food := addCategory(t, db, "Food")

	ids, err := s.CreateBatch(ctx, []expense.Expense{
		{Date: day("2026-11-10"), Description: "Cinema tickets", AmountCents: 2400, CategoryID: food},
		{Date: day("2026-11-10"), Description: "Popcorn", AmountCents: 750, CategoryID: food},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v", ids)
	}

	// Query PostgreSQL directly, not through the store.
	var n, total int64
	if err := db.QueryRow(ctx, `SELECT count(*), sum(amount_cents) FROM expenses WHERE id = ANY($1)`, ids).Scan(&n, &total); err != nil {
		t.Fatal(err)
	}
	if n != 2 || total != 3150 {
		t.Fatalf("committed rows: count %d, total %d; want 2 and 3150", n, total)
	}
}

func TestCreateBatchRollsBackFirstRowWhenSecondFails(t *testing.T) {
	ctx := context.Background()
	db := testdb.Migrated(t)
	s := expense.NewStore(db)
	food := addCategory(t, db, "Food")

	_, err := s.CreateBatch(ctx, []expense.Expense{
		{Date: day("2026-11-11"), Description: "Valid first row", AmountCents: 1000, CategoryID: food},
		{Date: day("2026-11-11"), Description: "Unknown category", AmountCents: 500, CategoryID: 999999},
	})

	// The second insert was rejected by PostgreSQL's foreign key...
	var be *expense.BatchError
	if !errors.As(err, &be) || be.Row != 1 {
		t.Fatalf("want BatchError on row 1, got %v", err)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("want PostgreSQL foreign_key_violation (23503) in chain, got %v", err)
	}
	var ve *expense.ValidationError
	if !errors.As(err, &ve) || ve.Field != "category_id" {
		t.Fatalf("want category_id ValidationError in chain, got %v", err)
	}

	// ...and the first insert, which had succeeded inside the transaction, is gone.
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM expenses WHERE description = 'Valid first row'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("first row was not rolled back: %d rows found", n)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM expenses`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("expenses table has %d rows (err %v), want 0", n, err)
	}
}
