package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgtype"
)

type Artist struct {
	Id            int
	UserId        int
	Name          string
	ImageUrl      string
	Bio           string
	SpotifyId     string
	MusicbrainzId string
}

type Album struct {
	Id            int
	UserId        int
	Title         string
	ArtistId      int
	CoverUrl      string
	SpotifyId     string
	MusicbrainzId string
}

type Song struct {
	Id            int
	UserId        int
	Title         string
	ArtistId      int
	AlbumId       int
	DurationMs    int
	SpotifyId     string
	MusicbrainzId string
}

func GetOrCreateArtist(userId int, name string) (int, bool, error) {
	if name == "" {
		return 0, false, nil
	}

	var id int
	err := Pool.QueryRow(context.Background(),
		"SELECT id FROM artists WHERE user_id = $1 AND name = $2",
		userId, name).Scan(&id)
	if err == nil {
		return id, false, nil
	}

	err = Pool.QueryRow(context.Background(),
		`INSERT INTO artists (user_id, name) VALUES ($1, $2) 
		ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`,
		userId, name).Scan(&id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating artist: %v\n", err)
		return 0, false, err
	}
	return id, true, nil
}

func GetArtistById(id int) (Artist, error) {
	var artist Artist
	var imageUrlPg, bioPg, spotifyIdPg, musicbrainzIdPg pgtype.Text
	err := Pool.QueryRow(context.Background(),
		"SELECT id, user_id, name, image_url, bio, spotify_id, musicbrainz_id FROM artists WHERE id = $1",
		id).Scan(&artist.Id, &artist.UserId, &artist.Name, &imageUrlPg, &bioPg, &spotifyIdPg, &musicbrainzIdPg)
	if err != nil {
		return Artist{}, err
	}
	if imageUrlPg.Status == pgtype.Present {
		artist.ImageUrl = imageUrlPg.String
	}
	if bioPg.Status == pgtype.Present {
		artist.Bio = bioPg.String
	}
	if spotifyIdPg.Status == pgtype.Present {
		artist.SpotifyId = spotifyIdPg.String
	}
	if musicbrainzIdPg.Status == pgtype.Present {
		artist.MusicbrainzId = musicbrainzIdPg.String
	}
	return artist, nil
}

func GetArtistByName(userId int, name string) (Artist, error) {
	var artist Artist
	var imageUrlPg, bioPg, spotifyIdPg, musicbrainzIdPg pgtype.Text
	err := Pool.QueryRow(context.Background(),
		"SELECT id, user_id, name, image_url, bio, spotify_id, musicbrainz_id FROM artists WHERE user_id = $1 AND name = $2",
		userId, name).Scan(&artist.Id, &artist.UserId, &artist.Name, &imageUrlPg, &bioPg, &spotifyIdPg, &musicbrainzIdPg)
	if err != nil {
		return Artist{}, err
	}
	if imageUrlPg.Status == pgtype.Present {
		artist.ImageUrl = imageUrlPg.String
	}
	if bioPg.Status == pgtype.Present {
		artist.Bio = bioPg.String
	}
	if spotifyIdPg.Status == pgtype.Present {
		artist.SpotifyId = spotifyIdPg.String
	}
	if musicbrainzIdPg.Status == pgtype.Present {
		artist.MusicbrainzId = musicbrainzIdPg.String
	}
	return artist, nil
}

func UpdateArtist(id int, name, imageUrl, bio, spotifyId, musicbrainzId string) error {
	_, err := Pool.Exec(context.Background(),
		`UPDATE artists SET name = $1, image_url = $2, bio = $3, spotify_id = $4, musicbrainz_id = $5 WHERE id = $6`,
		name, imageUrl, bio, spotifyId, musicbrainzId, id)
	return err
}

