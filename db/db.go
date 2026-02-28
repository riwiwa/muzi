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
	if err := CreateSessionsTable(); err != nil {
		return err
	}
	return CreateSpotifyLastTrackTable()
}

func GetDbUrl(dbName bool) string {
	host := os.Getenv("PGHOST")
	port := os.Getenv("PGPORT")
	user := os.Getenv("PGUSER")
	pass := os.Getenv("PGPASSWORD")

	if dbName {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
			user, pass, host, port, "muzi")
	} else {
		return fmt.Sprintf("postgres://%s:%s@%s:%s", user, pass, host, port)
	}
}

func CreateDB() error {
	conn, err := pgx.Connect(context.Background(),
		GetDbUrl(false),
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

func CreateUsersTable() error {
	_, err := Pool.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS users (
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL,
			bio TEXT DEFAULT 'This profile has no bio.',
			pfp TEXT DEFAULT '/files/assets/pfps/default.png',
			allow_duplicate_edits BOOLEAN DEFAULT FALSE,
			api_key TEXT,
			api_secret TEXT,
			spotify_client_id TEXT,
			spotify_client_secret TEXT,
			spotify_access_token TEXT,
			spotify_refresh_token TEXT,
			spotify_token_expires TIMESTAMPTZ,
			last_spotify_check TIMESTAMPTZ,
			pk SERIAL PRIMARY KEY
		);
		CREATE INDEX IF NOT EXISTS idx_users_api_key ON users(api_key);`)
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
		fmt.Fprintf(os.Stderr, "Error cleaning up sessions: %v\n", err)
		return err
	}
	return nil
}

func CreateSpotifyLastTrackTable() error {
	_, err := Pool.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS spotify_last_track (
			user_id INTEGER PRIMARY KEY REFERENCES users(pk) ON DELETE CASCADE,
			track_id TEXT NOT NULL,
			song_name TEXT NOT NULL,
			artist TEXT NOT NULL,
			album_name TEXT,
			duration_ms INTEGER NOT NULL,
			progress_ms INTEGER NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating spotify_last_track table: %v\n", err)
		return err
	}
	return nil
}
