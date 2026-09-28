package migrate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"muzi/db"
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
	// Last.fm reports failures (e.g. rate limiting) in the body, sometimes with a 200 status
	Error   int    `json:"error"`
	Message string `json:"message"`

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

// Attempts per page before giving up on it
const lastFMRetries = 5

// Concurrent page requests; Last.fm starts returning errors when hit much harder
const lastFMWorkers = 4

// Fetches one page of recent tracks. to pins the end of the range so pages don't shift if
// new scrobbles arrive during the import.
func getRecentTracks(client *http.Client, lfmUsername, apiKey string, page int, to int64) (Response, error) {
	params := url.Values{}
	params.Set("method", "user.getrecenttracks")
	params.Set("user", lfmUsername)
	params.Set("api_key", apiKey)
	params.Set("format", "json")
	params.Set("limit", "100")
	params.Set("page", strconv.Itoa(page))
	params.Set("to", strconv.FormatInt(to, 10))

	var data Response
	resp, err := client.Get("https://ws.audioscrobbler.com/2.0/?" + params.Encode())
	if err != nil {
		return data, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return data, fmt.Errorf("status %d: %w", resp.StatusCode, err)
	}
	if data.Error != 0 {
		return data, fmt.Errorf("lastfm error %d: %s", data.Error, data.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return data, fmt.Errorf("lastfm returned status %d", resp.StatusCode)
	}
	return data, nil
}

// Like getRecentTracks, retrying with backoff since Last.fm errors are usually transient
func getRecentTracksWithRetry(
	client *http.Client,
	lfmUsername, apiKey string,
	page int,
	to int64,
) (Response, error) {
	var data Response
	var err error
	for attempt := range lastFMRetries {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
		}
		data, err = getRecentTracks(client, lfmUsername, apiKey, page, to)
		if err == nil {
			return data, nil
		}
	}
	return data, err
}

func fetchPage(client *http.Client, page int, lfmUsername, apiKey string, userId int, to int64) pageResult {
	data, err := getRecentTracksWithRetry(client, lfmUsername, apiKey, page, to)
	if err != nil {
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
	lfmUsername string,
	apiKey string,
	userId int,
	progressChan chan<- ProgressUpdate,
	username string,
) error {
	totalImported := 0

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	importStart := time.Now().Unix()
	initialData, err := getRecentTracksWithRetry(client, lfmUsername, apiKey, 1, importStart)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting LastFM recent tracks: %v\n", err)
		if progressChan != nil {
			progressChan <- ProgressUpdate{Status: "error", Error: err.Error()}
		}
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
	fmt.Printf("%s started a LastFM import job of %d total pages\n", username,
		totalPages)

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
	wg.Add(lastFMWorkers)

	for worker := range lastFMWorkers {
		go func(workerID int) {
			defer wg.Done()
			for page := workerID + 1; page <= totalPages; page += lastFMWorkers {
				pageChan <- fetchPage(client, page, lfmUsername, apiKey, userId, importStart)
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

	var failedPages []int
	for result := range pageChan {
		if result.err != nil {
			fmt.Fprintf(os.Stderr, "Error on page %d after %d attempts: %v\n",
				result.pageNum, lastFMRetries, result.err)
			failedPages = append(failedPages, result.pageNum)
			continue
		}
		trackBatch = append(trackBatch, result.tracks...)
		for len(trackBatch) >= batchSize {
			batch := trackBatch[:batchSize]
			trackBatch = trackBatch[batchSize:]
			if err := insertBatch(batch, &totalImported); err != nil {
				fmt.Fprintf(os.Stderr, "Batch insert failed: %v\n", err)
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
		if err := insertBatch(trackBatch, &totalImported); err != nil {
			fmt.Fprintf(os.Stderr, "Final batch insert failed: %v\n", err)
		}
	}

	// Re-running the import is safe (existing plays are skipped), so point the user at that
	warning := ""
	if len(failedPages) > 0 {
		warning = fmt.Sprintf("%d of %d pages could not be fetched from Last.fm; run the import again "+
			"to fill them in", len(failedPages), totalPages)
		fmt.Fprintf(os.Stderr, "LastFM import for %s: %s (pages %v)\n", username, warning, failedPages)
	}

	fmt.Printf("User %s imported %d tracks from LastFM account %s\n",
		username,
		totalImported,
		lfmUsername)

	if err := db.BackfillEntities(); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating albums and songs after import: %v\n", err)
	}

	// send completion update
	if progressChan != nil {
		progressChan <- ProgressUpdate{
			CurrentPage:    totalPages,
			TotalPages:     totalPages,
			TracksImported: totalImported,
			Status:         "completed",
			Error:          warning,
		}
	}

	return nil
}

// Inserts a batch of Last.fm plays, skipping ones already in history
func insertBatch(tracks []LastFMTrack, totalImported *int) error {
	plays := make([]Play, len(tracks))
	for i, t := range tracks {
		plays[i] = Play{UserId: t.UserId, Timestamp: t.Timestamp, SongName: t.SongName, Artist: t.Artist, Album: t.Album}
	}
	inserted, err := insertPlays(plays, "lastfm", 0)
	*totalImported += inserted
	return err
}
