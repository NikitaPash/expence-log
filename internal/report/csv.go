// Package report writes the monthly category summary as CSV with
// encoding/csv. It is used by the export command (go run ./cmd/export).
package report

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"

	"github.com/NikitaPash/expence-log/internal/expense"
)

// Header is the first CSV row. Each following row holds the month (YYYY-MM),
// the category name, the number of expenses, and the total as a plain
// decimal such as "1234.50".
var Header = []string{"month", "category", "expense_count", "total"}

// WriteSummaryCSV writes s to path: the header, then one row per category,
// highest total first. An empty month produces just the header. An existing
// file at path is replaced, never appended to. The data goes to a temporary
// file in the same directory, which is then renamed over path, so readers
// never see a half-written file. On error the temporary file is removed.
func WriteSummaryCSV(path string, s expense.Summary) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".summary-*.csv.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	w := csv.NewWriter(tmp)
	month := s.Month.Format(expense.MonthLayout)
	if err := w.Write(Header); err != nil {
		return err
	}
	for _, c := range s.Categories {
		row := []string{month, c.CategoryName, strconv.FormatInt(c.ExpenseCount, 10), expense.PlainCents(c.TotalCents)}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
