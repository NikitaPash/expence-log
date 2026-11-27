# Expense Log

A small, local, single-user web app for recording expenses and seeing how much
was spent each month by category. It runs as one Go process, using Gin,
PostgreSQL through pgx, `html/template`, and one plain CSS file. There is no
JavaScript. All data is synthetic demo data.

## Pages

| URL | Page |
|---|---|
| `/expenses?month=2026-11&category=3` | Expenses for a month (defaults to the current month), with an optional category filter. Links to add, edit, the summary, and the previous/next month. |
| `/expenses/new?month=2026-11` | Add form (date, amount, category, description). Field errors appear next to each input, and submitted values are kept. |
| `/expenses/new?month=2026-11&mode=pair` | The same form with two expenses, saved in one transaction (both or neither). |
| `/expenses/{id}/edit` | Edit form, which reuses the same template. |
| `/summary?month=2026-11` | Total spent, and category totals from SQL, highest first. Has an empty state and a link back to that month's expenses. |

A successful save redirects (303) to the expense list for the saved expense's month.

## Run it

You need Go 1.26+ and Docker. Commands run from the repository root and work
in PowerShell and in bash.

```sh
# 0. Get the code (Go module: github.com/NikitaPash/expence-log)
git clone https://github.com/NikitaPash/expence-log.git
cd expence-log

# 1. Start PostgreSQL 17 (localhost:5432, user/password/db: expenses)
docker compose up -d --wait

# 2. Apply the migrations (records them in schema_migrations; safe to re-run)
go run ./cmd/migrate

# 3. Load the synthetic demo data (replaces all expenses and categories; repeatable)
go run ./cmd/seed

# 4. Run the app, then open http://localhost:8080/expenses?month=2026-11
go run ./cmd/server            # -addr localhost:8080 by default
```

The demo data covers October and November 2026, with expenses on both
month boundaries. Known totals: October 1,289.40 (7 expenses) and
November 1,321.50 (11 expenses). The figures per category are listed in
`internal/seed/demo.sql`.

### Configuration

| Variable | Default | Used by |
|---|---|---|
| `DATABASE_URL` | `postgres://expenses:expenses@localhost:5432/expenses?sslmode=disable` | `server`, `migrate`, `seed`, `export` |
| `TEST_DATABASE_URL` | `postgres://expenses:expenses@localhost:5432/postgres?sslmode=disable` | tests (the user must be able to `CREATE DATABASE`) |
| `GIN_MODE` | `release` | `server` |

## Migrations

- `migrations/001_create_expenses.sql` creates `expenses`. It has an identity `id`, `expense_date DATE`, `description`, and `amount_cents BIGINT CHECK (> 0)`.
- `migrations/002_add_categories.sql` creates `categories` and inserts `Uncategorized`. It adds `expenses.category_id` and backfills existing rows to `Uncategorized`. After that, it sets the column `NOT NULL`, adds the foreign key, and adds the index `(expense_date, category_id)` for the month list.

To apply them to a fresh local database:

```sh
docker compose up -d --wait
go run ./cmd/migrate             # applies 001 then 002, each in its own transaction
go run ./cmd/migrate -to 1       # (optional) stop after a given version
```

To start over from nothing, run `docker compose down -v` (this deletes the
database volume), then `docker compose up -d --wait`.

The test `migrations/TestMigration002PreservesExistingExpenses` is the
repeatable check for the upgrade path. It applies 001, inserts expenses,
applies 002, and verifies the IDs, amounts, the backfill, and the constraints.

## Tests

PostgreSQL must be running (`docker compose up -d --wait`). Each database test
creates a temporary database and drops it afterwards, so tests never touch your
`expenses` data.

```sh
go test ./...
go test ./internal/expense -run TestGroupedSummaryMatchesRowByRow -v   # prints rows/timings
go test -short ./...                                                    # skips the 100k-row measurement
```

What is covered: amount and date parsing, month bounds, migrations, the
month/category filters against real PostgreSQL, summary totals/order/empty
month, grouped vs row-by-row totals, the batch commit and its rollback, CSV
create/replace, and HTTP navigation, forms, errors and redirects.

## CSV export

```sh
go run ./cmd/export -month 2026-11 -out summary-2026-11.csv
```

This writes `month,category,expense_count,total` rows, highest total first,
using `encoding/csv`. Running it again with the same `-out` replaces the file.
The command writes a temporary file and renames it over the old one.

## Backup and restore (maintenance procedure)

These commands run inside the database container, so you don't need PostgreSQL
client tools on the host. In Git Bash, prefix each command with
`MSYS_NO_PATHCONV=1`, or Git Bash rewrites the `/tmp/...` paths.

