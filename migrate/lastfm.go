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
	UserId    int
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

type ProgressUpdate struct {
	CurrentPage    int    `json:"current_page"`
	CompletedPages int    `json:"completed_pages"`
	TotalPages     int    `json:"total_pages"`
	TracksImported int    `json:"tracks_imported"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
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

func ImportLastFM(
	username string,
	apiKey string,
	userId int,
	progressChan chan<- ProgressUpdate,
) error {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		if progressChan != nil {
			progressChan <- ProgressUpdate{Status: "error", Error: err.Error()}
		}
		return err
	}
	defer conn.Close(context.Background())

	totalImported := 0

	resp, err := http.Get(
		"https://ws.audioscrobbler.com/2.0/?method=user.getrecenttracks&user=" +
			username + "&api_key=" + apiKey + "&format=json&limit=100",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting LastFM HTTP response: %v\n", err)
		if progressChan != nil {
			progressChan <- ProgressUpdate{Status: "error", Error: err.Error()}
		}
		return err
	}
	var initialData Response
	json.NewDecoder(resp.Body).Decode(&initialData)
	totalPages, err := strconv.Atoi(initialData.Recenttracks.Attr.TotalPages)
	resp.Body.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing total pages: %v\n", err)
		if progressChan != nil {
			progressChan <- ProgressUpdate{Status: "error", Error: err.Error()}
		}
		return err
	}
	fmt.Printf("Total pages: %d\n", totalPages)

	// send initial progress update
	if progressChan != nil {
		progressChan <- ProgressUpdate{
			TotalPages: totalPages,
			Status:     "running",
		}
	}

	trackBatch := make([]LastFMTrack, 0, 1000)

	pageChan := make(chan pageResult, 20)

	var wg sync.WaitGroup
	wg.Add(10)

	for worker := range 10 {
		go func(workerID int) {
			defer wg.Done()
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
						UserId:    userId,
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
	completedPages := 0
	var completedMu sync.Mutex

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

		// increment completed pages counter
		completedMu.Lock()
		completedPages++
		currentCompleted := completedPages
		completedMu.Unlock()

		// send progress update after each page
		if progressChan != nil {
			progressChan <- ProgressUpdate{
				CurrentPage:    result.pageNum,
				CompletedPages: currentCompleted,
				TotalPages:     totalPages,
				TracksImported: totalImported,
				Status:         "running",
			}
		}
	}

	if len(trackBatch) > 0 {
		err := insertBatch(conn, trackBatch, &totalImported, batchSize)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Final batch insert failed: %v\n", err)
		}
	}

	fmt.Printf("%d tracks imported from LastFM for user %s\n", totalImported, username)

	// send completion update
	if progressChan != nil {
		progressChan <- ProgressUpdate{
			CurrentPage:    totalPages,
			TotalPages:     totalPages,
			TracksImported: totalImported,
			Status:         "completed",
		}
	}

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
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			len(batchArgs)+1,
			len(batchArgs)+2,
			len(batchArgs)+3,
			len(batchArgs)+4,
			len(batchArgs)+5,
			len(batchArgs)+6,
			len(batchArgs)+7,
		))
		// lastfm doesn't store playtime for each track, so set to 0
		batchArgs = append(
			batchArgs,
			track.UserId,
			track.Timestamp,
			track.SongName,
			track.Artist,
			track.Album,
			0,
			"lastfm",
		)

		if len(batchValues) >= batchSize || i == len(tracks)-1 {
			result, err := tx.Exec(
				context.Background(),
				`INSERT INTO history (user_id, timestamp, song_name, artist, album_name, ms_played, platform) VALUES `+
					strings.Join(
						batchValues,
						", ",
					)+` ON CONFLICT ON CONSTRAINT history_user_id_song_name_artist_timestamp_key DO NOTHING;`,
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
