// Package artwork fills in missing artist, album and song images from Spotify (when the user has
// Spotify credentials configured) and Deezer. Spotify images are preferred: Deezer images are replaced
// once the user adds Spotify credentials. Images set by users (image_source = 'custom') are never replaced.
package artwork

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"muzi/config"
	"muzi/db"
)

const batchSize = 25

// Minimum gap between requests; keeps under Deezer's 50 requests / 5 seconds limit
const requestInterval = 125 * time.Millisecond

// Lookups run concurrently so request latency doesn't limit throughput
const workers = 4

// How long to sleep when there's nothing left to fetch or a provider is failing
const idleInterval = 5 * time.Minute

// Rows where no image was found are looked up again after this long
const retryAfter = "interval '7 days'"

var (
	httpClient = &http.Client{Timeout: 15 * time.Second}
	ticker     = time.NewTicker(requestInterval)
)

func throttle() {
	<-ticker.C
}

// Starts the background image fetcher
func Start() {
	if !config.Get().Images.AutoFetch {
		return
	}
	go func() {
		for {
			n, err := fetchBatch()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching images: %v\n", err)
			}
			if n == 0 || err != nil {
				time.Sleep(idleInterval)
			}
		}
	}()
}

type result struct {
	imageUrl       string
	source         string
	spotifyId      string
	spotifyChecked bool // Spotify answered, whether or not it had an image
}

// Tries Spotify first, falling back to Deezer. A Deezer error is returned so the row is retried soon
// rather than recorded as having no image.
func lookup(spotifyFn func() (string, string, error), deezerFn func() (string, error)) (result, error) {
	var r result
	imageUrl, spotifyId, err := spotifyFn()
	r.spotifyChecked = err == nil
	r.spotifyId = spotifyId
	if err == nil && imageUrl != "" {
		r.imageUrl, r.source = imageUrl, "spotify"
		return r, nil
	}
	if err != nil && !errors.Is(err, errNoSpotify) {
		fmt.Fprintf(os.Stderr, "Spotify image lookup failed, trying Deezer: %v\n", err)
	}

	imageUrl, err = deezerFn()
	if err != nil {
		return result{}, err
	}
	if imageUrl != "" {
		r.imageUrl, r.source = imageUrl, "deezer"
	}
	return r, nil
}

// Column names for one kind of entity's image
type imageColumns struct {
	table, url, source, fetchedAt, spotifyChecked string
}

var (
	artistColumns = imageColumns{"artists", "image_url", "image_source", "image_fetched_at", "image_spotify_checked"}
	albumColumns  = imageColumns{"albums", "cover_url", "cover_source", "cover_fetched_at", "cover_spotify_checked"}
	songColumns   = imageColumns{"songs", "image_url", "image_source", "image_fetched_at", "image_spotify_checked"}
)

// Rows to look up: no image yet (retrying misses after retryAfter), or a non-Spotify image that can be
// upgraded now that the user has Spotify credentials. Custom and Spotify images are left alone.
// Expects the entity aliased as e and its user as u.
func (c imageColumns) pendingCondition() string {
	return fmt.Sprintf(`e.%[2]s IS DISTINCT FROM 'custom' AND e.%[2]s IS DISTINCT FROM 'spotify' AND (
			(e.%[1]s IS NULL AND (e.%[3]s IS NULL OR e.%[3]s < now() - %[5]s))
			OR (NOT e.%[4]s AND NULLIF(u.spotify_client_id, '') IS NOT NULL
				AND NULLIF(u.spotify_client_secret, '') IS NOT NULL AND e.%[3]s < now() - interval '1 hour')
		)`, c.url, c.source, c.fetchedAt, c.spotifyChecked, retryAfter)
}

// Saves a lookup result. An empty result keeps any existing (e.g. Deezer) image rather than clearing it,
// and a custom image set during the lookup is never overwritten.
func (c imageColumns) save(id int, r result) error {
	_, err := db.Pool.Exec(context.Background(),
		fmt.Sprintf(`UPDATE %[1]s SET
			%[2]s = COALESCE(NULLIF($2, ''), %[2]s),
			%[3]s = CASE WHEN $2 <> '' THEN $3 ELSE %[3]s END,
			%[4]s = now(),
			%[5]s = %[5]s OR $4,
			spotify_id = COALESCE(spotify_id, NULLIF($5, ''))
		WHERE id = $1 AND %[3]s IS DISTINCT FROM 'custom'`,
			c.table, c.url, c.source, c.fetchedAt, c.spotifyChecked),
		id, r.imageUrl, r.source, r.spotifyChecked, r.spotifyId)
	return err
}

// Records a failed lookup so a row that always errors can't block the rest of the queue.
// It's backdated so the row is retried in about an hour rather than after the full retryAfter.
func (c imageColumns) markAttempted(id int) {
	_, err := db.Pool.Exec(context.Background(),
		"UPDATE "+c.table+" SET "+c.fetchedAt+" = now() - "+retryAfter+" + interval '1 hour' WHERE id = $1", id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error recording image lookup: %v\n", err)
	}
}

type pending struct {
	id        int
	userId    int
	name      string
	artist    string
	spotifyId string
}

