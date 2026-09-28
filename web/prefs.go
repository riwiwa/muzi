package web

// Remembers how each logged-in user last set up the profile charts (period, custom range, grid or
// chart view, and size for top artists, albums and tracks), so the choices survive reloads, new
// logins and other devices. They're the viewer's own and apply to any profile they look at.

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"muzi/db"
)

// The chart sections' URL parameter prefixes: top artists, albums, tracks
var chartSections = []string{"", "album_", "track_"}

var chartSettingNames = []string{"period", "start", "end", "view", "limit"}

// Longest value worth saving; real ones are short (dates, "all_time", numbers)
const maxPrefLength = 32

// Returns the profile's query parameters with any chart settings the URL leaves out filled in from
// the viewer's saved choices, saving the ones the URL changes
func profileChartSettings(r *http.Request) url.Values {
	q := r.URL.Query()
	viewer := getLoggedInUsername(r)
	if viewer == "" {
		return q
	}
	viewerId, err := getUserIdByUsername(r.Context(), viewer)
	if err != nil {
		return q
	}
	saved, err := db.GetProfilePrefs(viewerId)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profile preferences: %v\n", err)
		return q
	}

	changed := false
	set := func(key, value string) {
		if value == "" || len(value) > maxPrefLength {
			if _, ok := saved[key]; ok {
				delete(saved, key)
				changed = true
			}
			return
		}
		if saved[key] != value {
			saved[key] = value
			changed = true
		}
	}

	for _, section := range chartSections {
		// switching between grid and chart without a size means the new view's default size
		if q.Has(section+"view") && !q.Has(section+"limit") {
			set(section+"limit", "")
		}
		for _, name := range chartSettingNames {
			key := section + name
			if q.Has(key) {
				set(key, q.Get(key))
			} else if v, ok := saved[key]; ok {
				q.Set(key, v)
			}
		}
	}

	if changed {
		if err := db.SetProfilePrefs(viewerId, saved); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving profile preferences: %v\n", err)
		}
	}
	return q
}
