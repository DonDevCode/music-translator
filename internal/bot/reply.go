package bot

import (
	"fmt"
	"strings"

	"github.com/DonDevCode/music-translator/internal/platform"
)

// result is the outcome of searching for a track or album on one platform.
type result struct {
	platform platform.Platform
	url      string // "" if not found or failed
	year     int    // 0 if unknown
	err      error  // set if the search failed for another reason than not found
}

// markdownEscaper escapes characters that Discord would treat as formatting.
// Escaping square brackets is enough to break masked links "[text](url)".
var markdownEscaper = strings.NewReplacer(
	`\`, `\\`, `*`, `\*`, `_`, `\_`, `~`, `\~`, "`", "\\`", `|`, `\|`,
	`[`, `\[`, `]`, `\]`,
)

// formatReply describes the source track and links to it on other platforms:
//
//	**Bohemian Rhapsody** – Queen · 1975
//	YouTube Music: https://music.youtube.com/watch?v=…
//
// The links are bare URLs in the message text so Discord shows its previews.
func formatReply(source *platform.Track, results []result) string {
	var b strings.Builder
	writeHeader(&b, source.Title, source.Artists, releaseYear(source.Year, results))
	writeResults(&b, results)
	return b.String()
}

// formatAlbumReply is formatReply for albums, which also shows the number of
// tracks so that singles and deluxe editions are recognizable:
//
//	**A Night At The Opera** – Queen · 1975 · 12 tracks
//	YouTube Music: https://music.youtube.com/playlist?list=…
func formatAlbumReply(source *platform.Album, results []result) string {
	var b strings.Builder
	writeHeader(&b, source.Title, source.Artists, releaseYear(source.Year, results))
	switch source.TrackCount {
	case 0:
	case 1:
		b.WriteString(" · 1 track")
	default:
		fmt.Fprintf(&b, " · %d tracks", source.TrackCount)
	}
	writeResults(&b, results)
	return b.String()
}

func writeHeader(b *strings.Builder, title string, artists []string, year int) {
	fmt.Fprintf(b, "**%s** – %s",
		markdownEscaper.Replace(title),
		markdownEscaper.Replace(strings.Join(artists, ", ")))
	if year != 0 {
		fmt.Fprintf(b, " · %d", year)
	}
}

func writeResults(b *strings.Builder, results []result) {
	for _, r := range results {
		b.WriteString("\n")
		rateLimitErr, rateLimited := platform.AsRateLimitError(r.err)
		switch {
		case r.url != "":
			fmt.Fprintf(b, "%s: %s", r.platform.Name(), r.url)
		case rateLimited:
			b.WriteString(rateLimitErr.Error())
		case r.err != nil:
			fmt.Fprintf(b, "Couldn't search %s", r.platform.Name())
		default:
			fmt.Fprintf(b, "Not found on %s", r.platform.Name())
		}
	}
}

// releaseYear prefers the source's year and falls back to the other
// platforms, since some sources (e.g. YouTube music videos) don't know it.
func releaseYear(sourceYear int, results []result) int {
	if sourceYear != 0 {
		return sourceYear
	}
	for _, r := range results {
		if r.url != "" && r.year != 0 {
			return r.year
		}
	}
	return 0
}
