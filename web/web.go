package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"

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

func tmp(w http.ResponseWriter, r *http.Request) {

	conn, err := pgx.Connect(context.Background(), "postgres://postgres:postgres@localhost:5432/muzi")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to muzi database: %v\n", err)
		return
	}
	defer conn.Close(context.Background())

	pageStr := r.URL.Query().Get("page")
	pageInt, err := strconv.Atoi(pageStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot convert page URL query from string to int: %v\n", err)
		return
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

	t, err := template.New("history.gohtml").Funcs(funcMap).ParseFiles("./templates/history.gohtml")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = t.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func Start() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/static/style.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./static/style.css")
	})
	r.Get("/history", tmp)
	http.ListenAndServe(":1234", r)
}
