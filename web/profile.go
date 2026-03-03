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
