// Command server runs the Expense Log web app (package web) against
// $DATABASE_URL.
//
//	go run ./cmd/server                    # http://localhost:8080
//	go run ./cmd/server -addr :9000
//
// Gin runs in release mode unless $GIN_MODE is set.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/NikitaPash/expence-log/internal/expense"
	"github.com/NikitaPash/expence-log/internal/web"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "listen address")
	flag.Parse()
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := expense.Connect(context.Background(), expense.DatabaseURL())
	if err != nil {
		log.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer db.Close()

	srv, err := web.NewServer(expense.NewStore(db))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
