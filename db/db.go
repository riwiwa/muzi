package db

import (
	"context"
	"fmt"
	"os"

	"muzi/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func CreateAllTables() error {
	if err := CreateExtensions(); err != nil {
		return err
	}
	if err := CreateHistoryTable(); err != nil {
		return err
	}
	if err := CreateUsersTable(); err != nil {
		return err
	}
	if err := CreateSessionsTable(); err != nil {
		return err
	}
	if err := CreateSpotifyLastTrackTable(); err != nil {
		return err
	}
	if err := CreateArtistsTable(); err != nil {
		return err
	}
	if err := CreateAlbumsTable(); err != nil {
		return err
	}
	if err := CreateSongsTable(); err != nil {
		return err
	}
	if err := AddHistoryEntityColumns(); err != nil {
		return err
	}
	return nil
}

func CreateExtensions() error {
	_, err := Pool.Exec(context.Background(),
		"CREATE EXTENSION IF NOT EXISTS pg_trgm;")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating pg_trgm extension: %v\n", err)
		return err
	}
	return nil
}

func GetDbUrl(dbName bool) string {
	return config.Get().Database.GetDbUrl(dbName)
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

func CreateArtistsTable() error {
	_, err := Pool.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS artists (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(pk) ON DELETE CASCADE,
			name TEXT NOT NULL,
			image_url TEXT,
			bio TEXT,
			spotify_id TEXT,
			musicbrainz_id TEXT,
			UNIQUE (user_id, name)
		);
		CREATE INDEX IF NOT EXISTS idx_artists_user_name ON artists(user_id, name);
		CREATE INDEX IF NOT EXISTS idx_artists_user_name_trgm ON artists USING gin(name gin_trgm_ops);`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating artists table: %v\n", err)
		return err
	}
	return nil
}

func CreateAlbumsTable() error {
	_, err := Pool.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS albums (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(pk) ON DELETE CASCADE,
			title TEXT NOT NULL,
			artist_id INTEGER REFERENCES artists(id) ON DELETE SET NULL,
			cover_url TEXT,
			spotify_id TEXT,
			musicbrainz_id TEXT,
			UNIQUE (user_id, title, artist_id)
		);
		CREATE INDEX IF NOT EXISTS idx_albums_user_title ON albums(user_id, title);
		CREATE INDEX IF NOT EXISTS idx_albums_user_title_trgm ON albums USING gin(title gin_trgm_ops);`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating albums table: %v\n", err)
		return err
	}
	return nil
}

func CreateSongsTable() error {
	_, err := Pool.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS songs (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(pk) ON DELETE CASCADE,
			title TEXT NOT NULL,
			artist_id INTEGER REFERENCES artists(id) ON DELETE SET NULL,
			album_id INTEGER REFERENCES albums(id) ON DELETE SET NULL,
			duration_ms INTEGER,
			spotify_id TEXT,
			musicbrainz_id TEXT,
			UNIQUE (user_id, title, artist_id)
		);
		CREATE INDEX IF NOT EXISTS idx_songs_user_title ON songs(user_id, title);
		CREATE INDEX IF NOT EXISTS idx_songs_user_title_trgm ON songs USING gin(title gin_trgm_ops);`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating songs table: %v\n", err)
		return err
	}
	return nil
}

func AddHistoryEntityColumns() error {
	_, err := Pool.Exec(context.Background(),
		`ALTER TABLE history ADD COLUMN IF NOT EXISTS artist_id INTEGER REFERENCES artists(id) ON DELETE SET NULL;
		ALTER TABLE history ADD COLUMN IF NOT EXISTS song_id INTEGER REFERENCES songs(id) ON DELETE SET NULL;
		ALTER TABLE history ADD COLUMN IF NOT EXISTS artist_ids INTEGER[] DEFAULT '{}';
		CREATE INDEX IF NOT EXISTS idx_history_artist_id ON history(artist_id);
		CREATE INDEX IF NOT EXISTS idx_history_song_id ON history(song_id);
		CREATE INDEX IF NOT EXISTS idx_history_artist_ids ON history USING gin(artist_ids);`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error adding history entity columns: %v\n", err)
		return err
	}
	return nil
}
