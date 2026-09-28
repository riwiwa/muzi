package artwork

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"muzi/scrobble"
)

var spotifyTokenURL = "https://accounts.spotify.com/api/token"
var spotifyAPIURL = "https://api.spotify.com/v1"

// Returned when a user hasn't configured Spotify credentials, so lookups skip straight to Deezer
var errNoSpotify = errors.New("spotify not configured")

type spotifyImage struct {
	Url    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type spotifyArtist struct {
	Id     string         `json:"id"`
	Name   string         `json:"name"`
	Images []spotifyImage `json:"images"`
}

type spotifyAlbum struct {
	Id      string          `json:"id"`
	Name    string          `json:"name"`
	Artists []spotifyArtist `json:"artists"`
	Images  []spotifyImage  `json:"images"`
}

type spotifyTrack struct {
	Id      string          `json:"id"`
	Name    string          `json:"name"`
	Artists []spotifyArtist `json:"artists"`
	Album   spotifyAlbum    `json:"album"`
}

type appToken struct {
	accessToken string
	expiresAt   time.Time
}

// App (client credentials) tokens per user; these are separate from the user's playback OAuth token
var (
	tokenMu sync.Mutex
	tokens  = map[int]appToken{}
	// Users whose credentials were rejected, and until when to stop retrying them
	tokenFailures = map[int]time.Time{}
)

// How long to skip Spotify for a user after their credentials are rejected
const tokenFailureBackoff = 10 * time.Minute

func spotifyAppToken(userId int) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	if t, ok := tokens[userId]; ok && time.Now().Before(t.expiresAt) {
		return t.accessToken, nil
	}
	if until, ok := tokenFailures[userId]; ok && time.Now().Before(until) {
		return "", errNoSpotify
	}

	clientId, clientSecret, _, _, _, err := scrobble.GetUserSpotifyCredentials(userId)
	if err != nil || clientId == "" || clientSecret == "" {
		return "", errNoSpotify
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	req, err := http.NewRequest("POST", spotifyTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientId, clientSecret)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tokenFailures[userId] = time.Now().Add(tokenFailureBackoff)
		fmt.Fprintf(os.Stderr, "Spotify rejected client credentials for user %d (status %d); "+
			"using Deezer for images for %v\n", userId, resp.StatusCode, tokenFailureBackoff)
		return "", errNoSpotify
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	tokens[userId] = appToken{
		accessToken: body.AccessToken,
		expiresAt:   time.Now().Add(time.Duration(body.ExpiresIn-60) * time.Second),
	}
	return body.AccessToken, nil
}

// When Spotify rate limits the user's app (429), every Spotify request for that user, including
// playback polling, waits out its Retry-After (see scrobble/ratelimit.go). Waits longer than
// maxSpotifyWait fail instead so lookups fall back to Deezer and retry Spotify later.
const maxSpotifyWait = time.Minute

var errSpotifyLimited = errors.New("spotify rate limited")

func waitForSpotify(userId int) error {
	wait := time.Until(scrobble.SpotifyPausedUntil(userId))
	if wait <= 0 {
		return nil
	}
	if wait > maxSpotifyWait {
		return errSpotifyLimited
	}
	time.Sleep(wait)
	return nil
}

// Image lookups share each user's Spotify quota with playback polling, which matters more, so they
// go slowly: a big upgrade run at a faster pace once earned a multi-hour penalty that also stopped
// Spotify scrobbling.
const spotifyRequestInterval = 2 * time.Second

var spotifyTicker = time.NewTicker(spotifyRequestInterval)

func spotifyGet(userId int, path string, out any) error {
	for attempt := 0; attempt < 3; attempt++ {
		if err := waitForSpotify(userId); err != nil {
			return err
		}
		token, err := spotifyAppToken(userId)
		if err != nil {
			return err
		}
		<-spotifyTicker.C
		req, err := http.NewRequest("GET", spotifyAPIURL+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		switch resp.StatusCode {
		case http.StatusOK:
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(out)
		case http.StatusTooManyRequests:
			scrobble.PauseSpotify(userId, resp.Header.Get("Retry-After"), "image lookups")
			resp.Body.Close()
			continue
		case http.StatusUnauthorized:
			tokenMu.Lock()
			delete(tokens, userId)
			tokenMu.Unlock()
		}
		resp.Body.Close()
		return fmt.Errorf("spotify %s returned %d", path, resp.StatusCode)
	}
	return errSpotifyLimited
}

func spotifySearch(userId int, kind, query string, out any) error {
	return spotifyGet(userId, "/search?limit=10&type="+kind+"&q="+url.QueryEscape(query), out)
}

// Spotify lists images largest first
func largestImage(images []spotifyImage) string {
	if len(images) == 0 {
		return ""
	}
	return images[0].Url
}

func firstArtist(artists []spotifyArtist) string {
	if len(artists) == 0 {
		return ""
	}
	return artists[0].Name
}

// Each lookup uses the entity's stored Spotify ID when there is one, otherwise searches by name.
// The returned id is the matched Spotify ID so it can be saved for next time.

func spotifyArtistImage(userId int, spotifyId, name string) (imageUrl, id string, err error) {
	if spotifyId != "" {
		var a spotifyArtist
		if err := spotifyGet(userId, "/artists/"+url.PathEscape(spotifyId), &a); err != nil {
			return "", "", err
		}
		return largestImage(a.Images), a.Id, nil
	}

	c, err := searchWithFallback(name, "", func(query string) ([]candidate, error) {
		var res struct {
			Artists struct {
				Items []spotifyArtist `json:"items"`
			} `json:"artists"`
		}
		if err := spotifySearch(userId, "artist", query, &res); err != nil {
			return nil, err
		}
		var cands []candidate
		for _, a := range res.Artists.Items {
			cands = append(cands, candidate{title: a.Name, image: largestImage(a.Images), spotifyId: a.Id})
		}
		return cands, nil
	})
	return c.image, c.spotifyId, err
}

func spotifyAlbumImage(userId int, spotifyId, title, artist string) (imageUrl, id string, err error) {
	if spotifyId != "" {
		var a spotifyAlbum
		if err := spotifyGet(userId, "/albums/"+url.PathEscape(spotifyId), &a); err != nil {
			return "", "", err
		}
		return largestImage(a.Images), a.Id, nil
	}

	c, err := searchWithFallback(title, artist, func(query string) ([]candidate, error) {
		var res struct {
			Albums struct {
				Items []spotifyAlbum `json:"items"`
			} `json:"albums"`
		}
		if err := spotifySearch(userId, "album", query, &res); err != nil {
			return nil, err
		}
		var cands []candidate
		for _, a := range res.Albums.Items {
			cands = append(cands, candidate{
				title:     a.Name,
				artist:    firstArtist(a.Artists),
				image:     largestImage(a.Images),
				spotifyId: a.Id,
			})
		}
		return cands, nil
	})
	return c.image, c.spotifyId, err
}

func spotifySongImage(userId int, spotifyId, title, artist string) (imageUrl, id string, err error) {
	if spotifyId != "" {
		var t spotifyTrack
		if err := spotifyGet(userId, "/tracks/"+url.PathEscape(spotifyId), &t); err != nil {
			return "", "", err
		}
		return largestImage(t.Album.Images), t.Id, nil
	}

	c, err := searchWithFallback(title, artist, func(query string) ([]candidate, error) {
		var res struct {
			Tracks struct {
				Items []spotifyTrack `json:"items"`
			} `json:"tracks"`
		}
		if err := spotifySearch(userId, "track", query, &res); err != nil {
			return nil, err
		}
		var cands []candidate
		for _, t := range res.Tracks.Items {
			cands = append(cands, candidate{
				title:     t.Name,
				artist:    firstArtist(t.Artists),
				image:     largestImage(t.Album.Images),
				spotifyId: t.Id,
			})
		}
		return cands, nil
	})
	return c.image, c.spotifyId, err
}
