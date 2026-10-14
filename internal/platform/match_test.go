package platform

import "testing"

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
