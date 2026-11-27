// Command export writes one month's category totals from $DATABASE_URL to a
// CSV file. Running it again with the same -out path refreshes the file.
//
//	go run ./cmd/export -month 2026-11 -out summary-2026-11.csv
//
// Flags:
//
//	-month YYYY-MM  month to export (required)
//	-out PATH       CSV file to create or replace (required)
//
// The totals come from expense.Store.MonthlySummary. The file format is
// described in package report.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/NikitaPash/expence-log/internal/expense"
	"github.com/NikitaPash/expence-log/internal/report"
)

func main() {
	monthFlag := flag.String("month", "", "month to export, YYYY-MM (required)")
	out := flag.String("out", "", "CSV file to write (required)")
	flag.Parse()
	if *monthFlag == "" || *out == "" {
		flag.Usage()
		log.Fatal("both -month and -out are required")
	}
	month, err := expense.ParseMonth(*monthFlag)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	db, err := expense.Connect(ctx, expense.DatabaseURL())
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	sum, err := expense.NewStore(db).MonthlySummary(ctx, month)
	if err != nil {
		log.Fatal(err)
	}
	if err := report.WriteSummaryCSV(*out, sum); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d categories, total %s, to %s", len(sum.Categories), expense.FormatCents(sum.TotalCents), *out)
}
