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

func TestSearchWebsiteFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(searchFixture))
		case "/us/search":
			http.Error(w, "blocked", http.StatusForbidden)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer srv.Close()

	c := New(srv.Client())
	c.baseURL = srv.URL
	c.webBaseURL = srv.URL

	_, err := c.Search(context.Background(), &platform.Track{Title: "ZickeZackeZecken", Artists: []string{"Swiss & Die Andern"}})
	if !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("Search: err = %v, want ErrNotFound", err)
	}
}

func TestRateLimit(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))

		c := New(srv.Client())
		c.baseURL = srv.URL
		c.webBaseURL = srv.URL

		_, lookupErr := c.Lookup(context.Background(), "us/6781024437")
		_, searchErr := c.Search(context.Background(), &platform.Track{Title: "Song", Artists: []string{"Artist"}})
		_, lookupAlbumErr := c.LookupAlbum(context.Background(), "us/6781024026")
		_, searchAlbumErr := c.SearchAlbum(context.Background(), &platform.Album{Title: "Album", Artists: []string{"Artist"}})
		for name, err := range map[string]error{
			"Lookup": lookupErr, "Search": searchErr, "LookupAlbum": lookupAlbumErr, "SearchAlbum": searchAlbumErr,
		} {
			if _, ok := platform.AsRateLimitError(err); !ok {
				t.Errorf("%s with status %d: err = %v, want rate limited", name, status, err)
			}
		}
		srv.Close()
	}
}

func TestParseAlbumURL(t *testing.T) {
	tests := []struct {
		url    string
		wantID string
		wantOK bool
	}{
		{"https://music.apple.com/de/album/une-bagga-de-keta/6780871189", "de/6780871189", true},
		{"https://music.apple.com/us/album/a-night-at-the-opera/6781024026?uo=4", "us/6781024026", true},
		{"https://music.apple.com/us/album/6781024026", "us/6781024026", true},
		{"https://geo.music.apple.com/us/album/a-night-at-the-opera/6781024026", "us/6781024026", true},
		{"https://music.apple.com/de/album/une-bagga-de-keta/6780871189?i=6780871554", "", false},
		{"https://music.apple.com/us/song/bohemian-rhapsody/6781024437", "", false},
		{"https://music.apple.com/us/album/a-night-at-the-opera", "", false},
		{"https://music.apple.com/us/artist/queen/3296287", "", false},
		{"https://example.com/us/album/x/6781024026", "", false},
	}

	c := New(http.DefaultClient)
	for _, tt := range tests {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Fatal(err)
		}
		id, ok := c.ParseAlbumURL(u)
		if id != tt.wantID || ok != tt.wantOK {
			t.Errorf("ParseAlbumURL(%q) = %q, %v; want %q, %v", tt.url, id, ok, tt.wantID, tt.wantOK)
		}
	}
}

// Trimmed real response for
// https://itunes.apple.com/lookup?id=6780871189&country=de&entity=album.
const albumLookupFixture = `{
 "resultCount":1,
 "results": [
{"wrapperType":"collection", "collectionType":"Album", "artistId":1634959678, "collectionId":6780871189, "artistName":"HET POMPSTATION, Opgekonkerd & Unicorn On K", "collectionName":"Une Bagga De Keta - Single", "collectionViewUrl":"https://music.apple.com/de/album/une-bagga-de-keta-single/6780871189?uo=4", "trackCount":1, "releaseDate":"2026-06-19T07:00:00Z", "country":"DEU"}]
}`

func TestLookupAlbum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/lookup" || q.Get("country") != "de" || q.Get("entity") != "album" {
			t.Errorf("unexpected request %s", r.URL)
		}
		switch q.Get("id") {
		case "6780871189":
			w.Write([]byte(albumLookupFixture))
		case "6781024437":
			// A track ID returns the track, which isn't an album.
			w.Write([]byte(lookupFixture))
		default:
			w.Write([]byte(`{"resultCount":0,"results":[]}`))
		}
	}))
	defer srv.Close()

	c := New(srv.Client())
	c.baseURL = srv.URL

	got, err := c.LookupAlbum(context.Background(), "de/6780871189")
	if err != nil {
		t.Fatal(err)
	}
	want := &platform.Album{
		Title:      "Une Bagga De Keta",
		Artists:    []string{"HET POMPSTATION, Opgekonkerd & Unicorn On K"},
		Year:       2026,
		TrackCount: 1,
		URL:        "https://music.apple.com/de/album/une-bagga-de-keta-single/6780871189",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LookupAlbum = %+v, want %+v", got, want)
	}

	for _, id := range []string{"de/1", "de/6781024437"} {
		if _, err := c.LookupAlbum(context.Background(), id); !errors.Is(err, platform.ErrNotFound) {
			t.Errorf("LookupAlbum(%q): err = %v, want ErrNotFound", id, err)
		}
	}
}

