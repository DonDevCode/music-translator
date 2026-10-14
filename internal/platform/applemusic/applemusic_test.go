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

// Trimmed real response for /search?term=Bohemian+Rhapsody+Queen&entity=song.
const searchFixture = `{
 "resultCount":3,
 "results": [
{"wrapperType":"track", "kind":"song", "artistName":"Queen", "trackName":"Bohemian Rhapsody", "trackViewUrl":"https://music.apple.com/us/album/bohemian-rhapsody/6781024026?i=6781024437&uo=4", "releaseDate":"1975-10-31T12:00:00Z"},
{"wrapperType":"track", "kind":"song", "artistName":"Queen", "trackName":"Bohemian Rhapsody (Operatic Section / 2011 A Cappella Mix)", "trackViewUrl":"https://music.apple.com/us/album/x/1?i=2&uo=4", "releaseDate":"1975-11-21T12:00:00Z"},
{"wrapperType":"track", "kind":"song", "artistName":"Queen", "trackName":"Bohemian Rhapsody (Live Aid)", "trackViewUrl":"https://music.apple.com/us/album/bohemian-rhapsody-live-aid/1434899831?i=1434900152&uo=4", "releaseDate":"2018-10-19T12:00:00Z"}]
}`

// Excerpt of the website's search page for "ZickeZackeZecken Swiss", a song
// the iTunes Search API doesn't return.
const webSearchFixture = `<a href="https://music.apple.com/us/album/zickezackezecken/1564056778?i=1564057396">ZickeZackeZecken</a>
<a href="https://music.apple.com/us/song/tetris/1828763302">Tetris</a>
<a href="https://music.apple.com/us/album/zickezackezecken/1564056778?i=1564057396">ZickeZackeZecken</a>`

// The lookup returns results in a different order than requested.
const webLookupFixture = `{"resultCount":2,"results":[
{"wrapperType":"track", "kind":"song", "trackId":1828763302, "artistName":"Swiss & Die Andern", "trackName":"Tetris", "trackViewUrl":"https://music.apple.com/us/album/tetris/1828763300?i=1828763302", "releaseDate":"2025-01-01T12:00:00Z"},
{"wrapperType":"track", "kind":"song", "trackId":1564057396, "artistName":"Swiss & Die Andern", "trackName":"ZickeZackeZecken", "trackViewUrl":"https://music.apple.com/us/album/zickezackezecken/1564056778?i=1564057396&uo=4", "releaseDate":"2021-07-02T12:00:00Z"}]}`

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(searchFixture))
		case "/us/search":
			w.Write([]byte(webSearchFixture))
		case "/lookup":
			if got := r.URL.Query().Get("id"); got != "1564057396,1828763302" {
				t.Errorf("lookup ids = %q, want them deduplicated in page order", got)
			}
			w.Write([]byte(webLookupFixture))
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer srv.Close()

	c := New(srv.Client())
	c.baseURL = srv.URL
	c.webBaseURL = srv.URL

	tests := []struct {
		title   string
		artist  string
		wantURL string // "" for ErrNotFound
	}{
		{"Bohemian Rhapsody", "Queen", "https://music.apple.com/us/album/bohemian-rhapsody/6781024026?i=6781024437"},
		{"Bohemian Rhapsody (Live Aid)", "Queen", "https://music.apple.com/us/album/bohemian-rhapsody-live-aid/1434899831?i=1434900152"},
		{"ZickeZackeZecken", "Swiss & Die Andern", "https://music.apple.com/us/album/zickezackezecken/1564056778?i=1564057396"},
		{"Bohemian Rhapsody", "Panic! At The Disco", ""},
	}

	for _, tt := range tests {
		got, err := c.Search(context.Background(), &platform.Track{Title: tt.title, Artists: []string{tt.artist}})
		if tt.wantURL == "" {
			if !errors.Is(err, platform.ErrNotFound) {
				t.Errorf("Search(%q, %q): err = %v, want ErrNotFound", tt.title, tt.artist, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Search(%q, %q): %v", tt.title, tt.artist, err)
			continue
		}
		if got.URL != tt.wantURL {
			t.Errorf("Search(%q, %q) = %s, want %s", tt.title, tt.artist, got.URL, tt.wantURL)
		}
	}
}
