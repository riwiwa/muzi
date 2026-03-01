package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"muzi/db"

	"github.com/go-chi/chi/v5"
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
	Artist           db.Artist
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

		artist, err := db.GetArtistByName(userId, artistName)
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
		for _, s := range songs {
			songIds = append(songIds, s.Id)
			if s.AlbumId > 0 && !seenAlbums[s.AlbumId] {
				seenAlbums[s.AlbumId] = true
				album, _ := db.GetAlbumById(s.AlbumId)
				albums = append(albums, album)
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

		songData := SongData{
			Username:         username,
			Song:             song,
			Artist:           artist,
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

		http.Redirect(w, r, "/profile/"+username+"/song/"+url.QueryEscape(artist.Name)+"/"+url.QueryEscape(title), http.StatusSeeOther)
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

		artist, err := db.GetArtistByName(userId, artistName)
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

		albumData := AlbumData{
			Username:         username,
			Album:            album,
			Artist:           artist,
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

		http.Redirect(w, r, "/profile/"+username+"/album/"+url.QueryEscape(artist.Name)+"/"+url.QueryEscape(title), http.StatusSeeOther)
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
			updateErr = db.UpdateArtist(artistId, req.Value, artist.ImageUrl, artist.Bio, artist.SpotifyId, artist.MusicbrainzId)
		case "bio":
			artist, _ := db.GetArtistById(artistId)
			updateErr = db.UpdateArtist(artistId, artist.Name, artist.ImageUrl, req.Value, artist.SpotifyId, artist.MusicbrainzId)
		case "image_url":
			artist, _ := db.GetArtistById(artistId)
			updateErr = db.UpdateArtist(artistId, artist.Name, req.Value, artist.Bio, artist.SpotifyId, artist.MusicbrainzId)
		default:
			http.Error(w, "Invalid field", http.StatusBadRequest)
			return
		}

		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "Error updating artist: %v\n", updateErr)
			http.Error(w, updateErr.Error(), http.StatusInternalServerError)
			return
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

		song, _ := db.GetSongById(songId)
		updateErr := db.UpdateSong(songId, req.Value, song.AlbumId, song.SpotifyId, song.MusicbrainzId)

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

		var req BatchEditRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		song, _ := db.GetSongById(songId)

		title := song.Title
		albumId := song.AlbumId
		spotifyId := song.SpotifyId
		musicbrainzId := song.MusicbrainzId

		if req.Title != "" {
			title = req.Title
		}
		if req.Duration > 0 {
			albumId = req.Duration
		}
		if req.SpotifyId != "" {
			spotifyId = req.SpotifyId
		}
		if req.MusicbrainzId != "" {
			musicbrainzId = req.MusicbrainzId
		}

		updateErr := db.UpdateSong(songId, title, albumId, spotifyId, musicbrainzId)
		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "Error updating song: %v\n", updateErr)
			http.Error(w, updateErr.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"success": "true"})
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
		if len(query) < 2 {
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
					Url:    "/profile/" + username + "/song/" + url.QueryEscape(artist.Name) + "/" + url.QueryEscape(s.Title),
					Count:  count,
					Score:  songSim,
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
					Url:    "/profile/" + username + "/album/" + url.QueryEscape(artist.Name) + "/" + url.QueryEscape(al.Title),
					Count:  count,
					Score:  albumSim,
				})
			}
		}

		sort.Slice(results, func(i, j int) bool {
			return results[i].Score+float64(results[i].Count)*0.01 > results[j].Score+float64(results[j].Count)*0.01
		})

		w.Header().Set("Content-Type", "application/json")
		jsonBytes, _ := json.Marshal(results)
		w.Write(jsonBytes)
	}
}

func imageUploadHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Error(w, "Not logged in", http.StatusUnauthorized)
			return
		}

		const maxFileSize = 5 * 1024 * 1024

		err := r.ParseMultipartForm(maxFileSize)
		if err != nil {
			http.Error(w, "File too large or invalid", http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "No file uploaded", http.StatusBadRequest)
			return
		}
		defer file.Close()

		if header.Size > maxFileSize {
			http.Error(w, "File exceeds 5MB limit", http.StatusBadRequest)
			return
		}

		ext := filepath.Ext(header.Filename)
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".webp" {
			http.Error(w, "Invalid file type", http.StatusBadRequest)
			return
		}

		hash := sha256.New()
		io.Copy(hash, file)
		file.Seek(0, 0)

		hashBytes := hash.Sum(nil)
		filename := hex.EncodeToString(hashBytes) + ext

		uploadDir := "./static/uploads"
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating upload dir: %v\n", err)
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}

		dst, err := os.Create(filepath.Join(uploadDir, filename))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating file: %v\n", err)
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}
		defer dst.Close()

		_, err = io.Copy(dst, file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error saving file: %v\n", err)
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"url": "/files/uploads/" + filename,
		})
	}
}
