/*
Package db sets up the database connection & queries.
*/
package db

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/embiem/go-web-template/data"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

var (
	Queries *data.Queries
	Pool    *pgxpool.Pool
	Conn    *pgxpool.Conn
)

func Init() error {
	if Queries != nil {
		// Return error if already initialized
		return fmt.Errorf("db already initialized")
	}

	// Run any outstanding migrations
	m, err := migrate.New(
		"file://db/migrations",
		os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("failed opening connection to postgres for migration: %v", err)
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed db migrations: %v", err)
	}

	// Connect to DB & setup Queries
	Pool, err = pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}

	Conn, err = Pool.Acquire(context.Background())
	if err != nil {
		return fmt.Errorf("failed opening connection to postgres: %v", err)
	}

	Queries = data.New(Conn)

	return nil
}

func Teardown() {
	Conn.Release()
	Pool.Close()
}
