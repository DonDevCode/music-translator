// Package youtubemusic looks up YouTube Music tracks via the YouTube Data API.
package youtubemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/DonDevCode/music-translator/internal/platform"
)

const defaultBaseURL = "https://www.googleapis.com/youtube/v3"

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Client is the YouTube Music platform.
type Client struct {
	http    *http.Client
	apiKey  string
	baseURL string
}

// New creates a YouTube Music client using a YouTube Data API v3 key.
func New(httpClient *http.Client, apiKey string) *Client {
	return &Client{http: httpClient, apiKey: apiKey, baseURL: defaultBaseURL}
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
		if isRateLimited(resp) {
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

// Error reasons the API uses when the daily quota or a rate limit is used up.
var rateLimitReasons = map[string]bool{
	"quotaExceeded":         true,
	"rateLimitExceeded":     true,
	"userRateLimitExceeded": true,
	"dailyLimitExceeded":    true,
}

// isRateLimited reports whether an error response is about quota or rate
// limits. Other errors share the same status code (e.g. 403 for a disabled
// API), so the reason in the body is checked.
func isRateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}

	var body struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	for _, e := range body.Error.Errors {
		if rateLimitReasons[e.Reason] {
			return true
		}
	}
	return false
}
