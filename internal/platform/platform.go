// Package platform defines the abstraction for music streaming services.
package platform

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrNotFound is returned by Lookup when the platform has no such track.
var ErrNotFound = errors.New("track not found")

// Track is a song as known to a single platform.
type Track struct {
	Title   string
	Artists []string
	Year    int    // 0 if unknown
	URL     string // canonical link to the track on its platform
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
}

// Link is a track link found in a message.
type Link struct {
	Platform Platform
	ID       string
}

// Trailing characters that are almost always punctuation or Discord
// markdown around a URL rather than part of it.
const urlTrailers = ".,!?;:)]|*'\""

var urlPattern = regexp.MustCompile(`https?://[^\s<>]+`)

// FindLinks returns up to limit distinct track links in content, in order of
// appearance.
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
			id, ok := p.ParseURL(u)
			if !ok {
				continue
			}
			key := p.Name() + "\x00" + id
			if !seen[key] {
				seen[key] = true
				links = append(links, Link{Platform: p, ID: id})
			}
			break
		}
	}

	return links
}
