package web

import (
	"context"
	"regexp"
	"slices"
	"testing"

	"github.com/NikitaPash/expence-log/internal/seed"
)

var summaryRowRe = regexp.MustCompile(`<tr><td>([^<]+)</td><td class="num">\d+</td><td class="num">([^<]+)</td></tr>`)

func TestSummaryPage(t *testing.T) {
	a := newTestApp(t)
	if err := seed.Load(context.Background(), a.db); err != nil {
		t.Fatal(err)
	}

	_, body := a.get(t, "/summary?month=2026-11")
	mustContain(t, body, "Summary for November 2026", "Total spent <strong>1,321.50</strong>",
		`href="/expenses?month=2026-11"`)
	var got []string
	for _, m := range summaryRowRe.FindAllStringSubmatch(body, -1) {
		got = append(got, m[1]+" "+m[2])
	}
	want := []string{"Rent 950.00", "Groceries 158.00", "Utilities 82.15", "Transport 68.10", "Dining 59.00", "Uncategorized 4.25"}
	if !slices.Equal(got, want) {
		t.Fatalf("summary rows\n got  %v\n want %v", got, want)
	}

	_, body = a.get(t, "/summary?month=2026-09")
	mustContain(t, body, "Nothing spent in September 2026", `href="/expenses?month=2026-09"`)
}
