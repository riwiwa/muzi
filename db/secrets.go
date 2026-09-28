package db

import (
	"context"
	"crypto/rand"
)

// Returns a server-wide random secret by name, creating it on first use. Kept in the database
// so values derived from it (like CSRF tokens) stay valid across restarts.
func GetOrCreateSecret(name string) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	// insert only if missing, then read whichever value won
	_, err := Pool.Exec(context.Background(),
		"INSERT INTO app_secrets (name, value) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING", name, fresh)
	if err != nil {
		return nil, err
	}
	var secret []byte
	err = Pool.QueryRow(context.Background(), "SELECT value FROM app_secrets WHERE name = $1", name).Scan(&secret)
	return secret, err
}
