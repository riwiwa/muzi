// Package dbtest sets up a real PostgreSQL database for integration tests. Tests using it are
// skipped unless MUZI_TEST_DATABASE_URL points at a throwaway database, since they create tables
// and write data. CI runs them against a PostgreSQL service.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"

	"muzi/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	once     sync.Once
	setupErr error
)

// Connects db.Pool to the test database and creates the schema (once per test binary), or skips
// the test when MUZI_TEST_DATABASE_URL isn't set
func Setup(t *testing.T) {
	t.Helper()
	url := os.Getenv("MUZI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MUZI_TEST_DATABASE_URL not set")
	}
	once.Do(func() {
		pool, err := pgxpool.New(context.Background(), url)
		if err != nil {
			setupErr = err
			return
		}
		db.Pool = pool
		setupErr = WithSchemaLock(pool, func() error {
			if err := db.CreateAllTables(); err != nil {
				return err
			}
			return db.RunMigrations()
		})
	})
	if setupErr != nil {
		t.Fatal(setupErr)
	}
}

// Test packages run as separate processes at the same time, and PostgreSQL rejects concurrent
// CREATE TABLE IF NOT EXISTS for the same table; this lock makes schema setup take turns
const schemaLockKey = 7243817

// Runs fn while holding a database-wide advisory lock for schema setup
func WithSchemaLock(pool *pgxpool.Pool, fn func() error) error {
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", schemaLockKey); err != nil {
		return err
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", schemaLockKey)
	return fn()
}

// Creates a user with a unique name, so tests (and test packages running in parallel) don't
// collide. Returns its id and username.
func CreateUser(t *testing.T) (int, string) {
	t.Helper()
	b := make([]byte, 6)
	rand.Read(b)
	username := "test_" + hex.EncodeToString(b)
	var id int
	err := db.Pool.QueryRow(context.Background(),
		"INSERT INTO users (username, password) VALUES ($1, 'unused') RETURNING pk", username).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id, username
}
