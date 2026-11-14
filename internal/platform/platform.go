// Package platform defines the abstraction for music streaming services.
package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ErrNotFound is returned by Lookup when the platform has no such track or
// album.
var ErrNotFound = errors.New("not found")

// Track is a song as known to a single platform.
type Track struct {
	Title   string
	Artists []string
	Year    int    // 0 if unknown
	URL     string // canonical link to the track on its platform
}

// Album is an album, EP or single as known to a single platform.
type Album struct {
	Title      string
	Artists    []string
	Year       int    // 0 if unknown
	TrackCount int    // 0 if unknown
	URL        string // canonical link to the album on its platform
}

// Platform is a music streaming service the bot can recognize and link to.
type Platform interface {
	// Name returns a human-readable name, e.g. "Spotify".
	Name() string
	// ParseURL returns the track ID if u links to a track on this platform.
	// The ID is opaque and only meaningful to the same platform's Lookup.
	ParseURL(u *url.URL) (id string, ok bool)
	// Lookup fetches the track with the given ID.
	Lookup(ctx context.Context, id string) (*Track, error)
	// Search finds the same song as t on this platform. It returns
	// ErrNotFound if there is no confident match.
	Search(ctx context.Context, t *Track) (*Track, error)
}

// AlbumPlatform is a Platform that can also recognize album links, so they
// can be translated to platforms that implement AlbumSearcher.
type AlbumPlatform interface {
	Platform
	// ParseAlbumURL returns the album ID if u links to an album on this
	// platform. It must not accept links that ParseURL accepts.
	ParseAlbumURL(u *url.URL) (id string, ok bool)
	// LookupAlbum fetches the album with the given ID.
	LookupAlbum(ctx context.Context, id string) (*Album, error)
}

// AlbumSearcher is an AlbumPlatform that album links from other platforms
// can be translated to.
type AlbumSearcher interface {
	AlbumPlatform
	// SearchAlbum finds the same album as a on this platform. It returns
	// ErrNotFound if there is no confident match.
	SearchAlbum(ctx context.Context, a *Album) (*Album, error)
}

// Kind is what a link points to.
type Kind int

const (
	KindTrack Kind = iota
	KindAlbum
)

// Link is a track or album link found in a message.
type Link struct {
	Platform Platform
	Kind     Kind
	ID       string
}

// Trailing characters that are almost always punctuation or Discord
// markdown around a URL rather than part of it.
const urlTrailers = ".,!?;:)]|*'\""

var urlPattern = regexp.MustCompile(`https?://[^\s<>]+`)

// FindLinks returns up to limit distinct track and album links in content, in
// order of appearance.
func FindLinks(content string, platforms []Platform, limit int) []Link {
	var links []Link
	seen := make(map[string]bool)

	for _, raw := range urlPattern.FindAllString(content, -1) {
		if len(links) >= limit {
			break
		}

		u, err := url.Parse(strings.TrimRight(raw, urlTrailers))
		if err != nil {
			continue
		}

		for _, p := range platforms {
			link, ok := parseLink(p, u)
			if !ok {
				continue
			}
			key := fmt.Sprint(p.Name(), "\x00", link.Kind, "\x00", link.ID)
			if !seen[key] {
				seen[key] = true
				links = append(links, link)
			}
			break
		}
	}

	return links
}

// parseLink returns the track or album link that u is on p, if any.
func parseLink(p Platform, u *url.URL) (Link, bool) {
	if id, ok := p.ParseURL(u); ok {
		return Link{Platform: p, Kind: KindTrack, ID: id}, true
	}
	if ap, ok := p.(AlbumPlatform); ok {
		if id, ok := ap.ParseAlbumURL(u); ok {
			return Link{Platform: p, Kind: KindAlbum, ID: id}, true
		}
	}
	return Link{}, false
}

// ParseYear extracts the year from a date starting with "YYYY", such as
// "1975-10-31T12:00:00Z". It returns 0 if there is none.
func ParseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	year, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return year
}
