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

// Start/end bounds for a chart period ("week", "month", "year", "custom" with YYYY-MM-DD dates,
// anything else is all time). The custom end date is inclusive.
func periodRange(period, startStr, endStr string, loc *time.Location) (startDate, endDate *time.Time) {
	now := time.Now()
	switch period {
	case "day":
		// since midnight in the profile owner's timezone
		start := startOfDay(now.In(loc))
		startDate = &start
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
		if t, _, ok := parseRangeTime(startStr, loc); ok {
			startDate = &t
		}
		if t, dateOnly, ok := parseRangeTime(endStr, loc); ok {
			if dateOnly {
				// a bare end date includes that whole day
				t = t.AddDate(0, 0, 1)
			}
			endDate = &t
		}
	}
	return startDate, endDate
}

// Parses a custom range bound: a date and time from the range picker ("2006-01-02T15:04"),
// or a bare date as older links use. dateOnly says which it was.
func parseRangeTime(s string, loc *time.Location) (t time.Time, dateOnly bool, ok bool) {
	if t, err := time.ParseInLocation("2006-01-02T15:04", s, loc); err == nil {
		return t, false, true
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t, true, true
	}
	return time.Time{}, false, false
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
		if !canViewProfile(r, userId) {
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

		loc, tz := userLocation(userId)
		startDate, endDate := periodRange(period, r.URL.Query().Get("start"), r.URL.Query().Get("end"), loc)

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

		albumStartDate, albumEndDate := periodRange(albumPeriod, r.URL.Query().Get("album_start"), r.URL.Query().Get("album_end"), loc)

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

		trackStartDate, trackEndDate := periodRange(trackPeriod, r.URL.Query().Get("track_start"), r.URL.Query().Get("track_end"), loc)

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
		profileData.History = inLocation(profileData.History, loc)
		if pageInt == 1 {
			profileData.Rhythm = buildRhythm(userId, loc, tz)
		}
		profileData.RawQuery = r.URL.RawQuery

		err = templates.ExecuteTemplate(w, "base", profileData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
