package bot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
				{apple, &platform.Track{URL: "https://music.apple.com/x", Year: 1982}, nil},
			},
			want: "**Under Pressure** – Queen, David Bowie · 1981\nApple Music: https://music.apple.com/x",
		},
		{
			name:   "year from other platform",
			source: &platform.Track{Title: "Song", Artists: []string{"Artist"}},
			results: []result{
				{spotify, nil, nil},
				{apple, &platform.Track{URL: "https://music.apple.com/x", Year: 2001}, nil},
			},
			want: "**Song** – Artist · 2001\nNot found on Spotify\nApple Music: https://music.apple.com/x",
		},
		{
			name:    "markdown in title",
			source:  &platform.Track{Title: "*NSYNC_Song", Artists: []string{"A|B"}, Year: 2000},
			results: []result{{apple, nil, nil}},
			want:    `**\*NSYNC\_Song** – A\|B · 2000` + "\nNot found on Apple Music",
		},
		{
			name:   "rate limited",
			source: &platform.Track{Title: "Song", Artists: []string{"Artist"}, Year: 2000},
			results: []result{
				{youtube, nil, fmt.Errorf("searching: %w", &platform.RateLimitError{API: "YouTube"})},
				{spotify, nil, errors.New("connection refused")},
			},
			want: "**Song** – Artist · 2000\nYouTube API Rate Limit reached\nNot found on Spotify",
		},
		{
			name:    "unknown year",
			source:  &platform.Track{Title: "Song", Artists: []string{"Artist"}},
			results: []result{{apple, nil, nil}},
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
