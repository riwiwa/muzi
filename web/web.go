package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"muzi/importsongs"
	"golang.org/x/crypto/bcrypt"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v5"
)

type PageData struct {
	Content int
	Artists []string
	Titles  []string
	Times   []string
	Page    int
}


func Sub(a int, b int) int {
	return a - b
}

func Add(a int, b int) int {
	return a + b
}

func getTimes(conn *pgx.Conn, lim int, off int) []string {
	var times []string
	rows, err := conn.Query(context.Background(), "SELECT timestamp FROM history ORDER BY timestamp DESC LIMIT $1 OFFSET $2;", lim, off)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT COUNT failed: %v\n", err)
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

func getTitles(conn *pgx.Conn, lim int, off int) []string {
	var titles []string
	rows, err := conn.Query(context.Background(), "SELECT song_name FROM history ORDER BY timestamp DESC LIMIT $1 OFFSET $2;", lim, off)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT COUNT failed: %v\n", err)
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

func getArtists(conn *pgx.Conn, lim int, off int) []string {
	var artists []string
	rows, err := conn.Query(context.Background(), "SELECT artist FROM history ORDER BY timestamp DESC LIMIT $1 OFFSET $2;", lim, off)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT COUNT failed: %v\n", err)
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

func getScrobbles(conn *pgx.Conn) int {
	var count int
	err := conn.QueryRow(context.Background(), "SELECT COUNT (*) FROM history;").Scan(&count)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT COUNT failed: %v\n", err)
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
	conn, err := pgx.Connect(context.Background(), "postgres://postgres:postgres@localhost:5432/muzi")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		return
	}
	defer conn.Close(context.Background())

	if r.Method == "POST" {
		r.ParseForm()

		username := r.FormValue("uname")
		hashedPassword := hashPassword([]byte(r.FormValue("pass")))

	if importsongs.TableExists("users", conn) == false {
		_, err = conn.Exec(
			context.Background(),
			`CREATE TABLE users (username TEXT, password TEXT, pk SERIAL, PRIMARY KEY (pk));`,
		)
		if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create users table: %v\n", err)
			panic(err)
		}
	}

		_, err = conn.Exec(
				context.Background(), `INSERT INTO users (username, password) VALUES ($1, $2);`,
				username,
				hashedPassword,
			)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot add new user to users table: %v\n", err)
			http.Redirect(w, r, "/createaccount", http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/profile/" + username, http.StatusSeeOther)
		}
	}
}

func createAccountHandler(w http.ResponseWriter, r *http.Request) {
	tmp, err := template.New("create_account.gohtml").ParseFiles("./templates/create_account.gohtml")
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

func loginSubmit(w http.ResponseWriter, r *http.Request) {
	conn, err := pgx.Connect(context.Background(), "postgres://postgres:postgres@localhost:5432/muzi")
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
		err := conn.QueryRow(context.Background(), "SELECT password FROM users WHERE username = $1;", username).Scan(&storedPassword)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot get password for entered username: %v\n", err)
		}

		if verifyPassword(storedPassword, []byte(password)) {
			http.Redirect(w, r, "/profile/" + username, http.StatusSeeOther)
		}
	}
}

func loginPage(w http.ResponseWriter, r *http.Request) {
	tmp, err := template.New("login.gohtml").ParseFiles("./templates/login.gohtml")
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

func historyPage(w http.ResponseWriter, r *http.Request) {

	conn, err := pgx.Connect(context.Background(), "postgres://postgres:postgres@localhost:5432/muzi")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		return
	}
	defer conn.Close(context.Background())

	var pageInt int

	pageStr := r.URL.Query().Get("page")
	if pageStr == "" {
		pageInt = 1
	} else {
		pageInt, err = strconv.Atoi(pageStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot convert page URL query from string to int: %v\n", err)
			return
		}
	}

	lim := 25
	off := 0 + (25 * (pageInt - 1))

	data := PageData{
		Content: getScrobbles(conn),
		Artists: getArtists(conn, lim, off),
		Titles:  getTitles(conn, lim, off),
		Times:   getTimes(conn, lim, off),
		Page:    pageInt,
	}

funcMap := template.FuncMap{
	"Sub": Sub,
	"Add": Add,
}

	tmp, err := template.New("history.gohtml").Funcs(funcMap).ParseFiles("./templates/history.gohtml")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = tmp.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

type Profile struct {
	Username string
	Bio string
}

func Start() {
	addr := ":1234"
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/static/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./static/style.css")
	})
	r.Get("/history", historyPage)
	r.Get("/login", loginPage)
	r.Get("/createaccount", createAccountHandler)
	// TODO: clean this up
	r.Get("/profile/{username}", func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")
		
		profileData := Profile {
			Username: username,
			Bio: "default",
		}

		tmp, err := template.ParseFiles("./templates/profile.gohtml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmp.Execute(w, profileData)
	})
	r.Post("/loginsubmit", loginSubmit)
	r.Post("/createaccountsubmit", createAccount)
	fmt.Printf("WebUI starting on %s\n", addr)
	http.ListenAndServe(addr, r)
}
