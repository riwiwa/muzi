package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"muzi/db"
)

type ScrobbleTrack struct {
	SongName  string `json:"song_name"`
	Artist    string `json:"artist"`
	AlbumName string `json:"album_name"`
	Timestamp string `json:"timestamp"`
	MsPlayed  int    `json:"ms_played"`
}

type ScrobbleRequest struct {
	Tracks []ScrobbleTrack `json:"tracks"`
}

type scrobbleData struct {
	Title            string
	LoggedInUsername string
	TemplateName     string
}

func scrobblePageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		data := scrobbleData{
			Title:            "muzi | Manual Scrobble",
			LoggedInUsername: username,
			TemplateName:     "scrobble",
		}

		err := templates.ExecuteTemplate(w, "base", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func scrobbleSubmitHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find user %s: %v\n", username, err)
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		var req ScrobbleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if len(req.Tracks) == 0 {
			http.Error(w, "No tracks provided", http.StatusBadRequest)
			return
		}

		count, err := insertScrobbles(r.Context(), userId, req.Tracks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error inserting scrobbles: %v\n", err)
			http.Error(w, fmt.Sprintf("Error inserting scrobbles: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"count":   count,
		})
	}
}

func insertScrobbles(ctx context.Context, userId int, tracks []ScrobbleTrack) (int, error) {
	artistIdMap := make(map[string][]int)

	for _, track := range tracks {
		if track.Artist == "" || track.SongName == "" {
			continue
		}

		artistNames := parseArtistString(track.Artist)
		var artistIds []int
		for _, name := range artistNames {
			artistId, _, err := db.GetOrCreateArtist(userId, name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating artist %s: %v\n", name, err)
				continue
			}
			artistIds = append(artistIds, artistId)
		}
		artistIdMap[track.Artist+"|"+track.SongName] = artistIds
	}

	imported := 0
	for _, track := range tracks {
		if track.Artist == "" || track.SongName == "" {
			continue
		}

		timestamp, err := time.Parse(time.RFC3339, track.Timestamp)
		if err != nil {
			timestamp = time.Now()
		}

		artistIds := artistIdMap[track.Artist+"|"+track.SongName]
		var artistId int
		if len(artistIds) > 0 {
			artistId = artistIds[0]
		}

		var albumId int
		if track.AlbumName != "" && artistId > 0 {
			albumId, _, _ = db.GetOrCreateAlbum(userId, track.AlbumName, artistId)
		}

		var songId int
		if track.SongName != "" && artistId > 0 {
			songId, _, _ = db.GetOrCreateSong(userId, track.SongName, artistId, albumId)
		}

		var albumNamePg *string
		if track.AlbumName != "" {
			albumNamePg = &track.AlbumName
		}

		_, err = db.Pool.Exec(ctx,
			`INSERT INTO history (user_id, timestamp, song_name, artist, album_name, ms_played, platform, artist_id, song_id, artist_ids)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (user_id, song_name, artist, timestamp) DO NOTHING`,
			userId, timestamp, track.SongName, track.Artist, albumNamePg, track.MsPlayed, "manual", artistId, songId, artistIds,
		)
		if err != nil {
			if !strings.Contains(err.Error(), "duplicate") {
				fmt.Fprintf(os.Stderr, "Error inserting scrobble: %v\n", err)
			}
			continue
		}
		imported++
	}

	return imported, nil
}

func parseArtistString(artist string) []string {
	if artist == "" {
		return nil
	}
	var artists []string
	for _, a := range strings.Split(artist, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			artists = append(artists, a)
		}
	}
	return artists
}
