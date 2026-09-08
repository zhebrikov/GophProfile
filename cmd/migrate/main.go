package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

func main() {
	var (
		dir     = flag.String("dir", "migrations", "directory with migration files")
		command = flag.String("command", "up", "goose command: up, down, status, reset, version")
	)
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := waitForDB(ctx, db); err != nil {
		log.Fatalf("postgres: %v", err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("goose dialect: %v", err)
	}

	if err := goose.RunContext(ctx, *command, db, *dir); err != nil {
		log.Fatalf("migrate %s: %v", *command, err)
	}
	log.Printf("migrate %s: ok", *command)
}

func waitForDB(ctx context.Context, db *sql.DB) error {
	var last error
	for i := 0; i < 30; i++ {
		if err := db.PingContext(ctx); err == nil {
			return nil
		} else {
			last = err
		}
		log.Printf("waiting for postgres: %v", last)
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %v", ctx.Err(), last)
		case <-time.After(time.Second):
		}
	}
	return last
}
