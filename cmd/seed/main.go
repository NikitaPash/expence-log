// Command seed replaces all expenses and categories in $DATABASE_URL with
// the synthetic demo data from package seed. Existing data is deleted. Run
// cmd/migrate first.
//
//	go run ./cmd/seed
package main

import (
	"context"
	"log"

	"github.com/NikitaPash/expence-log/internal/expense"
	"github.com/NikitaPash/expence-log/internal/seed"
)

func main() {
	ctx := context.Background()
	db, err := expense.Connect(ctx, expense.DatabaseURL())
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	if err := seed.Load(ctx, db); err != nil {
		log.Fatalf("load demo data: %v", err)
	}
	var cats, rows int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM categories), (SELECT count(*) FROM expenses)`).Scan(&cats, &rows); err != nil {
		log.Fatal(err)
	}
	log.Printf("loaded demo data: %d categories, %d expenses (October and November 2026)", cats, rows)
}