func SearchArtists(userId int, query string) ([]Artist, float64, error) {
	likePattern := "%" + query + "%"
	rows, err := Pool.Query(context.Background(),
		`SELECT id, user_id, name, image_url, bio, spotify_id, musicbrainz_id, similarity(name, $2) as sim
		FROM artists WHERE user_id = $1 AND (similarity(name, $2) > 0.1 OR LOWER(name) LIKE LOWER($3))
		ORDER BY similarity(name, $2) DESC LIMIT 20`,
		userId, query, likePattern)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var artists []Artist
	var maxSim float64
	for rows.Next() {
		var a Artist
		var imageUrlPg, bioPg, spotifyIdPg, musicbrainzIdPg pgtype.Text
		var sim float64
		err := rows.Scan(&a.Id, &a.UserId, &a.Name, &imageUrlPg, &bioPg, &spotifyIdPg, &musicbrainzIdPg, &sim)
		if err != nil {
			return nil, 0, err
		}
		if imageUrlPg.Status == pgtype.Present {
			a.ImageUrl = imageUrlPg.String
		}
		if bioPg.Status == pgtype.Present {
			a.Bio = bioPg.String
		}
		if spotifyIdPg.Status == pgtype.Present {
			a.SpotifyId = spotifyIdPg.String
		}
		if musicbrainzIdPg.Status == pgtype.Present {
			a.MusicbrainzId = musicbrainzIdPg.String
		}
		artists = append(artists, a)
		if sim > maxSim {
			maxSim = sim
		}
	}
	return artists, maxSim, nil
}

func GetOrCreateAlbum(userId int, title string, artistId int) (int, bool, error) {
	if title == "" {
		return 0, false, nil
	}

	var id int
	err := Pool.QueryRow(context.Background(),
		"SELECT id FROM albums WHERE user_id = $1 AND title = $2 AND (artist_id = $3 OR (artist_id IS NULL AND $3 IS NULL))",
		userId, title, artistId).Scan(&id)
	if err == nil {
		return id, false, nil
	}

	err = Pool.QueryRow(context.Background(),
		`INSERT INTO albums (user_id, title, artist_id) VALUES ($1, $2, $3) 
		ON CONFLICT (user_id, title, artist_id) DO UPDATE SET title = EXCLUDED.title
		RETURNING id`,
		userId, title, artistId).Scan(&id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating album: %v\n", err)
		return 0, false, err
	}
	return id, true, nil
}

func GetAlbumById(id int) (Album, error) {
	var album Album
	err := Pool.QueryRow(context.Background(),
		"SELECT id, user_id, title, artist_id, cover_url, spotify_id, musicbrainz_id FROM albums WHERE id = $1",
		id).Scan(&album.Id, &album.UserId, &album.Title, &album.ArtistId, &album.CoverUrl,
		&album.SpotifyId, &album.MusicbrainzId)
	if err != nil {
		return Album{}, err
	}
	return album, nil
}

func GetAlbumByName(userId int, title string, artistId int) (Album, error) {
	var album Album
	var artistIdVal int
	var coverUrlPg, spotifyIdPg, musicbrainzIdPg pgtype.Text

	var query string
	var args []interface{}
	if artistId > 0 {
		query = `SELECT id, user_id, title, artist_id, cover_url, spotify_id, musicbrainz_id 
			FROM albums WHERE user_id = $1 AND title = $2 AND artist_id = $3`
		args = []interface{}{userId, title, artistId}
	} else {
		query = `SELECT id, user_id, title, artist_id, cover_url, spotify_id, musicbrainz_id 
			FROM albums WHERE user_id = $1 AND title = $2`
		args = []interface{}{userId, title}
	}

	err := Pool.QueryRow(context.Background(), query, args...).Scan(
		&album.Id, &album.UserId, &album.Title, &artistIdVal, &coverUrlPg,
		&spotifyIdPg, &musicbrainzIdPg)
	if err != nil {
		return Album{}, err
	}
	album.ArtistId = artistIdVal
	if coverUrlPg.Status == pgtype.Present {
		album.CoverUrl = coverUrlPg.String
	}
	if spotifyIdPg.Status == pgtype.Present {
		album.SpotifyId = spotifyIdPg.String
	}
	if musicbrainzIdPg.Status == pgtype.Present {
		album.MusicbrainzId = musicbrainzIdPg.String
	}
	return album, nil
}

func UpdateAlbum(id int, title, coverUrl, spotifyId, musicbrainzId string) error {
	_, err := Pool.Exec(context.Background(),
		`UPDATE albums SET 
			title = COALESCE(NULLIF($1, ''), title),
			cover_url = COALESCE(NULLIF($2, ''), cover_url),
			spotify_id = COALESCE(NULLIF($3, ''), spotify_id),
			musicbrainz_id = COALESCE(NULLIF($4, ''), musicbrainz_id)
		WHERE id = $5`,
		title, coverUrl, spotifyId, musicbrainzId, id)
	return err
}

