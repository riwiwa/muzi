package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"muzi/db"

	"github.com/jackc/pgx/v5"
)

const batchSize = 1000

type SpotifyTrack struct {
	Timestamp string `json:"ts"`
	Played    int    `json:"ms_played"`
	Name      string `json:"master_metadata_track_name"`
	Artist    string `json:"master_metadata_album_artist_name"`
	Album     string `json:"master_metadata_album_album_name"`
}

type trackSource struct {
	tracks       []SpotifyTrack
	tracksToSkip map[string]struct{}
	idx          int
	userId       int
}

type dbTrack struct {
	Timestamp time.Time
	SongName  string
	Artist    string
}

func (s *trackSource) Next() bool {
	for s.idx < len(s.tracks) {
		t := s.tracks[s.idx]
		ts, err := normalizeTs(t.Timestamp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error normalizing timestamp: %v\n", err)
			s.idx++
			continue
		}
		key := fmt.Sprintf("%s|%s|%s", t.Artist, t.Name, ts)
		if _, shouldSkip := s.tracksToSkip[key]; shouldSkip {
			s.idx++
			continue
		}
		s.idx++
		return true
	}
	return false
}

func (s *trackSource) Values() ([]any, error) {
	// idx is already incremented in Next(), so use idx-1
	t := s.tracks[s.idx-1]
	ts, err := time.Parse(time.RFC3339Nano, t.Timestamp)
	if err != nil {
		return nil, err
	}
	return []any{
		s.userId,
		ts,
		t.Name,
		t.Artist,
		t.Album,
		t.Played,
		"spotify",
	}, nil
}

func (s *trackSource) Err() error {
	return nil
}

func normalizeTs(ts string) (string, error) {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return "", err
	}
	return t.Format(time.RFC3339Nano), nil
}

func getExistingTracks(
	userId int, tracks []SpotifyTrack,
) (map[string]struct{}, error) {
	minTs, maxTs := findTimeRange(tracks)
	if minTs.IsZero() {
		return map[string]struct{}{}, nil
	}

	dbTracks, err := fetchDbTracks(userId, minTs, maxTs)
	if err != nil {
		return nil, err
	}

	dbIndex := buildDbTrackIndex(dbTracks)

	return findDuplicates(tracks, dbIndex), nil
}

// get the min/max timestamp range for a batch of tracks
func findTimeRange(tracks []SpotifyTrack) (time.Time, time.Time) {
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
	return minTs, maxTs
}

/*
	 get all tracks in the database for a user that have the same timestamp
		range as the current batch
*/
func fetchDbTracks(userId int, minTs, maxTs time.Time) ([]dbTrack, error) {
	rows, err := db.Pool.Query(context.Background(),
		`SELECT song_name, artist, timestamp 
		 FROM history 
		 WHERE user_id = $1 
		 AND timestamp BETWEEN $2 AND $3`,
		userId,
		// adjust 20 seconds to find duplicates on edges of batch
		minTs.Add(-20*time.Second),
		maxTs.Add(20*time.Second))
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
	return dbTracks, nil
}

func buildDbTrackIndex(tracks []dbTrack) map[string][]time.Time {
	index := make(map[string][]time.Time)
	for _, t := range tracks {
		key := t.Artist + "|" + t.SongName
		index[key] = append(index[key], t.Timestamp)
	}
	return index
}

func findDuplicates(tracks []SpotifyTrack, dbIndex map[string][]time.Time) map[string]struct{} {
	duplicates := make(map[string]struct{})
	seenInBatch := make(map[string]struct{})

	for _, track := range tracks {
		trackKey, err := createTrackKey(track)
		if err != nil {
			continue
		}

		// in batch check
		if _, seen := seenInBatch[trackKey]; seen {
			duplicates[trackKey] = struct{}{}
			continue
		}
		seenInBatch[trackKey] = struct{}{}

		// in db check
		lookupKey := fmt.Sprintf("%s|%s", track.Artist, track.Name)
		if dbTimestamps, found := dbIndex[lookupKey]; found {
			if isDuplicateWithinWindow(track, dbTimestamps) {
				duplicates[trackKey] = struct{}{}
			}
		}
	}

	return duplicates
}

func createTrackKey(track SpotifyTrack) (string, error) {
	ts, err := normalizeTs(track.Timestamp)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s|%s|%s", track.Artist, track.Name, ts), nil
}

// check if a track timestamp falls < 20 seconds of another
func isDuplicateWithinWindow(track SpotifyTrack, existingTimestamps []time.Time) bool {
	trackTime, err := time.Parse(time.RFC3339Nano, track.Timestamp)
	if err != nil {
		return false
	}
	for _, existingTime := range existingTimestamps {
		diff := trackTime.Sub(existingTime)
		if diff < 0 {
			diff = -diff
		}
		if diff < 20*time.Second {
			return true
		}
	}
	return false
}

func ImportSpotify(tracks []SpotifyTrack, userId int, progressChan chan ProgressUpdate) error {
	totalImported := 0
	totalTracks := len(tracks)
	batchStart := 0
	totalBatches := (totalTracks + batchSize - 1) / batchSize

	// Send initial progress update
	if progressChan != nil {
		progressChan <- ProgressUpdate{
			TotalPages: totalBatches,
			Status:     "running",
		}
	}

	for batchStart < totalTracks {
		// cap batchEnd at total track count on final batch to prevent OOB error
		batchEnd := min(batchStart+batchSize, totalTracks)
		currentBatch := (batchStart / batchSize) + 1

		var validTracks []SpotifyTrack
		for i := batchStart; i < batchEnd; i++ {
			if tracks[i].Played >= 20000 && // 20 seconds
				tracks[i].Name != "" &&
				tracks[i].Artist != "" {
				validTracks = append(validTracks, tracks[i])
			}
		}

		if len(validTracks) == 0 {
			batchStart += batchSize
			// Send progress update even for empty batches
			if progressChan != nil {
				progressChan <- ProgressUpdate{
					CurrentPage:    currentBatch,
					CompletedPages: currentBatch,
					TotalPages:     totalBatches,
					TracksImported: totalImported,
					Status:         "running",
				}
			}
			continue
		}

		tracksToSkip, err := getExistingTracks(userId, validTracks)
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
		if err != nil {
			if !strings.Contains(err.Error(), "duplicate") {
				fmt.Fprintf(os.Stderr, "Spotify batch insert failed: %v\n", err)
			}
		} else {
			totalImported += int(copyCount)
		}

		// Send progress update
		if progressChan != nil {
			progressChan <- ProgressUpdate{
				CurrentPage:    currentBatch,
				CompletedPages: currentBatch,
				TotalPages:     totalBatches,
				TracksImported: totalImported,
				Status:         "running",
			}
		}

		batchStart += batchSize
	}

	// Send completion update
	if progressChan != nil {
		progressChan <- ProgressUpdate{
			CurrentPage:    totalBatches,
			CompletedPages: totalBatches,
			TotalPages:     totalBatches,
			TracksImported: totalImported,
			Status:         "completed",
		}
	}

	return nil
}
