package web

import (
	"net/http"
	"net/url"
	"testing"
)

func TestAddTwoExpensesForm(t *testing.T) {
	a := newTestApp(t)

	code, body := a.get(t, "/expenses/new?month=2026-11&mode=pair")
	if code != http.StatusOK {
		t.Fatalf("pair form = %d", code)
	}
	mustContain(t, body, "<h1>Add two expenses</h1>", "<legend>Expense 1</legend>", "<legend>Expense 2</legend>", `id="amount-1"`)

	before := a.count(t)
	groceries := itoa(a.cats["Groceries"])
	rec := a.post(t, "/expenses", url.Values{
		"mode": {"pair"}, "month": {"2026-11"},
		"date":        {"2026-11-20", "2026-11-20"},
		"amount":      {"12.00", "3.50"},
		"category_id": {groceries, groceries},
		"description": {"Cheese", "Bread"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	if got := a.count(t); got != before+2 {
		t.Fatalf("count = %d, want %d", got, before+2)
	}

	// Second row passes form validation but PostgreSQL rejects its category.
	rec = a.post(t, "/expenses", url.Values{
		"mode": {"pair"}, "month": {"2026-11"},
		"date":        {"2026-11-21", "2026-11-21"},
		"amount":      {"8.00", "2.00"},
		"category_id": {groceries, "999999"},
		"description": {"Should roll back", "Bad category"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	mustContain(t, rec.Body.String(), "Nothing was saved", "Category does not exist", `value="Should roll back"`, `value="Bad category"`)
	if got := a.count(t); got != before+2 {
		t.Fatalf("count after failed batch = %d, want %d", got, before+2)
	}

	// Form validation errors in either row also save nothing.
	rec = a.post(t, "/expenses", url.Values{
		"mode": {"pair"}, "date": {"2026-11-21", "2026-11-21"}, "amount": {"8.00", "abc"},
		"category_id": {groceries, groceries}, "description": {"A", "B"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if got := a.count(t); got != before+2 {
		t.Fatalf("count after invalid batch = %d, want %d", got, before+2)
	}
}
