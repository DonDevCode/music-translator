// Package applemusic looks up Apple Music tracks via the iTunes Lookup API.
package applemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonDevCode/music-translator/internal/platform"
)

const defaultBaseURL = "https://itunes.apple.com"

var (
	storefrontPattern = regexp.MustCompile(`^[a-z]{2}$`)
	numericPattern    = regexp.MustCompile(`^[0-9]+$`)
)

// Client is the Apple Music platform. It needs no credentials.
type Client struct {
	http    *http.Client
	baseURL string
}

// New creates an Apple Music client.
func New(httpClient *http.Client) *Client {
	return &Client{http: httpClient, baseURL: defaultBaseURL}
}

// Name implements platform.Platform.
func (c *Client) Name() string { return "Apple Music" }

// ParseURL implements platform.Platform. It recognizes
//
//	music.apple.com/{storefront}/song/[{slug}/]{trackID}
//	music.apple.com/{storefront}/album/[{slug}/]{albumID}?i={trackID}
//
// and returns an ID of the form "{storefront}/{trackID}", because tracks are
// only guaranteed to exist in the storefront they were linked from.
func (c *Client) ParseURL(u *url.URL) (string, bool) {
	if u.Host != "music.apple.com" && u.Host != "geo.music.apple.com" {
		return "", false
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || !storefrontPattern.MatchString(parts[0]) {
		return "", false
	}
	storefront := parts[0]

	var trackID string
	switch parts[1] {
	case "song":
		trackID = parts[len(parts)-1]
	case "album":
		trackID = u.Query().Get("i")
	default:
		return "", false
	}

	if !numericPattern.MatchString(trackID) {
		return "", false
	}
	return storefront + "/" + trackID, true
}

type lookupResponse struct {
	Results []struct {
		Kind         string `json:"kind"`
		TrackName    string `json:"trackName"`
		ArtistName   string `json:"artistName"`
		ReleaseDate  string `json:"releaseDate"`
		TrackViewURL string `json:"trackViewUrl"`
	} `json:"results"`
}

// Lookup implements platform.Platform.
func (c *Client) Lookup(ctx context.Context, id string) (*platform.Track, error) {
	storefront, trackID, ok := strings.Cut(id, "/")
	if !ok {
		return nil, fmt.Errorf("invalid apple music id %q", id)
	}

	q := url.Values{"id": {trackID}, "country": {storefront}, "entity": {"song"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/lookup?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("itunes lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("itunes lookup: unexpected status %s", resp.Status)
	}

	var body lookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding itunes lookup: %w", err)
	}

	for _, r := range body.Results {
		if r.Kind != "song" {
			continue
		}
		return &platform.Track{
			Title:   r.TrackName,
			Artists: []string{r.ArtistName},
			Year:    parseYear(r.ReleaseDate),
			URL:     cleanTrackURL(r.TrackViewURL),
		}, nil
	}

	return nil, platform.ErrNotFound
}

// parseYear extracts the year from an RFC 3339 date like "1975-10-31T12:00:00Z".
func parseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	year, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return year
}

// cleanTrackURL drops tracking parameters (e.g. "uo") but keeps the track
// selector "i".
func cleanTrackURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := url.Values{}
	if i := u.Query().Get("i"); i != "" {
		q.Set("i", i)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
