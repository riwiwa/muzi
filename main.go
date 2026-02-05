package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"muzi/migrate"
	"muzi/web"
)

func dbCheck() error {
	if !migrate.DbExists() {
		err := migrate.CreateDB()
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

	username := ""
	apiKey := ""
	fmt.Printf("Importing LastFM data for %s\n", username)
	err = migrate.ImportLastFM(username, apiKey)
	if err != nil {
		return
	}
	err = migrate.ImportSpotify()
	if err != nil {
		return
	}
	web.Start()
}
