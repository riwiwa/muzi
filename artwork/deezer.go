package artwork

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Deezer's public search API needs no credentials, so it's the fallback for every user
// Plain-text queries are used because Deezer's field filters (track:"..." artist:"...") often return nothing;
// results are matched against the exact names instead
const deezerAPIURL = "https://api.deezer.com"

type deezerArtist struct {
	Name       string `json:"name"`
	PictureXl  string `json:"picture_xl"`
	PictureBig string `json:"picture_big"`
}

type deezerAlbum struct {
	Title   string       `json:"title"`
	CoverXl string       `json:"cover_xl"`
	Artist  deezerArtist `json:"artist"`
}

type deezerTrack struct {
	Title  string       `json:"title"`
	Artist deezerArtist `json:"artist"`
	Album  deezerAlbum  `json:"album"`
}

type deezerError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func deezerSearch(kind, query string, out any) error {
	throttle()
	endpoint := deezerAPIURL + "/search/" + kind + "?limit=10&q=" + url.QueryEscape(query)
	resp, err := httpClient.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deezer search returned %d", resp.StatusCode)
	}

	var body struct {
		Data  json.RawMessage `json:"data"`
		Error *deezerError    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	// Deezer reports errors (including rate limiting) with a 200 status
	if body.Error != nil {
		return fmt.Errorf("deezer error %d: %s", body.Error.Code, body.Error.Message)
	}
	if len(body.Data) == 0 {
		return nil
	}
	return json.Unmarshal(body.Data, out)
}

// Deezer returns a generic silhouette URL (empty hash: ".../artist//...") for artists without a picture
func deezerArtistPicture(a deezerArtist) string {
	pic := a.PictureXl
	if pic == "" {
		pic = a.PictureBig
	}
	if pic == "" || strings.Contains(pic, "/artist//") {
		return ""
	}
	return pic
}

func deezerArtistImage(name string) (string, error) {
	var artists []deezerArtist
	if err := deezerSearch("artist", name, &artists); err != nil {
		return "", err
	}
	for _, a := range artists {
		if sameName(a.Name, name) {
			return deezerArtistPicture(a), nil
		}
	}
	return "", nil
}

func deezerAlbumImage(title, artist string) (string, error) {
	var albums []deezerAlbum
	if err := deezerSearch("album", strings.TrimSpace(title+" "+artist), &albums); err != nil {
		return "", err
	}
	for _, a := range albums {
		if sameName(a.Title, title) && (artist == "" || sameName(a.Artist.Name, artist)) {
			return a.CoverXl, nil
		}
	}
	return "", nil
}

func deezerSongImage(title, artist string) (string, error) {
	var tracks []deezerTrack
	if err := deezerSearch("track", strings.TrimSpace(title+" "+artist), &tracks); err != nil {
		return "", err
	}
	for _, t := range tracks {
		if sameName(t.Title, title) && (artist == "" || sameName(t.Artist.Name, artist)) {
			return t.Album.CoverXl, nil
		}
	}
	return "", nil
}
