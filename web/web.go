package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"

	"muzi/db"

	"golang.org/x/crypto/bcrypt"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v5"
)

type ProfileData struct {
	Username            string
	Bio                 string
	Pfp                 string
	AllowDuplicateEdits bool
	ScrobbleCount       int
	ArtistCount         int
	Artists             []string
	Titles              []string
	Times               []string
	Page                int
}

func Sub(a int, b int) int {
	return a - b
}

func Add(a int, b int) int {
	return a + b
}

func getUserIdByUsername(conn *pgx.Conn, username string) (int, error) {
	var userId int
	err := conn.QueryRow(context.Background(), "SELECT pk FROM users WHERE username = $1;", username).
		Scan(&userId)
	return userId, err
}

func getTimes(conn *pgx.Conn, userId int, lim int, off int) []string {
	var times []string
	rows, err := conn.Query(
		context.Background(),
		"SELECT timestamp FROM history WHERE user_id = $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3;",
		userId,
		lim,
		off,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT timestamp failed: %v\n", err)
		return nil
	}
	for rows.Next() {
		var time pgtype.Timestamptz
		err = rows.Scan(&time)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Scanning time failed: %v\n", err)
			return nil
		}
		times = append(times, time.Time.String())
	}
	return times
}

func getTitles(conn *pgx.Conn, userId int, lim int, off int) []string {
	var titles []string
	rows, err := conn.Query(
		context.Background(),
		"SELECT song_name FROM history WHERE user_id = $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3;",
		userId,
		lim,
		off,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT song_name failed: %v\n", err)
		return nil
	}
	for rows.Next() {
		var title string
		err = rows.Scan(&title)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Scanning title failed: %v\n", err)
			return nil
		}
		titles = append(titles, title)
	}
	return titles
}

func getArtists(conn *pgx.Conn, userId int, lim int, off int) []string {
	var artists []string
	rows, err := conn.Query(
		context.Background(),
		"SELECT artist FROM history WHERE user_id = $1 ORDER BY timestamp DESC LIMIT $2 OFFSET $3;",
		userId,
		lim,
		off,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT artist failed: %v\n", err)
		return nil
	}
	for rows.Next() {
		var artist string
		err = rows.Scan(&artist)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Scanning artist name failed: %v\n", err)
			return nil
		}
		artists = append(artists, artist)
	}
	return artists
}

func getScrobbles(conn *pgx.Conn, userId int) int {
	var count int
	err := conn.QueryRow(context.Background(), "SELECT COUNT(*) FROM history WHERE user_id = $1;", userId).
		Scan(&count)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT COUNT failed: %v\n", err)
		return 0
	}
	return count
}

func getArtistCount(conn *pgx.Conn, userId int) int {
	var count int
	err := conn.QueryRow(context.Background(), "SELECT COUNT(DISTINCT artist) FROM history WHERE user_id = $1;", userId).
		Scan(&count)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT artist count failed: %v\n", err)
		return 0
	}
	return count
}

func hashPassword(pass []byte) string {
	hashedPassword, err := bcrypt.GenerateFromPassword(pass, bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't hash password: %v\n", err)
	}
	return string(hashedPassword)
}

func verifyPassword(hashedPassword string, enteredPassword []byte) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), enteredPassword)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while comparing passwords: %v\n", err)
		return false
	}
	return true
}

func createAccount(w http.ResponseWriter, r *http.Request) {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		return
	}
	defer conn.Close(context.Background())

	if r.Method == "POST" {
		r.ParseForm()

		username := r.FormValue("uname")
		hashedPassword := hashPassword([]byte(r.FormValue("pass")))

		err = db.CreateUsersTable(conn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error ensuring users table exists: %v\n", err)
			http.Redirect(w, r, "/createaccount", http.StatusSeeOther)
			return
		}

		_, err = conn.Exec(
			context.Background(),
			`INSERT INTO users (username, password) VALUES ($1, $2);`,
			username,
			hashedPassword,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot add new user to users table: %v\n", err)
			http.Redirect(w, r, "/createaccount", http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
		}
	}
}

func createAccountPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmp, err := template.New("create_account.gohtml").
			ParseFiles("./templates/create_account.gohtml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = tmp.Execute(w, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func loginSubmit(w http.ResponseWriter, r *http.Request) {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		return
	}
	defer conn.Close(context.Background())

	if r.Method == "POST" {
		r.ParseForm()

		username := r.FormValue("uname")
		password := r.FormValue("pass")
		var storedPassword string
		err := conn.QueryRow(context.Background(), "SELECT password FROM users WHERE username = $1;", username).
			Scan(&storedPassword)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get password for entered username: %v\n", err)
		}

		if verifyPassword(storedPassword, []byte(password)) {
			http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
		}
	}
}

func loginPageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type data struct {
			ShowError bool
		}
		d := data{ShowError: false}
		if r.URL.Query().Get("error") != "" {
			d.ShowError = true
		}
		tmp, err := template.New("login.gohtml").ParseFiles("./templates/login.gohtml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = tmp.Execute(w, d)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func profilePageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")

		conn, err := pgx.Connect(
			context.Background(),
			"postgres://postgres:postgres@localhost:5432/muzi",
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Close(context.Background())

		userId, err := getUserIdByUsername(conn, username)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find user %s: %v\n", username, err)
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		pageStr := r.URL.Query().Get("page")
		var pageInt int
		if pageStr == "" {
			pageInt = 1
		} else {
			pageInt, err = strconv.Atoi(pageStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Cannot convert page URL query from string to int: %v\n", err)
				pageInt = 1
			}
		}

		lim := 15
		off := (pageInt - 1) * lim

		var profileData ProfileData

		err = conn.QueryRow(
			context.Background(),
			"SELECT bio, pfp, allow_duplicate_edits FROM users WHERE pk = $1;",
			userId,
		).Scan(&profileData.Bio, &profileData.Pfp, &profileData.AllowDuplicateEdits)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get profile for %s: %v\n", username, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		profileData.Username = username
		profileData.ScrobbleCount = getScrobbles(conn, userId)
		profileData.ArtistCount = getArtistCount(conn, userId)
		profileData.Artists = getArtists(conn, userId, lim, off)
		profileData.Titles = getTitles(conn, userId, lim, off)
		profileData.Times = getTimes(conn, userId, lim, off)
		profileData.Page = pageInt

		funcMap := template.FuncMap{
			"Sub": Sub,
			"Add": Add,
		}

		tmp, err := template.New("profile.gohtml").
			Funcs(funcMap).
			ParseFiles("./templates/profile.gohtml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmp.Execute(w, profileData)
	}
}

func updateDuplicateEditsSetting(w http.ResponseWriter, r *http.Request) {
	conn, err := pgx.Connect(
		context.Background(),
		"postgres://postgres:postgres@localhost:5432/muzi",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		return
	}
	defer conn.Close(context.Background())

	if r.Method == "POST" {
		r.ParseForm()
		username := r.FormValue("username")
		allow := r.FormValue("allow") == "true"

		_, err = conn.Exec(
			context.Background(),
			`UPDATE users SET allow_duplicate_edits = $1 WHERE username = $2;`,
			allow,
			username,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating setting: %v\n", err)
		}
		http.Redirect(w, r, "/profile/"+username, http.StatusSeeOther)
	}
}

func Start() {
	addr := ":1234"
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Handle("/files/*", http.StripPrefix("/files", http.FileServer(http.Dir("./static"))))
	r.Get("/login", loginPageHandler())
	r.Get("/createaccount", createAccountPageHandler())
	r.Get("/profile/{username}", profilePageHandler())
	r.Post("/loginsubmit", loginSubmit)
	r.Post("/createaccountsubmit", createAccount)
	r.Post("/settings/duplicate-edits", updateDuplicateEditsSetting)
	fmt.Printf("WebUI starting on %s\n", addr)
	http.ListenAndServe(addr, r)
}
