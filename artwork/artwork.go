// Package artwork fills in missing artist, album and song images from Spotify (when the user has
// Spotify credentials configured) and Deezer. Images set by users (image_source = 'custom') are never
// replaced; only rows with no image are looked up.
package artwork

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"muzi/config"
	"muzi/db"
)

const batchSize = 25

// Wait between lookups; also keeps well under Deezer's 50 requests / 5 seconds limit
const requestInterval = 250 * time.Millisecond

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

// Compares names ignoring case, punctuation and spacing
func sameName(a, b string) bool {
	return normalize(a) == normalize(b)
}

func normalize(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	// names made only of symbols (e.g. "!!!") would otherwise all match each other
	if sb.Len() == 0 {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return sb.String()
}

// Tries Spotify first, falling back to Deezer. A Deezer error is returned so the row is retried soon
// rather than recorded as having no image.
func lookup(
	spotifyFn func() (string, string, error),
	deezerFn func() (string, error),
) (imageUrl, source, spotifyId string, err error) {
	imageUrl, spotifyId, err = spotifyFn()
	if err == nil && imageUrl != "" {
		return imageUrl, "spotify", spotifyId, nil
	}
	if err != nil && !errors.Is(err, errNoSpotify) {
		fmt.Fprintf(os.Stderr, "Spotify image lookup failed, trying Deezer: %v\n", err)
	}

	imageUrl, err = deezerFn()
	if err != nil {
		return "", "", "", err
	}
	if imageUrl != "" {
		return imageUrl, "deezer", spotifyId, nil
	}
	return "", "", spotifyId, nil
}

type pending struct {
	id        int
	userId    int
	name      string
	artist    string
	spotifyId string
}

func fetchBatch() (int, error) {
	total := 0

	artists, err := queryPending(
		`SELECT id, user_id, name, '', COALESCE(spotify_id, '') FROM artists
		WHERE image_url IS NULL AND (image_fetched_at IS NULL OR image_fetched_at < now() - ` + retryAfter + `)
		ORDER BY image_fetched_at NULLS FIRST, id LIMIT $1`)
	if err != nil {
		return total, err
	}
	for _, p := range artists {
		if err := fetchArtist(p); err != nil {
			return total, err
		}
		total++
	}

	albums, err := queryPending(
		`SELECT al.id, al.user_id, al.title, COALESCE(ar.name, ''), COALESCE(al.spotify_id, '')
		FROM albums al LEFT JOIN artists ar ON ar.id = al.artist_id
		WHERE al.cover_url IS NULL AND (al.cover_fetched_at IS NULL OR al.cover_fetched_at < now() - ` + retryAfter + `)
		ORDER BY al.cover_fetched_at NULLS FIRST, al.id LIMIT $1`)
	if err != nil {
		return total, err
	}
	for _, p := range albums {
		if err := fetchAlbum(p); err != nil {
			return total, err
		}
		total++
	}

	// Songs normally show their album's cover, so only look them up once their album has none
	songs, err := queryPending(
		`SELECT s.id, s.user_id, s.title, COALESCE(ar.name, ''), COALESCE(s.spotify_id, '')
		FROM songs s
		LEFT JOIN artists ar ON ar.id = s.artist_id
		LEFT JOIN albums al ON al.id = s.album_id
		WHERE s.image_url IS NULL AND (s.image_fetched_at IS NULL OR s.image_fetched_at < now() - ` + retryAfter + `)
			AND (al.id IS NULL OR (al.cover_url IS NULL AND al.cover_fetched_at IS NOT NULL))
		ORDER BY s.image_fetched_at NULLS FIRST, s.id LIMIT $1`)
	if err != nil {
		return total, err
	}
	for _, p := range songs {
		if err := fetchSong(p); err != nil {
			return total, err
		}
		total++
	}

	return total, nil
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

// Records a failed lookup so a row that always errors can't block the rest of the queue
func markAttempted(table, column string, id int) {
	_, err := db.Pool.Exec(context.Background(),
		"UPDATE "+table+" SET "+column+" = now() WHERE id = $1", id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error recording image lookup: %v\n", err)
	}
}

// The image_url IS NULL guards keep a fetched image from overwriting one a user set mid-lookup

func fetchArtist(p pending) error {
	imageUrl, source, spotifyId, err := lookup(
		func() (string, string, error) { return spotifyArtistImage(p.userId, p.spotifyId, p.name) },
		func() (string, error) { return deezerArtistImage(p.name) },
	)
	if err != nil {
		markAttempted("artists", "image_fetched_at", p.id)
		return err
	}
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE artists SET image_url = NULLIF($2, ''), image_source = NULLIF($3, ''), image_fetched_at = now(),
			spotify_id = COALESCE(spotify_id, NULLIF($4, ''))
		WHERE id = $1 AND image_url IS NULL`,
		p.id, imageUrl, source, spotifyId)
	return err
}

func fetchAlbum(p pending) error {
	imageUrl, source, spotifyId, err := lookup(
		func() (string, string, error) { return spotifyAlbumImage(p.userId, p.spotifyId, p.name, p.artist) },
		func() (string, error) { return deezerAlbumImage(p.name, p.artist) },
	)
	if err != nil {
		markAttempted("albums", "cover_fetched_at", p.id)
		return err
	}
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE albums SET cover_url = NULLIF($2, ''), cover_source = NULLIF($3, ''), cover_fetched_at = now(),
			spotify_id = COALESCE(spotify_id, NULLIF($4, ''))
		WHERE id = $1 AND cover_url IS NULL`,
		p.id, imageUrl, source, spotifyId)
	return err
}

func fetchSong(p pending) error {
	imageUrl, source, spotifyId, err := lookup(
		func() (string, string, error) { return spotifySongImage(p.userId, p.spotifyId, p.name, p.artist) },
		func() (string, error) { return deezerSongImage(p.name, p.artist) },
	)
	if err != nil {
		markAttempted("songs", "image_fetched_at", p.id)
		return err
	}
	_, err = db.Pool.Exec(context.Background(),
		`UPDATE songs SET image_url = NULLIF($2, ''), image_source = NULLIF($3, ''), image_fetched_at = now(),
			spotify_id = COALESCE(spotify_id, NULLIF($4, ''))
		WHERE id = $1 AND image_url IS NULL`,
		p.id, imageUrl, source, spotifyId)
	return err
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
