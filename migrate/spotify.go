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
	"time"

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

type existingTrack struct {
	Timestamp time.Time
	SongName  string
	Artist    string
}

func getExistingTracks(conn *pgx.Conn, userId int, tracks []SpotifyTrack) (map[string]bool, error) {
	if len(tracks) == 0 {
		return map[string]bool{}, nil
	}

	// find min/max timestamps in this batch to create time window
	var minTs, maxTs time.Time
	for _, t := range tracks {
		ts, err := time.Parse(time.RFC3339Nano, t.Timestamp)
		if err != nil {
			continue
		}
		if minTs.IsZero() || ts.Before(minTs) {
			minTs = ts
		}
		if ts.After(maxTs) {
			maxTs = ts
		}
	}

	if minTs.IsZero() {
		return map[string]bool{}, nil
	}

	// query only tracks within [min-20s, max+20s] window using timestamp index
	rows, err := conn.Query(context.Background(),
		`SELECT song_name, artist, timestamp 
		 FROM history 
		 WHERE user_id = $1 
		 AND timestamp BETWEEN $2 AND $3`,
		userId,
		minTs.Add(-20*time.Second),
		maxTs.Add(20*time.Second))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := make(map[string]bool)
	var existingTracks []existingTrack
	for rows.Next() {
		var t existingTrack
		if err := rows.Scan(&t.SongName, &t.Artist, &t.Timestamp); err != nil {
			continue
		}
		existingTracks = append(existingTracks, t)
	}

	// check each incoming track against existing ones within 20 second window
	for _, newTrack := range tracks {
		newTs, err := time.Parse(time.RFC3339Nano, newTrack.Timestamp)
		if err != nil {
			continue
		}
		for _, existTrack := range existingTracks {
			if newTrack.Name == existTrack.SongName && newTrack.Artist == existTrack.Artist {
				diff := newTs.Sub(existTrack.Timestamp)
				if diff < 0 {
					diff = -diff
				}
				if diff < 20*time.Second {
					key := fmt.Sprintf(
						"%s|%s|%s",
						newTrack.Artist,
						newTrack.Name,
						newTrack.Timestamp,
					)
					existing[key] = true
					break
				}
			}
		}
	}

	return existing, nil
}

func JsonToDB(jsonFile string, userId int) error {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		panic(err)
	}
	defer conn.Close(context.Background())

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
			if tracks[i].Played >= 20000 && tracks[i].Name != "" && tracks[i].Artist != "" {
				validTracks = append(validTracks, tracks[i])
			}
		}

		if len(validTracks) == 0 {
			continue
		}

		existing, err := getExistingTracks(conn, userId, validTracks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking existing tracks: %v\n", err)
			continue
		}

		var batchValues []string
		var batchArgs []any

		for _, t := range validTracks {
			key := fmt.Sprintf("%s|%s|%s", t.Artist, t.Name, t.Timestamp)
			if existing[key] {
				continue
			}

			batchValues = append(batchValues, fmt.Sprintf(
				"($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
				len(batchArgs)+1,
				len(batchArgs)+2,
				len(batchArgs)+3,
				len(batchArgs)+4,
				len(batchArgs)+5,
				len(batchArgs)+6,
				len(batchArgs)+7,
			))
			batchArgs = append(
				batchArgs,
				userId,
				t.Timestamp,
				t.Name,
				t.Artist,
				t.Album,
				t.Played,
				"spotify",
			)
		}

		if len(batchValues) == 0 {
			continue
		}

		_, err = conn.Exec(
			context.Background(),
			`INSERT INTO history (user_id, timestamp, song_name, artist, album_name, ms_played, platform) VALUES `+
				strings.Join(
					batchValues,
					", ",
				)+
				` ON CONFLICT ON CONSTRAINT history_user_id_song_name_artist_timestamp_key DO NOTHING;`,
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

func AddDirToDB(path string, userId int) error {
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
			if strings.Contains(jsonFileName, "Video") {
				continue
			}
			jsonFilePath := filepath.Join(subPath, jsonFileName)
			err = JsonToDB(jsonFilePath, userId)
			if err != nil {
				fmt.Fprintf(os.Stderr,
					"Error adding json data (%s) to muzi database: %v", jsonFilePath, err)
				return err
			}
		}
	}
	return nil
}

func ImportSpotify(userId int) error {
	path := filepath.Join(".", "imports", "spotify", "zip")
	targetBase := filepath.Join(".", "imports", "spotify", "extracted")
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading path: %s: %v\n", path, err)
		return err
	}
	for _, f := range entries {
		reader, err := zip.OpenReader(filepath.Join(path, f.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening zip: %s: %v\n",
				filepath.Join(path, f.Name()), err)
			continue
		}
		defer reader.Close()
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
	err = AddDirToDB(targetBase, userId)
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
		defer fileToExtract.Close()
		extractedFile, err := f.Open()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file: %s: %v\n", f.Name, err)
			return err
		}
		defer extractedFile.Close()
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
	}
	return nil
}
