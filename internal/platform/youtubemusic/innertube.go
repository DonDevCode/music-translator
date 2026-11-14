package youtubemusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/DonDevCode/music-translator/internal/platform"
)

// The YouTube Data API can't search for albums or resolve album page links,
// so these use InnerTube, the internal API of the YouTube Music website. It
// needs no key and costs no quota, but it is undocumented and may change.
const (
	defaultInnerTubeURL = "https://music.youtube.com/youtubei/v1"
	// innerTubeClientVersion is a version of the YouTube Music web client.
	// Very old versions may eventually be rejected.
	innerTubeClientVersion = "1.20241106.01.00"
	// albumsFilter limits a search to albums, EPs and singles, like the
	// "Albums" chip on the website.
	albumsFilter = "EgWKAQIYAWoKEAkQBRAKEAMQBA%3D%3D"
	// songsFilter limits a search to songs, like the "Songs" chip.
	songsFilter = "EgWKAQIIAWoKEAkQBRAKEAMQBA%3D%3D"
)

// The language and country of the results. Album types ("Album", "Single")
// are only recognized in English.
var innerTubeContext = map[string]any{
	"client": map[string]string{
		"clientName":    "WEB_REMIX",
		"clientVersion": innerTubeClientVersion,
		"hl":            "en",
		"gl":            "US",
	},
}

type innerTubeSearchResponse struct {
	Contents struct {
		TabbedSearchResultsRenderer struct {
			Tabs []struct {
				TabRenderer struct {
					Content struct {
						SectionListRenderer struct {
							Contents []struct {
								MusicShelfRenderer struct {
									Contents []struct {
										Item listItem `json:"musicResponsiveListItemRenderer"`
									} `json:"contents"`
								} `json:"musicShelfRenderer"`
							} `json:"contents"`
						} `json:"sectionListRenderer"`
					} `json:"content"`
				} `json:"tabRenderer"`
			} `json:"tabs"`
		} `json:"tabbedSearchResultsRenderer"`
	} `json:"contents"`
}

// listItem is a search result row: the title in the first column and
// details in the second, "Album • Artist • 1969" for albums and
// "Artist • Album • 2:51" for songs.
type listItem struct {
	FlexColumns []struct {
		Column struct {
			Text struct {
				Runs []struct {
					Text     string `json:"text"`
					Endpoint struct {
						Browse struct {
							BrowseID string `json:"browseId"`
						} `json:"browseEndpoint"`
					} `json:"navigationEndpoint"`
				} `json:"runs"`
			} `json:"text"`
		} `json:"musicResponsiveListItemFlexColumnRenderer"`
	} `json:"flexColumns"`
	Overlay struct {
		Renderer struct {
			Content struct {
				PlayButton struct {
					Endpoint struct {
						WatchPlaylist struct {
							PlaylistID string `json:"playlistId"`
						} `json:"watchPlaylistEndpoint"`
					} `json:"playNavigationEndpoint"`
				} `json:"musicPlayButtonRenderer"`
			} `json:"content"`
		} `json:"musicItemThumbnailOverlayRenderer"`
	} `json:"overlay"`
}

type innerTubeBrowseResponse struct {
	Microformat struct {
		Renderer struct {
			URLCanonical string `json:"urlCanonical"`
		} `json:"microformatDataRenderer"`
	} `json:"microformat"`
}

// artistSeparatorPattern separates the artists in a search result row.
var artistSeparatorPattern = regexp.MustCompile(`, | & `)

// albumTypes are the release types shown in search results.
var albumTypes = map[string]bool{"Album": true, "EP": true, "Single": true}

// foundAlbum is an album search result with its playlist ID, or with its
// browse ID if it was found through one of its songs.
type foundAlbum struct {
	id    string
	album *platform.Album
}

// searchAlbums returns the albums that the website finds for query, in
// order. Their track counts are unknown.
func (c *Client) searchAlbums(ctx context.Context, query string) ([]foundAlbum, error) {
	rows, err := c.search(ctx, query, albumsFilter)
	if err != nil {
		return nil, err
	}

	var albums []foundAlbum
	for _, row := range rows {
		if a, ok := parseListItem(row); ok {
			albums = append(albums, a)
		}
	}
	return albums, nil
}

