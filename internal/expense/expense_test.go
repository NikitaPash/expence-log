package expense

import (
	"errors"
	"testing"
	"time"
)

func TestParseCents(t *testing.T) {
	valid := map[string]int64{
		"12":         1200,
		"12.5":       1250,
		"12.50":      1250,
		"0.01":       1,
		"0.10":       10,
		"007.10":     710,
		" 3.99 ":     399,
		"1000000":    100_000_000,
		"1000000.00": 100_000_000,
		"19.99":      1999, // 19.99 is not exactly representable as float64
	}
	for in, want := range valid {
		got, err := ParseCents(in)
		if err != nil || got != want {
			t.Errorf("ParseCents(%q) = %d, %v; want %d", in, got, err, want)
		}
	}

	invalid := map[string]string{
		"":                     "is required",
		"   ":                  "is required",
		"abc":                  "must be a number like 12.50",
		"12.":                  "must be a number like 12.50",
		".50":                  "must be a number like 12.50",
		"1,000":                "must be a number like 12.50",
		"1e3":                  "must be a number like 12.50",
		"+5":                   "must be a number like 12.50",
		"12.3.4":               "must be a number like 12.50",
		"١٢":                   "must be a number like 12.50", // non-ASCII digits
		"1.234":                "can have at most two decimal places",
		"-5":                   "must be greater than zero",
		"0":                    "must be greater than zero",
		"0.00":                 "must be greater than zero",
		"1000000.01":           "must be at most 1,000,000.00",
		"99999999999999999999": "must be at most 1,000,000.00", // would overflow int64
	}
	for in, reason := range invalid {
		_, err := ParseCents(in)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("ParseCents(%q): want *ValidationError, got %v", in, err)
			continue
		}
		if ve.Field != "amount" || ve.Reason != reason {
			t.Errorf("ParseCents(%q) = {%s %q}, want {amount %q}", in, ve.Field, ve.Reason, reason)
		}
	}
}

func TestFormatCents(t *testing.T) {
	for cents, want := range map[int64]string{1: "0.01", 1250: "12.50", 123456: "1,234.56", 100_000_000: "1,000,000.00", -505: "-5.05"} {
		if got := FormatCents(cents); got != want {
			t.Errorf("FormatCents(%d) = %q, want %q", cents, got, want)
		}
	}
}

func TestParseDate(t *testing.T) {
	for _, in := range []string{"2026-11-26", "2024-02-29", "2026-12-31"} {
		if _, err := ParseDate(in); err != nil {
			t.Errorf("ParseDate(%q): %v", in, err)
		}
	}
	for _, in := range []string{"", "2026-02-30", "2025-02-29", "2026-13-01", "2026-11-31", "26-11-01", "2026/11/01", "11/26/2026", "2026-11-26T10:00:00Z"} {
		_, err := ParseDate(in)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Field != "date" {
			t.Errorf("ParseDate(%q): want date ValidationError, got %v", in, err)
		}
	}
}

func TestMonthBounds(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(DateLayout, s); return v }
	cases := []struct{ in, start, end string }{
		{"2026-11-26", "2026-11-01", "2026-12-01"},
		{"2026-12-31", "2026-12-01", "2027-01-01"},
		{"2024-02-29", "2024-02-01", "2024-03-01"},
		{"2026-01-01", "2026-01-01", "2026-02-01"},
	}
	for _, c := range cases {
		s, e := MonthBounds(d(c.in))
		if !s.Equal(d(c.start)) || !e.Equal(d(c.end)) {
			t.Errorf("MonthBounds(%s) = %s..%s, want %s..%s", c.in, s.Format(DateLayout), e.Format(DateLayout), c.start, c.end)
		}
	}
	if _, err := ParseMonth("2026-13"); err == nil {
		t.Error("ParseMonth(2026-13) should fail")
	}
}

func TestInputParseReportsEveryField(t *testing.T) {
	_, err := Input{Date: "2026-02-30", Amount: "12.345", CategoryID: "x", Description: "  "}.Parse()
	got := map[string]bool{}
	for _, e := range err.(interface{ Unwrap() []error }).Unwrap() {
		var ve *ValidationError
		if errors.As(e, &ve) {
			got[ve.Field] = true
		}
	}
	for _, f := range []string{"date", "amount", "category_id", "description"} {
		if !got[f] {
			t.Errorf("missing error for %s; got %v", f, err)
		}
	}

	e, err := Input{Date: "2026-11-30", Amount: "19.99", CategoryID: "3", Description: " Lunch "}.Parse()
	if err != nil {
		t.Fatal(err)
	}
	if e.AmountCents != 1999 || e.CategoryID != 3 || e.Description != "Lunch" || e.Date.Format(DateLayout) != "2026-11-30" {
		t.Fatalf("unexpected parse result %+v", e)
	}
}
