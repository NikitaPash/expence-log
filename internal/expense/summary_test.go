package expense_test

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/NikitaPash/expence-log/internal/expense"
	"github.com/NikitaPash/expence-log/internal/seed"
	"github.com/NikitaPash/expence-log/internal/testdb"
)

type nameTotal struct {
	Name  string
	Count int64
	Cents int64
}

func summaryRows(s expense.Summary) []nameTotal {
	out := make([]nameTotal, len(s.Categories))
	for i, c := range s.Categories {
		out[i] = nameTotal{c.CategoryName, c.ExpenseCount, c.TotalCents}
	}
	return out
}

func TestMonthlySummaryFromFixture(t *testing.T) {
	ctx := context.Background()
	db := testdb.Migrated(t)
	if err := seed.Load(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := expense.NewStore(db)

	cases := []struct {
		month string
		total int64
		rows  []nameTotal
	}{
		{"2026-11-01", 132150, []nameTotal{
			{"Rent", 1, 95000}, {"Groceries", 4, 15800}, {"Utilities", 1, 8215},
			{"Transport", 2, 6810}, {"Dining", 2, 5900}, {"Uncategorized", 1, 425},
		}},
		{"2026-10-01", 128940, []nameTotal{
			{"Rent", 1, 95000}, {"Groceries", 2, 12480}, {"Transport", 2, 8340},
			{"Utilities", 1, 7690}, {"Dining", 1, 5430},
		}},
	}
	for _, c := range cases {
		sum, err := s.MonthlySummary(ctx, day(c.month))
		if err != nil {
			t.Fatal(err)
		}
		if sum.TotalCents != c.total {
			t.Errorf("%s total = %d, want %d", c.month, sum.TotalCents, c.total)
		}
		if got := summaryRows(sum); !slices.Equal(got, c.rows) {
			t.Errorf("%s rows (highest first):\n got  %v\n want %v", c.month, got, c.rows)
		}
	}

	empty, err := s.MonthlySummary(ctx, day("2026-09-01"))
	if err != nil {
		t.Fatal(err)
	}
	if empty.TotalCents != 0 || len(empty.Categories) != 0 {
		t.Errorf("September should be empty, got %+v", empty)
	}
}

func TestSeedIsRepeatable(t *testing.T) {
	ctx := context.Background()
	db := testdb.Migrated(t)
	for range 2 {
		if err := seed.Load(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var n, maxID int64
	if err := db.QueryRow(ctx, `SELECT count(*), max(id) FROM expenses`).Scan(&n, &maxID); err != nil {
		t.Fatal(err)
	}
	if n != 18 || maxID != 18 {
		t.Fatalf("after two loads: %d rows, max id %d; want 18 and 18", n, maxID)
	}
}

// rowByRow totals a month from individual expense rows in Go.
func rowByRow(t testing.TB, s *expense.Store, month time.Time) (map[string]int64, int) {
	rows, err := s.List(context.Background(), month, 0)
	if err != nil {
		t.Fatal(err)
	}
	totals := map[string]int64{}
	for _, e := range rows {
		totals[e.CategoryName] += e.AmountCents
	}
	return totals, len(rows)
}

func grouped(s expense.Summary) map[string]int64 {
	m := map[string]int64{}
	for _, c := range s.Categories {
		m[c.CategoryName] = c.TotalCents
	}
	return m
}

// TestGroupedSummaryMatchesRowByRow checks that the SQL GROUP BY gives the
// same totals as summing individual rows, on the fixture and on a larger
// synthetic month, and logs rows returned and measured timings.
func TestGroupedSummaryMatchesRowByRow(t *testing.T) {
	ctx := context.Background()
	db := testdb.Migrated(t)
	if err := seed.Load(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := expense.NewStore(db)

	for _, m := range []string{"2026-10-01", "2026-11-01", "2026-09-01"} {
		sum, err := s.MonthlySummary(ctx, day(m))
		if err != nil {
			t.Fatal(err)
		}
		byRow, n := rowByRow(t, s, day(m))
		if !maps.Equal(grouped(sum), byRow) {
			t.Errorf("%s: grouped %v != row-by-row %v", m[:7], grouped(sum), byRow)
		}
		t.Logf("fixture %s: grouped query returned %d rows, row-by-row returned %d rows", m[:7], len(sum.Categories), n)
	}

	if testing.Short() {
		t.Skip("skipping volume measurement in -short mode")
	}
	const n = 100_000
	_, err := db.Exec(ctx, `
		INSERT INTO expenses (expense_date, description, amount_cents, category_id)
		SELECT date '2027-03-01' + (g % 31), 'Synthetic expense ' || g, 100 + (g * 37) % 10000, 1 + g % 6
		FROM generate_series(1, $1::int) AS g`, n)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `ANALYZE expenses`); err != nil {
		t.Fatal(err)
	}

	march := day("2027-03-01")
	const runs = 5
	var gTimes, rTimes []time.Duration
	var gRows, rRows int
	for range runs {
		start := time.Now()
		sum, err := s.MonthlySummary(ctx, march)
		if err != nil {
			t.Fatal(err)
		}
		gTimes = append(gTimes, time.Since(start))
		gRows = len(sum.Categories)

		start = time.Now()
		byRow, cnt := rowByRow(t, s, march)
		rTimes = append(rTimes, time.Since(start))
		rRows = cnt

		if !maps.Equal(grouped(sum), byRow) {
			t.Fatalf("volume month: grouped %v != row-by-row %v", grouped(sum), byRow)
		}
	}
	slices.Sort(gTimes)
	slices.Sort(rTimes)
	t.Logf("volume month 2027-03 (%d expenses), median of %d runs:", n, runs)
	t.Logf("  grouped SQL (JOIN/GROUP BY/SUM): %6d rows returned, %v", gRows, gTimes[runs/2].Round(time.Microsecond))
	t.Logf("  row-by-row (List + Go sum):      %6d rows returned, %v", rRows, rTimes[runs/2].Round(time.Microsecond))
}
