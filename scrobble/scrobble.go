package scrobble

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"muzi/db"

	"github.com/jackc/pgtype"
)

const DuplicateToleranceSeconds = 20

type Scrobble struct {
	UserId    int
	Timestamp time.Time
	SongName  string
	Artist    string
	Album     string
	MsPlayed  int
	Platform  string
	Source    string
}

type NowPlaying struct {
	UserId    int
	SongName  string
	Artist    string
	Album     string
	MsPlayed  int
	Platform  string
	UpdatedAt time.Time
}

var CurrentNowPlaying = make(map[int]map[string]NowPlaying)

func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func GenerateAPISecret() (string, error) {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func GenerateSessionKey() (string, error) {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func GetUserByAPIKey(apiKey string) (int, string, error) {
	if apiKey == "" {
		return 0, "", fmt.Errorf("empty API key")
	}

	var userId int
	var username string
	err := db.Pool.QueryRow(context.Background(),
		"SELECT pk, username FROM users WHERE api_key = $1", apiKey).Scan(&userId, &username)
	if err != nil {
		return 0, "", err
	}
	return userId, username, nil
}

func GetUserByUsername(username string) (int, error) {
	if username == "" {
		return 0, fmt.Errorf("empty username")
	}

	var userId int
	err := db.Pool.QueryRow(context.Background(),
		"SELECT pk FROM users WHERE username = $1", username).Scan(&userId)
	if err != nil {
		return 0, err
	}
	return userId, nil
}

func GetUserBySessionKey(sessionKey string) (int, string, error) {
	if sessionKey == "" {
		return 0, "", fmt.Errorf("empty session key")
	}

	var userId int
	var username string
	err := db.Pool.QueryRow(context.Background(),
		"SELECT pk, username FROM users WHERE api_secret = $1", sessionKey).Scan(&userId, &username)
	if err != nil {
		return 0, "", err
	}
	return userId, username, nil
}

func SaveScrobble(scrobble Scrobble) error {
	exists, err := checkDuplicate(scrobble.UserId, scrobble.Artist, scrobble.SongName, scrobble.Timestamp)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("duplicate scrobble")
	}

	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO history (user_id, timestamp, song_name, artist, album_name, ms_played, platform)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, song_name, artist, timestamp) DO NOTHING`,
		scrobble.UserId, scrobble.Timestamp, scrobble.SongName, scrobble.Artist,
		scrobble.Album, scrobble.MsPlayed, scrobble.Platform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error saving scrobble: %v\n", err)
		return err
	}
	return nil
}

func SaveScrobbles(scrobbles []Scrobble) (int, int, error) {
	if len(scrobbles) == 0 {
		return 0, 0, nil
	}

	accepted := 0
	ignored := 0

	batchSize := 100
	for i := 0; i < len(scrobbles); i += batchSize {
		end := i + batchSize
		if end > len(scrobbles) {
			end = len(scrobbles)
		}

		for _, scrobble := range scrobbles[i:end] {
			err := SaveScrobble(scrobble)
			if err != nil {
				if err.Error() == "duplicate scrobble" {
					ignored++
				} else {
					fmt.Fprintf(os.Stderr, "Error saving scrobble: %v\n", err)
				}
				continue
			}
			accepted++
		}
	}

	return accepted, ignored, nil
}

func checkDuplicate(userId int, artist, songName string, timestamp time.Time) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(context.Background(),
		`SELECT EXISTS(
			SELECT 1 FROM history 
			WHERE user_id = $1 
			AND artist = $2 
			AND song_name = $3 
			AND ABS(EXTRACT(EPOCH FROM (timestamp - $4))) < $5
		)`,
		userId, artist, songName, timestamp, DuplicateToleranceSeconds).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func UpdateNowPlaying(np NowPlaying) {
	if CurrentNowPlaying[np.UserId] == nil {
		CurrentNowPlaying[np.UserId] = make(map[string]NowPlaying)
	}
	CurrentNowPlaying[np.UserId][np.Platform] = np
}

func GetNowPlaying(userId int) (NowPlaying, bool) {
	platforms := CurrentNowPlaying[userId]
	if platforms == nil {
		return NowPlaying{}, false
	}
	np, ok := platforms["lastfm_api"]
	if ok && np.SongName != "" {
		return np, true
	}
	np, ok = platforms["spotify"]
	if ok && np.SongName != "" {
		return np, true
	}
	return NowPlaying{}, false
}

func ClearNowPlaying(userId int) {
	delete(CurrentNowPlaying, userId)
}

func ClearNowPlayingPlatform(userId int, platform string) {
	if CurrentNowPlaying[userId] != nil {
		delete(CurrentNowPlaying[userId], platform)
	}
}

func GetUserSpotifyCredentials(userId int) (clientId, clientSecret, accessToken, refreshToken string, expiresAt time.Time, err error) {
	var clientIdPg, clientSecretPg, accessTokenPg, refreshTokenPg pgtype.Text
	var expiresAtPg pgtype.Timestamptz
	err = db.Pool.QueryRow(context.Background(),
		`SELECT spotify_client_id, spotify_client_secret, spotify_access_token, 
			spotify_refresh_token, spotify_token_expires 
		FROM users WHERE pk = $1`,
		userId).Scan(&clientIdPg, &clientSecretPg, &accessTokenPg, &refreshTokenPg, &expiresAtPg)
	if err != nil {
		return "", "", "", "", time.Time{}, err
	}

	if clientIdPg.Status == pgtype.Present {
		clientId = clientIdPg.String
	}
	if clientSecretPg.Status == pgtype.Present {
		clientSecret = clientSecretPg.String
	}
	if accessTokenPg.Status == pgtype.Present {
		accessToken = accessTokenPg.String
	}
	if refreshTokenPg.Status == pgtype.Present {
		refreshToken = refreshTokenPg.String
	}
	if expiresAtPg.Status == pgtype.Present {
		expiresAt = expiresAtPg.Time
	}

	return clientId, clientSecret, accessToken, refreshToken, expiresAt, nil
}

func UpdateUserSpotifyTokens(userId int, accessToken, refreshToken string, expiresIn int) error {
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE users SET 
			spotify_access_token = $1, 
			spotify_refresh_token = $2, 
			spotify_token_expires = $3
		WHERE pk = $4`,
		accessToken, refreshToken, expiresAt, userId)
	return err
}

