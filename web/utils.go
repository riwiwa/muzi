package web

// Functions used in the HTML templates

import (
	"fmt"
	"hash/fnv"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"muzi/db"
)

// Subtracts two integers
func sub(a int, b int) int {
	return a - b
}

// Adds two integers
func add(a int, b int) int {
	return a + b
}

// Divides two integers (integer division)
func div(a int, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}

// Returns a % b
func mod(a int, b int) int {
	return a % b
}

// Put a comma in the thousands place, ten-thousands place etc.
func formatInt(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	} else {
		return formatInt(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
	}
}

// Formats timestamps compared to local time
func formatTimestamp(timestamp time.Time) string {
	now := time.Now()
	duration := now.Sub(timestamp)

	if duration < 24*time.Hour {
		seconds := int(duration.Seconds())
		if seconds < 60 {
			return fmt.Sprintf("%d seconds ago", seconds)
		}
		minutes := seconds / 60
		if minutes < 60 {
			return fmt.Sprintf("%d minutes ago", minutes)
		}
		hours := minutes / 60
		return fmt.Sprintf("%d hours ago", hours)
	}

	year := now.Year()
	if timestamp.Year() == year {
		return timestamp.Format("2 Jan 3:04pm")
	}

	return timestamp.Format("2 Jan 2006 3:04pm")
}

// Full timestamp format for browser hover
func formatTimestampFull(timestamp time.Time) string {
	return timestamp.Format("Monday 2 Jan 2006, 3:04pm")
}

// GetArtistNames takes artist IDs and returns a slice of artist names
func GetArtistNames(artistIds []int) []string {
	if artistIds == nil {
		return nil
	}
	var names []string
	for _, id := range artistIds {
		artist, err := db.GetArtistById(id)
		if err == nil {
			names = append(names, artist.Name)
		}
	}
	return names
}

// Percentage of part in whole, for chart bars
func pct(part, whole int) int {
	if whole <= 0 {
		return 0
	}
	return part * 100 / whole
}

// Heading for a group of plays in a feed: "Today", "Yesterday", a weekday within the last
// week, otherwise a date
func dayLabel(t time.Time) string {
	// "today" in the timestamp's own zone, so labels follow the profile owner's timezone
	today := startOfDay(time.Now().In(t.Location()))
	day := startOfDay(t)
	switch days := int(today.Sub(day).Hours() / 24); {
	case days <= 0:
		return "Today"
	case days == 1:
		return "Yesterday"
	case days < 7:
		return day.Format("Monday")
	case day.Year() == today.Year():
		return day.Format("Monday, January 2")
	default:
		return day.Format("January 2, 2006")
	}
}

// Time of a play within its day group: relative for the last hour, otherwise a clock time
func feedTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return t.Format("3:04pm")
	}
}

// Rewrites the current query string with the given key/value pairs, for filter links that keep
// the rest of the page's state. An empty value removes the key.
func withParams(rawQuery string, kv ...string) string {
	q, _ := url.ParseQuery(rawQuery)
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "" {
			q.Del(kv[i])
		} else {
			q.Set(kv[i], kv[i+1])
		}
	}
	q.Del("page")
	return "?" + q.Encode()
}

// A stable hue (0-359) for a name, so placeholder art gets a consistent color
func hue(name string) int {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return int(h.Sum32() % 360)
}

// First letter or digit of a name for placeholder art
func initial(name string) string {
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	if r, _ := utf8.DecodeRuneInString(name); r != utf8.RuneError {
		return string(r)
	}
	return "?"
}

// CSS view-transition-name for an entity's artwork, so it morphs between pages
func artName(kind string, id int) string {
	if id <= 0 {
		return "none"
	}
	return fmt.Sprintf("%s-%d", kind, id)
}

// Builds a map from alternating keys and values, for passing several values to a sub-template
func dict(kv ...any) map[string]any {
	m := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		if key, ok := kv[i].(string); ok {
			m[key] = kv[i+1]
		}
	}
	return m
}

// Two-digit chart position from a zero-based index: 0 -> "01"
func rank(i int) string {
	return fmt.Sprintf("%02d", i+1)
}

type periodOption struct {
	Key   string
	Label string
}

// Period filters for profile charts, in display order
func periods() []periodOption {
	return []periodOption{
		{"week", "7d"},
		{"month", "30d"},
		{"year", "Year"},
		{"all_time", "All"},
	}
}

// Chart sizes offered for a view; grids fill evenly at these (see chartLimit)
func limits(view string) []int {
	if view == "grid" {
		return []int{5, 9, 13}
	}
	return []int{10, 20, 30}
}
