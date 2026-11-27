package expense

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by [Store.Get] and [Store.Update] when no expense
// has the given ID. Test for it with [errors.Is].
var ErrNotFound = errors.New("expense not found")

// Store reads and writes expenses and categories in PostgreSQL. It is the
// app's only data-access type and has no interface in front of it: handlers
// and commands use *Store directly, and tests run it against a real database.
//
// Every query is parameterized; no user input is concatenated into SQL. A
// Store is safe for concurrent use because it only holds a [pgxpool.Pool].
//
// The write methods expect an Expense that has passed [Input.Parse]. The
// database still checks the data itself: amount_cents > 0, non-blank
// description, NOT NULL, and the category foreign key. An unknown category is
// returned as a *ValidationError on "category_id"; other constraint
// violations come back as plain errors.
type Store struct {
	db *pgxpool.Pool
}

// NewStore returns a Store that uses db. The caller owns the pool and closes it.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Categories returns all categories sorted by name.
func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.Query(ctx, `SELECT id, name FROM categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Category, error) {
		var c Category
		return c, r.Scan(&c.ID, &c.Name)
	})
}

const selectExpense = `
	SELECT e.id, e.expense_date, e.description, e.amount_cents, e.category_id, c.name
	FROM expenses e
	JOIN categories c ON c.id = e.category_id`

func scanExpense(r pgx.CollectableRow) (Expense, error) {
	var e Expense
	err := r.Scan(&e.ID, &e.Date, &e.Description, &e.AmountCents, &e.CategoryID, &e.CategoryName)
	return e, err
}

// Get returns the expense with the given ID, including its category name,
// or [ErrNotFound].
func (s *Store) Get(ctx context.Context, id int64) (Expense, error) {
	rows, err := s.db.Query(ctx, selectExpense+` WHERE e.id = $1`, id)
	if err != nil {
		return Expense{}, err
	}
	e, err := pgx.CollectExactlyOneRow(rows, scanExpense)
	if errors.Is(err, pgx.ErrNoRows) {
		return Expense{}, ErrNotFound
	}
	return e, err
}

// List returns the expenses dated in the month containing month, oldest
// first (ties by ID). It uses [MonthBounds]: the 1st is included, the 1st of
// the next month is not. A categoryID of 0 means all categories; otherwise
// only that category's expenses are returned. An empty month gives an empty
// slice and no error.
func (s *Store) List(ctx context.Context, month time.Time, categoryID int64) ([]Expense, error) {
	start, end := MonthBounds(month)
	var cat *int64
	if categoryID != 0 {
		cat = &categoryID
	}
	rows, err := s.db.Query(ctx, selectExpense+`
		WHERE e.expense_date >= $1 AND e.expense_date < $2
		  AND ($3::bigint IS NULL OR e.category_id = $3)
		ORDER BY e.expense_date, e.id`,
		start, end, cat)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanExpense)
}

const insertExpense = `
	INSERT INTO expenses (expense_date, description, amount_cents, category_id)
	VALUES ($1, $2, $3, $4)
	RETURNING id`

// Create inserts one expense and returns the ID that PostgreSQL assigned.
// e.ID and e.CategoryName are ignored. If PostgreSQL rejects e.CategoryID
// through the foreign key, the error is a *ValidationError on "category_id",
// with the original *pgconn.PgError still in its chain.
func (s *Store) Create(ctx context.Context, e Expense) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, insertExpense, e.Date, e.Description, e.AmountCents, e.CategoryID).Scan(&id)
	return id, translate(err)
}

// Update overwrites the date, description, amount and category of the expense
// with ID e.ID. It returns [ErrNotFound] if that ID does not exist, and the
// same category *ValidationError as [Store.Create].
func (s *Store) Update(ctx context.Context, e Expense) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE expenses
		SET expense_date = $2, description = $3, amount_cents = $4, category_id = $5
		WHERE id = $1`,
		e.ID, e.Date, e.Description, e.AmountCents, e.CategoryID)
	if err != nil {
		return translate(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// BatchError is returned by [Store.CreateBatch] when inserting one of its
// rows fails. When you receive a BatchError, the transaction has already been
// rolled back and none of the batch's rows were saved, including rows before Row.
//
// Use [errors.As] to find the BatchError and the failing row, then inspect
// Err. Because BatchError unwraps to Err, errors.As also finds a
// *ValidationError (unknown category) or a *pgconn.PgError behind it.
type BatchError struct {
	// Row is the 0-based index in the slice passed to CreateBatch.
	Row int
	// Err is why that row failed.
	Err error
}

// Error returns the failing row (1-based, for people) and the cause.
func (e *BatchError) Error() string { return fmt.Sprintf("batch row %d: %v", e.Row+1, e.Err) }

// Unwrap returns Err, so errors.Is and errors.As see the underlying error.
func (e *BatchError) Unwrap() error { return e.Err }

// CreateBatch inserts the expenses in order inside one transaction, and
// returns their new IDs in the same order. The batch is all-or-nothing:
//
//   - If every insert succeeds, the transaction is committed and all rows are saved.
//   - If any insert fails, the transaction is rolled back, so rows inserted
//     earlier in the batch are discarded too. The error is a [*BatchError]
//     naming the row. For example, PostgreSQL rejecting row 1's unknown
//     category leaves the database exactly as it was before the call.
//   - If beginning or committing the transaction fails, the plain error is
//     returned without a BatchError, and nothing is saved.
//
// On error the returned IDs are nil. The web form's "Add two expenses" mode
// uses this method.
func (s *Store) CreateBatch(ctx context.Context, es []Expense) ([]int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) // no-op after a successful Commit

	ids := make([]int64, len(es))
	for i, e := range es {
		err := tx.QueryRow(ctx, insertExpense, e.Date, e.Description, e.AmountCents, e.CategoryID).Scan(&ids[i])
		if err != nil {
			return nil, &BatchError{Row: i, Err: translate(err)}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ids, nil
}

// MonthlySummary totals the expenses of the month containing month, per
// category. The work happens in PostgreSQL with one JOIN/GROUP BY/SUM query.
// Rows come back highest total first, ties by category name, and only
// categories with spending appear. Summary.TotalCents is the sum of those
// rows. An empty month gives a Summary with no categories and a zero total.
func (s *Store) MonthlySummary(ctx context.Context, month time.Time) (Summary, error) {
	start, end := MonthBounds(month)
	rows, err := s.db.Query(ctx, `
		SELECT c.id, c.name, COUNT(*) AS expense_count, SUM(e.amount_cents) AS total_cents
		FROM expenses e
		JOIN categories c ON c.id = e.category_id
		WHERE e.expense_date >= $1 AND e.expense_date < $2
		GROUP BY c.id, c.name
		ORDER BY total_cents DESC, c.name`,
		start, end)
	if err != nil {
		return Summary{}, err
	}
	cats, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (CategoryTotal, error) {
		var t CategoryTotal
		return t, r.Scan(&t.CategoryID, &t.CategoryName, &t.ExpenseCount, &t.TotalCents)
	})
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{Month: start, Categories: cats}
	for _, t := range cats {
		sum.TotalCents += t.TotalCents
	}
	return sum, nil
}

// translate turns constraint violations caused by user input into
// *ValidationError (keeping the original PostgreSQL error in the chain) and
// leaves everything else unchanged.
func translate(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "expenses_category_id_fkey" {
		return fmt.Errorf("%w: %w", &ValidationError{Field: "category_id", Reason: "does not exist"}, pgErr)
	}
	return err
}
