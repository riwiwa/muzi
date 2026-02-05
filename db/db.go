package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

func TableExists(name string, conn *pgx.Conn) bool {
	var exists bool
	err := conn.QueryRow(
		context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND 
		tablename = $1);`,
		name,
	).
		Scan(&exists)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT EXISTS failed: %v\n", err)
		return false
	}
	return exists
}

func DbExists() bool {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		return false
	}
	defer conn.Close(context.Background())
	return true
}

func CreateDB() error {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to PostgreSQL: %v\n", err)
		return err
	}
	defer conn.Close(context.Background())
	_, err = conn.Exec(context.Background(), "CREATE DATABASE muzi")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create muzi database: %v\n", err)
		return err
	}
	return nil
}

func CreateHistoryTable(conn *pgx.Conn) error {
	_, err := conn.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS history (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL,
			song_name TEXT NOT NULL,
			artist TEXT NOT NULL,
			album_name TEXT,
			ms_played INTEGER,
			platform TEXT DEFAULT 'spotify',
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

func CreateUsersTable(conn *pgx.Conn) error {
	_, err := conn.Exec(context.Background(),
		`CREATE TABLE IF NOT EXISTS users (
			username TEXT NOT NULL,
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
