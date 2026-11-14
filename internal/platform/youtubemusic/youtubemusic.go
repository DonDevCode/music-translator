// Package youtubemusic looks up YouTube Music tracks and albums via the
// YouTube Data API, and searches for albums and the songs in music videos via
// the YouTube Music website's internal API. It also recognizes links to
// YouTube videos that are music.
package youtubemusic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/DonDevCode/music-translator/internal/platform"
)

const defaultBaseURL = "https://www.googleapis.com/youtube/v3"

// musicCategoryID is the ID of the Music video category.
const musicCategoryID = "10"

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
// It implements platform.AlbumSearcher and platform.VideoPlatform.
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

// videoHosts are the hosts of YouTube video links, other than youtu.be.
var videoHosts = map[string]bool{"youtube.com": true, "www.youtube.com": true, "m.youtube.com": true}

// ParseVideoURL implements platform.VideoPlatform. It recognizes
//
//	youtube.com/watch?v={videoID}
//	youtube.com/shorts/{videoID}
//	youtube.com/live/{videoID}
//	youtu.be/{videoID}
//
// also with www. and m., and returns the video ID, which YouTube Music
// shares with YouTube.
func (c *Client) ParseVideoURL(u *url.URL) (string, bool) {
	var id string
	switch {
	case u.Host == "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	case !videoHosts[u.Host]:
		return "", false
	case u.Path == "/watch":
		id = u.Query().Get("v")
	default:
		for _, prefix := range []string{"/shorts/", "/live/"} {
			if rest, ok := strings.CutPrefix(u.Path, prefix); ok {
				id = rest
			}
		}
	}
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
		ID      string  `json:"id"`
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
	CategoryID   string `json:"categoryId"`
}

// Lookup implements platform.Platform.
func (c *Client) Lookup(ctx context.Context, id string) (*platform.Track, error) {
	return c.lookup(ctx, id, false)
}

// LookupVideo implements platform.VideoPlatform. A video is music if YouTube
// Music knows it as a music video or track, if it is on a Topic channel, or
// if its uploader put it in the Music category.
func (c *Client) LookupVideo(ctx context.Context, id string) (*platform.Track, error) {
	return c.lookup(ctx, id, true)
}

// lookup fetches the track in the video with the given ID, which costs 1
// quota unit. If requireMusic is set, it returns platform.ErrNotMusic for
// videos that aren't music.
//
// Only auto-generated tracks have exact details. For other videos the YouTube
// Music website is asked what they are. For a music video, the auto-generated
// track of its song is then looked for and returned instead, costing 1 more
// unit. If the website can't tell, artist and title are guessed from the
// video's title and channel.
func (c *Client) lookup(ctx context.Context, id string, requireMusic bool) (*platform.Track, error) {
	var body videosResponse
	if err := c.get(ctx, "/videos", url.Values{"part": {"snippet"}, "id": {id}}, &body); err != nil {
		return nil, err
	}
	if len(body.Items) == 0 {
		return nil, platform.ErrNotFound
	}
	s := body.Items[0].Snippet
	if t := parseAutoGenerated(s); t != nil {
		t.URL = trackURL(id)
		return t, nil
	}

	// The website only improves on the snippet, so its errors aren't fatal.
	video, err := c.watchInfo(ctx, id)
	if err != nil {
		log.Printf("Fetching YouTube Music details of video %s: %v", id, err)
	}
	if requireMusic && !isMusic(s, video) {
		return nil, platform.ErrNotMusic
	}
	if video == nil || video.track == nil {
		t := parseSnippet(s)
		t.URL = trackURL(id)
		return t, nil
	}

	if video.videoType == musicVideoType {
		t, err := c.audioTrack(ctx, video.track)
		if err == nil {
			return t, nil
		}
		if !errors.Is(err, platform.ErrNotFound) {
			log.Printf("Searching YouTube Music for the track of video %s: %v", id, err)
		}
	}
	video.track.URL = trackURL(id)
	return video.track, nil
}

// isMusic reports whether a video with snippet s is music. video is what
// YouTube Music knows about it, or nil if that is unknown.
func isMusic(s snippet, video *watchVideo) bool {
	if video != nil && (video.videoType == musicVideoType || video.videoType == audioTrackType) {
		return true
	}
	return isTopicChannel(s.ChannelTitle) || s.CategoryID == musicCategoryID
}

