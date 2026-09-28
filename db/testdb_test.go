package db

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connects Pool to the database in MUZI_TEST_DATABASE_URL, or skips the test when it isn't set.
// Use a throwaway database: tests create and change tables.
func useTestDB(t *testing.T) {
	t.Helper()
	url := os.Getenv("MUZI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MUZI_TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	Pool = pool
	t.Cleanup(pool.Close)
}