func UpdateAlbumField(id int, field string, value string) error {
	var query string
	switch field {
	case "title":
		query = "UPDATE albums SET title = $1 WHERE id = $2"
	case "cover_url":
		query = "UPDATE albums SET cover_url = $1 WHERE id = $2"
	case "spotify_id":
		query = "UPDATE albums SET spotify_id = $1 WHERE id = $2"
	case "musicbrainz_id":
		query = "UPDATE albums SET musicbrainz_id = $1 WHERE id = $2"
	default:
		return fmt.Errorf("unknown field: %s", field)
	}
	_, err := Pool.Exec(context.Background(), query, value, id)
	return err
}

func SearchAlbums(userId int, query string) ([]Album, float64, error) {
	likePattern := "%" + query + "%"
	rows, err := Pool.Query(context.Background(),
		`SELECT id, user_id, title, artist_id, cover_url, spotify_id, musicbrainz_id, similarity(title, $2) as sim
		FROM albums WHERE user_id = $1 AND (similarity(title, $2) > 0.1 OR LOWER(title) LIKE LOWER($3))
		ORDER BY similarity(title, $2) DESC LIMIT 20`,
		userId, query, likePattern)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var albums []Album
	var maxSim float64
	for rows.Next() {
		var a Album
		var artistIdVal int
		var coverUrlPg, spotifyIdPg, musicbrainzIdPg pgtype.Text
		var sim float64
		err := rows.Scan(&a.Id, &a.UserId, &a.Title, &artistIdVal, &coverUrlPg, &spotifyIdPg, &musicbrainzIdPg, &sim)
		if err != nil {
			return nil, 0, err
		}
		a.ArtistId = artistIdVal
		if coverUrlPg.Status == pgtype.Present {
			a.CoverUrl = coverUrlPg.String
		}
		if spotifyIdPg.Status == pgtype.Present {
			a.SpotifyId = spotifyIdPg.String
		}
		if musicbrainzIdPg.Status == pgtype.Present {
			a.MusicbrainzId = musicbrainzIdPg.String
		}
		albums = append(albums, a)
		if sim > maxSim {
			maxSim = sim
		}
	}
	return albums, maxSim, nil
}

func GetOrCreateSong(userId int, title string, artistId int, albumId int) (int, bool, error) {
	if title == "" {
		return 0, false, nil
	}

	var id int
	err := Pool.QueryRow(context.Background(),
		`SELECT id FROM songs 
		WHERE user_id = $1 AND title = $2 AND artist_id = $3 
		AND (album_id = $4 OR (album_id IS NULL AND $4 IS NULL))`,
		userId, title, artistId, albumId).Scan(&id)
	if err == nil {
		return id, false, nil
	}

	var albumIdVal pgtype.Int4
	if albumId > 0 {
		albumIdVal = pgtype.Int4{Int: int32(albumId), Status: pgtype.Present}
	} else {
		albumIdVal.Status = pgtype.Null
	}

	err = Pool.QueryRow(context.Background(),
		`INSERT INTO songs (user_id, title, artist_id, album_id) VALUES ($1, $2, $3, $4) 
		ON CONFLICT (user_id, title, artist_id, album_id) DO UPDATE SET album_id = EXCLUDED.album_id
		RETURNING id`,
		userId, title, artistId, albumIdVal).Scan(&id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating song: %v\n", err)
		return 0, false, err
	}
	return id, true, nil
}

func GetSongById(id int) (Song, error) {
	var song Song
	err := Pool.QueryRow(context.Background(),
		"SELECT id, user_id, title, artist_id, album_id, duration_ms, spotify_id, musicbrainz_id FROM songs WHERE id = $1",
		id).Scan(&song.Id, &song.UserId, &song.Title, &song.ArtistId, &song.AlbumId,
		&song.DurationMs, &song.SpotifyId, &song.MusicbrainzId)
	if err != nil {
		return Song{}, err
	}
	return song, nil
}

