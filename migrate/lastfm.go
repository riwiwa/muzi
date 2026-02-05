package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type LastFMTrack struct {
	Timestamp time.Time
	SongName  string
	Artist    string
	Album     string
}

type pageResult struct {
	pageNum int
	tracks  []LastFMTrack
	err     error
}

type Response struct {
	Recenttracks struct {
		Track []struct {
			Artist struct {
				Text string `json:"#text"`
			} `json:"artist"`
			Album struct {
				Text string `json:"#text"`
			} `json:"album"`
			Name string `json:"name"`
			Attr struct {
				Nowplaying string `json:"nowplaying"`
			} `json:"@attr,omitempty"`
			Date struct {
				Uts string `json:"uts"`
			} `json:"date"`
		} `json:"track"`
		Attr struct {
			TotalPages string `json:"totalPages"`
		} `json:"@attr"`
	} `json:"recenttracks"`
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
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot create history table: %v\n", err)
			panic(err)
		}
	}

	totalImported := 0

	resp, err := http.Get(
		"https://ws.audioscrobbler.com/2.0/?method=user.getrecenttracks&user=" +
			username + "&api_key=" + apiKey + "&format=json&limit=100",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting LastFM HTTP response: %v\n", err)
		return err
	}
	var initialData Response
	json.NewDecoder(resp.Body).Decode(&initialData)
	totalPages, err := strconv.Atoi(initialData.Recenttracks.Attr.TotalPages)
	resp.Body.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing total pages: %v\n", err)
		return err
	}
	fmt.Printf("Total pages: %d\n", totalPages)

	trackBatch := make([]LastFMTrack, 0, 1000)

	pageChan := make(chan pageResult, 20)

	var wg sync.WaitGroup
	// use 10 workers
	wg.Add(10)

	for worker := range 10 {
		go func(workerID int) {
			defer wg.Done()
			// distrubute 10 pages to each worker
			for page := workerID + 1; page <= totalPages; page += 10 {
				resp, err := http.Get(
					"https://ws.audioscrobbler.com/2.0/?method=user.getrecenttracks&user=" +
						username + "&api_key=" + apiKey + "&format=json&limit=100&page=" + strconv.Itoa(page),
				)
				if err != nil {
					pageChan <- pageResult{pageNum: page, err: err}
					continue
				}
				var data Response
				if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
					resp.Body.Close()
					pageChan <- pageResult{pageNum: page, err: err}
					continue
				}
				resp.Body.Close()

				var pageTracks []LastFMTrack
				for j := range data.Recenttracks.Track {
					if data.Recenttracks.Track[j].Attr.Nowplaying == "true" {
						continue
					}
					unixTime, err := strconv.ParseInt(data.Recenttracks.Track[j].Date.Uts, 10, 64)
					if err != nil {
						continue
					}
					pageTracks = append(pageTracks, LastFMTrack{
						Timestamp: time.Unix(unixTime, 0),
						SongName:  data.Recenttracks.Track[j].Name,
						Artist:    data.Recenttracks.Track[j].Artist.Text,
						Album:     data.Recenttracks.Track[j].Album.Text,
					})
				}
				pageChan <- pageResult{pageNum: page, tracks: pageTracks, err: nil}
			}
		}(worker)
	}

	go func() {
		wg.Wait()
		close(pageChan)
	}()

	batchSize := 500

	for result := range pageChan {
		if result.err != nil {
			fmt.Fprintf(os.Stderr, "Error on page %d: %v\n", result.pageNum, result.err)
			continue
		}
		trackBatch = append(trackBatch, result.tracks...)
		for len(trackBatch) >= batchSize {
			batch := trackBatch[:batchSize]
			trackBatch = trackBatch[batchSize:]
			err := insertBatch(conn, batch, &totalImported, batchSize)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Batch insert failed: %v\n", err)
			}
		}
		fmt.Printf("Processed page %d/%d\n", result.pageNum, totalPages)
	}

	if len(trackBatch) > 0 {
		err := insertBatch(conn, trackBatch, &totalImported, batchSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Final batch insert failed: %v\n", err)
		}
	}

	fmt.Printf("%d tracks imported from LastFM for user %s\n", totalImported, username)
	return nil
}

func insertBatch(conn *pgx.Conn, tracks []LastFMTrack, totalImported *int, batchSize int) error {
	tx, err := conn.Begin(context.Background())
	if err != nil {
		return err
	}

	var batchValues []string
	var batchArgs []any

	for i, track := range tracks {
		batchValues = append(batchValues, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d)",
			len(
				batchArgs,
			)+1,
			len(batchArgs)+2,
			len(batchArgs)+3,
			len(batchArgs)+4,
			len(batchArgs)+5,
		))
		batchArgs = append(batchArgs, track.Timestamp, track.SongName, track.Artist, track.Album, 0)

		if len(batchValues) >= batchSize || i == len(tracks)-1 {
			result, err := tx.Exec(
				context.Background(),
				`INSERT INTO history (timestamp, song_name, artist, album_name, ms_played) VALUES `+
					strings.Join(batchValues, ", ")+` ON CONFLICT DO NOTHING;`,
				batchArgs...,
			)
			if err != nil {
				tx.Rollback(context.Background())
				return err
			}
			rowsAffected := result.RowsAffected()
			if rowsAffected > 0 {
				*totalImported += int(rowsAffected)
			}
			batchValues = batchValues[:0]
			batchArgs = batchArgs[:0]
		}
	}

	if err := tx.Commit(context.Background()); err != nil {
		return err
	}

	return nil
}