func UpdateUserSpotifyCheck(userId int) error {
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE users SET last_spotify_check = $1 WHERE pk = $2`,
		time.Now(), userId)
	return err
}

func GetUsersWithSpotify() ([]int, error) {
	rows, err := db.Pool.Query(context.Background(),
		`SELECT pk FROM users WHERE spotify_client_id IS NOT NULL AND spotify_client_secret IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userIds []int
	for rows.Next() {
		var userId int
		if err := rows.Scan(&userId); err != nil {
			return nil, err
		}
		userIds = append(userIds, userId)
	}
	return userIds, nil
}

type User struct {
	Pk                  int
	Username            string
	Bio                 string
	Pfp                 string
	AllowDuplicateEdits bool
	ApiKey              *string
	ApiSecret           *string
	SpotifyClientId     *string
	SpotifyClientSecret *string
}

func GetUserById(userId int) (User, error) {
	var user User
	var apiKey, apiSecret, spotifyClientId, spotifyClientSecret pgtype.Text
	err := db.Pool.QueryRow(context.Background(),
		`SELECT pk, username, bio, pfp, allow_duplicate_edits, api_key, api_secret, 
			spotify_client_id, spotify_client_secret
		FROM users WHERE pk = $1`,
		userId).Scan(&user.Pk, &user.Username, &user.Bio, &user.Pfp,
		&user.AllowDuplicateEdits, &apiKey, &apiSecret, &spotifyClientId, &spotifyClientSecret)
	if err != nil {
		return User{}, err
	}

	if apiKey.Status == pgtype.Present {
		user.ApiKey = &apiKey.String
	}
	if apiSecret.Status == pgtype.Present {
		user.ApiSecret = &apiSecret.String
	}
	if spotifyClientId.Status == pgtype.Present {
		user.SpotifyClientId = &spotifyClientId.String
	}
	if spotifyClientSecret.Status == pgtype.Present {
		user.SpotifyClientSecret = &spotifyClientSecret.String
	}

	return user, nil
}

func UpdateUserAPIKey(userId int, apiKey, apiSecret string) error {
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE users SET api_key = $1, api_secret = $2 WHERE pk = $3`,
		apiKey, apiSecret, userId)
	return err
}

func UpdateUserSpotifyCredentials(userId int, clientId, clientSecret string) error {
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE users SET spotify_client_id = $1, spotify_client_secret = $2 WHERE pk = $3`,
		clientId, clientSecret, userId)
	return err
}

func DeleteUserSpotifyCredentials(userId int) error {
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE users SET 
			spotify_client_id = NULL, 
			spotify_client_secret = NULL,
			spotify_access_token = NULL,
			spotify_refresh_token = NULL,
			spotify_token_expires = NULL
		WHERE pk = $1`,
		userId)
	return err
}

func (u *User) IsSpotifyConnected() bool {
	_, _, accessToken, _, expiresAt, err := GetUserSpotifyCredentials(u.Pk)
	if err != nil || accessToken == "" {
		return false
	}
	return time.Now().Before(expiresAt)
}
