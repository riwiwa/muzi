package web

// Account management: changing your password, deleting your account, and an admin
// password reset for the command line

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"muzi/db"

	"golang.org/x/crypto/bcrypt"
)

func accountRedirect(w http.ResponseWriter, r *http.Request, key, value string) {
	http.Redirect(w, r, "/settings?tab=account&"+key+"="+url.QueryEscape(value), http.StatusSeeOther)
}

func changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	username := getLoggedInUsername(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	userId, err := getUserIdByUsername(r.Context(), username)
	if err != nil {
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	hash, err := db.GetPasswordHash(userId)
	if err != nil || !verifyPassword(hash, []byte(r.FormValue("current_password"))) {
		accountRedirect(w, r, "account_error", "Your current password is incorrect.")
		return
	}
	newPassword := r.FormValue("new_password")
	if newPassword != r.FormValue("confirm_password") {
		accountRedirect(w, r, "account_error", "The new passwords don't match.")
		return
	}
	newHash, err := hashPassword([]byte(newPassword))
	if err != nil {
		accountRedirect(w, r, "account_error", "Passwords must be 8–64 characters.")
		return
	}

	// stay logged in here, but sign out everywhere else
	keep := ""
	if cookie, err := r.Cookie("session"); err == nil {
		keep = cookie.Value
	}
	if err := db.SetPasswordHash(userId, newHash, keep); err != nil {
		fmt.Fprintf(os.Stderr, "Error changing password: %v\n", err)
		http.Error(w, "Error changing password", http.StatusInternalServerError)
		return
	}
	accountRedirect(w, r, "account_ok", "Password changed. Other devices have been logged out.")
}

func deleteAccountHandler(w http.ResponseWriter, r *http.Request) {
	username := getLoggedInUsername(r)
	if username == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	userId, err := getUserIdByUsername(r.Context(), username)
	if err != nil {
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	if r.FormValue("confirm_username") != username {
		accountRedirect(w, r, "account_error", "Type your username exactly to confirm deletion.")
		return
	}
	hash, err := db.GetPasswordHash(userId)
	if err != nil || !verifyPassword(hash, []byte(r.FormValue("password"))) {
		accountRedirect(w, r, "account_error", "Your password is incorrect.")
		return
	}

	if err := db.DeleteUser(userId); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting account %s: %v\n", username, err)
		http.Error(w, "Error deleting account", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
	// the root page sends you to login, or to signup if that was the last account
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Sets a new random password for a user and ends all their sessions, for `muzi reset-password`.
// Returns the new password so the admin can pass it on.
func ResetPassword(username string) (string, error) {
	userId, err := getUserIdByUsername(context.Background(), username)
	if err != nil {
		return "", fmt.Errorf("no user named %q", username)
	}

	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	password := base64.RawURLEncoding.EncodeToString(b)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	if err := db.SetPasswordHash(userId, string(hash), ""); err != nil {
		return "", err
	}
	return password, nil
}
