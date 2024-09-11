# Go Web Template

This is my (embiem's) favorite Go web stack at the time of making.

I use this to start new web projects quickly and will likely change this template over time.

## Prerequisites

- install Go
- install [migrate CLI](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate)
- install [sqlc](https://docs.sqlc.dev/en/stable/overview/install.html)
- install [air](https://github.com/air-verse/air#installation)
- install [tailwindcss-cli](https://tailwindcss.com/blog/standalone-cli) v3.4.10
- `cp .env.example .env` & fill-in any missing env vars
- spin-up local dev services like db: `docker compose up -d`


## Local dev

- run local dev setup via `air`
- run tests via `go test ./...`

## DB

Using golang-migrate for migrations ([Tutorial](https://github.com/golang-migrate/migrate/blob/master/database/postgres/TUTORIAL.md)) and sqlc for queries, mutations & codegen ([Tutorial](https://docs.sqlc.dev/en/stable/tutorials/getting-started-postgresql.html)).

[sqlc doc](https://docs.sqlc.dev/en/stable/howto/ddl.html) about handling SQL migrations.

### Migrations

For local dev, setup env var like so: `export POSTGRESQL_URL='postgres://postgres:password@localhost:5432/postgres?sslmode=disable'`.

Optionally, test migrations up & down on a separate local db instance e.g. by spinning up a stack with different name: `docker compose -p dbmigrations-testing up -d`.

1. Create Migration files: `migrate create -ext sql -dir db/migrations -seq your_migration_description`
2. Write the migrations in the created up & down files using SQL
3. Run up migrations: `migrate -database ${POSTGRESQL_URL} -path db/migrations up`
4. Check db & run down migrations to test they work as well: `migrate -database ${POSTGRESQL_URL} -path db/migrations down` & check db as well
5. run up migrations again

When dirty, force db to a version reflecting it's real state: `migrate -database ${POSTGRESQL_URL} -path db/migrations force VERSION`

Important: Write migration SQL in transactions. In Postgres, when we want our queries to be done in a transaction, we need to wrap it with `BEGIN` and `COMMIT` commands. Example:

```sql
-- up migration
BEGIN;

CREATE TYPE enum_mood AS ENUM (
	'happy',
	'sad',
	'neutral'
);
ALTER TABLE users ADD COLUMN mood enum_mood;

COMMIT;
```

```sql
-- down migration
BEGIN;

ALTER TABLE users DROP COLUMN mood;
DROP TYPE enum_mood;

COMMIT;
```

### Queries, Mutations & Codegen

Write the SQL queries & mutations in `db/query.sql` and then run `sqlc generate`.


## Templ / View / UI

Using Templ: [https://templ.guide/quick-start/creating-a-simple-templ-component](https://templ.guide/quick-start/creating-a-simple-templ-component)

- `templ generate` to generate go files after adding or editing .templ files
