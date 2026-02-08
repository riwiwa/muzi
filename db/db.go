package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func CreateAllTables() error {
	if err := CreateHistoryTable(); err != nil {
		return err
	}
	if err := CreateUsersTable(); err != nil {
		return err
	}
	return CreateSessionsTable()
}

func CreateDB() error {
	conn, err := pgx.Connect(context.Background(),
		"postgres://postgres:postgres@localhost:5432",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to PostgreSQL: %v\n", err)
		return err
	}
	defer conn.Close(context.Background())

	var exists bool
	err = conn.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = 'muzi')").Scan(&exists)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error checking if database exists: %v\n", err)
		return err
	}

	if exists {
		return nil
	}

	_, err = conn.Exec(context.Background(), "CREATE DATABASE muzi")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating muzi database: %v\n", err)
		return err
	}
	return nil
}

func CreateHistoryTable() error {
	_, err := Pool.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS history (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL,
			song_name TEXT NOT NULL,
			artist TEXT NOT NULL,
			album_name TEXT,
			ms_played INTEGER,
			platform TEXT,
			UNIQUE (user_id, song_name, artist, timestamp)
		);
		CREATE INDEX IF NOT EXISTS idx_history_user_timestamp ON history(user_id, timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_history_user_artist ON history(user_id, artist);
		CREATE INDEX IF NOT EXISTS idx_history_user_song ON history(user_id, song_name);`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating history table: %v\n", err)
		return err
	}
	return nil
}

// TODO: move user settings to jsonb in db
func CreateUsersTable() error {
	_, err := Pool.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS users (
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL,
			bio TEXT DEFAULT 'This profile has no bio.',
			pfp TEXT DEFAULT '/files/assets/default.png',
			allow_duplicate_edits BOOLEAN DEFAULT FALSE,
			pk SERIAL PRIMARY KEY
		);`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating users table: %v\n", err)
		return err
	}
	return nil
}

func CreateSessionsTable() error {
	_, err := Pool.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS sessions (
			session_id TEXT PRIMARY KEY,
			username TEXT NOT NULL REFERENCES users(username),
			created_at TIMESTAMPTZ DEFAULT NOW(),
			expires_at TIMESTAMPTZ DEFAULT NOW() + INTERVAL '30 days'
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating sessions table: %v\n", err)
		return err
	}
	return nil
}

func CleanupExpiredSessions() error {
	_, err := Pool.Exec(context.Background(),
		"DELETE FROM sessions WHERE expires_at < NOW();")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error cleaning up expired sessions: %v\n", err)
		return err
	}
	return nil
}
