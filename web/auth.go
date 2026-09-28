package web

// Functions used to authenticate web UI users.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"muzi/config"
	"muzi/db"
	"muzi/ratelimit"

	"golang.org/x/crypto/bcrypt"
)

// Generates a hex string 32 characters in length.
func generateID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Returns a salted hash of a password if valid (8-64 chars).
func hashPassword(pass []byte) (string, error) {
	if len([]rune(string(pass))) < 8 || len(pass) > 64 {
		return "", errors.New("Error: Password must be greater than 8 chars.")
	}
	hashedPassword, err := bcrypt.GenerateFromPassword(pass, bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't hash password: %v\n", err)
		return "", err
	}
	return string(hashedPassword), nil
}

// Compares a plaintext password and a hashed password. Returns T/F depending
// on comparison result.
func verifyPassword(hashedPassword string, enteredPassword []byte) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), enteredPassword) == nil
}

// A valid bcrypt hash (of a random password) that no login can match. Used for unknown
// usernames so failed logins take the same time whether or not the account exists.
var dummyPasswordHash = func() string {
	b := make([]byte, 32)
	rand.Read(b)
	h, _ := bcrypt.GenerateFromPassword(b, bcrypt.DefaultCost)
	return string(h)
}()

// Handles the submission of new account credentials. Stores credentials in
// the users table. Sets a browser cookie for successful new users.
// Signup is open for the first account, then only if the config allows it
func signupOpen(r *http.Request) bool {
	return config.Get().Server.AllowSignup || !hasUsers(r.Context())
}

func createAccount(w http.ResponseWriter, r *http.Request) {
	if !signupOpen(r) {
		http.Error(w, "Signup is closed on this server", http.StatusForbidden)
		return
	}
	if r.Method == "POST" {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		username := r.FormValue("uname")
		if len([]rune(string(username))) == 0 {
			http.Redirect(w, r, "/createaccount?error=userlength", http.StatusSeeOther)
			return
		}
		var usertaken bool
		err = db.Pool.QueryRow(r.Context(),
			"SELECT EXISTS(SELECT 1 FROM users WHERE username = $1)", username).
			Scan(&usertaken)
		if usertaken == true {
			http.Redirect(w, r, "/createaccount?error=usertaken", http.StatusSeeOther)
			return
		}
		hashedPassword, err := hashPassword([]byte(r.FormValue("pass")))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error hashing password: %v\n", err)
			http.Redirect(w, r, "/createaccount?error=passlength", http.StatusSeeOther)
			return
		}

		_, err = db.Pool.Exec(
			r.Context(),
			`INSERT INTO users (username, password) VALUES ($1, $2);`,
			username,
			hashedPassword,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot add new user to users table: %v\n", err)
			http.Redirect(w, r, "/createaccount", http.StatusSeeOther)
		} else {
			sessionID := createSession(username)
			if sessionID == "" {
				http.Redirect(w, r, "/login?error=session", http.StatusSeeOther)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     "session",
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				Secure:   false,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400 * 30,
			})
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
		}
	}
}

// Renders the create account page
func createAccountPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !signupOpen(r) {
			http.Redirect(w, r, "/login?error=signup-closed", http.StatusSeeOther)
			return
		}
		type data struct {
			Error string
		}
		d := data{Error: r.URL.Query().Get("error")}
		err := templates.ExecuteTemplate(w, "create_account.gohtml", d)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// Handles submission of login credentials by checking if the username
// is in the database and the stored password for that username matches the
// given password. Sets browser cookie on successful login.
func loginSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		username := r.FormValue("uname")
		if username == "" {
			http.Redirect(w, r, "/login?error=invalid-creds", http.StatusSeeOther)
			return
		}

		keys := ratelimit.LoginKeys(r, username)
		if locked, wait := ratelimit.Logins.Blocked(keys); locked {
			minutes := int(wait.Minutes()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Redirect(w, r, "/login?error=too-many&minutes="+strconv.Itoa(minutes), http.StatusSeeOther)
			return
		}

		password := r.FormValue("pass")
		var storedPassword string
		err = db.Pool.QueryRow(r.Context(), "SELECT password FROM users WHERE username = $1;", username).
			Scan(&storedPassword)
		if err != nil {
			// compare against a real hash anyway, so unknown usernames take as long as known ones
			storedPassword = dummyPasswordHash
		}

		if verifyPassword(storedPassword, []byte(password)) && storedPassword != dummyPasswordHash {
			ratelimit.Logins.Succeed(keys)
			sessionID := createSession(username)
			if sessionID == "" {
				http.Redirect(w, r, "/login?error=session", http.StatusSeeOther)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     "session",
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				Secure:   false,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400 * 30,
			})
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
		} else {
			ratelimit.Logins.Fail(keys)
			http.Redirect(w, r, "/login?error=invalid-creds", http.StatusSeeOther)
		}
	}
}

// Renders the login page
func loginPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type data struct {
			Error      string
			Minutes    string
			SignupOpen bool
		}
		d := data{
			Error:      r.URL.Query().Get("error"),
			Minutes:    r.URL.Query().Get("minutes"),
			SignupOpen: signupOpen(r),
		}
		err := templates.ExecuteTemplate(w, "login.gohtml", d)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
