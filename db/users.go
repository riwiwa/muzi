package db

import (
	"context"
	"fmt"
)

func GetPasswordHash(userId int) (string, error) {
	var hash string
	err := Pool.QueryRow(context.Background(), "SELECT password FROM users WHERE pk = $1", userId).Scan(&hash)
	return hash, err
}

// Replaces a user's password hash and ends their other sessions, keeping keepSession (if any)
func SetPasswordHash(userId int, hash, keepSession string) error {
	tx, err := Pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	var username string
	err = tx.QueryRow(context.Background(),
		"UPDATE users SET password = $1 WHERE pk = $2 RETURNING username", hash, userId).Scan(&username)
	if err != nil {
		return err
	}
	_, err = tx.Exec(context.Background(),
		"DELETE FROM sessions WHERE username = $1 AND session_id <> $2", username, keepSession)
	if err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

// Permanently deletes a user and everything they own. History has no foreign key to users and
// sessions block the delete, so those go first; artists, albums, songs and Spotify state cascade.
func DeleteUser(userId int) error {
	tx, err := Pool.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	var username string
	err = tx.QueryRow(context.Background(), "SELECT username FROM users WHERE pk = $1", userId).Scan(&username)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(context.Background(), "DELETE FROM history WHERE user_id = $1", userId); err != nil {
		return fmt.Errorf("deleting history: %w", err)
	}
	if _, err := tx.Exec(context.Background(), "DELETE FROM sessions WHERE username = $1", username); err != nil {
		return fmt.Errorf("deleting sessions: %w", err)
	}
	if _, err := tx.Exec(context.Background(), "DELETE FROM users WHERE pk = $1", userId); err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	return tx.Commit(context.Background())
}
