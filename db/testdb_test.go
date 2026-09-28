package db

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connects Pool to the database in MUZI_TEST_DATABASE_URL, or skips the test when it isn't set.
// Use a throwaway database: tests create and change tables. (Other packages use internal/dbtest,
// which imports this package and so can't be used from inside it.)
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

	// same lock as internal/dbtest (which this package can't import): test packages run in
	// parallel and PostgreSQL rejects concurrent creation of the same table
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(7243817)"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock(7243817)")
	if err := CreateAllTables(); err != nil {
		t.Fatal(err)
	}
}
