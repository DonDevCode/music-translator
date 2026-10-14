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

type snippet struct {
	Title        string `json:"title"`
	ChannelTitle string `json:"channelTitle"`
	Description  string `json:"description"`
}

// Lookup implements platform.Platform.
func (c *Client) Lookup(ctx context.Context, id string) (*platform.Track, error) {
	q := url.Values{"part": {"snippet"}, "id": {id}, "key": {c.apiKey}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/videos?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// The error contains the request URL, which includes the API key.
		if uerr, ok := err.(*url.Error); ok {
			err = uerr.Err
		}
		return nil, fmt.Errorf("youtube videos.list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube videos.list: unexpected status %s", resp.Status)
	}

	var body videosResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding youtube videos.list: %w", err)
	}
	if len(body.Items) == 0 {
		return nil, platform.ErrNotFound
	}

	track := parseSnippet(body.Items[0].Snippet)
	track.URL = "https://music.youtube.com/watch?v=" + id
	return track, nil
}
