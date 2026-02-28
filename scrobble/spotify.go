package scrobble

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"muzi/db"
)

const SpotifyTokenURL = "https://accounts.spotify.com/api/token"
const SpotifyAuthURL = "https://accounts.spotify.com/authorize"
const SpotifyAPIURL = "https://api.spotify.com/v1"

var (
	spotifyClient = &http.Client{Timeout: 30 * time.Second}
	spotifyMu     sync.Mutex
)

type SpotifyHandler struct{}

func NewSpotifyHandler() *SpotifyHandler {
	return &SpotifyHandler{}
}

type SpotifyTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

type SpotifyCurrentlyPlaying struct {
	Timestamp            int64        `json:"timestamp"`
	ProgressMs           int          `json:"progress_ms"`
	Item                 SpotifyTrack `json:"item"`
	CurrentlyPlayingType string       `json:"currently_playing_type"`
	IsPlaying            bool         `json:"is_playing"`
}

type SpotifyTrack struct {
	Id         string          `json:"id"`
	Name       string          `json:"name"`
	DurationMs int             `json:"duration_ms"`
	Artists    []SpotifyArtist `json:"artists"`
	Album      SpotifyAlbum    `json:"album"`
}

type SpotifyArtist struct {
	Name string `json:"name"`
}

type SpotifyAlbum struct {
	Name string `json:"name"`
}

type SpotifyRecentPlays struct {
	Items   []SpotifyPlayItem `json:"items"`
	Cursors SpotifyCursors    `json:"cursors"`
}

type SpotifyPlayItem struct {
	Track    SpotifyTrack `json:"track"`
	PlayedAt string       `json:"played_at"`
}

type SpotifyCursors struct {
	After  string `json:"after"`
	Before string `json:"before"`
}

