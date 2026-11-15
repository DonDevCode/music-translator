package platform

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var bracketPattern = regexp.MustCompile(`\s*[(\[][^)\]]*[)\]]`)

// BestMatch returns the candidate that is most likely the same song as want,
// or nil if none is. Both title and an artist must match. Among equally good
// candidates the earliest wins, so callers should pass them in order of
// preference (e.g. search ranking).
func BestMatch(want *Track, candidates []*Track) *Track {
	var best *Track
	bestScore := 0

	for _, c := range candidates {
		if score := matchScore(want.Title, c.Title, want.Artists, c.Artists); score > bestScore {
			best, bestScore = c, score
		}
	}

	return best
}

// BestAlbumMatch is BestMatch for albums. Track counts tell apart releases
// with the same title, such as a single and the album it is from, or an
// album and its deluxe edition. Bonus tracks make counts differ slightly
// between platforms, so only an exact count is preferred and only a large
// difference rules a candidate out.
func BestAlbumMatch(want *Album, candidates []*Album) *Album {
	var best *Album
	bestScore := 0

	for _, c := range candidates {
		// Doubled so that the same track count only breaks ties between
		// equally good title and artist matches.
		score := 2 * matchScore(want.Title, c.Title, want.Artists, c.Artists)
		if score == 0 || !similarTrackCounts(want.TrackCount, c.TrackCount) {
			continue
		}
		if want.TrackCount != 0 && want.TrackCount == c.TrackCount {
			score++
		}
		if score > bestScore {
			best, bestScore = c, score
		}
	}

	return best
}

// matchScore rates how likely two releases are the same, or returns 0 if
// they aren't. Both title and an artist must match.
func matchScore(titleA, titleB string, artistsA, artistsB []string) int {
	ts := titleScore(titleA, titleB)
	as := artistScore(artistsA, artistsB)
	if ts == 0 || as == 0 {
		return 0
	}
	return ts + as
}

// similarTrackCounts reports whether a and b could be counts of the same
// release, e.g. 17 and 19 but not 17 and 40. Unknown counts (0) match all.
func similarTrackCounts(a, b int) bool {
	if a == 0 || b == 0 {
		return true
	}
	return max(a, b) <= 2*min(a, b)+1
}

// titleScore compares titles, ignoring case and punctuation. Bracketed parts
// like "(Remastered)" only break ties, so "Song (Live)" prefers "Song (Live)"
// but still matches "Song". Unbracketed version suffixes like "Song
// Remastered 2011" match weakly; other extra words mean a different song.
func titleScore(a, b string) int {
	switch {
	case key(a) != "" && key(a) == key(b):
		return 4
	case key(baseTitle(a)) != "" && key(baseTitle(a)) == key(baseTitle(b)):
		return 3
	case extendsWithVersion(words(baseTitle(a)), words(baseTitle(b))) ||
		extendsWithVersion(words(baseTitle(b)), words(baseTitle(a))):
		return 1
	}
	return 0
}

// artistScore checks whether the artist lists overlap. Platforms disagree on
// how to list multiple artists ("A & B" vs. "A", "B"), so names are compared
// against the joined list of the other side, as whole words so that "Queen"
// doesn't match "Queensrÿche".
func artistScore(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if key(a[0]) != "" && key(a[0]) == key(b[0]) {
		return 2
	}

	joinedA, joinedB := words(strings.Join(a, " ")), words(strings.Join(b, " "))
	for _, artist := range a {
		if containsWords(joinedB, words(artist)) {
			return 1
		}
	}
	for _, artist := range b {
		if containsWords(joinedA, words(artist)) {
			return 1
		}
	}
	return 0
}

// baseTitle removes bracketed parts and " - " suffixes like "- Remastered 2011".
func baseTitle(s string) string {
	s = bracketPattern.ReplaceAllString(s, "")
	s, _, _ = strings.Cut(s, " - ")
	return s
}

// key reduces s to lowercase letters and digits for comparison.
func key(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// words splits s at whitespace and reduces each word with key, so "Don't"
// and "AC/DC" stay single words.
func words(s string) []string {
	var ws []string
	for _, f := range strings.Fields(s) {
		if k := key(f); k != "" {
			ws = append(ws, k)
		}
	}
	return ws
}

// ContainsName reports whether name appears in s as whole words, ignoring
// case and punctuation, so "Sia" is in "Sia - Chandelier" but not in "Asian
// Dub". An empty name is in nothing.
func ContainsName(s, name string) bool {
	return containsWords(words(s), words(name))
}

// containsWords reports whether sub appears as a contiguous run of words in s.
func containsWords(s, sub []string) bool {
	if len(sub) == 0 {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if slices.Equal(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

// versionWords describe a recording or release rather than the song itself.
var versionWords = map[string]bool{
	"remaster": true, "remastered": true, "live": true, "version": true,
	"edit": true, "mix": true, "remix": true, "acoustic": true, "mono": true,
	"stereo": true, "radio": true, "extended": true, "explicit": true,
	"clean": true, "instrumental": true, "demo": true, "single": true,
	"album": true, "original": true, "deluxe": true, "edition": true,
}

// extendsWithVersion reports whether long is title followed only by version
// words and numbers, as in "Song Remastered 2011" for "Song".
func extendsWithVersion(long, title []string) bool {
	if len(title) == 0 || len(long) <= len(title) || !slices.Equal(long[:len(title)], title) {
		return false
	}
	for _, w := range long[len(title):] {
		if !versionWords[w] && !isNumber(w) {
			return false
		}
	}
	return true
}

func isNumber(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
