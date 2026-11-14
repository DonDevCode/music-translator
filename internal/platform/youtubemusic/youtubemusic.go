// Package youtubemusic looks up YouTube Music tracks and albums via the
// YouTube Data API, and searches for albums via the YouTube Music website's
// internal API.
package youtubemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/DonDevCode/music-translator/internal/platform"
)

const defaultBaseURL = "https://www.googleapis.com/youtube/v3"

var (
	videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	// YouTube Music albums are playlists with IDs of this form. Other
	// playlists, such as those made by users, have other prefixes.
	albumIDPattern = regexp.MustCompile(`^OLAK5uy_[A-Za-z0-9_-]+$`)
	// Album pages have browse IDs of this form.
	browseIDPattern = regexp.MustCompile(`^MPREb_[A-Za-z0-9_-]+$`)
	// Release type prefixes that some album playlist titles have.
	releaseTypePattern = regexp.MustCompile(`^(Album|EP|Single) - `)
)

// Client is the YouTube Music platform.
//
// It implements platform.AlbumSearcher.
type Client struct {
	http         *http.Client
	apiKey       string
	baseURL      string
	innerTubeURL string
}

// New creates a YouTube Music client using a YouTube Data API v3 key.
func New(httpClient *http.Client, apiKey string) *Client {
	return &Client{http: httpClient, apiKey: apiKey, baseURL: defaultBaseURL, innerTubeURL: defaultInnerTubeURL}
}

// Name implements platform.Platform.
func (c *Client) Name() string { return "YouTube Music" }

// ParseURL implements platform.Platform. It recognizes
// music.youtube.com/watch?v={videoID} and returns the video ID.
func (c *Client) ParseURL(u *url.URL) (string, bool) {
	if u.Host != "music.youtube.com" || u.Path != "/watch" {
		return "", false
	}
	id := u.Query().Get("v")
	if !videoIDPattern.MatchString(id) {
		return "", false
	}
	return id, true
}

// ParseAlbumURL implements platform.AlbumPlatform. It recognizes
//
//	music.youtube.com/playlist?list={albumID}
//	music.youtube.com/browse/{browseID}
//
// and returns the playlist ID or browse ID. YouTube Music shares albums as
// playlist links, but the album pages in the browser have browse links.
func (c *Client) ParseAlbumURL(u *url.URL) (string, bool) {
	if u.Host != "music.youtube.com" {
		return "", false
	}
	var id string
	var ok bool
	if u.Path == "/playlist" {
		id = u.Query().Get("list")
		ok = albumIDPattern.MatchString(id)
	} else if browseID, found := strings.CutPrefix(u.Path, "/browse/"); found {
		id = browseID
		ok = browseIDPattern.MatchString(id)
	}
	if !ok {
		return "", false
	}
	return id, true
}

type videosResponse struct {
	Items []struct {
		Snippet snippet `json:"snippet"`
	} `json:"items"`
}