func (h *SpotifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if path == "/scrobble/spotify/authorize" {
		h.handleAuthorize(w, r)
	} else if path == "/scrobble/spotify/callback" {
		h.handleCallback(w, r)
	} else {
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (h *SpotifyHandler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	userId := r.URL.Query().Get("user_id")
	if userId == "" {
		http.Error(w, "Missing user_id", http.StatusBadRequest)
		return
	}

	clientId, _, _, _, _, err := GetUserSpotifyCredentials(userIdToInt(userId))
	fmt.Fprintf(os.Stderr, "handleAuthorize: userId=%s, clientId='%s', err=%v\n", userId, clientId, err)
	if err != nil || clientId == "" {
		http.Error(w, "Spotify credentials not configured", http.StatusBadRequest)
		return
	}

	baseURL := getBaseURL(r)
	redirectURI := baseURL + "/scrobble/spotify/callback"

	scope := "user-read-currently-playing user-read-recently-played"
	authURL := fmt.Sprintf("%s?client_id=%s&response_type=code&redirect_uri=%s&scope=%s&state=%s",
		SpotifyAuthURL, url.QueryEscape(clientId), url.QueryEscape(redirectURI), url.QueryEscape(scope), userId)

	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (h *SpotifyHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	userId := userIdToInt(state)

	if code == "" || state == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}

	clientId, clientSecret, _, _, _, err := GetUserSpotifyCredentials(userId)
	if err != nil || clientId == "" {
		http.Error(w, "Spotify credentials not configured", http.StatusBadRequest)
		return
	}

	baseURL := getBaseURL(r)
	redirectURI := baseURL + "/scrobble/spotify/callback"

	token, err := exchangeCodeForToken(clientId, clientSecret, code, redirectURI)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error exchanging code for token: %v\n", err)
		http.Error(w, "Failed to authenticate", http.StatusInternalServerError)
		return
	}

	err = UpdateUserSpotifyTokens(userId, token.AccessToken, token.RefreshToken, token.ExpiresIn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error saving Spotify tokens: %v\n", err)
		http.Error(w, "Failed to save credentials", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<html><body><h1>Spotify connected successfully!</h1><p>You can close this window.</p><script>setTimeout(() => window.close(), 2000);</script></body></html>`)
}

func exchangeCodeForToken(clientId, clientSecret, code, redirectURI string) (*SpotifyTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", clientId)
	data.Set("client_secret", clientSecret)

	req, err := http.NewRequest("POST", SpotifyTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := spotifyClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Spotify token exchange failed: %s", string(body))
	}

	var token SpotifyTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}

	return &token, nil
}

func refreshSpotifyToken(clientId, clientSecret, refreshToken string) (*SpotifyTokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientId)
	data.Set("client_secret", clientSecret)

	req, err := http.NewRequest("POST", SpotifyTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := spotifyClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Spotify token refresh failed: %s", string(body))
	}

	var token SpotifyTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}

	return &token, nil
}

func StartSpotifyPoller() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			spotifyMu.Lock()
			users, err := GetUsersWithSpotify()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting users with Spotify: %v\n", err)
				spotifyMu.Unlock()
				continue
			}

			for _, userId := range users {
				err := pollSpotify(userId)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error polling Spotify for user %d: %v\n", userId, err)
				}
			}
			spotifyMu.Unlock()
		}
	}()
}

func pollSpotify(userId int) error {
	clientId, clientSecret, accessToken, refreshToken, expiresAt, err := GetUserSpotifyCredentials(userId)
	if err != nil {
		return err
	}

	if accessToken == "" {
		return fmt.Errorf("no access token")
	}

	if time.Now().After(expiresAt.Add(-60 * time.Second)) {
		token, err := refreshSpotifyToken(clientId, clientSecret, refreshToken)
		if err != nil {
			return err
		}
		accessToken = token.AccessToken
		if token.RefreshToken != "" {
			refreshToken = token.RefreshToken
		}
		UpdateUserSpotifyTokens(userId, accessToken, refreshToken, token.ExpiresIn)
	}

	err = checkCurrentlyPlaying(userId, accessToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking currently playing: %v\n", err)
	}

	err = checkRecentPlays(userId, accessToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking recent plays: %v\n", err)
	}

	UpdateUserSpotifyCheck(userId)

	return nil
}

func checkCurrentlyPlaying(userId int, accessToken string) error {
	req, err := http.NewRequest("GET", SpotifyAPIURL+"/me/player/currently-playing", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := spotifyClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 {
		ClearNowPlaying(userId)
		return nil
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("currently playing returned %d", resp.StatusCode)
	}

	var playing SpotifyCurrentlyPlaying
	if err := json.NewDecoder(resp.Body).Decode(&playing); err != nil {
		return err
	}

	if !playing.IsPlaying || playing.Item.Name == "" {
		ClearNowPlaying(userId)
		return nil
	}

	artistName := ""
	if len(playing.Item.Artists) > 0 {
		artistName = playing.Item.Artists[0].Name
	}

	checkAndScrobbleHalfway(userId, &playing.Item, playing.ProgressMs)

	UpdateNowPlaying(NowPlaying{
		UserId:    userId,
		SongName:  playing.Item.Name,
		Artist:    artistName,
		Album:     playing.Item.Album.Name,
		MsPlayed:  playing.Item.DurationMs,
		Platform:  "spotify",
		UpdatedAt: time.Now(),
	})

	return nil
}

func checkRecentPlays(userId int, accessToken string) error {
	req, err := http.NewRequest("GET", SpotifyAPIURL+"/me/player/recently-played?limit=50", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := spotifyClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("recently played returned %d", resp.StatusCode)
	}

	var recent SpotifyRecentPlays
	if err := json.NewDecoder(resp.Body).Decode(&recent); err != nil {
		return err
	}

	if len(recent.Items) == 0 {
		return nil
	}

	scrobbles := make([]Scrobble, 0, len(recent.Items))
	for _, item := range recent.Items {
		artistName := ""
		if len(item.Track.Artists) > 0 {
			artistName = item.Track.Artists[0].Name
		}

		ts, err := time.Parse(time.RFC3339, item.PlayedAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  -> failed to parse timestamp %s: %v\n", item.PlayedAt, err)
			continue
		}

		scrobbles = append(scrobbles, Scrobble{
			UserId:    userId,
			Timestamp: ts,
			SongName:  item.Track.Name,
			Artist:    artistName,
			Album:     item.Track.Album.Name,
			MsPlayed:  item.Track.DurationMs,
			Platform:  "spotify",
		})
	}

	SaveScrobbles(scrobbles)

	return nil
}

func userIdToInt(s string) int {
	var id int
	fmt.Sscanf(s, "%d", &id)
	return id
}

func getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "localhost:1234" || host == "localhost" {
		host = "127.0.0.1:1234"
	}
	return scheme + "://" + host
}

func GetSpotifyAuthURL(userId int, baseURL string) (string, error) {
	clientId, _, _, _, _, err := GetUserSpotifyCredentials(userId)
	if err != nil || clientId == "" {
		return "", fmt.Errorf("Spotify credentials not configured")
	}

	redirectURI := baseURL + "/scrobble/spotify/callback"
	scope := "user-read-currently-playing user-read-recently-played"

	return fmt.Sprintf("%s?client_id=%s&response_type=code&redirect_uri=%s&scope=%s&state=%d",
		SpotifyAuthURL, url.QueryEscape(clientId), url.QueryEscape(redirectURI), url.QueryEscape(scope), userId), nil
}

type LastTrack struct {
	UserId     int
	TrackId    string
	SongName   string
	Artist     string
	AlbumName  string
	DurationMs int
	ProgressMs int
	UpdatedAt  time.Time
}

func GetLastTrack(userId int) (*LastTrack, error) {
	var track LastTrack
	err := db.Pool.QueryRow(context.Background(),
		`SELECT user_id, track_id, song_name, artist, album_name, duration_ms, progress_ms, updated_at
		 FROM spotify_last_track WHERE user_id = $1`,
		userId).Scan(&track.UserId, &track.TrackId, &track.SongName, &track.Artist,
		&track.AlbumName, &track.DurationMs, &track.ProgressMs, &track.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &track, nil
}

func SetLastTrack(userId int, trackId, songName, artist, albumName string, durationMs, progressMs int) error {
	_, err := db.Pool.Exec(context.Background(),
		`INSERT INTO spotify_last_track (user_id, track_id, song_name, artist, album_name, duration_ms, progress_ms, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		 ON CONFLICT (user_id) DO UPDATE SET
			track_id = $2, song_name = $3, artist = $4, album_name = $5, duration_ms = $6, progress_ms = $7, updated_at = NOW()`,
		userId, trackId, songName, artist, albumName, durationMs, progressMs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error saving last track: %v\n", err)
		return err
	}
	return nil
}

func checkAndScrobbleHalfway(userId int, currentTrack *SpotifyTrack, progressMs int) {
	if currentTrack.Id == "" || currentTrack.DurationMs == 0 {
		return
	}

	lastTrack, err := GetLastTrack(userId)
	if err != nil {
		if err.Error() == "no rows in result set" {
			SetLastTrack(userId, currentTrack.Id, currentTrack.Name,
				getArtistName(currentTrack.Artists), currentTrack.Album.Name, currentTrack.DurationMs, progressMs)
		}
		return
	}

	if lastTrack.TrackId != currentTrack.Id {
		if lastTrack.DurationMs > 0 {
			percentagePlayed := float64(lastTrack.ProgressMs) / float64(lastTrack.DurationMs)
			if percentagePlayed >= 0.5 || lastTrack.ProgressMs >= 240000 {
				msPlayed := lastTrack.ProgressMs
				if msPlayed > lastTrack.DurationMs {
					msPlayed = lastTrack.DurationMs
				}
				scrobble := Scrobble{
					UserId:    userId,
					Timestamp: lastTrack.UpdatedAt,
					SongName:  lastTrack.SongName,
					Artist:    lastTrack.Artist,
					Album:     lastTrack.AlbumName,
					MsPlayed:  msPlayed,
					Platform:  "spotify",
				}
				SaveScrobble(scrobble)
			}
		}

		SetLastTrack(userId, currentTrack.Id, currentTrack.Name,
			getArtistName(currentTrack.Artists), currentTrack.Album.Name, currentTrack.DurationMs, progressMs)
	} else {
		SetLastTrack(userId, currentTrack.Id, currentTrack.Name,
			getArtistName(currentTrack.Artists), currentTrack.Album.Name, currentTrack.DurationMs, progressMs)
	}
}

func getArtistName(artists []SpotifyArtist) string {
	if len(artists) > 0 {
		return artists[0].Name
	}
	return ""
}
