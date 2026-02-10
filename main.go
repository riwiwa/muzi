package main

import (
	"context"
	"fmt"
	"os"

	"muzi/db"
	"muzi/web"

	"github.com/jackc/pgx/v5/pgxpool"
)

func check(msg string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error %s: %v\n", msg, err)
		os.Exit(1)
	}
}

func main() {
	check("ensuring muzi DB exists", db.CreateDB())

	var err error
	db.Pool, err = pgxpool.New(context.Background(), db.GetDbUrl(true))
	check("connecting to muzi database", err)
	defer db.Pool.Close()

	check("ensuring all tables exist", db.CreateAllTables())
	check("cleaning expired sessions", db.CleanupExpiredSessions())
	web.Start()
}
