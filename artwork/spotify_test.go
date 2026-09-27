package artwork

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSpotifyArtistImageRetriesAfterRateLimit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing bearer token")
		}
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"artists": {"items": [
			{"id": "wrong", "name": "Goo Goo Dolls", "images": []},
			{"id": "gg", "name": "The Goo Goo Dolls", "images": [
				{"url": "https://i.scdn.co/big", "width": 640}, {"url": "https://i.scdn.co/small", "width": 64}
			]}
		]}}`))
	}))
	defer srv.Close()

	oldURL := spotifyAPIURL
	spotifyAPIURL = srv.URL
	defer func() { spotifyAPIURL = oldURL }()
	tokens[1] = appToken{accessToken: "test-token", expiresAt: time.Now().Add(time.Hour)}
	defer delete(tokens, 1)

	start := time.Now()
	img, id, err := spotifyArtistImage(1, "", "Goo Goo Dolls")
	if err != nil {
		t.Fatal(err)
	}
	if img != "https://i.scdn.co/big" || id != "gg" {
		t.Errorf("got (%q, %q), want the largest image of the artist with a picture", img, id)
	}
	if calls.Load() != 2 {
		t.Errorf("expected a retry after the 429, got %d calls", calls.Load())
	}
	if time.Since(start) < time.Second {
		t.Errorf("did not wait for Retry-After")
	}
}

func TestLookupPrefersSpotify(t *testing.T) {
	deezerCalled := false
	r, err := lookup(
		func() (string, string, error) { return "spotify-img", "sid", nil },
		func() (string, error) { deezerCalled = true; return "deezer-img", nil },
	)
	if err != nil || r.imageUrl != "spotify-img" || r.source != "spotify" || !r.spotifyChecked || deezerCalled {
		t.Errorf("unexpected result %+v, deezer called: %v", r, deezerCalled)
	}

	// Spotify answered with nothing: fall back to Deezer, but remember Spotify was checked
	r, _ = lookup(
		func() (string, string, error) { return "", "", nil },
		func() (string, error) { return "deezer-img", nil },
	)
	if r.source != "deezer" || !r.spotifyChecked {
		t.Errorf("unexpected fallback result %+v", r)
	}

	// Spotify not configured: Deezer, and not marked as checked so it's upgraded later
	r, _ = lookup(
		func() (string, string, error) { return "", "", errNoSpotify },
		func() (string, error) { return "deezer-img", nil },
	)
	if r.source != "deezer" || r.spotifyChecked {
		t.Errorf("unexpected no-spotify result %+v", r)
	}
}
