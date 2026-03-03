package web

// Functions used in the HTML templates

import (
	"fmt"
	"time"

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

// Returns a slice of a slice from start to end
func slice(a []db.TopArtist, start int, end int) []db.TopArtist {
	if start >= len(a) {
		return []db.TopArtist{}
	}
	if end > len(a) {
		end = len(a)
	}
	return a[start:end]
}

func sliceAlbum(a []db.TopAlbum, start int, end int) []db.TopAlbum {
	if start >= len(a) {
		return []db.TopAlbum{}
	}
	if end > len(a) {
		end = len(a)
	}
	return a[start:end]
}

func sliceTrack(a []db.TopTrack, start int, end int) []db.TopTrack {
	if start >= len(a) {
		return []db.TopTrack{}
	}
	if end > len(a) {
		end = len(a)
	}
	return a[start:end]
}

func gridReorder(artists []db.TopArtist) []db.TopArtist {
	if len(artists) < 2 {
		return artists
	}
	if len(artists)%2 == 0 {
		return artists
	}
	remaining := len(artists) - 1
	perRow := remaining / 2
	rest := artists[1:]
	firstRow := rest[:perRow]
	secondRow := rest[perRow:]
	var reordered []db.TopArtist
	reordered = append(reordered, artists[0])
	reordered = append(reordered, secondRow...)
	reordered = append(reordered, firstRow...)
	return reordered
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