// Each batch takes the most-played items without images first, so what's on profiles fills in first
func fetchBatch() (int, error) {
	artists, err := queryPending(
		`SELECT e.id, e.user_id, e.name, '', COALESCE(e.spotify_id, '')
		FROM artists e
		JOIN users u ON u.pk = e.user_id
		LEFT JOIN (SELECT artist_id, COUNT(*) AS plays FROM history GROUP BY artist_id) p ON p.artist_id = e.id
		WHERE ` + artistColumns.pendingCondition() + `
		ORDER BY e.image_fetched_at IS NOT NULL, p.plays DESC NULLS LAST, e.id LIMIT $1`)
	if err != nil {
		return 0, err
	}

	albums, err := queryPending(
		`SELECT e.id, e.user_id, e.title, COALESCE(ar.name, ''), COALESCE(e.spotify_id, '')
		FROM albums e
		JOIN users u ON u.pk = e.user_id
		LEFT JOIN artists ar ON ar.id = e.artist_id
		LEFT JOIN (
			SELECT s.album_id, COUNT(*) AS plays FROM history h JOIN songs s ON s.id = h.song_id GROUP BY s.album_id
		) p ON p.album_id = e.id
		WHERE ` + albumColumns.pendingCondition() + `
		ORDER BY e.cover_fetched_at IS NOT NULL, p.plays DESC NULLS LAST, e.id LIMIT $1`)
	if err != nil {
		return 0, err
	}

	// Songs normally show their album's cover, so only look them up once their album has none
	songs, err := queryPending(
		`SELECT e.id, e.user_id, e.title, COALESCE(ar.name, ''), COALESCE(e.spotify_id, '')
		FROM songs e
		JOIN users u ON u.pk = e.user_id
		LEFT JOIN artists ar ON ar.id = e.artist_id
		LEFT JOIN albums al ON al.id = e.album_id
		LEFT JOIN (SELECT song_id, COUNT(*) AS plays FROM history GROUP BY song_id) p ON p.song_id = e.id
		WHERE ` + songColumns.pendingCondition() + `
			AND (al.id IS NULL OR (al.cover_url IS NULL AND al.cover_fetched_at IS NOT NULL))
		ORDER BY e.image_fetched_at IS NOT NULL, p.plays DESC NULLS LAST, e.id LIMIT $1`)
	if err != nil {
		return 0, err
	}

	var jobs []func() error
	for _, p := range artists {
		jobs = append(jobs, func() error { return fetchArtist(p) })
	}
	for _, p := range albums {
		jobs = append(jobs, func() error { return fetchAlbum(p) })
	}
	for _, p := range songs {
		jobs = append(jobs, func() error { return fetchSong(p) })
	}
	return runJobs(jobs)
}

// Runs lookups a few at a time (requests are still paced by throttle); returns how many ran
// and the first error, if any
func runJobs(jobs []func() error) (int, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	sem := make(chan struct{}, workers)
	for _, job := range jobs {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := job(); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return len(jobs), firstErr
}

func queryPending(query string) ([]pending, error) {
	rows, err := db.Pool.Query(context.Background(), query, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.userId, &p.name, &p.artist, &p.spotifyId); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func fetchArtist(p pending) error {
	r, err := lookup(
		func() (string, string, error) { return spotifyArtistImage(p.userId, p.spotifyId, p.name) },
		func() (string, error) { return deezerArtistImage(p.name) },
	)
	if err != nil {
		artistColumns.markAttempted(p.id)
		return err
	}
	return artistColumns.save(p.id, r)
}

func fetchAlbum(p pending) error {
	r, err := lookup(
		func() (string, string, error) { return spotifyAlbumImage(p.userId, p.spotifyId, p.name, p.artist) },
		func() (string, error) { return deezerAlbumImage(p.name, p.artist) },
	)
	if err != nil {
		albumColumns.markAttempted(p.id)
		return err
	}
	return albumColumns.save(p.id, r)
}

func fetchSong(p pending) error {
	r, err := lookup(
		func() (string, string, error) { return spotifySongImage(p.userId, p.spotifyId, p.name, p.artist) },
		func() (string, error) { return deezerSongImage(p.name, p.artist) },
	)
	if err != nil {
		songColumns.markAttempted(p.id)
		return err
	}
	return songColumns.save(p.id, r)
}

// Immediately looks up an image for one entity ("artist", "album" or "song") that has none,
// e.g. right after a user resets it to automatic
func Refresh(entity string, id int) error {
	var query string
	switch entity {
	case "artist":
		query = `SELECT id, user_id, name, '', COALESCE(spotify_id, '') FROM artists
			WHERE id = $1 AND image_url IS NULL`
	case "album":
		query = `SELECT al.id, al.user_id, al.title, COALESCE(ar.name, ''), COALESCE(al.spotify_id, '')
			FROM albums al LEFT JOIN artists ar ON ar.id = al.artist_id
			WHERE al.id = $1 AND al.cover_url IS NULL`
	case "song":
		query = `SELECT s.id, s.user_id, s.title, COALESCE(ar.name, ''), COALESCE(s.spotify_id, '')
			FROM songs s
			LEFT JOIN artists ar ON ar.id = s.artist_id
			LEFT JOIN albums al ON al.id = s.album_id
			WHERE s.id = $1 AND s.image_url IS NULL AND (al.id IS NULL OR al.cover_url IS NULL)`
	default:
		return fmt.Errorf("unknown entity: %s", entity)
	}

	var p pending
	err := db.Pool.QueryRow(context.Background(), query, id).
		Scan(&p.id, &p.userId, &p.name, &p.artist, &p.spotifyId)
	if err != nil {
		return err
	}

	switch entity {
	case "artist":
		return fetchArtist(p)
	case "album":
		return fetchAlbum(p)
	default:
		return fetchSong(p)
	}
}
