// Package testdb creates a throwaway PostgreSQL database per test, so
// integration tests run against real PostgreSQL without touching the app's
// own data or each other.
//
// It connects to $TEST_DATABASE_URL (default: the compose server's "postgres"
// database, as user "expenses", which may create databases). It creates a
// uniquely named database and drops it when the test ends. If PostgreSQL is
// not reachable, the test fails with a hint to run docker compose up -d.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NikitaPash/expence-log/migrations"
)

const defaultAdminURL = "postgres://expenses:expenses@localhost:5432/postgres?sslmode=disable"

var counter atomic.Int64

// Empty returns a pool on a new database with no tables, for tests that
// apply migrations themselves. The pool is closed and the database dropped
// in t.Cleanup.
func Empty(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		adminURL = defaultAdminURL
	}
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL at TEST_DATABASE_URL (start it with `docker compose up -d`): %v", err)
	}
	defer admin.Close(ctx)

	name := fmt.Sprintf("expenses_test_%d_%d_%d", os.Getpid(), time.Now().UnixNano()%1e6, counter.Add(1))
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), adminURL)
		if err != nil {
			t.Logf("drop %s: %v", name, err)
			return
		}
		defer c.Close(context.Background())
		if _, err := c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})
	return pool
}

// Migrated returns a pool on a new database with all migrations applied:
// empty tables plus the "Uncategorized" category. Cleanup works as in [Empty].
func Migrated(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool := Empty(t)
	if _, err := migrations.ApplyAll(context.Background(), pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}
