package bot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"github.com/DonDevCode/music-translator/internal/platform"
)

type namedPlatform string

func (p namedPlatform) Name() string                     { return string(p) }
func (p namedPlatform) ParseURL(*url.URL) (string, bool) { return "", false }
func (p namedPlatform) Lookup(context.Context, string) (*platform.Track, error) {
	return nil, platform.ErrNotFound
}
func (p namedPlatform) Search(context.Context, *platform.Track) (*platform.Track, error) {
	return nil, platform.ErrNotFound
}

func TestFormatReply(t *testing.T) {
	apple := namedPlatform("Apple Music")
	spotify := namedPlatform("Spotify")
	youtube := namedPlatform("YouTube Music")

	tests := []struct {
		name    string
		source  *platform.Track
		results []result
		want    string
	}{
		{
			name:   "found",
			source: &platform.Track{Title: "Under Pressure", Artists: []string{"Queen", "David Bowie"}, Year: 1981},
			results: []result{
				{apple, "https://music.apple.com/x", 1982, nil},
			},
			want: "**Under Pressure** – Queen, David Bowie · 1981\nApple Music: https://music.apple.com/x",
		},
		{
			name:   "year from other platform",
			source: &platform.Track{Title: "Song", Artists: []string{"Artist"}},
			results: []result{
				{spotify, "", 0, nil},
				{apple, "https://music.apple.com/x", 2001, nil},
			},
			want: "**Song** – Artist · 2001\nNot found on Spotify\nApple Music: https://music.apple.com/x",
		},
		{
			name:    "markdown in title",
			source:  &platform.Track{Title: "*NSYNC_Song", Artists: []string{"A|B"}, Year: 2000},
			results: []result{{apple, "", 0, nil}},
			want:    `**\*NSYNC\_Song** – A\|B · 2000` + "\nNot found on Apple Music",
		},
		{
			name:    "masked link in artist",
			source:  &platform.Track{Title: "Song", Artists: []string{"[Click here](https://evil.example)"}, Year: 2000},
			results: []result{{apple, "", 0, nil}},
			want:    `**Song** – \[Click here\](https://evil.example) · 2000` + "\nNot found on Apple Music",
		},
		{
			name:   "rate limited",
			source: &platform.Track{Title: "Song", Artists: []string{"Artist"}, Year: 2000},
			results: []result{
				{youtube, "", 0, fmt.Errorf("searching: %w", &platform.RateLimitError{API: "YouTube"})},
				{spotify, "", 0, errors.New("connection refused")},
			},
			want: "**Song** – Artist · 2000\nYouTube API Rate Limit reached\nCouldn't search Spotify",
		},
		{
			name:    "unknown year",
			source:  &platform.Track{Title: "Song", Artists: []string{"Artist"}},
			results: []result{{apple, "", 0, nil}},
			want:    "**Song** – Artist\nNot found on Apple Music",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatReply(tt.source, tt.results); got != tt.want {
				t.Errorf("formatReply = %q, want %q", got, tt.want)
			}
		})
	}
}

// albumLinkPlatform is a namedPlatform that recognizes album links but
// can't search for albums.
type albumLinkPlatform struct{ namedPlatform }

func (p albumLinkPlatform) ParseAlbumURL(*url.URL) (string, bool) { return "", false }
func (p albumLinkPlatform) LookupAlbum(context.Context, string) (*platform.Album, error) {
	return nil, platform.ErrNotFound
}

// albumPlatform is a namedPlatform with full album support.
type albumPlatform struct{ albumLinkPlatform }

func (p albumPlatform) SearchAlbum(context.Context, *platform.Album) (*platform.Album, error) {
	return nil, platform.ErrNotFound
}

