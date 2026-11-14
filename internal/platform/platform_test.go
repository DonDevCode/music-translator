package platform

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// fakePlatform matches example.com/{id}.
type fakePlatform struct{ name, host string }

func (f fakePlatform) Name() string { return f.name }

func (f fakePlatform) ParseURL(u *url.URL) (string, bool) {
	if u.Host != f.host || len(u.Path) < 2 {
		return "", false
	}
	return u.Path[1:], true
}

func (f fakePlatform) Lookup(context.Context, string) (*Track, error) { return nil, ErrNotFound }

func (f fakePlatform) Search(context.Context, *Track) (*Track, error) { return nil, ErrNotFound }

// fakeAlbumPlatform also matches example.com/album/{id}, which ParseURL
// rejects.
type fakeAlbumPlatform struct{ fakePlatform }

func (f fakeAlbumPlatform) ParseURL(u *url.URL) (string, bool) {
	if strings.HasPrefix(u.Path, "/album/") {
		return "", false
	}
	return f.fakePlatform.ParseURL(u)
}

func (f fakeAlbumPlatform) ParseAlbumURL(u *url.URL) (string, bool) {
	id, ok := strings.CutPrefix(u.Path, "/album/")
	return id, ok && u.Host == f.host && id != ""
}

func (f fakeAlbumPlatform) LookupAlbum(context.Context, string) (*Album, error) {
	return nil, ErrNotFound
}

func (f fakeAlbumPlatform) SearchAlbum(context.Context, *Album) (*Album, error) {
	return nil, ErrNotFound
}

// fakeVideoPlatform also matches video.test/{id}.
type fakeVideoPlatform struct{ fakePlatform }

func (f fakeVideoPlatform) ParseVideoURL(u *url.URL) (string, bool) {
	if u.Host != "video.test" || len(u.Path) < 2 {
		return "", false
	}
	return u.Path[1:], true
}

func (f fakeVideoPlatform) LookupVideo(context.Context, string) (*Track, error) {
	return nil, ErrNotMusic
}

func TestFindLinks(t *testing.T) {
	a := fakePlatform{name: "A", host: "a.test"}
	b := fakePlatform{name: "B", host: "b.test"}
	c := fakeAlbumPlatform{fakePlatform{name: "C", host: "c.test"}}
	v := fakeVideoPlatform{fakePlatform{name: "V", host: "v.test"}}
	platforms := []Platform{a, b, c, v}

	track := func(p Platform, id string) Link { return Link{Platform: p, Kind: KindTrack, ID: id} }
	album := func(p Platform, id string) Link { return Link{Platform: p, Kind: KindAlbum, ID: id} }
	video := func(p Platform, id string) Link { return Link{Platform: p, Kind: KindVideo, ID: id} }

	tests := []struct {
		name    string
		content string
		limit   int
		want    []Link
	}{
		{"no links", "hello there", 3, nil},
		{"unknown host", "https://other.test/1", 3, nil},
		{"plain", "check https://a.test/1 out", 3, []Link{track(a, "1")}},
		{"suppressed embed", "<https://a.test/1>", 3, []Link{track(a, "1")}},
		{"trailing punctuation", "listen to https://a.test/1!", 3, []Link{track(a, "1")}},
		{"markdown link", "[song](https://a.test/1)", 3, []Link{track(a, "1")}},
		{"spoiler", "||https://a.test/1||", 3, []Link{track(a, "1")}},
		{"multiple platforms", "https://b.test/2 and https://a.test/1", 3, []Link{track(b, "2"), track(a, "1")}},
		{"duplicates", "https://a.test/1 https://a.test/1", 3, []Link{track(a, "1")}},
		{"same id on different platforms", "https://a.test/1 https://b.test/1", 3, []Link{track(a, "1"), track(b, "1")}},
		{"album", "https://c.test/album/1", 3, []Link{album(c, "1")}},
		{"track on album platform", "https://c.test/1", 3, []Link{track(c, "1")}},
		{"same id as track and album", "https://c.test/1 https://c.test/album/1", 3, []Link{track(c, "1"), album(c, "1")}},
		{"album on platform without albums", "https://a.test/album/1", 3, []Link{track(a, "album/1")}},
		{"video", "https://video.test/1", 3, []Link{video(v, "1")}},
		{"video and track", "https://video.test/1 https://v.test/2", 3, []Link{video(v, "1"), track(v, "2")}},
		{"same video as track", "https://v.test/1 https://video.test/1", 3, []Link{track(v, "1")}},
		{"same track as video", "https://video.test/1 https://v.test/1", 3, []Link{video(v, "1")}},
		{"limit", "https://a.test/1 https://a.test/2 https://a.test/3", 2, []Link{track(a, "1"), track(a, "2")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindLinks(tt.content, platforms, tt.limit)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindLinks(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}
