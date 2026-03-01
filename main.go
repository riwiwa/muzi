package main

import (
	"context"
	"fmt"
	"os"

	"muzi/config"
	"muzi/db"
	"muzi/scrobble"
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
	_, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	check("ensuring muzi DB exists", db.CreateDB())

	db.Pool, err = pgxpool.New(context.Background(), db.GetDbUrl(true))
	check("connecting to muzi database", err)
	defer db.Pool.Close()

	check("ensuring all tables exist", db.CreateAllTables())
	check("cleaning expired sessions", db.CleanupExpiredSessions())
	scrobble.StartSpotifyPoller()
	web.Start()
}
