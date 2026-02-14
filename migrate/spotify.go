package migrate

// Spotify import functionality for migrating Spotify listening history
// from JSON export files into the database

// This file handles:
// - Parsing Spotify JSON track data
// - Batch processing with deduplication (20-second window)
// - Efficient bulk inserts using pgx.CopyFrom

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"muzi/db"

	"github.com/jackc/pgx/v5"
)

const (
	batchSize   = 1000
	minPlayTime = 20000 // 20000 ms = 20 sec
	timeDiff    = 20 * time.Second
)

// Represents a single listening event from Spotify's JSON export format
type SpotifyTrack struct {
	Timestamp time.Time `json:"ts"`
	Played    int       `json:"ms_played"`
	Name      string    `json:"master_metadata_track_name"`
	Artist    string    `json:"master_metadata_album_artist_name"`
	Album     string    `json:"master_metadata_album_album_name"`
}

// Implements pgx.CopyFromSource for efficient bulk inserts.
// Filters out duplicates in-memory before sending to PostgreSQL
type trackSource struct {
	tracks       []SpotifyTrack      // Full batch of tracks to process
	tracksToSkip map[string]struct{} // Set of duplicate keys to skip
	idx          int                 // Current position in tracks slice
	userId       int                 // User ID to associate with imported tracks
}

// Represents a track already stored in the database, used for duplicate
// detection during import
type dbTrack struct {
	Timestamp time.Time
	SongName  string
	Artist    string
}

// Import Spotify listening history into the database.
// Processes tracks in batches of 1000 (default), filters out tracks played <
// 20 seconds, deduplicates against existing data, and sends progress updates
// via progressChan.
// The progressChan must not be closed by the caller. The receiver should
// stop reading when Status is "completed". This avoids panics from
// sending on a closed channel.

func ImportSpotify(tracks []SpotifyTrack,
	userId int, progressChan chan ProgressUpdate,
) {
	totalImported := 0
	totalTracks := len(tracks)
	batchStart := 0
	totalBatches := (totalTracks + batchSize - 1) / batchSize

	// Send initial progress update
	sendProgressUpdate(progressChan, 0, 0, totalBatches, totalImported, "running")

	for batchStart < totalTracks {
		// Cap batchEnd at total track count on final batch to prevent OOB error
		batchEnd := min(batchStart+batchSize, totalTracks)
		currentBatch := (batchStart / batchSize) + 1

		var validTracks []SpotifyTrack
		for i := batchStart; i < batchEnd; i++ {
			if tracks[i].Played >= minPlayTime &&
				tracks[i].Name != "" &&
				tracks[i].Artist != "" {
				validTracks = append(validTracks, tracks[i])
			}
		}

		if len(validTracks) == 0 {
			batchStart += batchSize
			// Send progress update even for empty batches
			sendProgressUpdate(
				progressChan,
				currentBatch,
				currentBatch,
				totalBatches,
				totalImported,
				"running",
			)
			continue
		}

		tracksToSkip, err := getDupes(userId, validTracks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking existing tracks: %v\n", err)
			batchStart += batchSize
			continue
		}

		src := &trackSource{
			tracks:       validTracks,
			tracksToSkip: tracksToSkip,
			idx:          0,
			userId:       userId,
		}

		copyCount, err := db.Pool.CopyFrom(
			context.Background(),
			pgx.Identifier{"history"},
			[]string{
				"user_id",
				"timestamp",
				"song_name",
				"artist",
				"album_name",
				"ms_played",
				"platform",
			},
			src,
		)
		// Do not log errors that come from adding duplicate songs
		if err != nil {
			if !strings.Contains(err.Error(), "duplicate") {
				fmt.Fprintf(os.Stderr, "Spotify batch insert failed: %v\n", err)
			}
		} else {
			totalImported += int(copyCount)
		}

		sendProgressUpdate(
			progressChan,
			currentBatch,
			currentBatch,
			totalBatches,
			totalImported,
			"running",
		)

		batchStart += batchSize
	}

	sendProgressUpdate(
		progressChan,
		totalBatches,
		totalBatches,
		totalBatches,
		totalImported,
		"completed",
	)
}

// Sends a progress update to the channel if it's not nil.
// To avoid panics from sending on a closed channel, the channel
// must never be closed by the receiver. The receiver should stop reading when
// Status reads "completed".
func sendProgressUpdate(
	ch chan ProgressUpdate,
	current, completed, total, imported int,
	status string,
) {
	if ch != nil {
		ch <- ProgressUpdate{
			CurrentPage:    current,
			CompletedPages: completed,
			TotalPages:     total,
			TracksImported: imported,
			Status:         status,
		}
	}
}

