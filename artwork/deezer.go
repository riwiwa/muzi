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
// results are matched against the names instead (see match.go)
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
	c, err := searchWithFallback(name, "", func(query string) ([]candidate, error) {
		var artists []deezerArtist
		if err := deezerSearch("artist", query, &artists); err != nil {
			return nil, err
		}
		cands := make([]candidate, 0, len(artists))
		for _, a := range artists {
			cands = append(cands, candidate{title: a.Name, image: deezerArtistPicture(a)})
		}
		return cands, nil
	})
	return c.image, err
}

func deezerAlbumImage(title, artist string) (string, error) {
	c, err := searchWithFallback(title, artist, func(query string) ([]candidate, error) {
		var albums []deezerAlbum
		if err := deezerSearch("album", query, &albums); err != nil {
			return nil, err
		}
		cands := make([]candidate, 0, len(albums))
		for _, a := range albums {
			cands = append(cands, candidate{title: a.Title, artist: a.Artist.Name, image: a.CoverXl})
		}
		return cands, nil
	})
	return c.image, err
}

func deezerSongImage(title, artist string) (string, error) {
	c, err := searchWithFallback(title, artist, func(query string) ([]candidate, error) {
		var tracks []deezerTrack
		if err := deezerSearch("track", query, &tracks); err != nil {
			return nil, err
		}
		cands := make([]candidate, 0, len(tracks))
		for _, t := range tracks {
			cands = append(cands, candidate{title: t.Title, artist: t.Artist.Name, image: t.Album.CoverXl})
		}
		return cands, nil
	})
	return c.image, err
}
