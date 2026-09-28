package migrate

// Imports Apple Music listening history from Apple's privacy export (privacy.apple.com, "Apple
// Media Services information"). Plays come from "Apple Music Play Activity.csv"; its artist
// column is often empty, so artists are looked up from the export's other files, which describe
// tracks as "Artist - Title". Columns are found by header name, since Apple adds and reorders
// them between export versions.

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"muzi/db"
)

const (
	// playback events of one song this close together are one listen split by seeking or pausing
	appleMergeGap = 30 * time.Second
	// skip plays this close to an existing play of the same song, in case they were also
	// scrobbled elsewhere (e.g. to Last.fm) with a slightly different timestamp
	appleNearDuplicate = 5 * time.Minute
)

type appleEvent struct {
	song, album, artist string
	start, end          time.Time
	played, length      int // milliseconds
}

// Collects the relevant files from an Apple export, in any combination of zips and extracted files
type AppleExport struct {
	events  []appleEvent
	artists map[string]map[string]int // lowercased title -> artist -> times seen
	files   []string                  // which relevant files were found
}

type AppleImportResult struct {
	Files         []string `json:"files"`
	Events        int      `json:"events"`          // playback events in Play Activity
	Plays         int      `json:"plays"`           // listens that count as plays
	Imported      int      `json:"imported"`        // newly added to history
	AlreadyInMuzi int      `json:"already_in_muzi"` // duplicates of plays muzi already had
	NoArtist      int      `json:"no_artist"`       // plays skipped because no artist could be found
}

func NewAppleExport() *AppleExport {
	return &AppleExport{artists: map[string]map[string]int{}}
}

func (x *AppleExport) HasPlayActivity() bool {
	for _, f := range x.files {
		if f == "Apple Music Play Activity.csv" {
			return true
		}
	}
	return false
}

// Reads a zip from the export, looking inside nested zips too (Apple nests them)
func (x *AppleExport) AddZip(r io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("not a readable zip: %w", err)
	}
	for _, f := range zr.File {
		name := path.Base(f.Name)
		switch {
		case strings.EqualFold(path.Ext(name), ".zip"):
			if err := x.addNestedZip(f); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		case isAppleMusicFile(name):
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = x.AddFile(name, rc)
			rc.Close()
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return nil
}

// Nested zips need random access, so they're copied to a temp file first
func (x *AppleExport) addNestedZip(f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp("", "muzi-apple-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, rc)
	if err != nil {
		return err
	}
	return x.AddZip(tmp, size)
}

func isAppleMusicFile(name string) bool {
	switch name {
	case "Apple Music Play Activity.csv",
		"Apple Music - Play History Daily Tracks.csv",
		"Apple Music - Recently Played Tracks.csv",
		"Apple Music - Track Play History.csv",
		"Apple Music Library Tracks.json":
		return true
	}
	return false
}

// Reads one file from the export by its name; files that aren't relevant are ignored
func (x *AppleExport) AddFile(name string, r io.Reader) error {
	name = path.Base(name)
	var err error
	switch name {
	case "Apple Music Play Activity.csv":
		err = x.readPlayActivity(r)
	case "Apple Music - Play History Daily Tracks.csv", "Apple Music - Recently Played Tracks.csv":
		err = x.readDescriptions(r, "Track Description")
	case "Apple Music - Track Play History.csv":
		err = x.readDescriptions(r, "Track Name")
	case "Apple Music Library Tracks.json":
		err = x.readLibrary(r)
	default:
		return nil
	}
	if err == nil {
		x.files = append(x.files, name)
	}
	return err
}

// Reads a CSV, calling row with a lookup by column name for each record
func readCSV(r io.Reader, row func(get func(string) string)) error {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	header, err := cr.Read()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	index := map[string]int{}
	for i, h := range header {
		index[strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))] = i
	}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		row(func(col string) string {
			if i, ok := index[col]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		})
	}
}

func parseAppleTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

