package web

// Functions that handle browser login sessions

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"muzi/db"
)

type Session struct {
	Username string
}

func createSession(username string) string {
	sessionID, err := generateID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating sessionID: %v\n", err)
		return ""
	}
	_, err = db.Pool.Exec(
		context.Background(),
		"INSERT INTO sessions (session_id, username, expires_at) VALUES ($1, $2, NOW() + INTERVAL '30 days');",
		sessionID,
		username,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
		return ""
	}
	return sessionID
}

func getSession(ctx context.Context, sessionID string) *Session {
	var username string
	err := db.Pool.QueryRow(
		ctx,
		"SELECT username FROM sessions WHERE session_id = $1 AND expires_at > NOW();",
		sessionID,
	).Scan(&username)
	if err != nil {
		return nil
	}
	return &Session{Username: username}
}

func deleteSession(sessionID string) {
	_, err := db.Pool.Exec(
		context.Background(),
		"DELETE FROM sessions WHERE session_id = $1;",
		sessionID,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting session: %v\n", err)
	}
}

func getLoggedInUsername(r *http.Request) string {
	cookie, err := r.Cookie("session")
	if err != nil {
		return ""
	}
	session := getSession(r.Context(), cookie.Value)
	if session == nil {
		return ""
	}
	return session.Username
}

func getUserIdByUsername(ctx context.Context, username string) (int, error) {
	var userId int
	err := db.Pool.QueryRow(ctx, "SELECT pk FROM users WHERE username = $1;", username).
		Scan(&userId)
	return userId, err
}
