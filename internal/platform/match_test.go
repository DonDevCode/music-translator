package platform

import (
	"fmt"
	"testing"
)

func TestBestMatch(t *testing.T) {
	track := func(title string, artists ...string) *Track {
		return &Track{Title: title, Artists: artists, URL: title + "|" + artists[0]}
	}

	tests := []struct {
		name       string
		want       *Track
		candidates []*Track
		wantURL    string // "" for no match
	}{
		{
			name:       "exact",
			want:       track("Bohemian Rhapsody", "Queen"),
			candidates: []*Track{track("Other", "Queen"), track("Bohemian Rhapsody", "Queen")},
			wantURL:    "Bohemian Rhapsody|Queen",
		},
		{
			name:       "case and punctuation",
			want:       track("Don't Stop Me Now", "Queen"),
			candidates: []*Track{track("DONT STOP ME NOW", "QUEEN")},
			wantURL:    "DONT STOP ME NOW|QUEEN",
		},
		{
			name:       "prefers matching version",
			want:       track("Bohemian Rhapsody (Live Aid)", "Queen"),
			candidates: []*Track{track("Bohemian Rhapsody", "Queen"), track("Bohemian Rhapsody (Live Aid)", "Queen")},
			wantURL:    "Bohemian Rhapsody (Live Aid)|Queen",
		},
		{
			name:       "falls back to other version",
			want:       track("Bohemian Rhapsody - Remastered 2011", "Queen"),
			candidates: []*Track{track("Bohemian Rhapsody", "Queen")},
			wantURL:    "Bohemian Rhapsody|Queen",
		},
		{
			name:       "earliest wins ties",
			want:       track("Song", "Artist"),
			candidates: []*Track{{Title: "Song", Artists: []string{"Artist"}, URL: "first"}, {Title: "Song", Artists: []string{"Artist"}, URL: "second"}},
			wantURL:    "first",
		},
		{
			name:       "joined artists",
			want:       track("Under Pressure", "Queen", "David Bowie"),
			candidates: []*Track{track("Under Pressure", "Queen & David Bowie")},
			wantURL:    "Under Pressure|Queen & David Bowie",
		},
		{
			name:       "artist spacing from channel names",
			want:       track("Blinding Lights", "TheWeeknd"),
			candidates: []*Track{track("Blinding Lights", "The Weeknd")},
			wantURL:    "Blinding Lights|The Weeknd",
		},
		{
			name:       "cover by other artist",
			want:       track("Bohemian Rhapsody", "Queen"),
			candidates: []*Track{track("Bohemian Rhapsody", "Panic! At The Disco")},
		},
		{
			name:       "other song by same artist",
			want:       track("Bohemian Rhapsody", "Queen"),
			candidates: []*Track{track("Radio Ga Ga", "Queen")},
		},
		{
			name:       "title with extra words",
			want:       track("Song", "Artist"),
			candidates: []*Track{track("Song Remastered 2011", "Artist")},
			wantURL:    "Song Remastered 2011|Artist",
		},
		{
			name:       "title contained in other song",
			want:       track("Love", "Queen"),
			candidates: []*Track{track("Somebody to Love", "Queen")},
		},
		{
			name:       "title is prefix of other song",
			want:       track("Love", "The Beatles"),
			candidates: []*Track{track("Love Me Do", "The Beatles")},
		},
		{
			name:       "title and artist only share a prefix",
			want:       track("Intro", "Queen"),
			candidates: []*Track{track("Introduction", "Queensrÿche")},
		},
		{
			name:       "featured artist is a prefix of another name",
			want:       track("Song", "Main", "Queen"),
			candidates: []*Track{track("Song", "Other & Queensrÿche")},
		},
		{
			name: "no candidates",
			want: track("Song", "Artist"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BestMatch(tt.want, tt.candidates)
			gotURL := ""
			if got != nil {
				gotURL = got.URL
			}
			if gotURL != tt.wantURL {
				t.Errorf("BestMatch = %q, want %q", gotURL, tt.wantURL)
			}
		})
	}
}

func TestBestAlbumMatch(t *testing.T) {
	album := func(title, artist string, tracks int) *Album {
		return &Album{Title: title, Artists: []string{artist}, TrackCount: tracks, URL: fmt.Sprint(title, "|", tracks)}
	}

	tests := []struct {
		name       string
		want       *Album
		candidates []*Album
		wantURL    string // "" for no match
	}{
		{
			name:       "exact",
			want:       album("Abbey Road", "The Beatles", 17),
			candidates: []*Album{album("Help!", "The Beatles", 14), album("Abbey Road", "The Beatles", 17)},
			wantURL:    "Abbey Road|17",
		},
		{
			name:       "prefers same track count",
			want:       album("Abbey Road", "The Beatles", 17),
			candidates: []*Album{album("Abbey Road", "The Beatles", 19), album("Abbey Road", "The Beatles", 17)},
			wantURL:    "Abbey Road|17",
		},
		{
			name:       "allows bonus tracks",
			want:       album("Abbey Road", "The Beatles", 17),
			candidates: []*Album{album("Abbey Road", "The Beatles", 19)},
			wantURL:    "Abbey Road|19",
		},
		{
			name:       "rejects deluxe edition with many more tracks",
			want:       album("Abbey Road", "The Beatles", 17),
			candidates: []*Album{album("Abbey Road", "The Beatles", 40)},
		},
		{
			name:       "single is not the album",
			want:       album("Let It Be", "The Beatles", 1),
			candidates: []*Album{album("Let It Be", "The Beatles", 12), album("Let It Be", "The Beatles", 2)},
			wantURL:    "Let It Be|2",
		},
		{
			name:       "title match beats track count",
			want:       album("Abbey Road (Remastered)", "The Beatles", 17),
			candidates: []*Album{album("Abbey Road", "The Beatles", 17), album("Abbey Road (Remastered)", "The Beatles", 19)},
			wantURL:    "Abbey Road (Remastered)|19",
		},
		{
			name:       "unknown track count",
			want:       album("Abbey Road", "The Beatles", 0),
			candidates: []*Album{album("Abbey Road", "The Beatles", 40)},
			wantURL:    "Abbey Road|40",
		},
		{
			name:       "other artist",
			want:       album("Abbey Road", "The Beatles", 17),
			candidates: []*Album{album("Abbey Road", "George Benson", 17)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BestAlbumMatch(tt.want, tt.candidates)
			gotURL := ""
			if got != nil {
				gotURL = got.URL
			}
			if gotURL != tt.wantURL {
				t.Errorf("BestAlbumMatch = %q, want %q", gotURL, tt.wantURL)
			}
		})
	}
}
