// Package applemusic looks up Apple Music tracks via the iTunes Search and
// Lookup APIs, falling back to the Apple Music website for searches.
package applemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/DonDevCode/music-translator/internal/platform"
)

const (
	defaultBaseURL    = "https://itunes.apple.com"
	defaultWebBaseURL = "https://music.apple.com"
)

// maxWebResults limits how many tracks from the website search are checked.
const maxWebResults = 10

// maxWebPageSize guards against unexpectedly large search pages.
const maxWebPageSize = 5 << 20

var (
	storefrontPattern = regexp.MustCompile(`^[a-z]{2}$`)
	numericPattern    = regexp.MustCompile(`^[0-9]+$`)
	// Track links on the website: ".../album/{slug}/{albumID}?i={trackID}"
	// or ".../song/{slug}/{trackID}".
	webTrackPattern = regexp.MustCompile(`[?&]i=([0-9]+)|/song/[^/"?]+/([0-9]+)`)
)

// Client is the Apple Music platform. It needs no credentials.
type Client struct {
	http       *http.Client
	baseURL    string
	webBaseURL string
}

// New creates an Apple Music client.
func New(httpClient *http.Client) *Client {
	return &Client{http: httpClient, baseURL: defaultBaseURL, webBaseURL: defaultWebBaseURL}
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

// searchStorefront is used for searches, where there is no link to take the
// storefront from.
const searchStorefront = "us"

type response struct {
	Results []result `json:"results"`
}

type result struct {
	TrackID      int64  `json:"trackId"`
	Kind         string `json:"kind"`
	TrackName    string `json:"trackName"`
	ArtistName   string `json:"artistName"`
	ReleaseDate  string `json:"releaseDate"`
	TrackViewURL string `json:"trackViewUrl"`
}

func (r result) track() *platform.Track {
	return &platform.Track{
		Title:   r.TrackName,
		Artists: []string{r.ArtistName},
		Year:    platform.ParseYear(r.ReleaseDate),
		URL:     cleanTrackURL(r.TrackViewURL),
	}
}

// Lookup implements platform.Platform.
func (c *Client) Lookup(ctx context.Context, id string) (*platform.Track, error) {
	storefront, trackID, ok := strings.Cut(id, "/")
	if !ok {
		return nil, fmt.Errorf("invalid apple music id %q", id)
	}

	results, err := c.get(ctx, "/lookup", url.Values{"id": {trackID}, "country": {storefront}, "entity": {"song"}})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, platform.ErrNotFound
	}
	return results[0].track(), nil
}

// Search implements platform.Platform. The iTunes Search API is missing
// some releases that are on Apple Music, so if it finds no match the search
// is repeated on the Apple Music website.
func (c *Client) Search(ctx context.Context, t *platform.Track) (*platform.Track, error) {
	term := t.Title
	if len(t.Artists) > 0 {
		term += " " + t.Artists[0]
	}

	results, err := c.get(ctx, "/search", url.Values{
		"term":    {term},
		"country": {searchStorefront},
		"entity":  {"song"},
		"limit":   {"10"},
	})
	if err != nil {
		return nil, err
	}
	if best := bestMatch(t, results); best != nil {
		return best, nil
	}

	ids, err := c.searchWebsite(ctx, term)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, platform.ErrNotFound
	}

	results, err = c.get(ctx, "/lookup", url.Values{
		"id":      {strings.Join(ids, ",")},
		"country": {searchStorefront},
		"entity":  {"song"},
	})
	if err != nil {
		return nil, err
	}

	// Keep the website's ranking, which the lookup doesn't preserve.
	rank := make(map[int64]int, len(ids))
	for i, id := range ids {
		n, _ := strconv.ParseInt(id, 10, 64)
		rank[n] = i
	}
	slices.SortStableFunc(results, func(a, b result) int { return rank[a.TrackID] - rank[b.TrackID] })

	if best := bestMatch(t, results); best != nil {
		return best, nil
	}
	return nil, platform.ErrNotFound
}

func bestMatch(t *platform.Track, results []result) *platform.Track {
	candidates := make([]*platform.Track, len(results))
	for i, r := range results {
		candidates[i] = r.track()
	}
	return platform.BestMatch(t, candidates)
}

// searchWebsite searches the Apple Music website and returns the IDs of the
// tracks on the results page, in order. The website has no official API, so
// this relies on the track links in its HTML.
func (c *Client) searchWebsite(ctx context.Context, term string) ([]string, error) {
	u := c.webBaseURL + "/" + searchStorefront + "/search?" + url.Values{"term": {term}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apple music website search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apple music website search: unexpected status %s", resp.Status)
	}

	page, err := io.ReadAll(io.LimitReader(resp.Body, maxWebPageSize))
	if err != nil {
		return nil, fmt.Errorf("reading apple music website search: %w", err)
	}

	var ids []string
	seen := make(map[string]bool)
	for _, m := range webTrackPattern.FindAllSubmatch(page, -1) {
		id := string(m[1]) + string(m[2])
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
			if len(ids) == maxWebResults {
				break
			}
		}
	}
	return ids, nil
}

// get calls an iTunes endpoint and returns the song results.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("itunes %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("itunes %s: unexpected status %s", path, resp.Status)
	}

	var body response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding itunes %s: %w", path, err)
	}

	var songs []result
	for _, r := range body.Results {
		if r.Kind == "song" {
			songs = append(songs, r)
		}
	}
	return songs, nil
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
