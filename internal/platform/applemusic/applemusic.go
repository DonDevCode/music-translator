// Package applemusic looks up Apple Music tracks and albums via the iTunes
// Search and Lookup APIs, falling back to the Apple Music website for
// searches.
package applemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	// Album links on the website: ".../album/{slug}/{albumID}". The optional
	// group catches track links on the album, which are skipped.
	webAlbumPattern = regexp.MustCompile(`/album/[^/"?]+/([0-9]+)(\?i=)?`)
	// Suffixes Apple adds to the names of singles and EPs.
	releaseTypePattern = regexp.MustCompile(` - (Single|EP)$`)
)

// Client is the Apple Music platform. It needs no credentials.
//
// It implements platform.AlbumSearcher.
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
	storefront, kind, lastPart, ok := parseLink(u)
	if !ok {
		return "", false
	}

	var trackID string
	switch kind {
	case "song":
		trackID = lastPart
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

// ParseAlbumURL implements platform.AlbumPlatform. It recognizes
//
//	music.apple.com/{storefront}/album/[{slug}/]{albumID}
//
// without a track selector "i" and returns an ID of the form
// "{storefront}/{albumID}", like ParseURL.
func (c *Client) ParseAlbumURL(u *url.URL) (string, bool) {
	storefront, kind, albumID, ok := parseLink(u)
	if !ok || kind != "album" || u.Query().Has("i") {
		return "", false
	}

	if !numericPattern.MatchString(albumID) {
		return "", false
	}
	return storefront + "/" + albumID, true
}

// parseLink splits a link of the form
// music.apple.com/{storefront}/{kind}/.../{lastPart}, which ParseURL and
// ParseAlbumURL share.
func parseLink(u *url.URL) (storefront, kind, lastPart string, ok bool) {
	if u.Host != "music.apple.com" && u.Host != "geo.music.apple.com" {
		return "", "", "", false
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || !storefrontPattern.MatchString(parts[0]) {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[len(parts)-1], true
}

// searchStorefront is used for searches, where there is no link to take the
// storefront from.
const searchStorefront = "us"

type response struct {
	Results []result `json:"results"`
}

// result is a song or a collection (album).
type result struct {
	WrapperType       string `json:"wrapperType"`
	Kind              string `json:"kind"`
	CollectionType    string `json:"collectionType"`
	TrackID           int64  `json:"trackId"`
	CollectionID      int64  `json:"collectionId"`
	TrackName         string `json:"trackName"`
	CollectionName    string `json:"collectionName"`
	ArtistName        string `json:"artistName"`
	ReleaseDate       string `json:"releaseDate"`
	TrackCount        int    `json:"trackCount"`
	TrackViewURL      string `json:"trackViewUrl"`
	CollectionViewURL string `json:"collectionViewUrl"`
}

func (r result) track() *platform.Track {
	return &platform.Track{
		Title:   r.TrackName,
		Artists: []string{r.ArtistName},
		Year:    platform.ParseYear(r.ReleaseDate),
		URL:     cleanTrackURL(r.TrackViewURL),
	}
}

// album converts a collection result. The " - Single" and " - EP" suffixes
// are dropped, since other platforms don't use them.
func (r result) album() *platform.Album {
	return &platform.Album{
		Title:      releaseTypePattern.ReplaceAllString(r.CollectionName, ""),
		Artists:    []string{r.ArtistName},
		Year:       platform.ParseYear(r.ReleaseDate),
		TrackCount: r.TrackCount,
		URL:        cleanTrackURL(r.CollectionViewURL),
	}
}

// entity describes one kind of iTunes catalog item, so that songs and albums
// share the lookup and search code.
type entity struct {
	name   string                     // "entity" parameter for the iTunes APIs
	is     func(result) bool          // whether a result is of this kind
	id     func(result) int64         // the result's ID
	webIDs func(page []byte) []string // IDs linked on a website search page
}

var (
	songs = entity{
		name:   "song",
		is:     func(r result) bool { return r.Kind == "song" },
		id:     func(r result) int64 { return r.TrackID },
		webIDs: trackIDs,
	}
	albums = entity{
		name:   "album",
		is:     func(r result) bool { return r.WrapperType == "collection" },
		id:     func(r result) int64 { return r.CollectionID },
		webIDs: albumIDs,
	}
)

// Lookup implements platform.Platform.
func (c *Client) Lookup(ctx context.Context, id string) (*platform.Track, error) {
	r, err := c.lookup(ctx, id, songs)
	if err != nil {
		return nil, err
	}
	return r.track(), nil
}

// LookupAlbum implements platform.AlbumPlatform.
func (c *Client) LookupAlbum(ctx context.Context, id string) (*platform.Album, error) {
	r, err := c.lookup(ctx, id, albums)
	if err != nil {
		return nil, err
	}
	return r.album(), nil
}

// lookup fetches the item of kind e with an ID from ParseURL or
// ParseAlbumURL.
func (c *Client) lookup(ctx context.Context, id string, e entity) (*result, error) {
	storefront, itemID, ok := strings.Cut(id, "/")
	if !ok {
		return nil, fmt.Errorf("invalid apple music id %q", id)
	}

	results, err := c.get(ctx, "/lookup", url.Values{"id": {itemID}, "country": {storefront}, "entity": {e.name}}, e)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, platform.ErrNotFound
	}
	return &results[0], nil
}

// Search implements platform.Platform. The iTunes Search API is missing
// some releases that are on Apple Music, so if it finds no match the search
// is repeated on the Apple Music website.
func (c *Client) Search(ctx context.Context, t *platform.Track) (*platform.Track, error) {
	return search(ctx, c, searchTerm(t.Title, t.Artists), songs, func(results []result) *platform.Track {
		candidates := make([]*platform.Track, len(results))
		for i, r := range results {
			candidates[i] = r.track()
		}
		return platform.BestMatch(t, candidates)
	})
}

// SearchAlbum implements platform.AlbumSearcher. Like Search, it falls back
// to the Apple Music website.
func (c *Client) SearchAlbum(ctx context.Context, a *platform.Album) (*platform.Album, error) {
	return search(ctx, c, searchTerm(a.Title, a.Artists), albums, func(results []result) *platform.Album {
		candidates := make([]*platform.Album, len(results))
		for i, r := range results {
			candidates[i] = r.album()
		}
		return platform.BestAlbumMatch(a, candidates)
	})
}

func searchTerm(title string, artists []string) string {
	if len(artists) > 0 {
		return title + " " + artists[0]
	}
	return title
}

// search finds items of kind e for term and returns the best one according
// to match, or ErrNotFound.
func search[T any](ctx context.Context, c *Client, term string, e entity, match func([]result) *T) (*T, error) {
	results, err := c.get(ctx, "/search", url.Values{
		"term":    {term},
		"country": {searchStorefront},
		"entity":  {e.name},
		"limit":   {"10"},
	}, e)
	if err != nil {
		return nil, err
	}
	if best := match(results); best != nil {
		return best, nil
	}

	// The iTunes API already found no match, so the website is only a
	// best-effort second try and its failures count as not found. Rate
	// limits still apply to the iTunes API as a whole and are reported, and
	// running out of time doesn't mean that nothing exists.
	results, err = c.searchFallback(ctx, term, e)
	if err != nil {
		if _, ok := platform.AsRateLimitError(err); ok || ctx.Err() != nil {
			return nil, err
		}
		log.Printf("Apple Music website search for %q: %v", term, err)
		return nil, platform.ErrNotFound
	}
	if best := match(results); best != nil {
		return best, nil
	}
	return nil, platform.ErrNotFound
}

// searchFallback searches the Apple Music website and looks up the items of
// kind e that it finds, in the website's order.
func (c *Client) searchFallback(ctx context.Context, term string, e entity) ([]result, error) {
	page, err := c.searchWebsite(ctx, term)
	if err != nil {
		return nil, err
	}
	ids := e.webIDs(page)
	if len(ids) == 0 {
		return nil, nil
	}

	results, err := c.get(ctx, "/lookup", url.Values{
		"id":      {strings.Join(ids, ",")},
		"country": {searchStorefront},
		"entity":  {e.name},
	}, e)
	if err != nil {
		return nil, err
	}

	// Keep the website's ranking, which the lookup doesn't preserve.
	rank := make(map[int64]int, len(ids))
	for i, id := range ids {
		n, _ := strconv.ParseInt(id, 10, 64)
		rank[n] = i
	}
	slices.SortStableFunc(results, func(a, b result) int { return rank[e.id(a)] - rank[e.id(b)] })
	return results, nil
}

// trackIDs returns the IDs of the tracks linked on a website search page, in
// order.
func trackIDs(page []byte) []string {
	var ids []string
	for _, m := range webTrackPattern.FindAllSubmatch(page, -1) {
		ids = append(ids, string(m[1])+string(m[2]))
	}
	return firstUnique(ids, maxWebResults)
}

// albumIDs returns the IDs of the albums linked on a website search page, in
// order.
func albumIDs(page []byte) []string {
	var ids []string
	for _, m := range webAlbumPattern.FindAllSubmatch(page, -1) {
		if m[2] == nil {
			ids = append(ids, string(m[1]))
		}
	}
	return firstUnique(ids, maxWebResults)
}

// firstUnique returns the first n distinct elements of s.
func firstUnique(s []string, n int) []string {
	var unique []string
	seen := make(map[string]bool)
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			unique = append(unique, v)
			if len(unique) == n {
				break
			}
		}
	}
	return unique
}

// searchWebsite searches the Apple Music website and returns the results
// page. The website has no official API, so callers rely on the links in its
// HTML.
func (c *Client) searchWebsite(ctx context.Context, term string) ([]byte, error) {
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
	return page, nil
}

// get calls an iTunes endpoint and returns the results of kind e.
func (c *Client) get(ctx context.Context, path string, q url.Values, e entity) ([]result, error) {
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
		if isRateLimited(resp) {
			return nil, &platform.RateLimitError{API: "iTunes"}
		}
		return nil, fmt.Errorf("itunes %s: unexpected status %s", path, resp.Status)
	}

	var body response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding itunes %s: %w", path, err)
	}

	var kept []result
	for _, r := range body.Results {
		if e.is(r) {
			kept = append(kept, r)
		}
	}
	return kept, nil
}

// cleanTrackURL drops tracking parameters (e.g. "uo") but keeps the track
// selector "i". It works for album links too, which have no selector.
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

// isRateLimited reports whether an iTunes Search or Lookup API error
// response means the client is being throttled (roughly 20 requests per
// minute). The API answers 403 without an explanation when it throttles.
func isRateLimited(resp *http.Response) bool {
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden
}
