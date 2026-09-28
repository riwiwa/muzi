package scrobble

import (
	"context"
	"testing"
	"time"

	"muzi/db"
	"muzi/internal/dbtest"
)

func TestSaveScrobbleSkipsDuplicates(t *testing.T) {
	dbtest.Setup(t)
	user, _ := dbtest.CreateUser(t)
	s := Scrobble{UserId: user, Timestamp: time.Date(2024, 2, 1, 9, 0, 0, 0, time.UTC),
		SongName: "Track", Artist: "Artist A, Artist B", Album: "Record", MsPlayed: 200000, Platform: "test"}

	if err := SaveScrobble(s); err != nil {
		t.Fatal(err)
	}
	if err := SaveScrobble(s); err == nil {
		t.Error("saving the same scrobble twice should report a duplicate")
	}

	var plays, artists int
	var linked bool
	db.Pool.QueryRow(context.Background(),
		`SELECT count(*), max(cardinality(artist_ids)), bool_and(song_id IS NOT NULL)
		FROM history WHERE user_id = $1`, user).Scan(&plays, &artists, &linked)
	if plays != 1 || artists != 2 || !linked {
		t.Errorf("got %d plays with %d artists (song linked: %v), want 1 play, 2 artists, linked", plays, artists, linked)
	}
}

func TestEditScrobblesOnlyTouchesOwnPlays(t *testing.T) {
	dbtest.Setup(t)
	owner, _ := dbtest.CreateUser(t)
	other, _ := dbtest.CreateUser(t)
	at := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	for _, u := range []int{owner, other} {
		if err := SaveScrobble(Scrobble{UserId: u, Timestamp: at, SongName: "Old Title", Artist: "Old Artist", Platform: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	var ownId, otherId int
	db.Pool.QueryRow(context.Background(), "SELECT id FROM history WHERE user_id = $1", owner).Scan(&ownId)
	db.Pool.QueryRow(context.Background(), "SELECT id FROM history WHERE user_id = $1", other).Scan(&otherId)

	title, artist := "New Title", "New Artist, Guest"
	updated, skipped, err := EditScrobbles(owner, []int{ownId, otherId}, ScrobbleEdit{SongName: &title, Artist: &artist})
	if err != nil || updated != 1 || skipped != 0 {
		t.Fatalf("edit: updated %d, skipped %d, err %v; want 1, 0, nil", updated, skipped, err)
	}

	var gotTitle, primary string
	var artists int
	db.Pool.QueryRow(context.Background(),
		`SELECT h.song_name, a.name, cardinality(h.artist_ids) FROM history h
		JOIN artists a ON a.id = h.artist_id WHERE h.id = $1`, ownId).Scan(&gotTitle, &primary, &artists)
	if gotTitle != "New Title" || primary != "New Artist" || artists != 2 {
		t.Errorf("edited play is %q by %q with %d artists", gotTitle, primary, artists)
	}
	var otherTitle string
	db.Pool.QueryRow(context.Background(), "SELECT song_name FROM history WHERE id = $1", otherId).Scan(&otherTitle)
	if otherTitle != "Old Title" {
		t.Errorf("another user's play was changed to %q", otherTitle)
	}
}