type searchResponse struct {
	Items []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`
		Snippet snippet `json:"snippet"`
	} `json:"items"`
}

type playlistsResponse struct {
	Items []struct {
		ID             string  `json:"id"`
		Snippet        snippet `json:"snippet"`
		ContentDetails struct {
			ItemCount int `json:"itemCount"`
		} `json:"contentDetails"`
	} `json:"items"`
}

type playlistItemsResponse struct {
	Items []struct {
		Snippet struct {
			ResourceID struct {
				VideoID string `json:"videoId"`
			} `json:"resourceId"`
		} `json:"snippet"`
	} `json:"items"`
}

type snippet struct {
	Title        string `json:"title"`
	ChannelTitle string `json:"channelTitle"`
	Description  string `json:"description"`
}

// Lookup implements platform.Platform.
func (c *Client) Lookup(ctx context.Context, id string) (*platform.Track, error) {
	var body videosResponse
	if err := c.get(ctx, "/videos", url.Values{"part": {"snippet"}, "id": {id}}, &body); err != nil {
		return nil, err
	}
	if len(body.Items) == 0 {
		return nil, platform.ErrNotFound
	}

	track := parseSnippet(body.Items[0].Snippet)
	track.URL = trackURL(id)
	return track, nil
}

// LookupAlbum implements platform.AlbumPlatform. Album playlists belong to
// YouTube itself and say nothing about the artist, so the artists and year
// are taken from the first track. It costs 3 quota units.
func (c *Client) LookupAlbum(ctx context.Context, id string) (*platform.Album, error) {
	if browseIDPattern.MatchString(id) {
		var err error
		if id, err = c.resolveBrowseID(ctx, id); err != nil {
			return nil, err
		}
	}

	var playlists playlistsResponse
	err := c.get(ctx, "/playlists", url.Values{"part": {"snippet,contentDetails"}, "id": {id}}, &playlists)
	if err != nil {
		return nil, err
	}
	if len(playlists.Items) == 0 {
		return nil, platform.ErrNotFound
	}

	var items playlistItemsResponse
	err = c.get(ctx, "/playlistItems", url.Values{"part": {"snippet"}, "playlistId": {id}, "maxResults": {"1"}}, &items)
	if err != nil {
		return nil, err
	}
	if len(items.Items) == 0 {
		return nil, platform.ErrNotFound
	}
	first, err := c.Lookup(ctx, items.Items[0].Snippet.ResourceID.VideoID)
	if err != nil {
		return nil, err
	}

	playlist := playlists.Items[0]
	return &platform.Album{
		Title:      releaseTypePattern.ReplaceAllString(playlist.Snippet.Title, ""),
		Artists:    first.Artists,
		Year:       first.Year,
		TrackCount: playlist.ContentDetails.ItemCount,
		URL:        albumURL(id),
	}, nil
}

// SearchAlbum implements platform.AlbumSearcher. The search results lack
// track counts, which are then fetched for all of them at a cost of 1 quota
// unit.
func (c *Client) SearchAlbum(ctx context.Context, a *platform.Album) (*platform.Album, error) {
	q := a.Title
	if len(a.Artists) > 0 {
		q = a.Artists[0] + " " + q
	}

	found, err := c.searchAlbums(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, platform.ErrNotFound
	}
	if len(found) > maxAlbumResults {
		found = found[:maxAlbumResults]
	}

	if err := c.addTrackCounts(ctx, found); err != nil {
		return nil, err
	}
	candidates := make([]*platform.Album, len(found))
	for i, f := range found {
		candidates[i] = f.album
	}
	if best := platform.BestAlbumMatch(a, candidates); best != nil {
		return best, nil
	}
	return nil, platform.ErrNotFound
}

// maxAlbumResults limits how many album search results are considered. The
// Data API accepts up to 50 playlist IDs per request.
const maxAlbumResults = 20

// addTrackCounts sets the track counts of the albums.
func (c *Client) addTrackCounts(ctx context.Context, albums []foundAlbum) error {
	ids := make([]string, len(albums))
	for i, a := range albums {
		ids[i] = a.id
	}

	var body playlistsResponse
	err := c.get(ctx, "/playlists", url.Values{
		"part":       {"contentDetails"},
		"id":         {strings.Join(ids, ",")},
		"maxResults": {"50"},
	}, &body)
	if err != nil {
		return err
	}

	counts := make(map[string]int, len(body.Items))
	for _, item := range body.Items {
		counts[item.ID] = item.ContentDetails.ItemCount
	}
	for _, a := range albums {
		a.album.TrackCount = counts[a.id]
	}
	return nil
}

// Search implements platform.Platform. A search costs 100 of the API's
// 10,000 daily quota units, compared to 1 for a lookup.
func (c *Client) Search(ctx context.Context, t *platform.Track) (*platform.Track, error) {
	q := t.Title
	if len(t.Artists) > 0 {
		q = t.Artists[0] + " " + q
	}

	var body searchResponse
	err := c.get(ctx, "/search", url.Values{
		"part":            {"snippet"},
		"q":               {q},
		"type":            {"video"},
		"videoCategoryId": {"10"}, // Music
		"maxResults":      {"10"},
	}, &body)
	if err != nil {
		return nil, err
	}

	// Auto-generated tracks are what YouTube Music itself plays, so they go
	// first and win ties against music videos and uploads.
	var topic, other []*platform.Track
	for _, item := range body.Items {
		track := parseSearchResult(item.Snippet)
		track.URL = trackURL(item.ID.VideoID)
		if isTopicChannel(item.Snippet.ChannelTitle) {
			topic = append(topic, track)
		} else {
			other = append(other, track)
		}
	}

	if best := platform.BestMatch(t, append(topic, other...)); best != nil {
		return best, nil
	}
	return nil, platform.ErrNotFound
}

// get calls a YouTube Data API endpoint and decodes the response into out.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	q.Set("key", c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// The error contains the request URL, which includes the API key.
		if uerr, ok := err.(*url.Error); ok {
			err = uerr.Err
		}
		return fmt.Errorf("youtube %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if platform.IsYouTubeRateLimited(resp) {
			return &platform.RateLimitError{API: "YouTube"}
		}
		return fmt.Errorf("youtube %s: unexpected status %s", path, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding youtube %s: %w", path, err)
	}
	return nil
}

func trackURL(videoID string) string {
	return "https://music.youtube.com/watch?v=" + videoID
}

func albumURL(playlistID string) string {
	return "https://music.youtube.com/playlist?list=" + playlistID
}
