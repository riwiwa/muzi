package web

import (
	"time"

	"muzi/db"
)

// The timezone a user's pages are shown in: theirs if set and valid, otherwise the server's.
// Returns the IANA name too ("" for the server default), for timezone-aware queries.
func userLocation(userId int) (*time.Location, string) {
	tz, err := db.GetUserTimezone(userId)
	if err != nil || tz == "" {
		return time.Local, ""
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Local, ""
	}
	return loc, tz
}

// Shows a page of plays in the given timezone (day groupings and times follow each timestamp's zone)
func inLocation(entries []db.ScrobbleEntry, loc *time.Location) []db.ScrobbleEntry {
	for i := range entries {
		entries[i].Timestamp = entries[i].Timestamp.In(loc)
	}
	return entries
}
