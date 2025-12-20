package importsongs

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	spotify = iota
	lastfm
	apple
)

func TableExists(name string, conn *pgx.Conn) bool {
	var exists bool
	err := conn.QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = $1);", name).Scan(&exists)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT EXISTS failed: %v\n", err)
		return false
	}
	return exists
}

func DbExists() bool {
	conn, err := pgx.Connect(context.Background(), "postgres://postgres:postgres@localhost:5432/muzi")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		return false
	}
	defer conn.Close(context.Background())
	return true
}

func CreateDB() error {
	conn, err := pgx.Connect(context.Background(), "postgres://postgres:postgres@localhost:5432")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to PostgreSQL: %v\n", err)
		return err
	}
	defer conn.Close(context.Background())
	_, err = conn.Exec(context.Background(), "CREATE DATABASE muzi")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create muzi database: %v\n", err)
		return err
	}
	return nil
}

func JsonToDB(jsonFile string, platform int) {
	if !DbExists() {
		err := CreateDB()
		if err != nil {
			panic(err)
		}
	}
	conn, err := pgx.Connect(context.Background(), "postgres://postgres:postgres@localhost:5432/muzi")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		panic(err)
	}
	defer conn.Close(context.Background())
	if !TableExists("history", conn) {
		_, err = conn.Exec(context.Background(), "CREATE TABLE history ( ms_played INTEGER, timestamp TIMESTAMPTZ, song_name TEXT, artist TEXT, album_name TEXT, PRIMARY KEY (timestamp, ms_played, artist, song_name));")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create history table: %v\n", err)
		panic(err)
	}
	jsonData, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot read %s: %v\n", jsonFile, err)
		panic(err)
	}
	if platform == spotify {
		type Track struct {
			Timestamp string `json:"ts"`
			//Platform            string `json:"platform"`
			Played int `json:"ms_played"`
			//Country             string `json:"conn_country"`
			//IP                  string `json:"ip_addr"`
			Name   string `json:"master_metadata_track_name"`
			Artist string `json:"master_metadata_album_artist_name"`
			Album  string `json:"master_metadata_album_album_name"`
			//TrackURI            string `json:"spotify_track_uri"`
			//Episode             string `json:"episode_name"`
			//Show                string `json:"episode_show_name"`
			//EpisodeURI          string `json:"spotify_episode_uri"`
			//Audiobook           string `json:"audiobook_title"`
			//AudiobookURI        string `json:"audiobook_uri"`
			//AudiobookChapterURI string `json:"audiobook_chapter_uri"`
			//AudiobookChapter    string `json:"audiobook_chapter_title"`
			//ReasonStart         string `json:"reason_start"`
			//ReasonEnd           string `json:"reason_end"`
			//Shuffle             bool   `json:"shuffle"`
			//Skipped             bool   `json:"skipped"`
			//Offline             bool   `json:"offline"`
			//OfflineTimestamp    int    `json:"offline_timestamp"`
			//Incognito           bool   `json:"incognito_mode"`
		}
		var tracks []Track
		err := json.Unmarshal(jsonData, &tracks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot unmarshal %s: %v\n", jsonFile, err)
			panic(err)
		}
		for _, track := range tracks {
			// skip adding a song if it was only listed to for less than 20 seconds
			if track.Played < 20000 {
				continue
			}
			_, err = conn.Exec(context.Background(), "INSERT INTO history (timestamp, song_name, artist, album_name, ms_played) VALUES ($1, $2, $3, $4, $5);", track.Timestamp, track.Name, track.Artist, track.Album, track.Played)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Couldn't add track to muzi DB (%s): %v\n", (track.Artist + " - " + track.Name), err)
			}
		}
	}
}

func AddDirToDB(path string, platform int) {
	dirs, err := os.ReadDir(path)
	if err != nil {
		panic(err)
	}
	for _, dir := range dirs {
		subPath := filepath.Join(path, dir.Name(), "Spotify Extended Streaming History")
		entries, err := os.ReadDir(subPath)
		if err != nil {
			panic(err)
		}
		for _, f := range entries {
			jsonFileName := f.Name()
			if platform == spotify {
				if !strings.Contains(jsonFileName, ".json") {
					continue
				}
				// prevents parsing spotify video data that causes duplicates
				if strings.Contains(jsonFileName, "Video") {
					continue
				}
			}
			jsonFilePath := filepath.Join(subPath, jsonFileName)
			JsonToDB(jsonFilePath, platform)
		}
	}
}

func ImportSpotify() {
	path := filepath.Join(".", "spotify-data", "zip")
	targetBase := filepath.Join(".", "spotify-data", "extracted")
	entries, err := os.ReadDir(path)
	if err != nil {
		panic(err)
	}
	for _, f := range entries {
		_, err := zip.OpenReader(filepath.Join(path, f.Name()))
		if err == nil {
			fileName := f.Name()
			fileFullPath := filepath.Join(path, fileName)
			fileBaseName := fileName[:(strings.LastIndex(fileName, "."))]
			targetDirFullPath := filepath.Join(targetBase, fileBaseName)

			Extract(fileFullPath, targetDirFullPath)
		}
	}
	AddDirToDB(targetBase, spotify)
}

func Extract(path string, target string) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		panic(err)
	}
	defer archive.Close()

	zipDir := filepath.Base(path)
	zipDir = zipDir[:(strings.LastIndex(zipDir, "."))]

	for _, f := range archive.File {
		filePath := filepath.Join(target, f.Name)
		fmt.Println("extracting:", filePath)

		if !strings.HasPrefix(filePath, filepath.Clean(target)+string(os.PathSeparator)) {
			fmt.Println("Invalid file path")
			return
		}
		if f.FileInfo().IsDir() {
			fmt.Println("Creating Directory", filePath)
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			panic(err)
		}
		fileToExtract, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			panic(err)
		}
		extractedFile, err := f.Open()
		if err != nil {
			panic(err)
		}
		if _, err := io.Copy(fileToExtract, extractedFile); err != nil {
			panic(err)
		}
		fileToExtract.Close()
		extractedFile.Close()
	}
}