func GetSongByName(userId int, title string, artistId int) (Song, error) {
	var song Song
	var artistIdVal, albumIdVal pgtype.Int4
	var durationMs *int
	var spotifyIdPg, musicbrainzIdPg pgtype.Text

	var query string
	var args []interface{}
	if artistId > 0 {
		query = `SELECT id, user_id, title, artist_id, album_id, duration_ms, spotify_id, musicbrainz_id 
			FROM songs WHERE user_id = $1 AND title = $2 AND artist_id = $3`
		args = []interface{}{userId, title, artistId}
	} else {
		query = `SELECT id, user_id, title, artist_id, album_id, duration_ms, spotify_id, musicbrainz_id 
			FROM songs WHERE user_id = $1 AND title = $2`
		args = []interface{}{userId, title}
	}

	err := Pool.QueryRow(context.Background(), query, args...).Scan(
		&song.Id, &song.UserId, &song.Title, &artistIdVal, &albumIdVal,
		&durationMs, &spotifyIdPg, &musicbrainzIdPg)
	if err != nil {
		return Song{}, err
	}
	if artistIdVal.Status == pgtype.Present {
		song.ArtistId = int(artistIdVal.Int)
	}
	if albumIdVal.Status == pgtype.Present {
		song.AlbumId = int(albumIdVal.Int)
	}
	if durationMs != nil {
		song.DurationMs = *durationMs
	}
	if spotifyIdPg.Status == pgtype.Present {
		song.SpotifyId = spotifyIdPg.String
	}
	if musicbrainzIdPg.Status == pgtype.Present {
		song.MusicbrainzId = musicbrainzIdPg.String
	}
	return song, nil
}

func GetSongsByName(userId int, title string, artistId int) ([]Song, error) {
	var query string
	var args []interface{}
	if artistId > 0 {
		query = `SELECT id, user_id, title, artist_id, album_id, duration_ms, spotify_id, musicbrainz_id 
			FROM songs WHERE user_id = $1 AND title = $2 AND artist_id = $3 ORDER BY id`
		args = []interface{}{userId, title, artistId}
	} else {
		query = `SELECT id, user_id, title, artist_id, album_id, duration_ms, spotify_id, musicbrainz_id 
			FROM songs WHERE user_id = $1 AND title = $2 ORDER BY id`
		args = []interface{}{userId, title}
	}

	rows, err := Pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var songs []Song
	for rows.Next() {
		var song Song
		var artistIdVal, albumIdVal pgtype.Int4
		var durationMs *int
		var spotifyIdPg, musicbrainzIdPg pgtype.Text

		err := rows.Scan(
			&song.Id, &song.UserId, &song.Title, &artistIdVal, &albumIdVal,
			&durationMs, &spotifyIdPg, &musicbrainzIdPg)
		if err != nil {
			return nil, err
		}
		if artistIdVal.Status == pgtype.Present {
			song.ArtistId = int(artistIdVal.Int)
		}
		if albumIdVal.Status == pgtype.Present {
			song.AlbumId = int(albumIdVal.Int)
		}
		if durationMs != nil {
			song.DurationMs = *durationMs
		}
		if spotifyIdPg.Status == pgtype.Present {
			song.SpotifyId = spotifyIdPg.String
		}
		if musicbrainzIdPg.Status == pgtype.Present {
			song.MusicbrainzId = musicbrainzIdPg.String
		}
		songs = append(songs, song)
	}
	return songs, nil
}

func UpdateSong(id int, title string, durationMs int, spotifyId, musicbrainzId string) error {
	_, err := Pool.Exec(context.Background(),
		`UPDATE songs SET title = $1, duration_ms = $2, spotify_id = $3, musicbrainz_id = $4 WHERE id = $5`,
		title, durationMs, spotifyId, musicbrainzId, id)
	return err
}

func SearchSongs(userId int, query string) ([]Song, float64, error) {
	likePattern := "%" + query + "%"
	rows, err := Pool.Query(context.Background(),
		`SELECT id, user_id, title, artist_id, album_id, duration_ms, spotify_id, musicbrainz_id, similarity(title, $2) as sim
		FROM songs WHERE user_id = $1 AND (similarity(title, $2) > 0.1 OR LOWER(title) LIKE LOWER($3))
		ORDER BY similarity(title, $2) DESC LIMIT 20`,
		userId, query, likePattern)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var songs []Song
	var maxSim float64
	for rows.Next() {
		var s Song
		var artistIdVal, albumIdVal int
		var durationMsVal *int
		var spotifyIdPg, musicbrainzIdPg pgtype.Text
		var sim float64
		err := rows.Scan(&s.Id, &s.UserId, &s.Title, &artistIdVal, &albumIdVal, &durationMsVal, &spotifyIdPg, &musicbrainzIdPg, &sim)
		if err != nil {
			return nil, 0, err
		}
		s.ArtistId = artistIdVal
		s.AlbumId = albumIdVal
		if durationMsVal != nil {
			s.DurationMs = *durationMsVal
		}
		if spotifyIdPg.Status == pgtype.Present {
			s.SpotifyId = spotifyIdPg.String
		}
		if musicbrainzIdPg.Status == pgtype.Present {
			s.MusicbrainzId = musicbrainzIdPg.String
		}
		songs = append(songs, s)
		if sim > maxSim {
			maxSim = sim
		}
	}
	return songs, maxSim, nil
}

