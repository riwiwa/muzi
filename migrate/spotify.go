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

type SpotifyTrack struct {
	Timestamp string `json:"ts"`
	Played    int    `json:"ms_played"`
	Name      string `json:"master_metadata_track_name"`
	Artist    string `json:"master_metadata_album_artist_name"`
	Album     string `json:"master_metadata_album_album_name"`
}

type existingTrack struct {
	Timestamp time.Time
	SongName  string
	Artist    string
}

// trackSource implements pgx.CopyFromSource for efficient bulk inserts
type trackSource struct {
	tracks   []SpotifyTrack
	existing map[string]struct{}
	idx      int
	userId   int
}

func (s *trackSource) Next() bool {
	for s.idx < len(s.tracks) {
		t := s.tracks[s.idx]
		key := fmt.Sprintf("%s|%s|%s", t.Artist, t.Name, t.Timestamp)
		if _, exists := s.existing[key]; exists {
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
	// parse spotify string timestamp to a real time object
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

// find tracks with the same artist and name in a 20 second +- timeframe
func getExistingTracks(
	userId int,
	tracks []SpotifyTrack,
) (map[string]struct{}, error) {
	// check for empty track import
	if len(tracks) == 0 {
		return map[string]struct{}{}, nil
	}

	// find min/max timestamps in this batch to create time window to
	// search for duplicates
	var minTs, maxTs time.Time
	// go through each track (t) in the array
	for _, t := range tracks {
		// parse spotify timestamp into operational time datatype
		ts, err := time.Parse(time.RFC3339Nano, t.Timestamp)
		if err != nil {
			continue
		}
		// if minTs uninitialized or timestamp predates minTs
		if minTs.IsZero() || ts.Before(minTs) {
			minTs = ts
		}
		// if timestamp comes after maxTs
		if ts.After(maxTs) {
			maxTs = ts
		}
	}

	// check if all parses failed, therefore no way to find duplicate by time
	if minTs.IsZero() {
		return map[string]struct{}{}, nil
	}

	// find all tracks within [min-20s, max+20s] window (duplicates)
	rows, err := db.Pool.Query(context.Background(),
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

	// prepare map to hold duplicate track keys
	existing := make(map[string]struct{})
	// create array of tracks
	var existingTracks []existingTrack
	// for each repeat play (-20s +20s)
	for rows.Next() {
		// write the data from json to the track in memory
		var t existingTrack
		if err := rows.Scan(&t.SongName, &t.Artist, &t.Timestamp); err != nil {
			continue
		}
		// add track in memory to existingTracks array
		existingTracks = append(existingTracks, t)
	}

	// index existing tracks by artist|name for O(1) lookup
	existingIndex := make(map[string][]time.Time)
	for _, t := range existingTracks {
		key := t.Artist + "|" + t.SongName
		existingIndex[key] = append(existingIndex[key], t.Timestamp)
	}

	// check each new track against indexed existing tracks
	for _, newTrack := range tracks {
		newTs, err := time.Parse(time.RFC3339Nano, newTrack.Timestamp)
		if err != nil {
			continue
		}

		lookupKey := newTrack.Artist + "|" + newTrack.Name
		if timestamps, found := existingIndex[lookupKey]; found {
			for _, existTs := range timestamps {
				diff := newTs.Sub(existTs)
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
					existing[key] = struct{}{}
					break
				}
			}
		}
	}

	return existing, nil
}

func ImportSpotify(tracks []SpotifyTrack, userId int) error {
	totalImported := 0
	batchSize := 1000

	for batchStart := 0; batchStart < len(tracks); batchStart += batchSize {
		// get the limit of the current batch
		batchEnd := batchStart + batchSize
		// set limit to track array length in current batch too big
		if batchEnd > len(tracks) {
			batchEnd = len(tracks)
		}

		// create array to hold valid listens
		var validTracks []SpotifyTrack
		for i := batchStart; i < batchEnd; i++ {
			// if current track is listened to for 20 sec and name and artist is not
			// blank, add to validTracks array
			if tracks[i].Played >= 20000 && tracks[i].Name != "" && tracks[i].Artist != "" {
				validTracks = append(validTracks, tracks[i])
			}
		}

		// if there are no valid tracks in this batch, go to the next
		if len(validTracks) == 0 {
			continue
		}

		// find replayed tracks in the batch that was just gathered
		existing, err := getExistingTracks(userId, validTracks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking existing tracks: %v\n", err)
			continue
		}

		// get data of struct pointer
		src := &trackSource{
			tracks:   validTracks,
			existing: existing,
			idx:      0,
			userId:   userId,
		}

		// insert all valid tracks from current batch into db
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
	}
	return nil
}
