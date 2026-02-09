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

	"muzi/db"

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

func fetchPage(client *http.Client, page int, lfmUsername, apiKey string, userId int) pageResult {
	resp, err := client.Get(
		"https://ws.audioscrobbler.com/2.0/?method=user.getrecenttracks&user=" +
			lfmUsername + "&api_key=" + apiKey + "&format=json&limit=100&page=" + strconv.Itoa(page),
	)
	if err != nil {
		return pageResult{pageNum: page, err: err}
	}
	defer resp.Body.Close()
	var data Response
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return pageResult{pageNum: page, err: err}
	}

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
	return pageResult{pageNum: page, tracks: pageTracks, err: nil}
}

func ImportLastFM(
	username string,
	apiKey string,
	userId int,
	progressChan chan<- ProgressUpdate,
) error {
	totalImported := 0

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(
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
	defer resp.Body.Close()
	var initialData Response
	err = json.NewDecoder(resp.Body).Decode(&initialData)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Error decoding initial LastFM response: %v\n", err)
		return err
	}
	totalPages, err := strconv.Atoi(initialData.Recenttracks.Attr.TotalPages)
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
				pageChan <- fetchPage(client, page, lfmUsername, apiKey, userId)
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
			err := insertBatch(batch, &totalImported)
			if err != nil {
				// prevent logs being filled by duplicate warnings
				if !strings.Contains(err.Error(), "duplicate") {
					fmt.Fprintf(os.Stderr, "Batch insert failed: %v\n", err)
				}
			}
		}
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
		err := insertBatch(trackBatch, &totalImported)
		if err != nil {
			// prevent logs being filled by duplicate warnings
			if !strings.Contains(err.Error(), "duplicate") {
				fmt.Fprintf(os.Stderr, "Final batch insert failed: %v\n", err)
			}
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

func insertBatch(tracks []LastFMTrack, totalImported *int) error {
	copyCount, err := db.Pool.CopyFrom(context.Background(),
		pgx.Identifier{"history"},
		[]string{
			"user_id", "timestamp", "song_name", "artist", "album_name",
			"ms_played", "platform",
		},
		pgx.CopyFromSlice(len(tracks), func(i int) ([]any, error) {
			t := tracks[i]
			return []any{
				t.UserId, t.Timestamp, t.SongName, t.Artist,
				t.Album, 0, "lastfm",
			}, nil
		}),
	)
	*totalImported += int(copyCount)
	return err
}