// audioTrack finds the auto-generated track of song, the song in a music
// video. It searches the YouTube Music website, which costs no quota unlike
// Search, and looks up the found track for its exact details.
func (c *Client) audioTrack(ctx context.Context, song *platform.Track) (*platform.Track, error) {
	rows, err := c.search(ctx, searchQuery(song.Title, song.Artists), songsFilter)
	if err != nil {
		return nil, err
	}

	var ids []string
	var tracks []*platform.Track
	for _, row := range rows {
		if id, t, ok := parseSongTrack(row); ok {
			ids = append(ids, id)
			tracks = append(tracks, t)
		}
	}
	best := platform.BestMatch(song, tracks)
	if best == nil {
		return nil, platform.ErrNotFound
	}

	found, err := c.videos(ctx, []string{ids[slices.Index(tracks, best)]})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, platform.ErrNotFound
	}
	return found[0], nil
}

// searchQuery returns the query to search for a release: its first artist
// and title.
func searchQuery(title string, artists []string) string {
	if len(artists) > 0 {
		return artists[0] + " " + title
	}
	return title
}

// videos looks up the videos with the given IDs (at most 50) in one request
// and returns them in the same order. Missing videos, e.g. deleted or private
// ones, are left out.
func (c *Client) videos(ctx context.Context, ids []string) ([]*platform.Track, error) {
	var body videosResponse
	if err := c.get(ctx, "/videos", url.Values{"part": {"snippet"}, "id": {strings.Join(ids, ",")}}, &body); err != nil {
		return nil, err
	}

	byID := make(map[string]snippet, len(body.Items))
	for _, item := range body.Items {
		byID[item.ID] = item.Snippet
	}
	var tracks []*platform.Track
	for _, id := range ids {
		if s, ok := byID[id]; ok {
			track := parseSnippet(s)
			track.URL = trackURL(id)
			tracks = append(tracks, track)
		}
	}
	return tracks, nil
}

// LookupAlbum implements platform.AlbumPlatform. Album playlists belong to
// YouTube itself and say nothing about the artist, so the artists and year
// are taken from the tracks (up to the first 50). It costs 3 quota units.
func (c *Client) LookupAlbum(ctx context.Context, id string) (*platform.Album, error) {
	if browseIDPattern.MatchString(id) {
		var err error
		if id, err = c.resolveBrowseID(ctx, id); err != nil {
			return nil, err
		}
	}

	// Both requests only need the playlist ID, so they run concurrently.
	var playlists playlistsResponse
	var items playlistItemsResponse
	var playlistsErr, itemsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		playlistsErr = c.get(ctx, "/playlists", url.Values{"part": {"snippet,contentDetails"}, "id": {id}}, &playlists)
	}()
	go func() {
		defer wg.Done()
		itemsErr = c.get(ctx, "/playlistItems", url.Values{"part": {"snippet"}, "playlistId": {id}, "maxResults": {"50"}}, &items)
	}()
	wg.Wait()
	if playlistsErr != nil {
		return nil, playlistsErr
	}
	if itemsErr != nil {
		return nil, itemsErr
	}
	if len(playlists.Items) == 0 || len(items.Items) == 0 {
		return nil, platform.ErrNotFound
	}

	ids := make([]string, len(items.Items))
	for i, item := range items.Items {
		ids[i] = item.Snippet.ResourceID.VideoID
	}
	tracks, err := c.videos(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, platform.ErrNotFound
	}

	playlist := playlists.Items[0]
	album := &platform.Album{
		Title:      releaseTypePattern.ReplaceAllString(playlist.Snippet.Title, ""),
		Artists:    albumArtists(tracks),
		TrackCount: playlist.ContentDetails.ItemCount,
		URL:        albumURL(id),
	}
	for _, t := range tracks {
		if t.Year != 0 {
			album.Year = t.Year
			break
		}
	}
	return album, nil
}

