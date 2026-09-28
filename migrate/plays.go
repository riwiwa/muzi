package migrate

import (
	"context"
	"fmt"
	"os"
	"time"

	"muzi/db"

	"github.com/jackc/pgx/v5"
)

// A play to add to history, from any import source
type Play struct {
	UserId    int
	Timestamp time.Time
	SongName  string
	Artist    string
	Album     string
	MsPlayed  int
}

// Inserts plays into history and returns how many were added. Plays already in history are
// skipped individually (COPY alone can't, and one conflict would fail the whole batch, so the
// plays go through a temp table). With nearDuplicates > 0, plays within that long of an existing
// play of the same song and artist are skipped too, for sources whose plays may have been
// scrobbled elsewhere with slightly different timestamps.
func insertPlays(plays []Play, platform string, nearDuplicates time.Duration) (int, error) {
	if len(plays) == 0 {
		return 0, nil
	}

	artistIdMap, err := resolvePlayArtistIds(plays)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving artist IDs: %v\n", err)
		return 0, err
	}

	rows := make([][]any, 0, len(plays))
	for _, p := range plays {
		var artistIds []int
		for _, name := range parseArtistString(p.Artist) {
			if ids, ok := artistIdMap[name]; ok {
				artistIds = append(artistIds, ids...)
			}
		}
		// NULL rather than 0, which would violate the artists foreign key
		var primaryArtistId any
		if len(artistIds) > 0 {
			primaryArtistId = artistIds[0]
		}
		rows = append(rows, []any{
			p.UserId, p.Timestamp, p.SongName, p.Artist,
			p.Album, p.MsPlayed, platform, primaryArtistId, artistIds,
		})
	}

	ctx := context.Background()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`CREATE TEMP TABLE import_plays (
			user_id INTEGER, timestamp TIMESTAMPTZ, song_name TEXT, artist TEXT, album_name TEXT,
			ms_played INTEGER, platform TEXT, artist_id INTEGER, artist_ids INTEGER[]
		) ON COMMIT DROP`)
	if err != nil {
		return 0, err
	}

	columns := []string{
		"user_id", "timestamp", "song_name", "artist", "album_name",
		"ms_played", "platform", "artist_id", "artist_ids",
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"import_plays"}, columns, pgx.CopyFromRows(rows)); err != nil {
		return 0, err
	}

	// the time-range condition comes first so the (user_id, timestamp) index narrows the search
	tag, err := tx.Exec(ctx,
		`INSERT INTO history (user_id, timestamp, song_name, artist, album_name,
			ms_played, platform, artist_id, artist_ids)
		SELECT user_id, timestamp, song_name, artist, album_name, ms_played, platform, artist_id, artist_ids
		FROM import_plays t
		WHERE $1::float8 <= 0 OR NOT EXISTS (
			SELECT 1 FROM history h
			WHERE h.user_id = t.user_id
				AND h.timestamp BETWEEN t.timestamp - make_interval(secs => $1) AND t.timestamp + make_interval(secs => $1)
				AND lower(h.song_name) = lower(t.song_name) AND lower(h.artist) = lower(t.artist)
		)
		ON CONFLICT (user_id, song_name, artist, timestamp) DO NOTHING`,
		nearDuplicates.Seconds())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func resolvePlayArtistIds(plays []Play) (map[string][]int, error) {
	artistIdMap := make(map[string][]int)
	for _, p := range plays {
		for _, name := range parseArtistString(p.Artist) {
			if _, exists := artistIdMap[name]; !exists {
				artistId, _, err := db.GetOrCreateArtist(p.UserId, name)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating artist %s: %v\n", name, err)
					continue
				}
				artistIdMap[name] = []int{artistId}
			}
		}
	}
	return artistIdMap, nil
}