```sh
# 1. Back up to a custom-format archive and copy it to the host
docker compose exec -T db pg_dump -U expenses -d expenses --format=custom --file=/tmp/expenses.dump
docker compose cp db:/tmp/expenses.dump ./expenses.dump

# 2. Create a separate, empty database
docker compose exec -T db dropdb -U expenses --if-exists expenses_restore
docker compose exec -T db createdb -U expenses expenses_restore

# 3. Restore into it
docker compose exec -T db pg_restore -U expenses -d expenses_restore --exit-on-error --no-owner /tmp/expenses.dump

# 4. Compare counts and monthly totals; the two outputs must be identical
docker compose cp scripts/compare.sql db:/tmp/compare.sql
docker compose exec -T db psql -U expenses -d expenses         -f /tmp/compare.sql
docker compose exec -T db psql -U expenses -d expenses_restore -f /tmp/compare.sql
```

Actual results on the seeded database (2026-11-26, PostgreSQL 17.12):

- The archive was 6,567 bytes. `expenses_restore` had 0 tables before the restore, and `pg_restore` exited with code 0.
- Both databases returned identical output: 18 expenses, 6 categories, 2 migrations; October: 7 expenses, 128940 cents; November: 11 expenses, 132150 cents. The per-category totals were identical too.
- A trial insert in the restored database received ID 19, so the identity sequence was restored. `cmd/export` against the restored database produced the same CSV.

The full output is in [docs/VALIDATION.md](docs/VALIDATION.md#backup-and-restore-step-8).

## Layout check

With the app running on the seeded data, `.\scripts\measure-layout.ps1`
(PowerShell, headless Chrome) prints measurements for a 375px viewport:
horizontal overflow, the long description's cell width, and the gap between
the panel and its heading. The before/after numbers for the overflow fix
(`overflow-wrap: anywhere` plus a stacked row layout on phones) and the
spacing fix (`display: flow-root` on the panel) are in
[docs/VALIDATION.md](docs/VALIDATION.md#layout-step-5).

## API documentation (go doc)

All exported types and functions have doc comments, so the standard `go doc`
tool can show them. To write the docs for all of the module's own packages to
one file:

```sh
make docs                    # writes build/docs/go-api.txt
```

**Output:** `build/docs/go-api.txt` holds the `go doc -all` output of each
package from `go list ./...`, one after another. Each section starts with the
`package … // import "github.com/NikitaPash/expence-log/…"` line. The target creates `build/docs` if
needed and overwrites the file on each run. The file is generated, so don't
edit it.

**Regenerating:** the file is not updated automatically. Run `make docs`
again after changing code or doc comments. To write it somewhere else, use
`make docs DOCS_OUT=path/to/api.txt`. The recipe uses POSIX `sh` syntax, so
run it on Linux or macOS (or from Git Bash/WSL on Windows).

To read a single package or symbol in the terminal:

```sh
go doc -all ./internal/expense
go doc ./internal/expense Store.CreateBatch
go doc ./internal/expense ValidationError
```

Start with the package comment of `internal/expense`, which covers money and
dates, validation errors, the PostgreSQL store, and batch inserts. Then read
`ValidationError`, `Store`, `Store.CreateBatch` and `BatchError`.

## Code layout

```
cmd/server     web app                 cmd/migrate   apply migrations
cmd/seed       load demo data          cmd/export    monthly summary -> CSV
migrations/    001, 002 SQL + small embedded runner
internal/expense   model, ValidationError, money/date/month parsing, PostgreSQL Store
internal/web       Gin routes and handlers, templates/, static/style.css
internal/report    CSV writer
internal/seed      demo.sql fixture
internal/testdb    throwaway database per test
Makefile           `make docs`
scripts/           compare.sql (backup check), measure-layout.ps1
docs/VALIDATION.md recorded test, measurement and database results
build/docs/        generated go-api.txt (safe to delete)
```

Here is the path of one expense through the code. The form posts to
`POST /expenses`, handled by `web.Server.create`. `expense.Input.Parse` checks
each field and returns a `*ValidationError` for each invalid one. The handler
finds these with `errors.As` and re-renders the form with status 422. A valid
expense goes to `expense.Store.Create`, which runs a parameterized `INSERT`. An
unknown category comes back from PostgreSQL as a foreign-key violation, and
`Create` returns it as a `category_id` `ValidationError`. On success the
handler redirects (303) to `/expenses?month=YYYY-MM`. `Store.MonthlySummary`
then includes the new row in its `GROUP BY` totals.
