package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"muzi/artwork"
	"muzi/config"
	"muzi/db"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type ArtistData struct {
	Username         string
	Artist           db.Artist
	ListenCount      int
	Songs            []string
	Titles           []string
	Times            []db.ScrobbleEntry
	Page             int
	Title            string
	LoggedInUsername string
	TemplateName     string
}

type SongData struct {
	Username         string
	Song             db.Song
	ImageUrl         string
	Artist           db.Artist
	ArtistNames      []string
	Albums           []db.Album
	ListenCount      int
	Times            []db.ScrobbleEntry
	Page             int
	Title            string
	LoggedInUsername string
	TemplateName     string
}

type AlbumData struct {
	Username         string
	Album            db.Album
	Artist           db.Artist
	ArtistNames      []string
	ListenCount      int
	Times            []db.ScrobbleEntry
	Page             int
	Title            string
	LoggedInUsername string
	TemplateName     string
}

func artistPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")
		artistName, err := url.QueryUnescape(chi.URLParam(r, "artist"))
		if err != nil {
			http.Error(w, "Invalid artist name", http.StatusBadRequest)
			return
		}

		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find user %s: %v\n", username, err)
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		if !canViewProfile(r, userId) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		artist, err := db.GetArtistByName(userId, artistName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find artist %s: %v\n", artistName, err)
			http.Error(w, "Artist not found", http.StatusNotFound)
			return
		}

		pageStr := r.URL.Query().Get("page")
		var pageInt int
		if pageStr == "" {
			pageInt = 1
		} else {
			pageInt, err = strconv.Atoi(pageStr)
			if err != nil {
				pageInt = 1
			}
		}

		lim := 15
		off := (pageInt - 1) * lim

		listenCount, err := db.GetArtistStats(userId, artist.Id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get artist stats: %v\n", err)
		}

		entries, err := db.GetHistoryForArtist(userId, artist.Id, lim, off)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get history for artist: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		artistData := ArtistData{
			Username:         username,
			Artist:           artist,
			ListenCount:      listenCount,
			Times:            entries,
			Page:             pageInt,
			Title:            artistName + " - " + username,
			LoggedInUsername: getLoggedInUsername(r),
			TemplateName:     "artist",
		}

		err = templates.ExecuteTemplate(w, "base", artistData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// Profile links use the scrobbled artist string, which can list several artists ("A, B");
// fall back to the primary (first) artist, which is what albums and songs are filed under
func getLinkedArtist(userId int, artistName string) (db.Artist, error) {
	artist, err := db.GetArtistByName(userId, artistName)
	if err == nil {
		return artist, nil
	}
	primary := strings.TrimSpace(strings.Split(artistName, ",")[0])
	if primary == artistName || primary == "" {
		return db.Artist{}, err
	}
	return db.GetArtistByName(userId, primary)
}

func songPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")
		artistName, err := url.QueryUnescape(chi.URLParam(r, "artist"))
		if err != nil {
			http.Error(w, "Invalid artist name", http.StatusBadRequest)
			return
		}
		songTitle, err := url.QueryUnescape(chi.URLParam(r, "song"))
		if err != nil {
			http.Error(w, "Invalid song title", http.StatusBadRequest)
			return
		}

		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find user %s: %v\n", username, err)
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		if !canViewProfile(r, userId) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		artist, err := getLinkedArtist(userId, artistName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find artist %s: %v\n", artistName, err)
			http.Error(w, "Artist not found", http.StatusNotFound)
			return
		}

		songs, err := db.GetSongsByName(userId, songTitle, artist.Id)
		if err != nil || len(songs) == 0 {
			songList, _, searchErr := db.SearchSongs(userId, songTitle)
			if searchErr == nil && len(songList) > 0 {
				songs = songList
			} else {
				fmt.Fprintf(os.Stderr, "Cannot find song %s: %v\n", songTitle, err)
				http.Error(w, "Song not found", http.StatusNotFound)
				return
			}
		}

		song := songs[0]
		artist, _ = db.GetArtistById(song.ArtistId)

		var songIds []int
		var albums []db.Album
		seenAlbums := make(map[int]bool)
		seenArtistIds := make(map[int]bool)
		var allArtistIds []int
		for _, s := range songs {
			songIds = append(songIds, s.Id)
			if s.ArtistId > 0 && !seenArtistIds[s.ArtistId] {
				seenArtistIds[s.ArtistId] = true
				allArtistIds = append(allArtistIds, s.ArtistId)
			}
			if s.AlbumId > 0 && !seenAlbums[s.AlbumId] {
				seenAlbums[s.AlbumId] = true
				album, _ := db.GetAlbumById(s.AlbumId)
				albums = append(albums, album)
			}
		}

		var artistNames []string
		seenArtistIdsMap := make(map[int]bool)
		rows, err := db.Pool.Query(context.Background(),
			`SELECT DISTINCT artist_ids FROM history WHERE song_id = ANY($1)`,
			songIds)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var artistIds []int
				if err := rows.Scan(&artistIds); err == nil {
					for _, id := range artistIds {
						if !seenArtistIdsMap[id] {
							seenArtistIdsMap[id] = true
							a, err := db.GetArtistById(id)
							if err == nil {
								artistNames = append(artistNames, a.Name)
							}
						}
					}
				}
			}
		}

		pageStr := r.URL.Query().Get("page")
		var pageInt int
		if pageStr == "" {
			pageInt = 1
		} else {
			pageInt, err = strconv.Atoi(pageStr)
			if err != nil {
				pageInt = 1
			}
		}

		lim := 15
		off := (pageInt - 1) * lim

		listenCount, err := db.GetSongStatsForSongs(userId, songIds)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get song stats: %v\n", err)
		}

		entries, err := db.GetHistoryForSongs(userId, songIds, lim, off)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get history for song: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// a song's own image (custom, or fetched when its album has no cover) wins over the album cover
		imageUrl := ""
		for _, s := range songs {
			if s.ImageUrl != "" {
				imageUrl = s.ImageUrl
				break
			}
		}
		for _, a := range albums {
			if imageUrl == "" && a.CoverUrl != "" {
				imageUrl = a.CoverUrl
			}
		}

		songData := SongData{
			Username:         username,
			Song:             song,
			ImageUrl:         imageUrl,
			Artist:           artist,
			ArtistNames:      artistNames,
			Albums:           albums,
			ListenCount:      listenCount,
			Times:            entries,
			Page:             pageInt,
			Title:            songTitle + " - " + username,
			LoggedInUsername: getLoggedInUsername(r),
			TemplateName:     "song",
		}

		err = templates.ExecuteTemplate(w, "base", songData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func editArtistHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		artistIdStr := chi.URLParam(r, "id")
		artistId, err := strconv.Atoi(artistIdStr)
		if err != nil {
			http.Error(w, "Invalid artist ID", http.StatusBadRequest)
			return
		}
		if !requireOwner(w, r, "artists", artistId) {
			return
		}

		r.ParseForm()
		name := r.Form.Get("name")
		imageUrl := r.Form.Get("image_url")
		bio := r.Form.Get("bio")
		spotifyId := r.Form.Get("spotify_id")
		musicbrainzId := r.Form.Get("musicbrainz_id")

		err = db.UpdateArtist(artistId, name, imageUrl, bio, spotifyId, musicbrainzId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating artist: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/artist/"+url.QueryEscape(name), http.StatusSeeOther)
	}
}

func editSongHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		r.ParseForm()
		title := r.Form.Get("title")
		spotifyId := r.Form.Get("spotify_id")
		musicbrainzId := r.Form.Get("musicbrainz_id")

		songIdStr := chi.URLParam(r, "id")
		songId, err := strconv.Atoi(songIdStr)
		if err != nil {
			http.Error(w, "Invalid song ID", http.StatusBadRequest)
			return
		}
		if !requireOwner(w, r, "songs", songId) {
			return
		}

		err = db.UpdateSong(songId, title, 0, spotifyId, musicbrainzId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating song: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		song, err := db.GetSongById(songId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting song after update: %v\n", err)
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
			return
		}

		artist, err := db.GetArtistById(song.ArtistId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting artist: %v\n", err)
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
			return
		}

		http.Redirect(
			w,
			r,
			"/profile/"+username+"/song/"+url.QueryEscape(artist.Name)+"/"+url.QueryEscape(title),
			http.StatusSeeOther,
		)
	}
}

func albumPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")
		artistName, err := url.QueryUnescape(chi.URLParam(r, "artist"))
		if err != nil {
			http.Error(w, "Invalid artist name", http.StatusBadRequest)
			return
		}
		albumTitle, err := url.QueryUnescape(chi.URLParam(r, "album"))
		if err != nil {
			http.Error(w, "Invalid album title", http.StatusBadRequest)
			return
		}

		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find user %s: %v\n", username, err)
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		if !canViewProfile(r, userId) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		artist, err := getLinkedArtist(userId, artistName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find artist %s: %v\n", artistName, err)
			http.Error(w, "Artist not found", http.StatusNotFound)
			return
		}

		album, err := db.GetAlbumByName(userId, albumTitle, artist.Id)
		if err != nil {
			albums, _, searchErr := db.SearchAlbums(userId, albumTitle)
			if searchErr == nil && len(albums) > 0 {
				album = albums[0]
			} else {
				fmt.Fprintf(os.Stderr, "Cannot find album %s: %v\n", albumTitle, err)
				http.Error(w, "Album not found", http.StatusNotFound)
				return
			}
		}

		artist, _ = db.GetArtistById(album.ArtistId)

		pageStr := r.URL.Query().Get("page")
		var pageInt int
		if pageStr == "" {
			pageInt = 1
		} else {
			pageInt, err = strconv.Atoi(pageStr)
			if err != nil {
				pageInt = 1
			}
		}

		lim := 15
		off := (pageInt - 1) * lim

		listenCount, err := db.GetAlbumStats(userId, album.Id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get album stats: %v\n", err)
		}

		entries, err := db.GetHistoryForAlbum(userId, album.Id, lim, off)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get history for album: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var artistNames []string
		seenArtistIds := make(map[int]bool)
		for _, e := range entries {
			for _, artistId := range e.ArtistIds {
				if !seenArtistIds[artistId] {
					seenArtistIds[artistId] = true
					a, err := db.GetArtistById(artistId)
					if err == nil {
						artistNames = append(artistNames, a.Name)
					}
				}
			}
		}

		albumData := AlbumData{
			Username:         username,
			Album:            album,
			Artist:           artist,
			ArtistNames:      artistNames,
			ListenCount:      listenCount,
			Times:            entries,
			Page:             pageInt,
			Title:            albumTitle + " - " + username,
			LoggedInUsername: getLoggedInUsername(r),
			TemplateName:     "album",
		}

		err = templates.ExecuteTemplate(w, "base", albumData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func editAlbumHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		r.ParseForm()
		title := r.Form.Get("title")
		coverUrl := r.Form.Get("cover_url")
		spotifyId := r.Form.Get("spotify_id")
		musicbrainzId := r.Form.Get("musicbrainz_id")

		albumIdStr := chi.URLParam(r, "id")
		albumId, err := strconv.Atoi(albumIdStr)
		if err != nil {
			http.Error(w, "Invalid album ID", http.StatusBadRequest)
			return
		}
		if !requireOwner(w, r, "albums", albumId) {
			return
		}

		err = db.UpdateAlbum(albumId, title, coverUrl, spotifyId, musicbrainzId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating album: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		album, err := db.GetAlbumById(albumId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting album after update: %v\n", err)
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
			return
		}

		artist, err := db.GetArtistById(album.ArtistId)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting artist: %v\n", err)
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
			return
		}

		http.Redirect(
			w,
			r,
			"/profile/"+username+"/album/"+url.QueryEscape(artist.Name)+"/"+url.QueryEscape(title),
			http.StatusSeeOther,
		)
	}
}