func (x *AppleExport) readPlayActivity(r io.Reader) error {
	return readCSV(r, func(get func(string) string) {
		// older exports have no event type; newer ones also log lyric views and the like
		if t := get("Event Type"); t != "" && t != "PLAY_END" {
			return
		}
		song := get("Song Name")
		if song == "" {
			return
		}
		played, _ := strconv.Atoi(get("Play Duration Milliseconds"))
		length, _ := strconv.Atoi(get("Media Duration In Milliseconds"))
		end, hasEnd := parseAppleTime(get("Event End Timestamp"))
		start, hasStart := parseAppleTime(get("Event Start Timestamp"))
		if !hasStart {
			if !hasEnd {
				return
			}
			start = end.Add(-time.Duration(played) * time.Millisecond)
		}
		if !hasEnd {
			end = start.Add(time.Duration(played) * time.Millisecond)
		}
		album := get("Album Name")
		if album == "" {
			album = get("Container Album Name")
		}
		artist := get("Artist Name")
		if artist == "" {
			artist = get("Container Artist Name")
		}
		x.events = append(x.events, appleEvent{
			song: song, album: album, artist: artist,
			start: start, end: end, played: played, length: length,
		})
	})
}

func (x *AppleExport) learnArtist(title, artist string) {
	title, artist = strings.ToLower(strings.TrimSpace(title)), strings.TrimSpace(artist)
	if title == "" || artist == "" {
		return
	}
	if x.artists[title] == nil {
		x.artists[title] = map[string]int{}
	}
	x.artists[title][artist]++
}

// Learns artists from "Artist - Title" track descriptions
func (x *AppleExport) readDescriptions(r io.Reader, column string) error {
	return readCSV(r, func(get func(string) string) {
		if artist, title, ok := strings.Cut(get(column), " - "); ok {
			x.learnArtist(title, artist)
		}
	})
}

func (x *AppleExport) readLibrary(r io.Reader) error {
	var tracks []struct {
		Title  string `json:"Title"`
		Artist string `json:"Artist"`
	}
	if err := json.NewDecoder(r).Decode(&tracks); err != nil {
		return err
	}
	for _, t := range tracks {
		x.learnArtist(t.Title, t.Artist)
	}
	return nil
}

// The artist seen most often with this title (ties broken alphabetically), or ""
func (x *AppleExport) artistFor(title string) string {
	best, bestCount := "", 0
	for artist, count := range x.artists[strings.ToLower(strings.TrimSpace(title))] {
		if count > bestCount || (count == bestCount && artist < best) {
			best, bestCount = artist, count
		}
	}
	return best
}

// Last.fm's rule: at least half the track, or four minutes. Without a known length only the
// four minutes can be checked.
func countsAsPlay(played, length int) bool {
	if length > 0 && played*2 >= length {
		return true
	}
	return played >= 240000
}

// Turns playback events into plays: listens split by seeking or pausing are merged, then
// Last.fm's rule decides what counts. Returns the plays and how many lacked an artist.
func (x *AppleExport) plays(userId int) (plays []Play, listens int, noArtist int) {
	events := append([]appleEvent(nil), x.events...)
	sort.Slice(events, func(i, j int) bool { return events[i].start.Before(events[j].start) })

	var merged []appleEvent
	for _, e := range events {
		if n := len(merged); n > 0 {
			last := &merged[n-1]
			if last.song == e.song && last.album == e.album && e.start.Sub(last.end) <= appleMergeGap {
				last.played += e.played
				if e.end.After(last.end) {
					last.end = e.end
				}
				last.length = max(last.length, e.length)
				continue
			}
		}
		merged = append(merged, e)
	}

	for _, l := range merged {
		if !countsAsPlay(l.played, l.length) {
			continue
		}
		artist := l.artist
		if artist == "" {
			artist = x.artistFor(l.song)
		}
		if artist == "" {
			noArtist++
			continue
		}
		plays = append(plays, Play{
			UserId:    userId,
			Timestamp: l.start,
			SongName:  l.song,
			Artist:    artist,
			Album:     l.album,
			MsPlayed:  cappedPlay(l.played, l.length),
		})
	}
	return plays, len(merged), noArtist
}

// Merged listens can add up to more than the track (replays after seeking); store at most its length
func cappedPlay(played, length int) int {
	if length > 0 && played > length {
		return length
	}
	return played
}

// Adds the export's plays to the user's history
func ImportApple(userId int, x *AppleExport) (AppleImportResult, error) {
	res := AppleImportResult{Files: x.files, Events: len(x.events)}
	plays, _, noArtist := x.plays(userId)
	res.Plays, res.NoArtist = len(plays)+noArtist, noArtist

	inserted, err := insertPlays(plays, "apple_music", appleNearDuplicate)
	if err != nil {
		return res, err
	}
	res.Imported = inserted
	res.AlreadyInMuzi = len(plays) - inserted

	if err := db.BackfillEntities(); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating albums and songs after Apple Music import: %v\n", err)
	}
	return res, nil
}
