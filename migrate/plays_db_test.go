package migrate

import (
	"context"
	"testing"
	"time"

	"muzi/db"
	"muzi/internal/dbtest"
)

func countPlays(t *testing.T, userId int) int {
	t.Helper()
	var n int
	db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM history WHERE user_id = $1", userId).Scan(&n)
	return n
}

func TestInsertPlays(t *testing.T) {
	dbtest.Setup(t)
	user, _ := dbtest.CreateUser(t)
	other, _ := dbtest.CreateUser(t)
	at := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	plays := []Play{
		{UserId: user, Timestamp: at, SongName: "Song A", Artist: "Artist X, Artist Y", Album: "Album", MsPlayed: 180000},
		{UserId: user, Timestamp: at.Add(10 * time.Minute), SongName: "Song B", Artist: "Artist X"},
	}
	if n, err := insertPlays(plays, "test", 0); err != nil || n != 2 {
		t.Fatalf("first insert: %d, %v; want 2", n, err)
	}
	if n, err := insertPlays(plays, "test", 0); err != nil || n != 0 {
		t.Errorf("re-inserting the same plays added %d (%v), want 0", n, err)
	}

	var artists int
	db.Pool.QueryRow(context.Background(),
		"SELECT cardinality(artist_ids) FROM history WHERE user_id = $1 AND song_name = 'Song A'", user).Scan(&artists)
	if artists != 2 {
		t.Errorf("a two-artist credit got %d artist ids, want 2", artists)
	}

	// the same song two minutes later, with different capitalization, as another source might log it
	near := []Play{{UserId: user, Timestamp: at.Add(2 * time.Minute), SongName: "song a", Artist: "artist x, artist y"}}
	if n, _ := insertPlays(near, "test", 5*time.Minute); n != 0 {
		t.Errorf("a play within the near-duplicate window was added")
	}
	// another user's history doesn't count as a duplicate
	near[0].UserId = other
	if n, _ := insertPlays(near, "test", 5*time.Minute); n != 1 {
		t.Errorf("another user's play was treated as a duplicate")
	}
	// without a window it's a separate play
	near[0].UserId = user
	if n, _ := insertPlays(near, "test", 0); n != 1 {
		t.Errorf("without a near-duplicate window the play should be added")
	}
	if got := countPlays(t, user); got != 3 {
		t.Errorf("user has %d plays, want 3", got)
	}
}
