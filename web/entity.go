package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
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
	Album            db.Album
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

		song, err := db.GetSongByName(userId, songTitle, 0)
		if err != nil {
			songs, searchErr := db.SearchSongs(userId, songTitle)
			if searchErr == nil && len(songs) > 0 {
				song = songs[0]
			} else {
				fmt.Fprintf(os.Stderr, "Cannot find song %s: %v\n", songTitle, err)
				http.Error(w, "Song not found", http.StatusNotFound)
				return
			}
		}

		artist, _ := db.GetArtistById(song.ArtistId)
		var album db.Album
		if song.AlbumId > 0 {
			album, _ = db.GetAlbumById(song.AlbumId)
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

		listenCount, err := db.GetSongStats(userId, song.Id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get song stats: %v\n", err)
		}

		entries, err := db.GetHistoryForSong(userId, song.Id, lim, off)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get history for song: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		songData := SongData{
			Username:         username,
			Song:             song,
			Artist:           artist,
			Album:            album,
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

		http.Redirect(w, r, "/song/"+url.QueryEscape(title)+"?username="+username, http.StatusSeeOther)
	}
}

func albumPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")
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

		album, err := db.GetAlbumByName(userId, albumTitle, 0)
		if err != nil {
			albums, searchErr := db.SearchAlbums(userId, albumTitle)
			if searchErr == nil && len(albums) > 0 {
				album = albums[0]
			} else {
				fmt.Fprintf(os.Stderr, "Cannot find album %s: %v\n", albumTitle, err)
				http.Error(w, "Album not found", http.StatusNotFound)
				return
			}
		}

		artist, _ := db.GetArtistById(album.ArtistId)

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

		http.Redirect(w, r, "/profile/"+username+"/album/"+url.QueryEscape(title), http.StatusSeeOther)
	}
}

type SearchResult struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Url   string `json:"url"`
	Count int    `json:"count"`
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

		artists, err := db.SearchArtists(userId, query)
		if err == nil {
			for _, a := range artists {
				count, _ := db.GetArtistStats(userId, a.Id)
				results = append(results, SearchResult{
					Type:  "artist",
					Name:  a.Name,
					Url:   "/profile/" + username + "/artist/" + url.QueryEscape(a.Name),
					Count: count,
				})
			}
		}

		songs, err := db.SearchSongs(userId, query)
		if err == nil {
			for _, s := range songs {
				count, _ := db.GetSongStats(userId, s.Id)
				results = append(results, SearchResult{
					Type:  "song",
					Name:  s.Title,
					Url:   "/profile/" + username + "/song/" + url.QueryEscape(s.Title),
					Count: count,
				})
			}
		}

		albums, err := db.SearchAlbums(userId, query)
		if err == nil {
			for _, al := range albums {
				count, _ := db.GetAlbumStats(userId, al.Id)
				results = append(results, SearchResult{
					Type:  "album",
					Name:  al.Title,
					Url:   "/profile/" + username + "/album/" + url.QueryEscape(al.Title),
					Count: count,
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		jsonBytes, _ := json.Marshal(results)
		w.Write(jsonBytes)
	}
}
