package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"muzi/db"
	"muzi/migrate"
	"muzi/web"

	"github.com/jackc/pgx/v5"
)

func dbCheck() error {
	if !db.DbExists() {
		err := db.CreateDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating muzi DB: %v\n", err)
			return err
		}
	}
	return nil
}

func dirCheck(path string) error {
	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			os.MkdirAll(path, os.ModePerm)
		} else {
			fmt.Fprintf(os.Stderr, "Error checking dir: %s: %v\n", path, err)
			return err
		}
	}

	return nil
}

func main() {
	dirImports := filepath.Join(".", "imports")

	dirSpotify := filepath.Join(".", "imports", "spotify")
	dirSpotifyZip := filepath.Join(".", "imports", "spotify", "zip")
	dirSpotifyExt := filepath.Join(".", "imports", "spotify", "extracted")

	fmt.Printf("Checking if directory %s exists...\n", dirImports)
	err := dirCheck(dirImports)
	if err != nil {
		return
	}
	fmt.Printf("Checking if directory %s exists...\n", dirSpotify)
	err = dirCheck(dirSpotify)
	if err != nil {
		return
	}
	fmt.Printf("Checking if directory %s exists...\n", dirSpotifyZip)
	err = dirCheck(dirSpotifyZip)
	if err != nil {
		return
	}
	fmt.Printf("Checking if directory %s exists...\n", dirSpotifyExt)
	err = dirCheck(dirSpotifyExt)
	if err != nil {
		return
	}
	fmt.Println("Checking if muzi database exists...")
	err = dbCheck()
	if err != nil {
		return
	}

	fmt.Println("Setting up database tables...")
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		return
	}
	defer conn.Close(context.Background())

	err = db.CreateHistoryTable(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating history table: %v\n", err)
		return
	}

	err = db.CreateUsersTable(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating users table: %v\n", err)
		return
	}

	username := ""
	apiKey := ""
	fmt.Printf("Importing LastFM data for %s\n", username)
	// TODO:
	// remove hardcoded userID by creating webUI import pages and getting
	// userID from login session
	err = migrate.ImportLastFM(username, apiKey, 1)
	if err != nil {
		return
	}
	err = migrate.ImportSpotify(1)
	if err != nil {
		return
	}
	web.Start()
}
