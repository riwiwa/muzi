package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"muzi/db"
	"muzi/web"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	zipDir := filepath.Join(".", "imports", "spotify", "zip")
	extDir := filepath.Join(".", "imports", "spotify", "extracted")

	dirs := []string{zipDir, extDir}
	for _, dir := range dirs {
		err := dirCheck(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking dir: %s: %v\n", dir, err)
			return
		}
	}
	err := db.CreateDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error ensuring muzi DB exists: %v\n", err)
		return
	}

	db.Pool, err = pgxpool.New(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		return
	}
	defer db.Pool.Close()

	err = db.CreateAllTables()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error ensuring all tables exist: %v\n", err)
		return
	}

	err = db.CleanupExpiredSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error cleaning expired sessions: %v\n", err)
		return
	}

	/*
		err = migrate.ImportSpotify(1)
		if err != nil {
			return
		}
	*/
	web.Start()
}
