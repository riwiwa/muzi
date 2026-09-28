package web

// The grid maker: an NxN collage of your top albums or artists for a period, which the page
// can save as a PNG or copy to the clipboard (drawn client-side in static/app.js)

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"muzi/db"
)

const (
	gridMinSize     = 1
	gridMaxSize     = 10
	gridDefaultSize = 3
)

type GridItem struct {
	Name     string
	Sub      string // artist for albums
	ImageUrl string
	Plays    int
	Url      string
}

type GridData struct {
	Title            string
	LoggedInUsername string
	TemplateName     string
	RawQuery         string
	Kind             string // "albums" or "artists"
	Period           string
	Size             int
	Sizes            []int
	Names            bool
	Items            []GridItem
	Empty            []int // placeholder cells when there aren't enough items to fill the grid
}

func gridPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := getLoggedInUsername(r)
		if username == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		userId, err := getUserIdByUsername(r.Context(), username)
		if err != nil {
			http.Error(w, "User not found", http.StatusInternalServerError)
			return
		}

		q := r.URL.Query()
		d := GridData{
			Title:            "Grid",
			LoggedInUsername: username,
			TemplateName:     "grid",
			RawQuery:         r.URL.RawQuery,
			Kind:             "albums",
			Period:           "week",
			Size:             gridDefaultSize,
			Names:            q.Get("names") == "1",
		}
		if q.Get("kind") == "artists" {
			d.Kind = "artists"
		}
		switch p := q.Get("period"); p {
		case "week", "month", "year", "all_time", "custom":
			d.Period = p
		}
		if n, err := strconv.Atoi(q.Get("size")); err == nil && n >= gridMinSize && n <= gridMaxSize {
			d.Size = n
		}
		for n := gridMinSize; n <= gridMaxSize; n++ {
			d.Sizes = append(d.Sizes, n)
		}

		start, end := periodRange(d.Period, q.Get("start"), q.Get("end"))
		limit := d.Size * d.Size
		if d.Kind == "artists" {
			artists, err := db.GetTopArtists(userId, limit, start, end)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cannot get top artists for grid: %v\n", err)
			}
			for _, a := range artists {
				d.Items = append(d.Items, GridItem{
					Name:     a.Artist.Name,
					ImageUrl: a.Artist.ImageUrl,
					Plays:    a.ListenCount,
					Url:      "/profile/" + username + "/artist/" + url.QueryEscape(a.Artist.Name),
				})
			}
		} else {
			albums, err := db.GetTopAlbums(userId, limit, start, end)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cannot get top albums for grid: %v\n", err)
			}
			for _, a := range albums {
				d.Items = append(d.Items, GridItem{
					Name:     a.AlbumName,
					Sub:      a.Artist,
					ImageUrl: a.CoverUrl,
					Plays:    a.ListenCount,
					Url:      "/profile/" + username + "/album/" + url.QueryEscape(a.Artist) + "/" + url.QueryEscape(a.AlbumName),
				})
			}
		}
		d.Empty = make([]int, limit-len(d.Items))

		if err := templates.ExecuteTemplate(w, "base", d); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