// albumArtists returns the artists credited on at least half of the tracks,
// in the order they first appear, so that guests on single tracks are left
// out but all artists of a collaboration album are kept. If no artist is on
// half of the tracks, as on compilations, the most frequent one is returned.
func albumArtists(tracks []*platform.Track) []string {
	var order []string
	counts := map[string]int{}
	for _, t := range tracks {
		for i, a := range t.Artists {
			if slices.Contains(t.Artists[:i], a) {
				continue
			}
			if counts[a] == 0 {
				order = append(order, a)
			}
			counts[a]++
		}
	}

	var artists []string
	for _, a := range order {
		if 2*counts[a] >= len(tracks) {
			artists = append(artists, a)
		}
	}
	if len(artists) > 0 {
		return artists
	}

	var top string
	for _, a := range order {
		if counts[a] > counts[top] {
			top = a
		}
	}
	if top == "" {
		return nil
	}
	return []string{top}
}

// SearchAlbum implements platform.AlbumSearcher. The search results lack
// track counts, which are then fetched for all of them at a cost of 1 quota
// unit. The counts only help to tell releases apart, so if they can't be
// fetched (e.g. because the quota is used up) the albums are matched without
// them.
//
// If the album search finds no match, the albums of matching songs are tried,
// since the album search misses some releases.
func (c *Client) SearchAlbum(ctx context.Context, a *platform.Album) (*platform.Album, error) {
	q := searchQuery(a.Title, a.Artists)

	found, err := c.searchAlbums(ctx, q)
	if err != nil {
		return nil, err
	}
	if best := c.bestAlbum(ctx, a, found, q); best != nil {
		return best.album, nil
	}

	found, err = c.searchSongAlbums(ctx, q)
	if err != nil {
		return nil, err
	}
	best := platform.BestAlbumMatch(a, albumsOf(found))
	if best == nil {
		return nil, platform.ErrNotFound
	}
	browseID := found[slices.IndexFunc(found, func(f foundAlbum) bool { return f.album == best })].id
	id, err := c.resolveBrowseID(ctx, browseID)
	if err != nil {
		return nil, err
	}

	// Without its track count the album may be a different release with the
	// same title, e.g. the album named after a single, so it is checked again.
	best.URL = albumURL(id)
	if match := c.bestAlbum(ctx, a, []foundAlbum{{id: id, album: best}}, q); match != nil {
		return match.album, nil
	}
	return nil, platform.ErrNotFound
}

// bestAlbum adds the track counts to the found albums and returns the best
// match for a, or nil. q is only used for logging.
func (c *Client) bestAlbum(ctx context.Context, a *platform.Album, found []foundAlbum, q string) *foundAlbum {
	if len(found) == 0 {
		return nil
	}
	if len(found) > maxAlbumResults {
		found = found[:maxAlbumResults]
	}

	if err := c.addTrackCounts(ctx, found); err != nil {
		log.Printf("Fetching YouTube track counts for %q: %v", q, err)
	}
	best := platform.BestAlbumMatch(a, albumsOf(found))
	if best == nil {
		return nil
	}
	return &found[slices.IndexFunc(found, func(f foundAlbum) bool { return f.album == best })]
}

func albumsOf(found []foundAlbum) []*platform.Album {
	albums := make([]*platform.Album, len(found))
	for i, f := range found {
		albums[i] = f.album
	}
	return albums
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
	var body searchResponse
	err := c.get(ctx, "/search", url.Values{
		"part":            {"snippet"},
		"q":               {searchQuery(t.Title, t.Artists)},
		"type":            {"video"},
		"videoCategoryId": {musicCategoryID},
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

// Error reasons the YouTube Data API uses when the daily quota or a rate
// limit is used up.
var rateLimitReasons = map[string]bool{
	"quotaExceeded":         true,
	"rateLimitExceeded":     true,
	"userRateLimitExceeded": true,
	"dailyLimitExceeded":    true,
}

// isRateLimited reports whether a YouTube Data API error response is
// about quota or rate limits. Other errors share the same status code (e.g.
// 403 for a disabled API), so the reason in the body is checked.
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

func trackURL(videoID string) string {
	return "https://music.youtube.com/watch?v=" + videoID
}

func albumURL(playlistID string) string {
	return "https://music.youtube.com/playlist?list=" + playlistID
}
