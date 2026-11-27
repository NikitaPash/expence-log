package expense_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NikitaPash/expence-log/internal/expense"
	"github.com/NikitaPash/expence-log/internal/testdb"
)

func day(s string) time.Time {
	d, err := time.Parse(expense.DateLayout, s)
	if err != nil {
		panic(err)
	}
	return d
}

func addCategory(t *testing.T, db *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(context.Background(), `INSERT INTO categories (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func mustCreate(t *testing.T, s *expense.Store, date, desc string, cents, cat int64) int64 {
	t.Helper()
	id, err := s.Create(context.Background(), expense.Expense{Date: day(date), Description: desc, AmountCents: cents, CategoryID: cat})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ids(es []expense.Expense) []int64 {
	out := make([]int64, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

func TestListFiltersByMonthAndCategory(t *testing.T) {
	ctx := context.Background()
	db := testdb.Migrated(t)
	s := expense.NewStore(db)
	food := addCategory(t, db, "Food")
	travel := addCategory(t, db, "Travel")

	mustCreate(t, s, "2026-10-31", "Last day of October", 1000, food) // excluded: before start
	novFirst := mustCreate(t, s, "2026-11-01", "First of November", 200, food)
	novMid := mustCreate(t, s, "2026-11-15", "Mid November train", 3000, travel)
	novLast := mustCreate(t, s, "2026-11-30", "Last of November", 450, food)
	mustCreate(t, s, "2026-12-01", "First of December", 999, food) // excluded: end is exclusive

	all, err := s.List(ctx, day("2026-11-20"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{novFirst, novMid, novLast}; !slices.Equal(ids(all), want) {
		t.Fatalf("November, all categories = %v, want %v", ids(all), want)
	}
	if all[0].CategoryName != "Food" || all[0].AmountCents != 200 || !all[0].Date.Equal(day("2026-11-01")) {
		t.Fatalf("row not read back intact: %+v", all[0])
	}

	foodOnly, err := s.List(ctx, day("2026-11-01"), food)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{novFirst, novLast}; !slices.Equal(ids(foodOnly), want) {
		t.Fatalf("November, Food = %v, want %v", ids(foodOnly), want)
	}

	travelDec, err := s.List(ctx, day("2026-12-01"), travel)
	if err != nil {
		t.Fatal(err)
	}
	if len(travelDec) != 0 {
		t.Fatalf("December, Travel = %v, want none", ids(travelDec))
	}
}

func TestCreateGetUpdate(t *testing.T) {
	ctx := context.Background()
	db := testdb.Migrated(t)
	s := expense.NewStore(db)
	food := addCategory(t, db, "Food")

	id := mustCreate(t, s, "2026-11-03", "Lunch", 1250, food)
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Lunch" || got.AmountCents != 1250 || got.CategoryName != "Food" {
		t.Fatalf("Get = %+v", got)
	}

	got.AmountCents, got.Date = 1399, day("2026-12-02")
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Get(ctx, id)
	if again.AmountCents != 1399 || !again.Date.Equal(day("2026-12-02")) {
		t.Fatalf("after update = %+v", again)
	}

	if _, err := s.Get(ctx, id+1000); !errors.Is(err, expense.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v", err)
	}
	if err := s.Update(ctx, expense.Expense{ID: id + 1000, Date: day("2026-11-01"), Description: "x", AmountCents: 1, CategoryID: food}); !errors.Is(err, expense.ErrNotFound) {
		t.Fatalf("Update(missing) err = %v", err)
	}
}

func TestUnknownCategoryIsValidationError(t *testing.T) {
	db := testdb.Migrated(t)
	s := expense.NewStore(db)
	_, err := s.Create(context.Background(), expense.Expense{Date: day("2026-11-03"), Description: "x", AmountCents: 100, CategoryID: 424242})
	var ve *expense.ValidationError
	if !errors.As(err, &ve) || ve.Field != "category_id" {
		t.Fatalf("want category_id ValidationError, got %v", err)
	}
}
