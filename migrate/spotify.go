package migrate

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

type SpotifyTrack struct {
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

func trackKey(t SpotifyTrack) string {
	return fmt.Sprintf("%s|%d|%s|%s", t.Timestamp, t.Played, t.Artist, t.Name)
}

func getExistingTracks(conn *pgx.Conn, tracks []SpotifyTrack) (map[string]bool, error) {
	if len(tracks) == 0 {
		return map[string]bool{}, nil
	}

	var conditions []string
	var args []any

	for i, t := range tracks {
		base := i * 4
		conditions = append(conditions,
			fmt.Sprintf("(timestamp=$%d AND ms_played=$%d AND artist=$%d AND song_name=$%d)",
				base+1, base+2, base+3, base+4))
		args = append(args, t.Timestamp, t.Played, t.Artist, t.Name)
	}

	query := fmt.Sprintf(
		"SELECT timestamp, ms_played, artist, song_name FROM history WHERE %s",
		strings.Join(conditions, " OR "))

	rows, err := conn.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var ts string
		var played int
		var artist, song string
		if err := rows.Scan(&ts, &played, &artist, &song); err != nil {
			continue
		}
		key := fmt.Sprintf("%s|%d|%s|%s", ts, played, artist, song)
		existing[key] = true
	}

	return existing, nil
}

func JsonToDB(jsonFile string) error {
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
	var tracks []SpotifyTrack
	err = json.Unmarshal(jsonData, &tracks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot unmarshal %s: %v\n", jsonFile, err)
		return err
	}

	totalImported := 0
	batchSize := 1000

	for batchStart := 0; batchStart < len(tracks); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(tracks) {
			batchEnd = len(tracks)
		}

		var validTracks []SpotifyTrack
		for i := batchStart; i < batchEnd; i++ {
			if tracks[i].Played >= 20000 {
				validTracks = append(validTracks, tracks[i])
			}
		}

		if len(validTracks) == 0 {
			continue
		}

		existing, err := getExistingTracks(conn, validTracks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking existing tracks: %v\n", err)
			continue
		}

		var batchValues []string
		var batchArgs []any

		for _, t := range validTracks {
			key := trackKey(t)
			if existing[key] {
				continue
			}

			batchValues = append(batchValues, fmt.Sprintf(
				"($%d, $%d, $%d, $%d, $%d)",
				len(batchArgs)+1,
				len(batchArgs)+2,
				len(batchArgs)+3,
				len(batchArgs)+4,
				len(batchArgs)+5,
			))
			batchArgs = append(batchArgs, t.Timestamp, t.Name, t.Artist, t.Album, t.Played)
		}

		if len(batchValues) == 0 {
			continue
		}

		_, err = conn.Exec(
			context.Background(),
			`INSERT INTO history (timestamp, song_name, artist, album_name, ms_played) VALUES `+
				strings.Join(batchValues, ", ")+
				` ON CONFLICT DO NOTHING;`,
			batchArgs...,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Batch insert failed: %v\n", err)
		} else {
			totalImported += len(batchValues)
		}
	}

	fmt.Printf("%d tracks imported from %s\n", totalImported, jsonFile)
	return nil
}

func AddDirToDB(path string) error {
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
			if !strings.Contains(jsonFileName, ".json") {
				continue
			}
			// prevents parsing spotify video data that causes duplicates
			if strings.Contains(jsonFileName, "Video") {
				continue
			}
			jsonFilePath := filepath.Join(subPath, jsonFileName)
			err = JsonToDB(jsonFilePath)
			if err != nil {
				fmt.Fprintf(os.Stderr,
					"Error adding json data (%s) to muzi database: %v", jsonFilePath, err)
				return err
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
	err = AddDirToDB(targetBase)
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
