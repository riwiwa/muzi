package importsongs

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	spotify = iota
	lastfm
	apple
)

func TableExists(name string, conn *pgx.Conn) bool {
	var exists bool
	err := conn.QueryRow(
		context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND 
		tablename = $1);`,
		name,
	).
		Scan(&exists)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT EXISTS failed: %v\n", err)
		return false
	}
	return exists
}

func DbExists() bool {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		return false
	}
	defer conn.Close(context.Background())
	return true
}

func CreateDB() error {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432",
	)
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

func JsonToDB(jsonFile string, platform int) error {
	if !DbExists() {
		err := CreateDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating muzi database: %v\n", err)
			panic(err)
		}
	}
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		panic(err)
	}
	defer conn.Close(context.Background())
	if !TableExists("history", conn) {
		_, err = conn.Exec(
			context.Background(),
			`CREATE TABLE history ( ms_played INTEGER, timestamp TIMESTAMPTZ, 
			song_name TEXT, artist TEXT, album_name TEXT, PRIMARY KEY (timestamp, 
			ms_played, artist, song_name));`,
		)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create history table: %v\n", err)
		panic(err)
	}
	jsonData, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot read %s: %v\n", jsonFile, err)
		return err
	}
	if platform == spotify {
		type Track struct {
			Timestamp           string `json:"ts"`
			Platform            string `json:"-"`
			Played              int    `json:"ms_played"`
			Country             string `json:"-"`
			IP                  string `json:"-"`
			Name                string `json:"master_metadata_track_name"`
			Artist              string `json:"master_metadata_album_artist_name"`
			Album               string `json:"master_metadata_album_album_name"`
			TrackURI            string `json:"-"`
			Episode             string `json:"-"`
			Show                string `json:"-"`
			EpisodeURI          string `json:"-"`
			Audiobook           string `json:"-"`
			AudiobookURI        string `json:"-"`
			AudiobookChapterURI string `json:"-"`
			AudiobookChapter    string `json:"-"`
			ReasonStart         string `json:"-"`
			ReasonEnd           string `json:"-"`
			Shuffle             bool   `json:"-"`
			Skipped             bool   `json:"-"`
			Offline             bool   `json:"-"`
			OfflineTimestamp    int    `json:"-"`
			Incognito           bool   `json:"-"`
		}
		var tracks []Track
		err := json.Unmarshal(jsonData, &tracks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot unmarshal %s: %v\n", jsonFile, err)
			return err
		}
		for _, track := range tracks {
			// skip adding a song if it was only listed to for less than 20 seconds
			if track.Played < 20000 {
				continue
			}
			_, err = conn.Exec(
				context.Background(),
				`INSERT INTO history (timestamp, song_name, artist, album_name,
				 ms_played) VALUES ($1, $2, $3, $4, $5);`,
				track.Timestamp,
				track.Name,
				track.Artist,
				track.Album,
				track.Played,
			)
			if err != nil {
				fmt.Fprintf(
					os.Stderr,
					"Couldn't add track to muzi DB (%s): %v\n",
					(track.Artist + " - " + track.Name),
					err,
				)
			}
		}
	}
	return nil
}

func AddDirToDB(path string, platform int) error {
	dirs, err := os.ReadDir(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while reading path: %s: %v\n", path, err)
		return err
	}
	for _, dir := range dirs {
		subPath := filepath.Join(
			path,
			dir.Name(),
			"Spotify Extended Streaming History",
		)
		entries, err := os.ReadDir(subPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while reading path: %s: %v\n", subPath, err)
			return err
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
			err = JsonToDB(jsonFilePath, platform)
			if err != nil {
				fmt.Fprintf(os.Stderr,
					"Error adding json data (%s) to muzi database: %v", jsonFilePath, err)
				return err
			}
		}
	}
	return nil
}

func ImportLastFM(username string, apiKey string) error {
	if !DbExists() {
		err := CreateDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating muzi database: %v\n", err)
			panic(err)
		}
	}
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		panic(err)
	}
	defer conn.Close(context.Background())
	if !TableExists("history", conn) {
		_, err = conn.Exec(
			context.Background(),
			`CREATE TABLE history ( ms_played INTEGER, timestamp TIMESTAMPTZ, 
			song_name TEXT, artist TEXT, album_name TEXT, PRIMARY KEY (timestamp, 
			ms_played, artist, song_name));`,
		)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create history table: %v\n", err)
		panic(err)
	}

	resp, err := http.Get(
		"https://ws.audioscrobbler.com/2.0/?method=user.getrecenttracks&user=" +
			username + "&api_key=" + apiKey + "&format=json&limit=1",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting LastFM http response: %v\n", err)
		return err
	}
	type Response struct {
		Recenttracks struct {
			Track []struct {
				Artist struct {
					Mbid string `json:"-"`
					Text string `json:"#text"`
				} `json:"artist"`
				Streamable string `json:"-"`
				Image      []struct {
					Size string `json:"-"`
					Text string `json:"-"`
				} `json:"-"`
				Mbid  string `json:"-"`
				Album struct {
					Mbid string `json:"-"`
					Text string `json:"#text"`
				} `json:"album"`
				Name string `json:"name"`
				Attr struct {
					Nowplaying string `json:"nowplaying"`
				} `json:"@attr,omitempty"`
				URL  string `json:"-"`
				Date struct {
					Uts  string `json:"uts"`
					Text string `json:"-"`
				} `json:"date"`
			} `json:"track"`
			Attr struct {
				PerPage    string `json:"-"`
				TotalPages string `json:"totalPages"`
				Page       string `json:"page"`
				Total      string `json:"-"`
				User       string `json:"-"`
			} `json:"@attr"`
		} `json:"recenttracks"`
	}
	var data Response
	json.NewDecoder(resp.Body).Decode(&data)
	totalPages, err := strconv.Atoi(data.Recenttracks.Attr.TotalPages)
	if totalPages%100 != 0 {
		totalPages = totalPages / 100
		totalPages++
	} else {
		totalPages = totalPages / 100
	}

	for i := 1; i <= totalPages; i++ {
		resp, err := http.Get(
			"https://ws.audioscrobbler.com/2.0/?method=user.getrecenttracks&user=" +
				username + "&api_key=" + apiKey + "&format=json&limit=100&page=" +
				strconv.Itoa(
					i,
				),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting LastFM http response: %v\n", err)
			return err
		}
		json.NewDecoder(resp.Body).Decode(&data)
		for j := range data.Recenttracks.Track {
			if data.Recenttracks.Track[j].Attr.Nowplaying == "true" {
				continue
			}
			unixTime, err := strconv.ParseInt(
				data.Recenttracks.Track[j].Date.Uts,
				10,
				64,
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing string for int: %v\n", err)
				return err
			}
			ts := time.Unix(unixTime, 0)
			_, err = conn.Exec(
				context.Background(),
				`INSERT INTO history (timestamp, song_name, artist, album_name, 
				ms_played) VALUES ($1, $2, $3, $4, $5);`,
				ts,
				data.Recenttracks.Track[j].Name,
				data.Recenttracks.Track[j].Artist.Text,
				data.Recenttracks.Track[j].Album.Text,
				0,
			)
			if err != nil {
				fmt.Fprintf(
					os.Stderr,
					"Couldn't add track to muzi DB (%s): %v\n",
					(data.Recenttracks.Track[j].Artist.Text + " - " +
						data.Recenttracks.Track[j].Name), err)
			}
		}
	}
	return nil
}

func ImportSpotify() error {
	path := filepath.Join(".", "imports", "spotify", "zip")
	targetBase := filepath.Join(".", "imports", "spotify", "extracted")
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading path: %s: %v\n", path, err)
		return err
	}
	for _, f := range entries {
		_, err := zip.OpenReader(filepath.Join(path, f.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening zip: %s: %v\n",
				filepath.Join(path, f.Name()), err)
			continue
		}
		fileName := f.Name()
		fileFullPath := filepath.Join(path, fileName)
		fileBaseName := fileName[:(strings.LastIndex(fileName, "."))]
		targetDirFullPath := filepath.Join(targetBase, fileBaseName)

		err = Extract(fileFullPath, targetDirFullPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error extracting %s to %s: %v\n",
				fileFullPath, targetDirFullPath, err)
			return err
		}
	}
	err = AddDirToDB(targetBase, spotify)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Error adding directory of data (%s) to muzi database: %v\n",
			targetBase, err)
		return err
	}
	return nil
}

func Extract(path string, target string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening zip: %s: %v\n", path, err)
		return err
	}
	defer archive.Close()

	zipDir := filepath.Base(path)
	zipDir = zipDir[:(strings.LastIndex(zipDir, "."))]

	for _, f := range archive.File {
		filePath := filepath.Join(target, f.Name)
		fmt.Println("extracting:", filePath)

		if !strings.HasPrefix(
			filePath,
			filepath.Clean(target)+string(os.PathSeparator),
		) {
			err = fmt.Errorf("Invalid file path: %s", filePath)
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return err
		}
		if f.FileInfo().IsDir() {
			fmt.Println("Creating Directory", filePath)
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			fmt.Fprintf(os.Stderr, "Error making directory: %s: %v\n",
				filepath.Dir(filePath), err)
			return err
		}
		fileToExtract, err := os.OpenFile(
			filePath,
			os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
			f.Mode(),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file: %s: %v\n", filePath, err)
			return err
		}
		extractedFile, err := f.Open()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file: %s: %v\n", f.Name, err)
			return err
		}
		if _, err := io.Copy(fileToExtract, extractedFile); err != nil {
			fmt.Fprintf(
				os.Stderr,
				"Error while copying file: %s to: %s: %v\n",
				fileToExtract.Name(),
				extractedFile,
				err,
			)
			return err
		}
		fileToExtract.Close()
		extractedFile.Close()
	}
	return nil
}
