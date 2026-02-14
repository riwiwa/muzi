package web

// Functions used in the HTML templates

import (
	"fmt"
	"time"
)

// Subtracts two integers
func sub(a int, b int) int {
	return a - b
}

// Adds two integers
func add(a int, b int) int {
	return a + b
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
