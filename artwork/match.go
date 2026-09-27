package artwork

import (
	"regexp"
	"strings"
	"unicode"
)

// A search result from a provider. For artist searches title is the artist's name and artist is empty.
type candidate struct {
	title     string
	artist    string
	image     string
	spotifyId string
}

// Separators between artists in a combined credit, e.g. "Bladee • Ecco2k", "A x B", "A (feat. B)"
var artistSeparator = regexp.MustCompile(`(?i)\s*(,|;|•|&|\+|\(\+|\s+x\s+|\s+(feat|ft|featuring|with)\.?\s+)\s*`)

// Bracketed or dashed suffixes naming a release variant rather than a different release
var variantWords = regexp.MustCompile(
	`(?i)remaster|edition|deluxe|expanded|version|bonus|anniversary|explicit|clean|mono|stereo|` +
		`original album mix|reissue|feat\.?|ft\.`,
)
var bracketSuffix = regexp.MustCompile(`\s*[(\[]([^()\[\]]*)[)\]]\s*$`)
var dashSuffix = regexp.MustCompile(`\s+-\s+([^-]*)$`)

// Compares names ignoring case, punctuation and spacing
func sameName(a, b string) bool {
	return normalize(a) == normalize(b)
}

func normalize(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	// names made only of symbols (e.g. "!!!") would otherwise all match each other
	if sb.Len() == 0 {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return sb.String()
}

// Normalizes a title with variant suffixes ("(Remastered 2024)", " - Deluxe Edition") and a
// leading "The" removed, so "Building the Perfect Beast" matches "Building The Perfect Beast (Remastered)"
func looseKey(s string) string {
	for {
		trimmed := s
		if m := bracketSuffix.FindStringSubmatch(trimmed); m != nil && variantWords.MatchString(m[1]) {
			trimmed = strings.TrimSpace(trimmed[:len(trimmed)-len(m[0])])
		}
		if m := dashSuffix.FindStringSubmatch(trimmed); m != nil && variantWords.MatchString(m[1]) {
			trimmed = strings.TrimSpace(trimmed[:len(trimmed)-len(m[0])])
		}
		if trimmed == s || trimmed == "" {
			break
		}
		s = trimmed
	}
	s = strings.TrimSpace(s)
	if len(s) > 4 && strings.EqualFold(s[:4], "the ") {
		s = s[4:]
	}
	return normalize(s)
}

// The first artist of a combined credit
func primaryArtist(s string) string {
	parts := artistSeparator.Split(s, 2)
	primary := strings.TrimSpace(parts[0])
	if primary == "" {
		return s
	}
	return primary
}

func titleMatches(got, want string, loose bool) bool {
	if sameName(got, want) {
		return true
	}
	return loose && looseKey(got) == looseKey(want)
}

func artistMatches(got, want string) bool {
	if got == "" || want == "" {
		return true
	}
	for _, w := range []string{want, primaryArtist(want)} {
		if sameName(got, w) || looseKey(got) == looseKey(w) || looseKey(primaryArtist(got)) == looseKey(w) {
			return true
		}
	}
	return false
}

// Picks the first result with an image whose name matches exactly, falling back to a loose match.
// Results without images are skipped, since providers often list a duplicate entry with no picture.
func pickBest(cands []candidate, title, artist string) (candidate, bool) {
	for _, loose := range []bool{false, true} {
		for _, c := range cands {
			if c.image != "" && titleMatches(c.title, title, loose) && artistMatches(c.artist, artist) {
				return c, true
			}
		}
	}
	return candidate{}, false
}

// Searches for title (and artist), retrying with just the primary artist when the credit is combined.
// search receives the text to query and the artist to match against.
func searchWithFallback(
	title, artist string,
	search func(query string) ([]candidate, error),
) (candidate, error) {
	primary := primaryArtist(artist)
	queries := []string{strings.TrimSpace(title + " " + primary)}
	if artist == "" {
		// artist search: title is the artist's name
		queries = []string{title}
		if p := primaryArtist(title); p != title {
			queries = append(queries, p)
		}
	}

	for i, q := range queries {
		cands, err := search(q)
		if err != nil {
			return candidate{}, err
		}
		matchTitle := title
		if artist == "" && i > 0 {
			matchTitle = q
		}
		if c, ok := pickBest(cands, matchTitle, artist); ok {
			return c, nil
		}
	}
	return candidate{}, nil
}