// Trimmed real response for /search?term=Abbey+Road+The+Beatles&entity=album.
const albumSearchFixture = `{
 "resultCount":3,
 "results": [
{"wrapperType":"collection", "collectionType":"Album", "collectionId":1441164426, "artistName":"The Beatles", "collectionName":"Abbey Road (Remastered)", "collectionViewUrl":"https://music.apple.com/us/album/abbey-road-remastered/1441164426?uo=4", "trackCount":19, "releaseDate":"1969-09-26T07:00:00Z"},
{"wrapperType":"collection", "collectionType":"Album", "collectionId":1474815798, "artistName":"The Beatles", "collectionName":"Abbey Road (2019 Mix)", "collectionViewUrl":"https://music.apple.com/us/album/abbey-road-2019-mix/1474815798?uo=4", "trackCount":17, "releaseDate":"1969-09-26T07:00:00Z"},
{"wrapperType":"collection", "collectionType":"Album", "collectionId":1474833332, "artistName":"The Beatles", "collectionName":"Abbey Road (Super Deluxe Edition) [2019 Remix & Remaster]", "collectionViewUrl":"https://music.apple.com/us/album/abbey-road-super-deluxe-edition-2019-remix-remaster/1474833332?uo=4", "trackCount":40, "releaseDate":"1969-09-26T07:00:00Z"}]
}`

// Excerpt of the website's search page for "Une Bagga De Keta", with links
// to the album and to its track.
const webAlbumSearchFixture = `<a href="https://music.apple.com/us/album/une-bagga-de-keta/6780871189?i=6780871554">Une Bagga De Keta</a>
<a href="https://music.apple.com/us/album/une-bagga-de-keta-single/6780871189">Une Bagga De Keta - Single</a>
<a href="https://music.apple.com/us/album/other-single/1000000001">Other - Single</a>
<a href="https://music.apple.com/us/album/une-bagga-de-keta-single/6780871189">Une Bagga De Keta - Single</a>`

// The lookup returns results in a different order than requested.
const webAlbumLookupFixture = `{"resultCount":2,"results":[
{"wrapperType":"collection", "collectionType":"Album", "collectionId":1000000001, "artistName":"HET POMPSTATION", "collectionName":"Other - Single", "collectionViewUrl":"https://music.apple.com/us/album/other-single/1000000001?uo=4", "trackCount":1, "releaseDate":"2025-01-01T07:00:00Z"},
{"wrapperType":"collection", "collectionType":"Album", "collectionId":6780871189, "artistName":"HET POMPSTATION, Opgekonkerd & Unicorn On K", "collectionName":"Une Bagga De Keta - Single", "collectionViewUrl":"https://music.apple.com/us/album/une-bagga-de-keta-single/6780871189?uo=4", "trackCount":1, "releaseDate":"2026-06-19T07:00:00Z"}]}`

func TestSearchAlbum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("entity"); r.URL.Path != "/us/search" && got != "album" {
			t.Errorf("entity = %q, want album", got)
		}
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(albumSearchFixture))
		case "/us/search":
			w.Write([]byte(webAlbumSearchFixture))
		case "/lookup":
			if got := r.URL.Query().Get("id"); got != "6780871189,1000000001" {
				t.Errorf("lookup ids = %q, want album IDs deduplicated in page order", got)
			}
			w.Write([]byte(webAlbumLookupFixture))
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
		tracks  int
		wantURL string // "" for ErrNotFound
	}{
		{"Abbey Road (2019 Mix)", "The Beatles", 17, "https://music.apple.com/us/album/abbey-road-2019-mix/1474815798"},
		{"Abbey Road", "The Beatles", 17, "https://music.apple.com/us/album/abbey-road-2019-mix/1474815798"},
		{"Abbey Road (Super Deluxe Edition)", "The Beatles", 40, "https://music.apple.com/us/album/abbey-road-super-deluxe-edition-2019-remix-remaster/1474833332"},
		{"Une Bagga De Keta", "HET POMPSTATION", 1, "https://music.apple.com/us/album/une-bagga-de-keta-single/6780871189"},
		{"Abbey Road", "George Benson", 0, ""},
	}

	for _, tt := range tests {
		got, err := c.SearchAlbum(context.Background(), &platform.Album{Title: tt.title, Artists: []string{tt.artist}, TrackCount: tt.tracks})
		if tt.wantURL == "" {
			if !errors.Is(err, platform.ErrNotFound) {
				t.Errorf("SearchAlbum(%q, %q): err = %v, want ErrNotFound", tt.title, tt.artist, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("SearchAlbum(%q, %q): %v", tt.title, tt.artist, err)
			continue
		}
		if got.URL != tt.wantURL {
			t.Errorf("SearchAlbum(%q, %q) = %s, want %s", tt.title, tt.artist, got.URL, tt.wantURL)
		}
	}
}
