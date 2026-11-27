package expense

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultDatabaseURL points at the PostgreSQL service in compose.yaml
// (localhost:5432, user, password and database all "expenses").
const DefaultDatabaseURL = "postgres://expenses:expenses@localhost:5432/expenses?sslmode=disable"

// DatabaseURL returns $DATABASE_URL, or [DefaultDatabaseURL] when it is unset.
// The server, migrate, seed and export commands all use it.
func DatabaseURL() string {
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u
	}
	return DefaultDatabaseURL
}

// Connect opens a connection pool to url and pings the server, so a wrong
// URL or a stopped database fails here rather than on the first query. The
// caller must Close the pool.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