func TestFormatAlbumReply(t *testing.T) {
	apple := namedPlatform("Apple Music")

	tests := []struct {
		name    string
		source  *platform.Album
		results []result
		want    string
	}{
		{
			name:    "album",
			source:  &platform.Album{Title: "A Night At The Opera", Artists: []string{"Queen"}, Year: 1975, TrackCount: 12},
			results: []result{{apple, "https://music.apple.com/x", 0, nil}},
			want:    "**A Night At The Opera** – Queen · 1975 · 12 tracks\nApple Music: https://music.apple.com/x",
		},
		{
			name:    "single with year from other platform",
			source:  &platform.Album{Title: "Song", Artists: []string{"Artist"}, TrackCount: 1},
			results: []result{{apple, "https://music.apple.com/x", 2001, nil}},
			want:    "**Song** – Artist · 2001 · 1 track\nApple Music: https://music.apple.com/x",
		},
		{
			name:    "unknown track count",
			source:  &platform.Album{Title: "Album", Artists: []string{"Artist"}, Year: 2000},
			results: []result{{apple, "", 0, nil}},
			want:    "**Album** – Artist · 2000\nNot found on Apple Music",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatAlbumReply(tt.source, tt.results); got != tt.want {
				t.Errorf("formatAlbumReply = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTargets(t *testing.T) {
	apple := albumPlatform{albumLinkPlatform{"Apple Music"}}
	spotify := albumPlatform{albumLinkPlatform{"Spotify"}}
	youtube := albumLinkPlatform{"YouTube Music"}
	b := &Bot{platforms: []platform.Platform{apple, youtube, spotify}}

	tests := []struct {
		name string
		link platform.Link
		want []platform.Platform
	}{
		{"track", platform.Link{Platform: apple, Kind: platform.KindTrack}, []platform.Platform{youtube, spotify}},
		{"album", platform.Link{Platform: apple, Kind: platform.KindAlbum}, []platform.Platform{spotify}},
		{"album from platform that can't search albums", platform.Link{Platform: youtube, Kind: platform.KindAlbum}, []platform.Platform{apple, spotify}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.targets(tt.link); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("targets = %v, want %v", got, tt.want)
			}
		})
	}

	b.platforms = []platform.Platform{apple, youtube}
	if got := b.targets(platform.Link{Platform: apple, Kind: platform.KindAlbum}); len(got) != 0 {
		t.Errorf("targets of album with only YouTube Music to search = %v, want none", got)
	}
}

// videoPlatform is a VideoPlatform whose videos all contain track, or aren't
// music if track is nil.
type videoPlatform struct {
	namedPlatform
	track *platform.Track
}

func (p videoPlatform) ParseVideoURL(*url.URL) (string, bool) { return "", false }
func (p videoPlatform) LookupVideo(context.Context, string) (*platform.Track, error) {
	if p.track == nil {
		return nil, platform.ErrNotMusic
	}
	return p.track, nil
}

// findingPlatform is a namedPlatform that finds every track at url.
type findingPlatform struct {
	namedPlatform
	url string
}

func (p findingPlatform) Search(_ context.Context, t *platform.Track) (*platform.Track, error) {
	return &platform.Track{Title: t.Title, Artists: t.Artists, URL: p.url}, nil
}

func TestTranslateVideo(t *testing.T) {
	apple := findingPlatform{"Apple Music", "https://music.apple.com/song/1"}
	youtube := videoPlatform{"YouTube Music", &platform.Track{
		Title:   "Song",
		Artists: []string{"Artist"},
		Year:    2001,
		URL:     "https://music.youtube.com/watch?v=x",
	}}
	b := &Bot{platforms: []platform.Platform{apple, youtube}}
	link := platform.Link{Platform: youtube, Kind: platform.KindVideo, ID: "x"}

	got, err := b.translate(link)
	if err != nil {
		t.Fatal(err)
	}
	want := "**Song** – Artist · 2001\nYouTube Music: https://music.youtube.com/watch?v=x\nApple Music: https://music.apple.com/song/1"
	if got != want {
		t.Errorf("translate = %q, want %q", got, want)
	}

	b.platforms = []platform.Platform{namedPlatform("Apple Music"), youtube}
	if _, err := b.translate(link); !errors.Is(err, errNoMatch) {
		t.Errorf("translate of video found nowhere else: err = %v, want errNoMatch", err)
	}

	notMusic := videoPlatform{"YouTube Music", nil}
	b.platforms = []platform.Platform{apple, notMusic}
	if _, err := b.translate(platform.Link{Platform: notMusic, Kind: platform.KindVideo, ID: "x"}); !errors.Is(err, platform.ErrNotMusic) {
		t.Errorf("translate of video that isn't music: err = %v, want ErrNotMusic", err)
	}
}
