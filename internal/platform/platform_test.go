package platform

import (
	"context"
	"net/url"
	"reflect"
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

func TestFindLinks(t *testing.T) {
	a := fakePlatform{name: "A", host: "a.test"}
	b := fakePlatform{name: "B", host: "b.test"}
	platforms := []Platform{a, b}

	tests := []struct {
		name    string
		content string
		limit   int
		want    []Link
	}{
		{"no links", "hello there", 3, nil},
		{"unknown host", "https://other.test/1", 3, nil},
		{"plain", "check https://a.test/1 out", 3, []Link{{a, "1"}}},
		{"suppressed embed", "<https://a.test/1>", 3, []Link{{a, "1"}}},
		{"trailing punctuation", "listen to https://a.test/1!", 3, []Link{{a, "1"}}},
		{"markdown link", "[song](https://a.test/1)", 3, []Link{{a, "1"}}},
		{"spoiler", "||https://a.test/1||", 3, []Link{{a, "1"}}},
		{"multiple platforms", "https://b.test/2 and https://a.test/1", 3, []Link{{b, "2"}, {a, "1"}}},
		{"duplicates", "https://a.test/1 https://a.test/1", 3, []Link{{a, "1"}}},
		{"same id on different platforms", "https://a.test/1 https://b.test/1", 3, []Link{{a, "1"}, {b, "1"}}},
		{"limit", "https://a.test/1 https://a.test/2 https://a.test/3", 2, []Link{{a, "1"}, {a, "2"}}},
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
