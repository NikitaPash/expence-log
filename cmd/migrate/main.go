// Command migrate applies the SQL migrations in package migrations to
// $DATABASE_URL. It is safe to run repeatedly; already-applied versions are
// skipped.
//
//	go run ./cmd/migrate          # apply all pending migrations
//	go run ./cmd/migrate -to 1    # stop after version 1
package main

import (
	"context"
	"flag"
	"log"

	"github.com/NikitaPash/expence-log/internal/expense"
	"github.com/NikitaPash/expence-log/migrations"
)

func main() {
	to := flag.Int("to", 0, "apply migrations up to this version (0 = all)")
	flag.Parse()

	ctx := context.Background()
	db, err := expense.Connect(ctx, expense.DatabaseURL())
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	var applied []string
	if *to > 0 {
		applied, err = migrations.ApplyUpTo(ctx, db, *to)
	} else {
		applied, err = migrations.ApplyAll(ctx, db)
	}
	for _, name := range applied {
		log.Printf("applied %s", name)
	}
	if err != nil {
		log.Fatal(err)
	}
	if len(applied) == 0 {
		log.Print("database already up to date")
	}
}
