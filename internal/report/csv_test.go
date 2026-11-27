package report_test

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/NikitaPash/expence-log/internal/expense"
	"github.com/NikitaPash/expence-log/internal/report"
	"github.com/NikitaPash/expence-log/internal/seed"
	"github.com/NikitaPash/expence-log/internal/testdb"
)

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestWriteSummaryCSVCreatesAndReplaces(t *testing.T) {
	ctx := context.Background()
	db := testdb.Migrated(t)
	if err := seed.Load(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := expense.NewStore(db)
	path := filepath.Join(t.TempDir(), "summary.csv")

	// Create: November has six categories.
	nov, err := store.MonthlySummary(ctx, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := report.WriteSummaryCSV(path, nov); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		report.Header,
		{"2026-11", "Rent", "1", "950.00"},
		{"2026-11", "Groceries", "4", "158.00"},
		{"2026-11", "Utilities", "1", "82.15"},
		{"2026-11", "Transport", "2", "68.10"},
		{"2026-11", "Dining", "2", "59.00"},
		{"2026-11", "Uncategorized", "1", "4.25"},
	}
	if got := readCSV(t, path); !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("created CSV:\n got  %v\n want %v", got, want)
	}

	// Replace: October has five, so a stale sixth line would show up.
	oct, err := store.MonthlySummary(ctx, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := report.WriteSummaryCSV(path, oct); err != nil {
		t.Fatal(err)
	}
	want = [][]string{
		report.Header,
		{"2026-10", "Rent", "1", "950.00"},
		{"2026-10", "Groceries", "2", "124.80"},
		{"2026-10", "Transport", "2", "83.40"},
		{"2026-10", "Utilities", "1", "76.90"},
		{"2026-10", "Dining", "1", "54.30"},
	}
	if got := readCSV(t, path); !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("replaced CSV:\n got  %v\n want %v", got, want)
	}

	// Empty month: header only. No temp files left behind.
	if err := report.WriteSummaryCSV(path, expense.Summary{Month: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if got := readCSV(t, path); len(got) != 1 {
		t.Fatalf("empty month CSV = %v", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("directory has %d entries, want only summary.csv", len(entries))
	}
}

func TestWriteSummaryCSVQuotesCommas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.csv")
	s := expense.Summary{Month: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		Categories: []expense.CategoryTotal{{CategoryName: `Food, "fancy"`, ExpenseCount: 1, TotalCents: 100}}}
	if err := report.WriteSummaryCSV(path, s); err != nil {
		t.Fatal(err)
	}
	if got := readCSV(t, path)[1][1]; got != `Food, "fancy"` {
		t.Fatalf("category read back as %q", got)
	}
}
