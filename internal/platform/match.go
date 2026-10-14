package platform

import (
	"regexp"
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
		ts := titleScore(want.Title, c.Title)
		as := artistScore(want.Artists, c.Artists)
		if ts == 0 || as == 0 {
			continue
		}
		if score := ts + as; score > bestScore {
			best, bestScore = c, score
		}
	}

	return best
}

// titleScore compares titles, ignoring case and punctuation. Bracketed parts
// like "(Remastered)" only break ties, so "Song (Live)" prefers "Song (Live)"
// but still matches "Song".
func titleScore(a, b string) int {
	switch {
	case key(a) != "" && key(a) == key(b):
		return 4
	case key(baseTitle(a)) != "" && key(baseTitle(a)) == key(baseTitle(b)):
		return 3
	case containsEither(key(baseTitle(a)), key(baseTitle(b))):
		return 1
	}
	return 0
}

// artistScore checks whether the artist lists overlap. Platforms disagree on
// how to list multiple artists ("A & B" vs. "A", "B"), so names are compared
// against the joined list of the other side.
func artistScore(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if key(a[0]) != "" && key(a[0]) == key(b[0]) {
		return 2
	}

	joinedA, joinedB := key(strings.Join(a, " ")), key(strings.Join(b, " "))
	for _, artist := range a {
		if k := key(artist); k != "" && strings.Contains(joinedB, k) {
			return 1
		}
	}
	for _, artist := range b {
		if k := key(artist); k != "" && strings.Contains(joinedA, k) {
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

func containsEither(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}