// Finds tracks that already exist in the database or are duplicates within the
// current batch, using a 20-second window to handle minor timestamp variations
func getDupes(userId int, tracks []SpotifyTrack) (map[string]struct{}, error) {
	minTs, maxTs := findTimeRange(tracks)
	if minTs.IsZero() {
		return map[string]struct{}{}, nil
	}

	dbTracks, err := fetchDbTracks(userId, minTs, maxTs)
	if err != nil {
		return nil, err
	}

	dbIndex := buildDbTrackIndex(dbTracks)
	duplicates := make(map[string]struct{})
	seenInBatch := make(map[string]struct{})

	for _, track := range tracks {
		trackKey := createTrackKey(track)

		// Check in batch
		if _, seen := seenInBatch[trackKey]; seen {
			duplicates[trackKey] = struct{}{}
			continue
		}
		seenInBatch[trackKey] = struct{}{}

		// Check in DB
		lookupKey := fmt.Sprintf("%s|%s", track.Artist, track.Name)
		if dbTimestamps, found := dbIndex[lookupKey]; found {
			if isDuplicateWithinWindow(track, dbTimestamps) {
				duplicates[trackKey] = struct{}{}
			}
		}
	}

	return duplicates, nil
}

// Get the min/max timestamp range for a batch of tracks
func findTimeRange(tracks []SpotifyTrack) (time.Time, time.Time) {
	var minTs, maxTs time.Time
	for _, t := range tracks {
		if minTs.IsZero() || t.Timestamp.Before(minTs) {
			minTs = t.Timestamp
		}
		if t.Timestamp.After(maxTs) {
			maxTs = t.Timestamp
		}
	}
	return minTs, maxTs
}

// Get all tracks in the database for a user that have the same timestamp
// range as the current batch
func fetchDbTracks(userId int, minTs, maxTs time.Time) ([]dbTrack, error) {
	rows, err := db.Pool.Query(context.Background(),
		`SELECT song_name, artist, timestamp 
		 FROM history 
		 WHERE user_id = $1 
		 AND timestamp BETWEEN $2 AND $3`,
		userId,
		// Adjust 20 seconds to find duplicates on edges of batch
		minTs.Add(-timeDiff),
		maxTs.Add(timeDiff))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbTracks []dbTrack
	for rows.Next() {
		var t dbTrack
		if err := rows.Scan(&t.SongName, &t.Artist, &t.Timestamp); err != nil {
			continue
		}
		dbTracks = append(dbTracks, t)
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}
	return dbTracks, nil
}

// Create a lookup map from Artist|Name to timestamps for efficient duplicate
// detection.
func buildDbTrackIndex(tracks []dbTrack) map[string][]time.Time {
	index := make(map[string][]time.Time)
	for _, t := range tracks {
		key := t.Artist + "|" + t.SongName
		index[key] = append(index[key], t.Timestamp)
	}
	return index
}

// Generate a unique identifier for a track using artist, name, and
// normalized timestamp.
func createTrackKey(track SpotifyTrack) string {
	ts := track.Timestamp.Format(time.RFC3339Nano)
	return fmt.Sprintf("%s|%s|%s", track.Artist, track.Name, ts)
}

// Check if a track timestamp falls < 20 seconds of another
func isDuplicateWithinWindow(track SpotifyTrack,
	existingTimestamps []time.Time,
) bool {
	for _, existingTime := range existingTimestamps {
		diff := track.Timestamp.Sub(existingTime)
		if diff < 0 {
			diff = -diff
		}
		if diff < timeDiff {
			return true
		}
	}
	return false
}

// Advances to the next valid track, skipping duplicates and invalid timestamps.
// Returns false when all tracks have been processed
func (s *trackSource) Next() bool {
	for s.idx < len(s.tracks) {
		t := s.tracks[s.idx]
		key := createTrackKey(t)
		if _, shouldSkip := s.tracksToSkip[key]; shouldSkip {
			s.idx++
			continue
		}
		s.idx++
		return true
	}
	return false
}

// Returns the current track's data formatted for database insertion.
// Must only be called after Next() returns true
func (s *trackSource) Values() ([]any, error) {
	// idx is already incremented in Next(), so use idx-1
	t := s.tracks[s.idx-1]
	return []any{
		s.userId,
		t.Timestamp,
		t.Name,
		t.Artist,
		t.Album,
		t.Played,
		"spotify",
	}, nil
}

// Returns any error encountered during iteration.
// Currently always returns nil as errors are logged in Next()
func (s *trackSource) Err() error {
	return nil
}

// Implements custom JSON unmarshaling to parse the timestamp
func (s *SpotifyTrack) UnmarshalJSON(data []byte) error {
	type Alias SpotifyTrack
	aux := &struct {
		Timestamp string `json:"ts"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	ts, err := time.Parse(time.RFC3339Nano, aux.Timestamp)
	if err != nil {
		return fmt.Errorf("parsing timestamp: %w", err)
	}
	s.Timestamp = ts
	return nil
}
