package applemusic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/DonDevCode/music-translator/internal/platform"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		url    string
		wantID string
		wantOK bool
	}{
		{"https://music.apple.com/us/album/bohemian-rhapsody/6781024026?i=6781024437", "us/6781024437", true},
		{"https://music.apple.com/de/album/bohemian-rhapsody/6781024026?i=6781024437&uo=4", "de/6781024437", true},
		{"https://music.apple.com/us/album/6781024026?i=6781024437", "us/6781024437", true},
		{"https://music.apple.com/us/song/bohemian-rhapsody/6781024437", "us/6781024437", true},
		{"https://music.apple.com/us/song/6781024437", "us/6781024437", true},
		{"https://geo.music.apple.com/us/song/bohemian-rhapsody/6781024437", "us/6781024437", true},
		{"https://music.apple.com/us/album/a-night-at-the-opera/6781024026", "", false},
		{"https://music.apple.com/us/artist/queen/3296287", "", false},
		{"https://music.apple.com/us/playlist/todays-hits/pl.f4d106fed2bd41149aaacabb233eb5eb", "", false},
		{"https://music.apple.com/us/song/bohemian-rhapsody", "", false},
		{"https://music.apple.com/browse", "", false},
		{"https://example.com/us/song/x/6781024437", "", false},
	}

	c := New(http.DefaultClient)
	for _, tt := range tests {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Fatal(err)
		}
		id, ok := c.ParseURL(u)
		if id != tt.wantID || ok != tt.wantOK {
			t.Errorf("ParseURL(%q) = %q, %v; want %q, %v", tt.url, id, ok, tt.wantID, tt.wantOK)
		}
	}
}

// Trimmed real response for https://itunes.apple.com/lookup?id=6781024437&country=us.
const lookupFixture = `{
 "resultCount":1,
 "results": [
{"wrapperType":"track", "kind":"song", "artistId":3296287, "collectionId":6781024026, "trackId":6781024437, "artistName":"Queen", "collectionName":"A Night At The Opera (Deluxe Edition)", "trackName":"Bohemian Rhapsody", "trackViewUrl":"https://music.apple.com/us/album/bohemian-rhapsody/6781024026?i=6781024437&uo=4", "releaseDate":"1975-10-31T12:00:00Z", "country":"USA"}]
}`

func TestLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/lookup" || q.Get("country") != "us" {
			t.Errorf("unexpected request %s", r.URL)
		}
		if q.Get("id") == "6781024437" {
			w.Write([]byte(lookupFixture))
		} else {
			w.Write([]byte(`{"resultCount":0,"results":[]}`))
		}
	}))
	defer srv.Close()

	c := New(srv.Client())
	c.baseURL = srv.URL

	got, err := c.Lookup(context.Background(), "us/6781024437")
	if err != nil {
		t.Fatal(err)
	}
	want := &platform.Track{
		Title:   "Bohemian Rhapsody",
		Artists: []string{"Queen"},
		Year:    1975,
		URL:     "https://music.apple.com/us/album/bohemian-rhapsody/6781024026?i=6781024437",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lookup = %+v, want %+v", got, want)
	}

	if _, err := c.Lookup(context.Background(), "us/1"); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("Lookup of unknown track: err = %v, want ErrNotFound", err)
	}
}
