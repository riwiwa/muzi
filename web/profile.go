package web

// Functions used for user profiles in the web UI

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"muzi/db"
	"muzi/scrobble"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgtype"
)

type ProfileData struct {
	Username            string
	Bio                 string
	Pfp                 string
	AllowDuplicateEdits bool
	ScrobbleCount       int
	TrackCount          int
	ArtistCount         int
	Artists             []string
	ArtistIdsList       [][]int
	Titles              []string
	Times               []time.Time
	Page                int
	Title               string
	LoggedInUsername    string
	TemplateName        string
	NowPlayingArtist    string
	NowPlayingTitle     string
	TopArtists          []db.TopArtist
	TopArtistsPeriod    string
	TopArtistsLimit     int
	TopArtistsView      string
	TopAlbums           []db.TopAlbum
	TopAlbumsPeriod     string
	TopAlbumsLimit      int
	TopAlbumsView       string
	TopTracks           []db.TopTrack
	TopTracksPeriod     string
	TopTracksLimit      int
}

// Render a page of the profile in the URL
func profilePageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")

		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find user %s: %v\n", username, err)
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		pageStr := r.URL.Query().Get("page")
		var pageInt int
		if pageStr == "" {
			pageInt = 1
		} else {
			pageInt, err = strconv.Atoi(pageStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cannot convert page URL query from string to int: %v\n", err)
				pageInt = 1
			}
		}

		lim := 15
		off := (pageInt - 1) * lim

		var profileData ProfileData
		profileData.Username = username
		profileData.Page = pageInt
		profileData.Title = username + "'s Profile"
		profileData.LoggedInUsername = getLoggedInUsername(r)
		profileData.TemplateName = "profile"

		err = db.Pool.QueryRow(
			r.Context(),
			`SELECT bio, pfp, allow_duplicate_edits,
				(SELECT COUNT(*) FROM history WHERE user_id = $1) as scrobble_count,
				(SELECT COUNT(*) FROM songs WHERE user_id = $1) as track_count,
				(SELECT COUNT(DISTINCT artist) FROM history WHERE user_id = $1) as artist_count
			FROM users WHERE pk = $1;`,
			userId,
		).Scan(&profileData.Bio, &profileData.Pfp, &profileData.AllowDuplicateEdits, &profileData.ScrobbleCount, &profileData.TrackCount, &profileData.ArtistCount)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get profile for %s: %v\n", username, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		period := r.URL.Query().Get("period")
		if period == "" {
			period = "all_time"
		}

		view := r.URL.Query().Get("view")
		if view == "" {
			view = "grid"
		}

		maxLimit := 30
		if view == "grid" {
			maxLimit = 8
		}

		limitStr := r.URL.Query().Get("limit")
		limit := 10
		if limitStr != "" {
			limit, err = strconv.Atoi(limitStr)
			if err != nil || limit < 5 {
				limit = 10
			}
			if limit > maxLimit {
				limit = maxLimit
			}
		}

		profileData.TopArtistsPeriod = period
		profileData.TopArtistsLimit = limit
		profileData.TopArtistsView = view

		var startDate, endDate *time.Time
		now := time.Now()
		switch period {
		case "week":
			start := now.AddDate(0, 0, -7)
			startDate = &start
		case "month":
			start := now.AddDate(0, -1, 0)
			startDate = &start
		case "year":
			start := now.AddDate(-1, 0, 0)
			startDate = &start
		case "custom":
			startStr := r.URL.Query().Get("start")
			endStr := r.URL.Query().Get("end")
			if startStr != "" {
				if t, err := time.Parse("2006-01-02", startStr); err == nil {
					startDate = &t
				}
			}
			if endStr != "" {
				if t, err := time.Parse("2006-01-02", endStr); err == nil {
					t = t.AddDate(0, 0, 1)
					endDate = &t
				}
			}
		}

		topArtists, err := db.GetTopArtists(userId, limit, startDate, endDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get top artists: %v\n", err)
		} else {
			profileData.TopArtists = topArtists
		}

		albumPeriod := r.URL.Query().Get("album_period")
		if albumPeriod == "" {
			albumPeriod = "all_time"
		}

		var albumStartDate, albumEndDate *time.Time
		albumNow := time.Now()
		switch albumPeriod {
		case "week":
			start := albumNow.AddDate(0, 0, -7)
			albumStartDate = &start
		case "month":
			start := albumNow.AddDate(0, -1, 0)
			albumStartDate = &start
		case "year":
			start := albumNow.AddDate(-1, 0, 0)
			albumStartDate = &start
		case "custom":
			albumStartStr := r.URL.Query().Get("album_start")
			albumEndStr := r.URL.Query().Get("album_end")
			if albumStartStr != "" {
				if t, err := time.Parse("2006-01-02", albumStartStr); err == nil {
					albumStartDate = &t
				}
			}
			if albumEndStr != "" {
				if t, err := time.Parse("2006-01-02", albumEndStr); err == nil {
					t = t.AddDate(0, 0, 1)
					albumEndDate = &t
				}
			}
		}

		albumLimitStr := r.URL.Query().Get("album_limit")
		albumLimit := 10
		if albumLimitStr != "" {
			albumLimit, err = strconv.Atoi(albumLimitStr)
			if err != nil || albumLimit < 5 {
				albumLimit = 10
			}
			if albumLimit > 30 {
				albumLimit = 30
			}
		}

		albumView := r.URL.Query().Get("album_view")
		if albumView == "" {
			albumView = "grid"
		}
		albumMaxLimit := 30
		if albumView == "grid" {
			albumMaxLimit = 8
		}
		if albumLimit > albumMaxLimit {
			albumLimit = albumMaxLimit
		}

		profileData.TopAlbumsPeriod = albumPeriod
		profileData.TopAlbumsLimit = albumLimit
		profileData.TopAlbumsView = albumView

		topAlbums, err := db.GetTopAlbums(userId, albumLimit, albumStartDate, albumEndDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get top albums: %v\n", err)
		} else {
			profileData.TopAlbums = topAlbums
		}

		trackPeriod := r.URL.Query().Get("track_period")
		if trackPeriod == "" {
			trackPeriod = "all_time"
		}

		var trackStartDate, trackEndDate *time.Time
		trackNow := time.Now()
		switch trackPeriod {
		case "week":
			start := trackNow.AddDate(0, 0, -7)
			trackStartDate = &start
		case "month":
			start := trackNow.AddDate(0, -1, 0)
			trackStartDate = &start
		case "year":
			start := trackNow.AddDate(-1, 0, 0)
			trackStartDate = &start
		case "custom":
			trackStartStr := r.URL.Query().Get("track_start")
			trackEndStr := r.URL.Query().Get("track_end")
			if trackStartStr != "" {
				if t, err := time.Parse("2006-01-02", trackStartStr); err == nil {
					trackStartDate = &t
				}
			}
			if trackEndStr != "" {
				if t, err := time.Parse("2006-01-02", trackEndStr); err == nil {
					t = t.AddDate(0, 0, 1)
					trackEndDate = &t
				}
			}
		}

		trackLimitStr := r.URL.Query().Get("track_limit")
		trackLimit := 10
		if trackLimitStr != "" {
			trackLimit, err = strconv.Atoi(trackLimitStr)
			if err != nil || trackLimit < 5 {
				trackLimit = 10
			}
			if trackLimit > 30 {
				trackLimit = 30
			}
		}

		profileData.TopTracksPeriod = trackPeriod
		profileData.TopTracksLimit = trackLimit

		topTracks, err := db.GetTopTracks(userId, trackLimit, trackStartDate, trackEndDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get top tracks: %v\n", err)
		} else {
			profileData.TopTracks = topTracks
		}

		if pageInt == 1 {
			if np, ok := scrobble.GetNowPlaying(userId); ok {
				profileData.NowPlayingArtist = np.Artist
				profileData.NowPlayingTitle = np.SongName
			}
		}

		rows, err := db.Pool.Query(
			r.Context(),
			"SELECT artist_id, song_name, timestamp, artist_ids FROM history WHERE user_id = $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3;",
			userId,
			lim,
			off,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SELECT history failed: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var artistId int
			var title string
			var time pgtype.Timestamptz
			var artistIds []int
			err = rows.Scan(&artistId, &title, &time, &artistIds)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Scanning history row failed: %v\n", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			var artistName string
			if artistId > 0 {
				artist, err := db.GetArtistById(artistId)
				if err == nil {
					artistName = artist.Name
				}
			}

			profileData.Artists = append(profileData.Artists, artistName)
			profileData.ArtistIdsList = append(profileData.ArtistIdsList, artistIds)
			profileData.Titles = append(profileData.Titles, title)
			profileData.Times = append(profileData.Times, time.Time)
		}

		err = templates.ExecuteTemplate(w, "base", profileData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
