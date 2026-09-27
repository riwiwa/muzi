package artwork

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"muzi/scrobble"
)

const spotifyTokenURL = "https://accounts.spotify.com/api/token"
const spotifyAPIURL = "https://api.spotify.com/v1"

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
)

func spotifyAppToken(userId int) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	if t, ok := tokens[userId]; ok && time.Now().Before(t.expiresAt) {
		return t.accessToken, nil
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
		return "", fmt.Errorf("spotify token request returned %d", resp.StatusCode)
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

func spotifyGet(userId int, path string, out any) error {
	token, err := spotifyAppToken(userId)
	if err != nil {
		return err
	}
	throttle()
	req, err := http.NewRequest("GET", spotifyAPIURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		tokenMu.Lock()
		delete(tokens, userId)
		tokenMu.Unlock()
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("spotify %s returned %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
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

func hasArtist(artists []spotifyArtist, name string) bool {
	if name == "" {
		return true
	}
	for _, a := range artists {
		if sameName(a.Name, name) {
			return true
		}
	}
	return false
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

	var res struct {
		Artists struct {
			Items []spotifyArtist `json:"items"`
		} `json:"artists"`
	}
	if err := spotifySearch(userId, "artist", name, &res); err != nil {
		return "", "", err
	}
	for _, a := range res.Artists.Items {
		if sameName(a.Name, name) {
			return largestImage(a.Images), a.Id, nil
		}
	}
	return "", "", nil
}

func spotifyAlbumImage(userId int, spotifyId, title, artist string) (imageUrl, id string, err error) {
	if spotifyId != "" {
		var a spotifyAlbum
		if err := spotifyGet(userId, "/albums/"+url.PathEscape(spotifyId), &a); err != nil {
			return "", "", err
		}
		return largestImage(a.Images), a.Id, nil
	}

	var res struct {
		Albums struct {
			Items []spotifyAlbum `json:"items"`
		} `json:"albums"`
	}
	if err := spotifySearch(userId, "album", strings.TrimSpace(title+" "+artist), &res); err != nil {
		return "", "", err
	}
	for _, a := range res.Albums.Items {
		if sameName(a.Name, title) && hasArtist(a.Artists, artist) {
			return largestImage(a.Images), a.Id, nil
		}
	}
	return "", "", nil
}

func spotifySongImage(userId int, spotifyId, title, artist string) (imageUrl, id string, err error) {
	if spotifyId != "" {
		var t spotifyTrack
		if err := spotifyGet(userId, "/tracks/"+url.PathEscape(spotifyId), &t); err != nil {
			return "", "", err
		}
		return largestImage(t.Album.Images), t.Id, nil
	}

	var res struct {
		Tracks struct {
			Items []spotifyTrack `json:"items"`
		} `json:"tracks"`
	}
	if err := spotifySearch(userId, "track", strings.TrimSpace(title+" "+artist), &res); err != nil {
		return "", "", err
	}
	for _, t := range res.Tracks.Items {
		if sameName(t.Name, title) && hasArtist(t.Artists, artist) {
			return largestImage(t.Album.Images), t.Id, nil
		}
	}
	return "", "", nil
}
