package artwork

import "testing"

func TestPrimaryArtist(t *testing.T) {
	cases := map[string]string{
		"Bladee • Ecco2k":                  "Bladee",
		"bladee x yung lean":               "bladee",
		"Thaiboy Digital(+ECCO2K&Bladee)":  "Thaiboy Digital",
		"Bladee & ssaliva":                 "Bladee",
		"bladee + black kray":              "bladee",
		"Xaviersobased; jtxpo":             "Xaviersobased",
		"2hollis, Circleain":               "2hollis",
		"nettspend feat. Xaviersobased":    "nettspend",
		"Malcolm X":                        "Malcolm X",
		"Godspeed You! Black Emperor":      "Godspeed You! Black Emperor",
		"xaviersobased • Xavier Lopez • A": "xaviersobased",
	}
	for in, want := range cases {
		if got := primaryArtist(in); got != want {
			t.Errorf("primaryArtist(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickBest(t *testing.T) {
	cases := []struct {
		name          string
		cands         []candidate
		title, artist string
		want          string
	}{
		{
			name: "remastered suffix",
			cands: []candidate{
				{title: "In The Court Of The Crimson King (Expanded & Remastered Original Album Mix)",
					artist: "King Crimson", image: "kc"},
				{title: "In The Court Of The Crimson King", artist: "The Doraemons", image: "wrong"},
			},
			title: "In the Court of the Crimson King", artist: "King Crimson", want: "kc",
		},
		{
			name:  "leading the on artist",
			cands: []candidate{{title: "Christmas Album", artist: "Jackson 5", image: "j5"}},
			title: "Christmas Album", artist: "The Jackson 5", want: "j5",
		},
		{
			name: "skips placeholder duplicate",
			cands: []candidate{
				{title: "The Goo Goo Dolls", image: "real"},
				{title: "Goo Goo Dolls", image: ""},
			},
			title: "Goo Goo Dolls", want: "real",
		},
		{
			name:  "combined credit matches primary artist",
			cands: []candidate{{title: "Like A Virgin", artist: "Bladee", image: "b"}},
			title: "Like A Virgin", artist: "ADAMN KILLA X BLADEE • Bladee", want: "",
		},
		{
			name:  "combined credit starting with primary",
			cands: []candidate{{title: "undergone", artist: "Bladee", image: "b"}},
			title: "undergone", artist: "Bladee & ssaliva", want: "b",
		},
		{
			name: "exact beats loose",
			cands: []candidate{
				{title: "Flex Musix (Deluxe)", artist: "OsamaSon", image: "deluxe"},
				{title: "Flex Musix", artist: "OsamaSon", image: "exact"},
			},
			title: "Flex Musix", artist: "OsamaSon", want: "exact",
		},
		{
			name:  "different volume is not a match",
			cands: []candidate{{title: "Minecraft - Volume Beta", artist: "C418", image: "beta"}},
			title: "Minecraft - Volume Alpha", artist: "C418", want: "",
		},
		{
			name:  "wrong artist is not a match",
			cands: []candidate{{title: "In The Court Of The Crimson King", artist: "The Doraemons", image: "x"}},
			title: "In the Court of the Crimson King", artist: "King Crimson", want: "",
		},
	}
	for _, c := range cases {
		got, _ := pickBest(c.cands, c.title, c.artist)
		if got.image != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got.image, c.want)
		}
	}
}
