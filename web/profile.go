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
)

type ProfileData struct {
	Username            string
	Bio                 string
	Pfp                 string
	AllowDuplicateEdits bool
	ScrobbleCount       int
	TrackCount          int
	ArtistCount         int
	History             []db.ScrobbleEntry
	Rhythm              *Rhythm
	RawQuery            string
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

// Chart sizes: grids are a 2x2 hero tile plus rows of four, so they fill evenly at 5, 9 or 13
func chartLimit(view, raw string) int {
	limit, err := strconv.Atoi(raw)
	if view == "grid" {
		if limit == 5 || limit == 9 || limit == 13 {
			return limit
		}
		return 9
	}
	if err != nil || limit < 5 {
		return 10
	}
	return min(limit, 30)
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

		limit := chartLimit(view, r.URL.Query().Get("limit"))

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

		albumView := r.URL.Query().Get("album_view")
		if albumView == "" {
			albumView = "grid"
		}
		albumLimit := chartLimit(albumView, r.URL.Query().Get("album_limit"))

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

		trackLimit := chartLimit("list", r.URL.Query().Get("track_limit"))

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

		profileData.History, err = db.GetHistory(userId, lim, off)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get history: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if pageInt == 1 {
			profileData.Rhythm = buildRhythm(userId)
		}
		profileData.RawQuery = r.URL.RawQuery

		err = templates.ExecuteTemplate(w, "base", profileData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
