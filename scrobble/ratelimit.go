package scrobble

// Spotify rate limits each app (client ID), and every muzi user brings their own app. Playback
// polling and image lookups share that quota, so they share one pause per user: when either gets
// a 429, both stop until Spotify's Retry-After has passed. The pause is saved to the database so
// a restart doesn't immediately hit Spotify again and earn a fresh penalty.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"muzi/db"
)

// Used when a 429 doesn't say how long to wait
const defaultSpotifyPause = 30 * time.Second

var (
	spotifyPauseMu sync.Mutex
	spotifyPauses  = map[int]time.Time{}
	pausesLoaded   = map[int]bool{}
)

// When the user's Spotify rate-limit pause ends (zero if there isn't one)
func SpotifyPausedUntil(userId int) time.Time {
	spotifyPauseMu.Lock()
	defer spotifyPauseMu.Unlock()

	if !pausesLoaded[userId] && db.Pool != nil {
		var until *time.Time
		err := db.Pool.QueryRow(context.Background(),
			"SELECT spotify_paused_until FROM users WHERE pk = $1", userId).Scan(&until)
		if err == nil && until != nil {
			spotifyPauses[userId] = *until
		}
		pausesLoaded[userId] = true
	}
	return spotifyPauses[userId]
}

// Whether Spotify is currently rate limiting the user's app
func SpotifyPaused(userId int) bool {
	return time.Now().Before(SpotifyPausedUntil(userId))
}

// Records a 429 from Spotify. retryAfter is the response's Retry-After header (seconds);
// source says what hit the limit, for the log. Logs once per pause, not once per request.
func PauseSpotify(userId int, retryAfter, source string) {
	wait := defaultSpotifyPause
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
		wait = time.Duration(seconds) * time.Second
	}
	until := time.Now().Add(wait)
	wasPaused := SpotifyPaused(userId)

	spotifyPauseMu.Lock()
	extended := until.After(spotifyPauses[userId])
	if extended {
		spotifyPauses[userId] = until
	}
	spotifyPauseMu.Unlock()

	if extended && db.Pool != nil {
		_, err := db.Pool.Exec(context.Background(),
			"UPDATE users SET spotify_paused_until = $1 WHERE pk = $2", until, userId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error saving Spotify rate limit: %v\n", err)
		}
	}
	if !wasPaused {
		fmt.Fprintf(os.Stderr, "Spotify rate limited %s for user %d; pausing all Spotify requests for %v\n",
			source, userId, wait.Round(time.Second))
	}
}
