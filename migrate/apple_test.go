package migrate

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

const testPlayActivity = "\ufeffEvent Type,Event Start Timestamp,Event End Timestamp,Song Name,Album Name,Container Artist Name,Play Duration Milliseconds,Media Duration In Milliseconds,Client IP Address\n" +
	// full play
	"PLAY_END,2020-09-18T18:00:00.000Z,2020-09-18T18:03:00.000Z,Blue Plastic,Warlord,,180000,180000,1.2.3.4\n" +
	// one listen split by seeking: 60s + 70s of a 200s track, merged to 130s -> counts
	"PLAY_END,2020-09-18T18:10:00.000Z,2020-09-18T18:11:00.000Z,Reality Surf,Icedancer,,60000,200000,1.2.3.4\n" +
	"PLAY_END,2020-09-18T18:11:10.000Z,2020-09-18T18:12:20.000Z,Reality Surf,Icedancer,,70000,200000,1.2.3.4\n" +
	// skipped after 20s -> not a play
	"PLAY_END,2020-09-18T18:20:00.000Z,2020-09-18T18:20:20.000Z,Be Nice 2 Me,Icedancer,,20000,128000,1.2.3.4\n" +
	// lyric view, not a playback
	"LYRIC_DISPLAY,2020-09-18T18:21:00.000Z,2020-09-18T18:22:00.000Z,Be Nice 2 Me,Icedancer,,0,128000,1.2.3.4\n" +
	// no start time: start = end - played
	"PLAY_END,,2020-09-18T19:00:00.000Z,Somebody to Love,A Day at the Races,,297000,297000,1.2.3.4\n" +
	// artist given directly in the row
	"PLAY_END,2020-09-18T20:00:00.000Z,2020-09-18T20:05:00.000Z,Hate on Me,Hate on Me,YG,300000,165000,1.2.3.4\n" +
	// full play, but no artist anywhere in the export
	"PLAY_END,2020-09-18T21:00:00.000Z,2020-09-18T21:03:00.000Z,Mystery Song,Unknown,,180000,180000,1.2.3.4\n"

const testDailyTracks = "Date Played,Track Description\n" +
	"20200918,Yung Lean - Blue Plastic\n" +
	"20200918,Bladee - Reality Surf\n" +
	"20200918,Queen - Somebody to Love\n" +
	// a cover with the same title, seen less often than Queen's
	"20200918,Someone Else - Somebody to Love\n"

const testTrackHistory = "Track Name,Last Played Date\nQueen - Somebody to Love,1600000000000\n"

func testExport(t *testing.T) *AppleExport {
	x := NewAppleExport()
	for name, body := range map[string]string{
		"Apple Music Play Activity.csv":               testPlayActivity,
		"Apple Music - Play History Daily Tracks.csv": testDailyTracks,
		"Apple Music - Track Play History.csv":        testTrackHistory,
		"Some Unrelated File.csv":                     "a,b\n1,2\n",
	} {
		if err := x.AddFile(name, strings.NewReader(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	return x
}

func TestApplePlays(t *testing.T) {
	x := testExport(t)
	plays, listens, noArtist := x.plays(7)

	if got := len(x.events); got != 7 {
		t.Errorf("read %d playback events, want 7 (the lyric view is not one)", got)
	}
	if listens != 6 {
		t.Errorf("got %d listens, want 6 (two split events merge into one)", listens)
	}
	if noArtist != 1 {
		t.Errorf("got %d plays without an artist, want 1", noArtist)
	}

	want := map[string]string{
		"Blue Plastic":     "Yung Lean",
		"Reality Surf":     "Bladee",
		"Somebody to Love": "Queen",
		"Hate on Me":       "YG",
	}
	if len(plays) != len(want) {
		t.Fatalf("got %d plays, want %d: %+v", len(plays), len(want), plays)
	}
	for _, p := range plays {
		if p.UserId != 7 {
			t.Errorf("%s: user %d, want 7", p.SongName, p.UserId)
		}
		if want[p.SongName] != p.Artist {
			t.Errorf("%s: artist %q, want %q", p.SongName, p.Artist, want[p.SongName])
		}
		switch p.SongName {
		case "Reality Surf":
			if p.MsPlayed != 130000 || p.Timestamp.Format("15:04:05") != "18:10:00" {
				t.Errorf("merged listen: played %d at %s, want 130000 at 18:10:00", p.MsPlayed, p.Timestamp.Format("15:04:05"))
			}
		case "Somebody to Love":
			if p.Timestamp.Format("15:04:05") != "18:55:03" {
				t.Errorf("start without a timestamp should be end minus played, got %s", p.Timestamp.Format("15:04:05"))
			}
		case "Hate on Me":
			if p.MsPlayed != 165000 {
				t.Errorf("played time should be capped at the track length, got %d", p.MsPlayed)
			}
		}
	}
}

func TestCountsAsPlay(t *testing.T) {
	cases := []struct {
		played, length int
		want           bool
	}{
		{100000, 200000, true},  // exactly half
		{99999, 200000, false},  // just under half
		{240000, 600000, true},  // four minutes of a long track
		{239999, 600000, false}, // not quite
		{240000, 0, true},       // unknown length: four minutes still counts
		{100000, 0, false},
	}
	for _, c := range cases {
		if got := countsAsPlay(c.played, c.length); got != c.want {
			t.Errorf("countsAsPlay(%d, %d) = %v, want %v", c.played, c.length, got, c.want)
		}
	}
}

// Apple ships the music files inside a zip inside the zip you download
func TestAppleNestedZip(t *testing.T) {
	var inner bytes.Buffer
	zw := zip.NewWriter(&inner)
	for name, body := range map[string]string{
		"Apple_Media_Services/Apple Music Activity/Apple Music Play Activity.csv":               testPlayActivity,
		"Apple_Media_Services/Apple Music Activity/Apple Music - Play History Daily Tracks.csv": testDailyTracks,
		"Apple_Media_Services/Stores Activity/Apple Pay Activity.csv":                           "private,data\n",
	} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()

	var outer bytes.Buffer
	zw = zip.NewWriter(&outer)
	w, _ := zw.Create("Apple Media Services Information Part 1 of 2/Apple_Media_Services.zip")
	w.Write(inner.Bytes())
	zw.Close()

	x := NewAppleExport()
	if err := x.AddZip(bytes.NewReader(outer.Bytes()), int64(outer.Len())); err != nil {
		t.Fatal(err)
	}
	if !x.HasPlayActivity() || len(x.files) != 2 {
		t.Fatalf("found files %v, want the two Apple Music files", x.files)
	}
	if plays, _, _ := x.plays(1); len(plays) != 4 {
		t.Errorf("got %d plays from the nested zip, want 4", len(plays))
	}
}
