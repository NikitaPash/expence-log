// Package expense holds the expense model, input validation, money and month
// helpers, and the PostgreSQL store. The other packages build on it: the web
// handlers, the CSV report, and the commands.
//
// # Money and dates
//
// Amounts are int64 cents everywhere. [ParseCents] turns form input such as
// "12.5" into 1250 using integer arithmetic only, never float64.
// [FormatCents] and [PlainCents] turn cents back into text. Dates are
// calendar days stored as PostgreSQL DATE and held in Go as a [time.Time]
// at UTC midnight. A month is identified by its first day. [MonthBounds]
// returns that day (inclusive) and the first day of the next month
// (exclusive), and every month query filters on that half-open range.
//
// # Validation errors
//
// Problems with user input are reported as a [*ValidationError], which names
// the offending field and gives a human-readable reason.
// [Input.Parse] checks all four form fields and joins one *ValidationError
// per invalid field with [errors.Join]. [Store.Create], [Store.Update] and
// [Store.CreateBatch] also return a *ValidationError, on the "category_id"
// field, when PostgreSQL rejects an unknown category through its foreign key.
// Callers find these errors with [errors.As]:
//
//	var ve *expense.ValidationError
//	if errors.As(err, &ve) {
//		showNextToInput(ve.Field, ve.Reason)
//	}
//
// Any other error (lost connection, SQL bug) is not a *ValidationError and
// should be treated as a server error.
//
// # PostgreSQL store
//
// [Store] is the only data-access type. It wraps a [pgxpool.Pool], sends
// parameterized SQL only, and is safe for concurrent use. Reads join
// categories so that [Expense.CategoryName] is filled. [Store.MonthlySummary]
// computes category totals in PostgreSQL with JOIN, GROUP BY, SUM and
// ORDER BY. A missing row is reported as [ErrNotFound].
//
// # Batch inserts
//
// [Store.CreateBatch] inserts several expenses in one transaction: either all
// rows are committed or none are. If a row fails, the transaction is rolled
// back. That includes rows that were already inserted, and also covers an
// unknown category rejected by PostgreSQL. The error is a [*BatchError]
// carrying the 0-based row index. Its chain still contains the underlying
// error, so errors.As finds both the *BatchError and, for an unknown category,
// the *ValidationError and the [*pgconn.PgError].
package expense

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Expense is one recorded expense, a row of the expenses table.
type Expense struct {
	// ID is assigned by PostgreSQL. It is zero for an expense that has not
	// been saved yet.
	ID int64
	// Date is the calendar day of the expense, at UTC midnight.
	Date time.Time
	// Description is trimmed, non-empty text of at most MaxDescriptionLen characters.
	Description string
	// AmountCents is the amount in integer cents, from 1 to MaxAmountCents.
	AmountCents int64
	// CategoryID references categories.id. PostgreSQL enforces it with a foreign key.
	CategoryID int64
	// CategoryName is filled by Store reads. Writes ignore it.
	CategoryName string
}

// Category is a spending category. Migration 002 creates "Uncategorized";
// the demo seed adds the others.
type Category struct {
	ID   int64
	Name string
}

// CategoryTotal is one row of the monthly summary: how many expenses a
// category had in the month and their total.
type CategoryTotal struct {
	CategoryID   int64
	CategoryName string
	ExpenseCount int64
	TotalCents   int64
}

// Summary is one month's spending, as returned by [Store.MonthlySummary].
type Summary struct {
	// Month is the first day of the summarised month.
	Month time.Time
	// TotalCents is the sum of all Categories' totals.
	TotalCents int64
	// Categories lists only categories with spending in the month, sorted
	// highest total first (ties by name). It is empty for a month with no expenses.
	Categories []CategoryTotal
}

// ValidationError reports that one input field is invalid. It is the only
// error type that the web handlers show to the user next to a form field.
//
// Field is the form field name: "date", "amount", "category_id" or
// "description", or "month" from [ParseMonth]. Reason is a lower-case phrase
// that reads naturally after the field's label, for example
// {Field: "amount", Reason: "can have at most two decimal places"}.
//
// The parse functions return a ValidationError directly. [Input.Parse] joins
// several of them, and the [Store] write methods wrap one around PostgreSQL's
// foreign-key error. Find it with [errors.As] in all cases.
type ValidationError struct {
	Field  string
	Reason string
}

// Error returns the field and reason, for example "amount must be greater than zero".
func (e *ValidationError) Error() string {
	return e.Field + " " + e.Reason
}

const (
	// MaxAmountCents caps a single expense at 1,000,000.00.
	MaxAmountCents = 100_000_000
	// MaxDescriptionLen is the longest description accepted, in characters.
	MaxDescriptionLen = 500

	// DateLayout is the time layout for dates in forms and URLs (YYYY-MM-DD).
	DateLayout = "2006-01-02"
	// MonthLayout is the time layout for the month query parameter (YYYY-MM).
	MonthLayout = "2006-01"
)