// searchSongAlbums returns the albums of the songs that the website finds
// for query, in order and without duplicates. The album search misses some
// releases, such as new singles, whose songs can already be found. The
// albums have browse IDs instead of playlist IDs and no URL, year or track
// count.
func (c *Client) searchSongAlbums(ctx context.Context, query string) ([]foundAlbum, error) {
	rows, err := c.search(ctx, query, songsFilter)
	if err != nil {
		return nil, err
	}

	var albums []foundAlbum
	seen := map[string]bool{}
	for _, row := range rows {
		if a, ok := parseSongItem(row); ok && !seen[a.id] {
			seen[a.id] = true
			albums = append(albums, a)
		}
	}
	return albums, nil
}

// search returns the result rows that the website finds for query with a
// search filter.
func (c *Client) search(ctx context.Context, query, filter string) ([]listItem, error) {
	var body innerTubeSearchResponse
	if err := c.innerTube(ctx, "/search", map[string]any{"query": query, "params": filter}, &body); err != nil {
		return nil, err
	}

	var rows []listItem
	for _, tab := range body.Contents.TabbedSearchResultsRenderer.Tabs {
		for _, section := range tab.TabRenderer.Content.SectionListRenderer.Contents {
			for _, row := range section.MusicShelfRenderer.Contents {
				rows = append(rows, row.Item)
			}
		}
	}
	return rows, nil
}

// parseListItem returns the album in a search result row, or false if the
// row isn't an album.
func parseListItem(item listItem) (foundAlbum, bool) {
	id := item.Overlay.Renderer.Content.PlayButton.Endpoint.WatchPlaylist.PlaylistID
	if !albumIDPattern.MatchString(id) || len(item.FlexColumns) < 2 {
		return foundAlbum{}, false
	}

	var title strings.Builder
	for _, run := range item.FlexColumns[0].Column.Text.Runs {
		title.WriteString(run.Text)
	}

	// Multiple artists are separate runs, joined by ", " and " & " runs, so
	// the runs are joined before splitting at the " • " separators. The
	// artists are then split again, because other platforms may list them in
	// another order.
	var details strings.Builder
	for _, run := range item.FlexColumns[1].Column.Text.Runs {
		details.WriteString(run.Text)
	}
	parts := strings.Split(details.String(), " • ")
	if len(parts) < 2 || !albumTypes[parts[0]] {
		return foundAlbum{}, false
	}

	a := &platform.Album{Title: title.String(), Artists: artistSeparatorPattern.Split(parts[1], -1), URL: albumURL(id)}
	if len(parts) > 2 {
		a.Year = platform.ParseYear(parts[len(parts)-1])
	}
	return foundAlbum{id: id, album: a}, true
}

// parseSongItem returns the album of the song in a search result row, or
// false if the row has no album. The album is the run that links to an
// album page, and the artists are the part before the first " • ".
func parseSongItem(item listItem) (foundAlbum, bool) {
	if len(item.FlexColumns) < 2 {
		return foundAlbum{}, false
	}

	var details strings.Builder
	var a foundAlbum
	for _, run := range item.FlexColumns[1].Column.Text.Runs {
		details.WriteString(run.Text)
		if id := run.Endpoint.Browse.BrowseID; browseIDPattern.MatchString(id) {
			a = foundAlbum{id: id, album: &platform.Album{Title: run.Text}}
		}
	}
	artists, _, found := strings.Cut(details.String(), " • ")
	if a.album == nil || !found {
		return foundAlbum{}, false
	}
	a.album.Artists = artistSeparatorPattern.Split(artists, -1)
	return a, true
}

// resolveBrowseID returns the playlist ID of the album page with the given
// browse ID (MPREb_…).
func (c *Client) resolveBrowseID(ctx context.Context, browseID string) (string, error) {
	var body innerTubeBrowseResponse
	if err := c.innerTube(ctx, "/browse", map[string]any{"browseId": browseID}, &body); err != nil {
		return "", err
	}

	u, err := url.Parse(body.Microformat.Renderer.URLCanonical)
	if err != nil {
		return "", fmt.Errorf("youtube music browse %s: %w", browseID, err)
	}
	id := u.Query().Get("list")
	if !albumIDPattern.MatchString(id) {
		return "", platform.ErrNotFound
	}
	return id, nil
}

// innerTube calls an InnerTube endpoint with the request fields in req and
// decodes the response into out.
func (c *Client) innerTube(ctx context.Context, path string, req map[string]any, out any) error {
	req["context"] = innerTubeContext
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.innerTubeURL+path+"?prettyPrint=false", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("youtube music %s: %w", path, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return &platform.RateLimitError{API: "YouTube Music"}
	case http.StatusNotFound:
		return platform.ErrNotFound
	default:
		return fmt.Errorf("youtube music %s: unexpected status %s", path, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding youtube music %s: %w", path, err)
	}
	return nil
}