func GetArtistStats(userId, artistId int) (int, error) {
	var count int
	err := Pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM history WHERE user_id = $1 AND $2 = ANY(artist_ids)",
		userId, artistId).Scan(&count)
	return count, err
}

func GetSongStats(userId, songId int) (int, error) {
	var count int
	err := Pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM history WHERE user_id = $1 AND song_id = $2",
		userId, songId).Scan(&count)
	return count, err
}

func MergeArtists(userId int, fromArtistId, toArtistId int) error {
	_, err := Pool.Exec(context.Background(),
		`UPDATE history SET artist_id = $1 WHERE user_id = $2 AND artist_id = $3`,
		toArtistId, userId, fromArtistId)
	if err != nil {
		return err
	}
	_, err = Pool.Exec(context.Background(),
		`UPDATE songs SET artist_id = $1 WHERE user_id = $2 AND artist_id = $3`,
		toArtistId, userId, fromArtistId)
	if err != nil {
		return err
	}
	_, err = Pool.Exec(context.Background(),
		`DELETE FROM artists WHERE id = $1 AND user_id = $2`,
		fromArtistId, userId)
	return err
}

func GetHistoryForArtist(userId, artistId int, limit, offset int) ([]ScrobbleEntry, error) {
	rows, err := Pool.Query(context.Background(),
		`SELECT h.timestamp, h.song_name, h.album_name, h.ms_played, h.platform,
			(SELECT name FROM artists WHERE id = h.artist_id) as artist_name,
			h.artist_ids
		FROM history h WHERE h.user_id = $1 AND $2 = ANY(h.artist_ids) 
		ORDER BY h.timestamp DESC LIMIT $3 OFFSET $4`,
		userId, artistId, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []ScrobbleEntry
	for rows.Next() {
		var e ScrobbleEntry
		err := rows.Scan(&e.Timestamp, &e.SongName, &e.AlbumName, &e.MsPlayed, &e.Platform, &e.ArtistName, &e.ArtistIds)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func GetHistoryForSong(userId, songId int, limit, offset int) ([]ScrobbleEntry, error) {
	rows, err := Pool.Query(context.Background(),
		`SELECT h.timestamp, h.song_name, h.album_name, h.ms_played, h.platform,
			(SELECT name FROM artists WHERE id = h.artist_id) as artist_name,
			h.artist_ids
		FROM history h WHERE h.user_id = $1 AND h.song_id = $2 
		ORDER BY h.timestamp DESC LIMIT $3 OFFSET $4`,
		userId, songId, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []ScrobbleEntry
	for rows.Next() {
		var e ScrobbleEntry
		err := rows.Scan(&e.Timestamp, &e.SongName, &e.AlbumName, &e.MsPlayed, &e.Platform, &e.ArtistName, &e.ArtistIds)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

type ScrobbleEntry struct {
	Timestamp  time.Time
	SongName   string
	ArtistName string
	AlbumName  string
	MsPlayed   int
	Platform   string
	ArtistIds  []int
}

func MigrateHistoryEntities() error {
	rows, err := Pool.Query(context.Background(),
		`SELECT DISTINCT h.user_id, h.artist, h.song_name, h.album_name 
		FROM history h 
		WHERE h.artist_id IS NULL OR h.song_id IS NULL`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching history for migration: %v\n", err)
		return err
	}
	defer rows.Close()

	type UniqueEntity struct {
		UserId   int
		Artist   string
		SongName string
		Album    string
	}

	var entities []UniqueEntity
	seen := make(map[string]bool)
	for rows.Next() {
		var r UniqueEntity
		if err := rows.Scan(&r.UserId, &r.Artist, &r.SongName, &r.Album); err != nil {
			continue
		}
		key := fmt.Sprintf("%d-%s-%s-%s", r.UserId, r.Artist, r.SongName, r.Album)
		if !seen[key] {
			seen[key] = true
			entities = append(entities, r)
		}
	}

	artistIds := make(map[string]int)
	albumIds := make(map[string]int)

	for _, e := range entities {
		if e.Artist == "" {
			continue
		}
		key := fmt.Sprintf("%d-%s", e.UserId, e.Artist)
		if _, exists := artistIds[key]; !exists {
			id, _, err := GetOrCreateArtist(e.UserId, e.Artist)
			if err != nil {
				continue
			}
			artistIds[key] = id
		}
	}

	for _, e := range entities {
		if e.Album == "" || e.Artist == "" {
			continue
		}
		artistKey := fmt.Sprintf("%d-%s", e.UserId, e.Artist)
		artistId, ok := artistIds[artistKey]
		if !ok {
			continue
		}
		albumKey := fmt.Sprintf("%d-%s-%d", e.UserId, e.Album, artistId)
		if _, exists := albumIds[albumKey]; !exists {
			id, _, err := GetOrCreateAlbum(e.UserId, e.Album, artistId)
			if err != nil {
				continue
			}
			albumIds[albumKey] = id
		}
	}

	for _, e := range entities {
		if e.SongName == "" || e.Artist == "" {
			continue
		}
		artistKey := fmt.Sprintf("%d-%s", e.UserId, e.Artist)
		artistId, ok := artistIds[artistKey]
		if !ok {
			continue
		}

		var albumId int
		if e.Album != "" {
			albumKey := fmt.Sprintf("%d-%s-%d", e.UserId, e.Album, artistId)
			albumId = albumIds[albumKey]
		}

		songId, _, err := GetOrCreateSong(e.UserId, e.SongName, artistId, albumId)
		if err != nil {
			continue
		}

		_, err = Pool.Exec(context.Background(),
			`UPDATE history SET artist_id = $1, song_id = $2 
			WHERE user_id = $3 AND artist = $4 AND song_name = $5 
			AND (artist_id IS NULL OR song_id IS NULL)`,
			artistId, songId, e.UserId, e.Artist, e.SongName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating history entity IDs: %v\n", err)
		}
	}

	return nil
}

func GetAlbumStats(userId, albumId int) (int, error) {
	var count int
	err := Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM history h 
		JOIN songs s ON h.song_id = s.id 
		WHERE h.user_id = $1 AND s.album_id = $2`,
		userId, albumId).Scan(&count)
	return count, err
}

func GetHistoryForAlbum(userId, albumId int, limit, offset int) ([]ScrobbleEntry, error) {
	rows, err := Pool.Query(context.Background(),
		`SELECT h.timestamp, h.song_name, h.album_name, h.ms_played, h.platform,
			(SELECT name FROM artists WHERE id = h.artist_id) as artist_name,
			h.artist_ids
		FROM history h
		JOIN songs s ON h.song_id = s.id
		WHERE h.user_id = $1 AND s.album_id = $2 
		ORDER BY h.timestamp DESC LIMIT $3 OFFSET $4`,
		userId, albumId, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []ScrobbleEntry
	for rows.Next() {
		var e ScrobbleEntry
		err := rows.Scan(&e.Timestamp, &e.SongName, &e.AlbumName, &e.MsPlayed, &e.Platform, &e.ArtistName, &e.ArtistIds)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func GetSongStatsForSongs(userId int, songIds []int) (int, error) {
	if len(songIds) == 0 {
		return 0, nil
	}
	var count int
	err := Pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM history WHERE user_id = $1 AND song_id = ANY($2)",
		userId, songIds).Scan(&count)
	return count, err
}

func GetHistoryForSongs(userId int, songIds []int, limit, offset int) ([]ScrobbleEntry, error) {
	if len(songIds) == 0 {
		return []ScrobbleEntry{}, nil
	}
	rows, err := Pool.Query(context.Background(),
		`SELECT h.timestamp, h.song_name, h.album_name, h.ms_played, h.platform,
			(SELECT name FROM artists WHERE id = h.artist_id) as artist_name,
			h.artist_ids
		FROM history h WHERE h.user_id = $1 AND h.song_id = ANY($2) 
		ORDER BY h.timestamp DESC LIMIT $3 OFFSET $4`,
		userId, songIds, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []ScrobbleEntry
	for rows.Next() {
		var e ScrobbleEntry
		err := rows.Scan(&e.Timestamp, &e.SongName, &e.AlbumName, &e.MsPlayed, &e.Platform, &e.ArtistName, &e.ArtistIds)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}
