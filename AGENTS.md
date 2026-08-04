# Repository Guidelines

## Project Overview

A batteries-included Go web application template. Server-side rendered HTML via [templ](https://templ.guide), type-safe PostgreSQL access via [sqlc](https://sqlc.dev), htmx for progressive interactivity, and Tailwind CSS for styling. Ships with password auth, sessions, DB migrations, live reload, and Docker. Module: `github.com/embiem/go-web-template`.

## Architecture & Data Flow

```mermaid
graph LR
  A[Browser + htmx] -->|HTTP :3000| B[chi router]
  B --> C[SessionManager.LoadAndSave]
  C --> D[handler.Make wrapper]
  D --> E[handler funcs]
  E --> F[templ components .Render]
  E --> G[db.Queries sqlc]
  G --> H[(PostgreSQL / pgxpool)]
  E --> I[scs session store / pgxstore]
```

- **Entry (`main.go`)**: `initialize()` loads `.env` (godotenv), runs `db.Init()` and `handler.InitSession()`. Builds `chi.NewRouter()`, installs `middleware.Logger` + `middleware.Recoverer`, serves `/healthz` and static `/public`, registers routes, then serves `:3000` wrapped in `handler.SessionManager.LoadAndSave(router)`. `teardown()` runs on `os.Interrupt`/`SIGTERM`.
- **Request flow**: chi route → `handler.Make(fn)` adapts an `error`-returning handler to `http.HandlerFunc` and logs failures via `slog.Error` → handler renders a templ component with `.Render(ctx, w)`, redirects, or returns an htmx partial.
- **Auth/session**: `scs/v2` session backed by `pgxstore` (24h lifetime). Logged-in `data.User` stored under `SessionKeyUser` (gob-registered). Passwords hashed/verified with `bcrypt`. Signup/login create user + account inside a pgx transaction (`db.Pool.Begin` → `db.Queries.WithTx(tx)` → `tx.Commit`).
- **Data layer**: `db/query.sql` + `db/migrations/*.sql` → `sqlc generate` → `data/` package (`Queries`, `models.go`). Runtime uses `pgxpool`; migrations applied on startup from `file://db/migrations` via golang-migrate.

## Key Directories

- `handler/` — HTTP handlers. `shared.go` (`Make`, `AccountProvider`, `SessionKey`), `session.go` (`InitSession`), `index.go`, `auth.go`.
- `view/` — templ views. Edit `*.templ`; `*_templ.go` are **generated** (never hand-edit). `layout.templ` (base shell), `components.templ` (`Button`), `index.templ`, `auth.templ`.
- `data/` — **generated** sqlc output (`db.go`, `models.go`, `query.sql.go`). Do not edit.
- `db/` — `init.go` (pool + migrate bootstrap), `query.sql` (sqlc source), `migrations/`.
- `util/` — standalone helpers: `markdown.go` (gomarkdown rendering), `logwriter.go`.
- `public/` — checked-in static assets + generated `output.css`.

## Development Commands

```bash
cp .env.example .env          # set DATABASE_URL
docker compose up -d          # Postgres 16 (:5432) + Adminer (:8080)
air                           # live-reload dev loop (see .air.toml)
go test ./...                 # tests (none exist yet)
```

Air build chain (`.air.toml`), run manually if not using air:

```bash
templ generate                                          # after editing *.templ
sqlc generate                                           # after editing db/query.sql
tailwindcss -i input.css -o public/output.css           # rebuild CSS
go build -tags dev -o ./tmp/main .
```

Migrations (golang-migrate, wrap SQL in `BEGIN`/`COMMIT`):

```bash
migrate create -ext sql -dir db/migrations -seq <name>
migrate -database ${POSTGRESQL_URL} -path db/migrations up   # down | force <VERSION>
```

## Code Conventions & Common Patterns

- **Handler signature**: `func(w http.ResponseWriter, r *http.Request) error`, wired via `handler.Make(...)`. Return errors instead of writing them; `Make` logs them.
- **Naming**: page handlers `Get<Name>Page`; form actions `Post<Name>`. Views are exported templ funcs (`IndexPage`, `LoginPage`, `Button`). sqlc queries are PascalCase (`GetUserByUsername`, `CreateUser`, `CreateAccount`, `GetUserAccounts`, `GetUserById`).
- **State/DI**: package-global singletons — `db.Queries`, `db.Pool`, `handler.SessionManager`. No DI container; handlers reference globals directly.
- **Rendering**: `view.SomeComponent(args).Render(r.Context(), w)`. Layout uses templ `{ children... }` slots. Forms post via htmx (`hx-post`, `hx-swap="outerHTML"`) and re-render partial components on validation errors; redirects use `HX-Redirect` header or `http.Redirect`.
- **Auth details**: honeypot field `organization` rejected; login/signup pages set cache-control headers; empty-field validation re-renders the form.
- **DB types**: sqlc emits `pgtype.*` (e.g. `pgtype.UUID`, `pgtype.Text`) with the `pgx/v5` SQL package.

## Important Files

- `main.go` — router, middleware, route table, server bootstrap.
- `db/init.go` — `DATABASE_URL`, migrations, `pgxpool`, `data.Queries`.
- `handler/session.go` — session manager setup.
- `handler/auth.go` — signup/login/logout logic.
- `sqlc.yaml` — engine `postgresql`, queries `db/query.sql`, schema `db/migrations`, output package `data`, `sql_package: pgx/v5`.
- `input.css` — Tailwind v4 CSS entrypoint; `@source "./view/**/*.templ"`; plugins `@tailwindcss/forms`, `@tailwindcss/typography`.
- `.air.toml`, `Dockerfile`, `docker-compose.yml`, `.env.example`.

## Runtime/Tooling Preferences

- **Go**: `go 1.25.0` (README prereq: Go 1.23.1). Standard `go` module tooling; no vendoring.
- **Required tools**: `templ`, `sqlc`, `air`, `migrate` (golang-migrate), Tailwind standalone CLI.
- **Env**: `DATABASE_URL` (e.g. `postgres://postgres:password@localhost:5432/postgres?sslmode=disable`), `PORT` (default `3000`), `APP_ENV` (`development`/`production`).
- **Key deps**: `go-chi/chi/v5`, `a-h/templ`, `jackc/pgx/v5`, `golang-migrate/migrate/v4` (+ `lib/pq`), `alexedwards/scs/v2` + `scs/pgxstore`, `golang.org/x/crypto/bcrypt`, `gomarkdown/markdown`, `joho/godotenv`.
- **Docker**: two-stage build (`golang:1.25-alpine`), downloads Tailwind v4 CLI, emits minified CSS, builds `CGO_ENABLED=0` Linux binary, runs on `:3000` as non-root (alpine runtime, copies `public/` and `db/migrations`).

## Testing & QA

- **Tests exist** for `util` (markdown) and `handler` (`Make`), runnable DB-free via `go test ./...`. DB-dependent handler tests (auth) are intentionally omitted to keep the suite hermetic.
- When adding tests, use the standard Go `testing` package and place `*_test.go` beside the code (air excludes `_test.go` and `*_templ.go` from watching).
- QA today is manual: verify migrations with `up`/`down`/`force`, ideally against a separate DB stack (`docker compose -p dbmigrations-testing up -d`).
