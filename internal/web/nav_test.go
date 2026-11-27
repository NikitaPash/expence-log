package web

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NikitaPash/expence-log/internal/expense"
	"github.com/NikitaPash/expence-log/internal/testdb"
)

func init() { gin.SetMode(gin.TestMode) }

type testApp struct {
	h     http.Handler
	db    *pgxpool.Pool
	store *expense.Store
	cats  map[string]int64
}

// newTestApp starts a server on a fresh migrated database with a fixed
// "today" of 2026-11-15 and a few sample rows.
func newTestApp(t *testing.T) *testApp {
	t.Helper()
	ctx := context.Background()
	db := testdb.Migrated(t)
	store := expense.NewStore(db)
	cats := map[string]int64{}
	for _, name := range []string{"Groceries", "Transport"} {
		var id int64
		if err := db.QueryRow(ctx, `INSERT INTO categories (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		cats[name] = id
	}
	for _, e := range []expense.Expense{
		{Date: date("2026-11-02"), Description: "Weekly shop", AmountCents: 6420, CategoryID: cats["Groceries"]},
		{Date: date("2026-11-05"), Description: "Bus pass", AmountCents: 4500, CategoryID: cats["Transport"]},
		{Date: date("2026-11-09"), Description: "Bakery", AmountCents: 780, CategoryID: cats["Groceries"]},
	} {
		if _, err := store.Create(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewServer(store)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) }
	return &testApp{h: s.Handler(), db: db, store: store, cats: cats}
}

func date(s string) time.Time {
	d, _ := time.Parse(expense.DateLayout, s)
	return d
}

var hrefRe = regexp.MustCompile(`href="([^"]+)"`)

// links returns every href in body, HTML-unescaped.
func links(body string) []string {
	var out []string
	for _, m := range hrefRe.FindAllStringSubmatch(body, -1) {
		out = append(out, html.UnescapeString(m[1]))
	}
	return out
}

// findLink returns the first href with the given prefix.
func findLink(t *testing.T, body, prefix string) string {
	t.Helper()
	for _, l := range links(body) {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	t.Fatalf("no link starting with %q in page; links: %v", prefix, links(body))
	return ""
}

func (a *testApp) get(t *testing.T, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	b, _ := io.ReadAll(rec.Body)
	return rec.Code, string(b)
}

func (a *testApp) post(t *testing.T, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	return rec
}

func mustContain(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("page missing %q", w)
		}
	}
}

func TestNavigationFlow(t *testing.T) {
	a := newTestApp(t)

	code, list := a.get(t, "/expenses")
	if code != http.StatusOK {
		t.Fatalf("GET /expenses = %d", code)
	}
	mustContain(t, list, "Expenses for November 2026", "Weekly shop", "64.20", "Groceries", "117.00")

	code, form := a.get(t, findLink(t, list, "/expenses/new"))
	if code != http.StatusOK {
		t.Fatalf("add form = %d", code)
	}
	mustContain(t, form, "<h1>Add expense</h1>", `name="amount"`, `name="date"`, `name="category_id"`, `name="description"`, `value="2026-11-15"`)

	code, edit := a.get(t, findLink(t, list, "/expenses/1/edit"))
	if code != http.StatusOK {
		t.Fatalf("edit form = %d", code)
	}
	mustContain(t, edit, "<h1>Edit expense</h1>", `value="Weekly shop"`, `value="64.20"`)

	code, sum := a.get(t, findLink(t, list, "/summary"))
	if code != http.StatusOK {
		t.Fatalf("summary = %d", code)
	}
	mustContain(t, sum, "Summary for November 2026")

	code, back := a.get(t, findLink(t, sum, "/expenses?month=2026-11"))
	if code != http.StatusOK {
		t.Fatalf("back link = %d", code)
	}
	mustContain(t, back, "Expenses for November 2026")
}

func TestListCategoryFilterAndEmptyMonth(t *testing.T) {
	a := newTestApp(t)
	_, body := a.get(t, "/expenses?month=2026-11&category="+itoa(a.cats["Transport"]))
	mustContain(t, body, "Bus pass", "45.00")
	if strings.Contains(body, "Weekly shop") {
		t.Error("category filter leaked a Groceries row")
	}

	_, body = a.get(t, "/expenses?month=2026-07")
	mustContain(t, body, "No expenses recorded for July 2026")

	if code, _ := a.get(t, "/expenses?month=2026-13"); code != http.StatusBadRequest {
		t.Errorf("bad month = %d, want 400", code)
	}
}
