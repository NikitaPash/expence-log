package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (a *testApp) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.db.QueryRow(context.Background(), `SELECT count(*) FROM expenses`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAddExpenseInvalidKeepsValuesAndShowsErrors(t *testing.T) {
	a := newTestApp(t)
	before := a.count(t)

	rec := a.post(t, "/expenses", url.Values{
		"month":       {"2026-11"},
		"date":        {"2026-02-30"},
		"amount":      {"12.345"},
		"category_id": {itoa(a.cats["Transport"])},
		"description": {"Taxi <home>"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body,
		"Date must be a real date in YYYY-MM-DD format",
		"Amount can have at most two decimal places",
		`value="2026-02-30"`, `value="12.345"`, `value="Taxi &lt;home&gt;"`,
		`<option value="`+itoa(a.cats["Transport"])+`" selected>Transport</option>`,
	)
	if a.count(t) != before {
		t.Fatal("invalid form wrote to the database")
	}

	for _, amount := range []string{"abc", "-3", "0", "1,50", ""} {
		rec := a.post(t, "/expenses", url.Values{"date": {"2026-11-03"}, "amount": {amount}, "category_id": {itoa(a.cats["Transport"])}, "description": {"x"}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("amount %q: status %d, want 422", amount, rec.Code)
		}
	}
}

func TestAddExpenseUnknownCategoryFromDatabase(t *testing.T) {
	a := newTestApp(t)
	rec := a.post(t, "/expenses", url.Values{"date": {"2026-11-03"}, "amount": {"5.00"}, "category_id": {"987654"}, "description": {"Mystery"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	mustContain(t, rec.Body.String(), "Category does not exist", `value="Mystery"`)
}

func TestAddExpenseRedirectsToItsMonth(t *testing.T) {
	a := newTestApp(t)
	rec := a.post(t, "/expenses", url.Values{
		"month": {"2026-11"}, "date": {"2026-12-01"}, "amount": {"19.99"},
		"category_id": {itoa(a.cats["Groceries"])}, "description": {"Advent calendar"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/expenses?month=2026-12" {
		t.Fatalf("Location = %q", loc)
	}
	_, body := a.get(t, "/expenses?month=2026-12")
	mustContain(t, body, "Advent calendar", "19.99")
}

func TestEditExpense(t *testing.T) {
	a := newTestApp(t)
	rec := a.post(t, "/expenses/2", url.Values{
		"date": {"2026-11-06"}, "amount": {"47.50"}, "category_id": {itoa(a.cats["Transport"])}, "description": {"Bus pass (monthly)"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	e, err := a.store.Get(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if e.AmountCents != 4750 || e.Description != "Bus pass (monthly)" {
		t.Fatalf("after edit = %+v", e)
	}

	rec = a.post(t, "/expenses/2", url.Values{"date": {"2026-11-06"}, "amount": {"0"}, "category_id": {itoa(a.cats["Transport"])}, "description": {"x"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid edit status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "Edit expense", "Amount must be greater than zero")

	if code, _ := a.get(t, "/expenses/9999/edit"); code != http.StatusNotFound {
		t.Errorf("missing expense edit = %d, want 404", code)
	}
}
