package main

import (
	"context"
	"fmt"
	"os"

	"muzi/artwork"
	"muzi/config"
	"muzi/db"
	"muzi/scrobble"
	"muzi/web"

	"github.com/jackc/pgx/v5/pgxpool"

	// bundled timezone data, so per-user timezones work where the system has none (e.g. containers)
	_ "time/tzdata"
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

	// `muzi reset-password <username>` sets a new random password and exits
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: muzi reset-password <username>")
			os.Exit(2)
		}
		password, err := web.ResetPassword(os.Args[2])
		check("resetting password", err)
		fmt.Printf("New password for %s: %s\nAll of their sessions have been logged out.\n", os.Args[2], password)
		return
	}

	check("creating albums and songs for imported history", db.BackfillEntities())
	check("cleaning expired sessions", db.CleanupExpiredSessions())
	scrobble.StartSpotifyPoller()
	artwork.Start()
	web.Start()
}
