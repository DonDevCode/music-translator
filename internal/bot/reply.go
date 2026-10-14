package bot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/DonDevCode/music-translator/internal/platform"
)

// result is the outcome of searching for a track on one platform.
type result struct {
	platform platform.Platform
	track    *platform.Track // nil if not found or failed
	err      error           // set if the search failed for another reason than not found
}

// markdownEscaper escapes characters that Discord would treat as formatting.
var markdownEscaper = strings.NewReplacer(
	`\`, `\\`, `*`, `\*`, `_`, `\_`, `~`, `\~`, "`", "\\`", `|`, `\|`,
)

// formatReply describes the source track and links to it on other platforms:
//
//	**Bohemian Rhapsody** – Queen · 1975
//	YouTube Music: https://music.youtube.com/watch?v=…
//
// The links are bare URLs in the message text so Discord shows its previews.
func formatReply(source *platform.Track, results []result) string {
	var b strings.Builder

	fmt.Fprintf(&b, "**%s** – %s",
		markdownEscaper.Replace(source.Title),
		markdownEscaper.Replace(strings.Join(source.Artists, ", ")))
	if year := releaseYear(source, results); year != 0 {
		fmt.Fprintf(&b, " · %d", year)
	}

	for _, r := range results {
		b.WriteString("\n")
		var rateLimitErr *platform.RateLimitError
		switch {
		case r.track != nil:
			fmt.Fprintf(&b, "%s: %s", r.platform.Name(), r.track.URL)
		case errors.As(r.err, &rateLimitErr):
			b.WriteString(rateLimitErr.Error())
		case r.err != nil:
			fmt.Fprintf(&b, "Couldn't search %s", r.platform.Name())
		default:
			fmt.Fprintf(&b, "Not found on %s", r.platform.Name())
		}
	}

	return b.String()
}

// releaseYear prefers the source track's year and falls back to the other
// platforms, since some sources (e.g. YouTube music videos) don't know it.
func releaseYear(source *platform.Track, results []result) int {
	if source.Year != 0 {
		return source.Year
	}
	for _, r := range results {
		if r.track != nil && r.track.Year != 0 {
			return r.track.Year
		}
	}
	return 0
}