type InlineEditRequest struct {
	Value string `json:"value"`
}

type BatchEditRequest struct {
	Name          string `json:"name"`
	Bio           string `json:"bio"`
	ImageUrl      string `json:"image_url"`
	SpotifyId     string `json:"spotify_id"`
	MusicbrainzId string `json:"musicbrainz_id"`
	Title         string `json:"title"`
	CoverUrl      string `json:"cover_url"`
	Duration      int    `json:"duration"`
}

func artistInlineEditHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Not logged in", http.StatusUnauthorized)
			return
		}

		artistIdStr := chi.URLParam(r, "id")
		artistId, err := strconv.Atoi(artistIdStr)
		if err != nil {
			http.Error(w, "Invalid artist ID", http.StatusBadRequest)
			return
		}
		if !requireOwner(w, r, "artists", artistId) {
			return
		}

		field := r.URL.Query().Get("field")
		if field == "" {
			http.Error(w, "Field required", http.StatusBadRequest)
			return
		}

		var req InlineEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		var updateErr error
		switch field {
		case "name":
			artist, _ := db.GetArtistById(artistId)
			updateErr = db.UpdateArtist(
				artistId,
				req.Value,
				artist.ImageUrl,
				artist.Bio,
				artist.SpotifyId,
				artist.MusicbrainzId,
			)
		case "bio":
			artist, _ := db.GetArtistById(artistId)
			updateErr = db.UpdateArtist(
				artistId,
				artist.Name,
				artist.ImageUrl,
				req.Value,
				artist.SpotifyId,
				artist.MusicbrainzId,
			)
		case "image_url":
			artist, _ := db.GetArtistById(artistId)
			updateErr = db.UpdateArtist(
				artistId,
				artist.Name,
				req.Value,
				artist.Bio,
				artist.SpotifyId,
				artist.MusicbrainzId,
			)
		default:
			http.Error(w, "Invalid field", http.StatusBadRequest)
			return
		}

		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "Error updating artist: %v\n", updateErr)
			http.Error(w, updateErr.Error(), http.StatusInternalServerError)
			return
		}

		if field == "image_url" && req.Value == "" {
			refreshImage("artist", artistId)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"success": "true"})
	}
}

func artistBatchEditHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Not logged in", http.StatusUnauthorized)
			return
		}

		artistIdStr := chi.URLParam(r, "id")
		artistId, err := strconv.Atoi(artistIdStr)
		if err != nil {
			http.Error(w, "Invalid artist ID", http.StatusBadRequest)
			return
		}
		if !requireOwner(w, r, "artists", artistId) {
			return
		}

		var req BatchEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		artist, _ := db.GetArtistById(artistId)

		name := artist.Name
		bio := artist.Bio
		imageUrl := artist.ImageUrl
		spotifyId := artist.SpotifyId
		musicbrainzId := artist.MusicbrainzId

		if req.Name != "" {
			name = req.Name
		}
		if req.Bio != "" || req.Bio == "" {
			bio = req.Bio
		}
		if req.ImageUrl != "" {
			imageUrl = req.ImageUrl
		}
		if req.SpotifyId != "" {
			spotifyId = req.SpotifyId
		}
		if req.MusicbrainzId != "" {
			musicbrainzId = req.MusicbrainzId
		}

		updateErr := db.UpdateArtist(artistId, name, imageUrl, bio, spotifyId, musicbrainzId)
		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "Error updating artist: %v\n", updateErr)
			http.Error(w, updateErr.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"success": "true"})
	}
}

func songInlineEditHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Not logged in", http.StatusUnauthorized)
			return
		}

		songIdStr := chi.URLParam(r, "id")
		songId, err := strconv.Atoi(songIdStr)
		if err != nil {
			http.Error(w, "Invalid song ID", http.StatusBadRequest)
			return
		}
		if !requireOwner(w, r, "songs", songId) {
			return
		}

		field := r.URL.Query().Get("field")
		if field == "" {
			http.Error(w, "Field required", http.StatusBadRequest)
			return
		}

		var req InlineEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		var updateErr error
		if field == "image_url" {
			updateErr = db.UpdateSongImage(songId, req.Value)
			if updateErr == nil && req.Value == "" {
				refreshImage("song", songId)
			}
		} else {
			song, _ := db.GetSongById(songId)
			updateErr = db.UpdateSong(
				songId,
				req.Value,
				song.DurationMs,
				song.SpotifyId,
				song.MusicbrainzId,
			)
		}

		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "Error updating song: %v\n", updateErr)
			http.Error(w, updateErr.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"success": "true"})
	}
}

func songBatchEditHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Not logged in", http.StatusUnauthorized)
			return
		}

		songIdStr := chi.URLParam(r, "id")
		songId, err := strconv.Atoi(songIdStr)
		if err != nil {
			http.Error(w, "Invalid song ID", http.StatusBadRequest)
			return
		}
		if !requireOwner(w, r, "songs", songId) {
			return
		}

		var req BatchEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		song, _ := db.GetSongById(songId)

		title := song.Title
		durationMs := song.DurationMs
		spotifyId := song.SpotifyId
		musicbrainzId := song.MusicbrainzId

		if req.Title != "" {
			title = req.Title
		}
		if req.Duration > 0 {
			durationMs = req.Duration
		}
		if req.SpotifyId != "" {
			spotifyId = req.SpotifyId
		}
		if req.MusicbrainzId != "" {
			musicbrainzId = req.MusicbrainzId
		}

		updateErr := db.UpdateSong(songId, title, durationMs, spotifyId, musicbrainzId)
		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "Error updating song: %v\n", updateErr)
			http.Error(w, updateErr.Error(), http.StatusInternalServerError)
			return
		}

		artist, _ := db.GetArtistById(song.ArtistId)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"success":  "true",
			"artist":   artist.Name,
			"title":    title,
			"username": username,
		})
	}
}

func albumInlineEditHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Not logged in", http.StatusUnauthorized)
			return
		}

		albumIdStr := chi.URLParam(r, "id")
		albumId, err := strconv.Atoi(albumIdStr)
		if err != nil {
			http.Error(w, "Invalid album ID", http.StatusBadRequest)
			return
		}
		if !requireOwner(w, r, "albums", albumId) {
			return
		}

		field := r.URL.Query().Get("field")
		if field == "" {
			http.Error(w, "Field required", http.StatusBadRequest)
			return
		}

		var req InlineEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		updateErr := db.UpdateAlbumField(albumId, field, req.Value)

		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "Error updating album: %v\n", updateErr)
			http.Error(w, updateErr.Error(), http.StatusInternalServerError)
			return
		}

		if field == "cover_url" && req.Value == "" {
			refreshImage("album", albumId)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"success": "true"})
	}
}

func albumBatchEditHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Not logged in", http.StatusUnauthorized)
			return
		}

		albumIdStr := chi.URLParam(r, "id")
		albumId, err := strconv.Atoi(albumIdStr)
		if err != nil {
			http.Error(w, "Invalid album ID", http.StatusBadRequest)
			return
		}
		if !requireOwner(w, r, "albums", albumId) {
			return
		}

		var req BatchEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		album, _ := db.GetAlbumById(albumId)

		title := album.Title
		coverUrl := album.CoverUrl
		spotifyId := album.SpotifyId
		musicbrainzId := album.MusicbrainzId

		if req.Title != "" {
			title = req.Title
		}
		if req.CoverUrl != "" {
			coverUrl = req.CoverUrl
		}
		if req.SpotifyId != "" {
			spotifyId = req.SpotifyId
		}
		if req.MusicbrainzId != "" {
			musicbrainzId = req.MusicbrainzId
		}

		updateErr := db.UpdateAlbum(albumId, title, coverUrl, spotifyId, musicbrainzId)
		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "Error updating album: %v\n", updateErr)
			http.Error(w, updateErr.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"success": "true"})
	}
}

type SearchResult struct {
	Type   string  `json:"type"`
	Name   string  `json:"name"`
	Artist string  `json:"artist"`
	Url    string  `json:"url"`
	Count  int     `json:"count"`
	Score  float64 `json:"-"`
}

// Name match plus a nudge for things you play a lot. People rank by name match only: a user's
// total plays would otherwise put them above everything in your library.
func searchRank(r SearchResult) float64 {
	if r.Type == "user" {
		return r.Score
	}
	return r.Score + float64(r.Count)*0.01
}

func searchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Not logged in", http.StatusUnauthorized)
			return
		}

		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		query := r.URL.Query().Get("q")
		if len(query) < 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}

		var results []SearchResult

		artists, artistSim, err := db.SearchArtists(userId, query)
		if err == nil {
			for _, a := range artists {
				count, _ := db.GetArtistStats(userId, a.Id)
				results = append(results, SearchResult{
					Type:  "artist",
					Name:  a.Name,
					Url:   "/profile/" + username + "/artist/" + url.QueryEscape(a.Name),
					Count: count,
					Score: artistSim,
				})
			}
		}

		songs, songSim, err := db.SearchSongs(userId, query)
		if err == nil {
			for _, s := range songs {
				count, _ := db.GetSongStats(userId, s.Id)
				artist, _ := db.GetArtistById(s.ArtistId)
				results = append(results, SearchResult{
					Type:   "song",
					Name:   s.Title,
					Artist: artist.Name,
					Url: "/profile/" + username + "/song/" + url.QueryEscape(
						artist.Name,
					) + "/" + url.QueryEscape(
						s.Title,
					),
					Count: count,
					Score: songSim,
				})
			}
		}

		albums, albumSim, err := db.SearchAlbums(userId, query)
		if err == nil {
			for _, al := range albums {
				count, _ := db.GetAlbumStats(userId, al.Id)
				artist, _ := db.GetArtistById(al.ArtistId)
				results = append(results, SearchResult{
					Type:   "album",
					Name:   al.Title,
					Artist: artist.Name,
					Url: "/profile/" + username + "/album/" + url.QueryEscape(
						artist.Name,
					) + "/" + url.QueryEscape(
						al.Title,
					),
					Count: count,
					Score: albumSim,
				})
			}
		}

		users, userSim, err := db.SearchUsers(userId, query)
		if err == nil {
			for _, u := range users {
				results = append(results, SearchResult{
					Type:  "user",
					Name:  u.Username,
					Url:   "/profile/" + url.PathEscape(u.Username),
					Count: u.Plays,
					Score: userSim + 0.3,
				})
			}
		}

		sort.Slice(results, func(i, j int) bool {
			return searchRank(results[i]) > searchRank(results[j])
		})

		w.Header().Set("Content-Type", "application/json")
		jsonBytes, _ := json.Marshal(results)
		w.Write(jsonBytes)
	}
}

// Looks up a new image right away after a user resets one to automatic, so the page reload shows it
func refreshImage(entity string, id int) {
	if !config.Get().Images.AutoFetch {
		return
	}
	err := artwork.Refresh(entity, id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		fmt.Fprintf(os.Stderr, "Error fetching %s image: %v\n", entity, err)
	}
}

// Checks that the logged-in user owns the artist, album or song being edited, writing a 404 when
// they don't (so other users' IDs can't be probed). table must be "artists", "albums" or "songs".
func requireOwner(w http.ResponseWriter, r *http.Request, table string, id int) bool {
	userId, err := getUserIdByUsername(r.Context(), getLoggedInUsername(r))
	if err != nil {
		http.Error(w, "Not logged in", http.StatusUnauthorized)
		return false
	}
	var owner int
	err = db.Pool.QueryRow(r.Context(), "SELECT user_id FROM "+table+" WHERE id = $1", id).Scan(&owner)
	if err != nil || owner != userId {
		http.Error(w, "Not found", http.StatusNotFound)
		return false
	}
	return true
}

const maxImageSize = 5 * 1024 * 1024

var imageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Saves the image in the request's "file" field to ./static/uploads, named by content hash,
// and returns its public URL. The type is sniffed from the file's contents, not trusted
// from its name. Errors are safe to show to the user.
func saveUploadedImage(r *http.Request) (string, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxImageSize+64*1024)
	if err := r.ParseMultipartForm(maxImageSize); err != nil {
		return "", fmt.Errorf("file too large or invalid (5MB max)")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", fmt.Errorf("no file uploaded")
	}
	defer file.Close()
	if header.Size > maxImageSize {
		return "", fmt.Errorf("file exceeds 5MB limit")
	}

	sniff := make([]byte, 512)
	n, _ := io.ReadFull(file, sniff)
	ext, ok := imageTypes[http.DetectContentType(sniff[:n])]
	if !ok {
		return "", fmt.Errorf("unsupported image type; use JPEG, PNG, GIF or WebP")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("could not read file")
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("could not read file")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("could not read file")
	}
	filename := hex.EncodeToString(hash.Sum(nil)) + ext

	uploadDir := config.Get().Storage.UploadsDir
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating upload dir: %v\n", err)
		return "", fmt.Errorf("server error")
	}
	dst, err := os.Create(filepath.Join(uploadDir, filename))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating file: %v\n", err)
		return "", fmt.Errorf("server error")
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving file: %v\n", err)
		return "", fmt.Errorf("server error")
	}
	return "/files/uploads/" + filename, nil
}

func imageUploadHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if getLoggedInUsername(r) == "" {
			http.Error(w, "Not logged in", http.StatusUnauthorized)
			return
		}

		imageUrl, err := saveUploadedImage(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"url": imageUrl})
	}
}

func deleteScrobbleHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		var ids []int
		if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
			fmt.Fprintf(os.Stderr, "Error decoding request: %v\n", err)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		err = db.DeleteHistoryByIds(userId, ids)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting scrobbles: %v\n", err)
			http.Error(w, "Error deleting scrobbles", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