// ParseCents converts a decimal string such as "12.5" or "1234.56" into
// integer cents without going through floating point. Surrounding spaces are
// ignored. Only ASCII digits with an optional '.' and at most two decimals are
// accepted, so signs, exponents and thousands separators are rejected. The
// result must be between 0.01 and 1,000,000.00. Any failure is a
// *ValidationError on the "amount" field.
func ParseCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, &ValidationError{Field: "amount", Reason: "is required"}
	}
	if strings.HasPrefix(s, "-") {
		return 0, &ValidationError{Field: "amount", Reason: "must be greater than zero"}
	}
	whole, frac, hasDot := strings.Cut(s, ".")
	if !allDigits(whole) || (hasDot && !allDigits(frac)) {
		return 0, &ValidationError{Field: "amount", Reason: "must be a number like 12.50"}
	}
	if len(frac) > 2 {
		return 0, &ValidationError{Field: "amount", Reason: "can have at most two decimal places"}
	}
	whole = strings.TrimLeft(whole, "0")
	if len(whole) > 7 { // anything with 8+ integer digits is over the cap
		return 0, &ValidationError{Field: "amount", Reason: "must be at most 1,000,000.00"}
	}
	var cents int64
	for _, r := range whole + (frac + "00")[:2] {
		cents = cents*10 + int64(r-'0')
	}
	if cents == 0 {
		return 0, &ValidationError{Field: "amount", Reason: "must be greater than zero"}
	}
	if cents > MaxAmountCents {
		return 0, &ValidationError{Field: "amount", Reason: "must be at most 1,000,000.00"}
	}
	return cents, nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// FormatCents renders cents for display, e.g. 123456 -> "1,234.56".
func FormatCents(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	whole := strconv.FormatInt(cents/100, 10)
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("%s%s.%02d", sign, b.String(), cents%100)
}

// PlainCents renders cents the way the form accepts them, e.g. "1234.56".
func PlainCents(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// ParseDate parses a YYYY-MM-DD calendar date into UTC midnight. Impossible
// dates such as 2026-02-30 are rejected. Any failure is a *ValidationError on
// the "date" field.
func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, &ValidationError{Field: "date", Reason: "is required"}
	}
	d, err := time.Parse(DateLayout, s)
	if err != nil {
		return time.Time{}, &ValidationError{Field: "date", Reason: "must be a real date in YYYY-MM-DD format"}
	}
	return d, nil
}

// ParseMonth parses a YYYY-MM month and returns its first day. A failure is
// a *ValidationError on the "month" field.
func ParseMonth(s string) (time.Time, error) {
	m, err := time.Parse(MonthLayout, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, &ValidationError{Field: "month", Reason: "must be in YYYY-MM format"}
	}
	return m, nil
}

// MonthOf returns the first day of the month containing t.
func MonthOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// MonthBounds returns the first day of the month containing t (inclusive)
// and the first day of the following month (exclusive). Store queries filter
// with expense_date >= start AND expense_date < end.
func MonthBounds(t time.Time) (start, end time.Time) {
	start = MonthOf(t)
	return start, start.AddDate(0, 1, 0)
}

// Input is the raw, unvalidated form input for one expense. The fields hold
// the submitted strings unchanged, so a form that fails validation can be
// shown again with exactly what the user typed. Call [Input.Parse] to turn
// it into an [Expense].
type Input struct {
	Date        string
	Amount      string
	CategoryID  string
	Description string
}

// InputFrom fills an Input from a stored expense, for the edit form.
func InputFrom(e Expense) Input {
	return Input{
		Date:        e.Date.Format(DateLayout),
		Amount:      PlainCents(e.AmountCents),
		CategoryID:  strconv.FormatInt(e.CategoryID, 10),
		Description: e.Description,
	}
}

// Parse validates every field and returns the expense, which has no ID yet.
// It does not stop at the first problem. On failure the error is an
// [errors.Join] of one *ValidationError per invalid field, so a form can mark
// all of them at once. Iterate over the fields with
// err.(interface{ Unwrap() []error }), or use [errors.As] to get the first one.
// Parse does not check that the category exists; PostgreSQL does that when
// the expense is saved.
func (in Input) Parse() (Expense, error) {
	var e Expense
	var errs []error

	if d, err := ParseDate(in.Date); err != nil {
		errs = append(errs, err)
	} else {
		e.Date = d
	}
	if c, err := ParseCents(in.Amount); err != nil {
		errs = append(errs, err)
	} else {
		e.AmountCents = c
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(in.CategoryID), 10, 64); err != nil || id <= 0 {
		errs = append(errs, &ValidationError{Field: "category_id", Reason: "must be chosen from the list"})
	} else {
		e.CategoryID = id
	}
	desc := strings.TrimSpace(in.Description)
	switch {
	case desc == "":
		errs = append(errs, &ValidationError{Field: "description", Reason: "is required"})
	case utf8.RuneCountInString(desc) > MaxDescriptionLen:
		errs = append(errs, &ValidationError{Field: "description", Reason: fmt.Sprintf("must be at most %d characters", MaxDescriptionLen)})
	default:
		e.Description = desc
	}
	return e, errors.Join(errs...)
}
