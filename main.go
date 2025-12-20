package main

import (
	"errors"
	"fmt"
	"muzi/importsongs"
	"muzi/web"
	"os"
)

func dbCheck() error {
	if !importsongs.DbExists() {
		err := importsongs.CreateDB()
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
	dirImports := "./imports/"

	dirSpotify := "./imports/spotify/"
	dirSpotifyZip := "./imports/spotify/zip/"
	dirSpotifyExt := "./imports/spotify/extracted/"

	dirLastFM := "./imports/lastfm/"

	err := dirCheck(dirImports)
	if err != nil {
		return
	}
	err = dirCheck(dirSpotify)
	if err != nil {
		return
	}
	err = dirCheck(dirSpotifyZip)
	if err != nil {
		return
	}
	err = dirCheck(dirSpotifyExt)
	if err != nil {
		return
	}
	err = dirCheck(dirLastFM)
	if err != nil {
		return
	}
	err = dbCheck()
	if err != nil {
		return
	}

	//importsongs.ImportSpotify()
	web.Start()
}
