package bot

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/DonDevCode/music-translator/internal/platform"
)

// translate looks up the linked track or album, searches for it on all
// other platforms and returns the reply text.
func (b *Bot) translate(link platform.Link) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), linkTimeout)
	defer cancel()

	if link.Kind == platform.KindAlbum {
		return b.translateAlbum(ctx, link)
	}

	source, err := link.Platform.Lookup(ctx, link.ID)
	if err != nil {
		return "", err
	}

	results := searchAll(ctx, b.targets(link), source.Title, func(p platform.Platform) (string, int, error) {
		track, err := p.Search(ctx, source)
		if err != nil {
			return "", 0, err
		}
		return track.URL, track.Year, nil
	})
	return formatReply(source, results), nil
}

func (b *Bot) translateAlbum(ctx context.Context, link platform.Link) (string, error) {
	source, err := link.Platform.(platform.AlbumPlatform).LookupAlbum(ctx, link.ID)
	if err != nil {
		return "", err
	}

	results := searchAll(ctx, b.targets(link), source.Title, func(p platform.Platform) (string, int, error) {
		album, err := p.(platform.AlbumSearcher).SearchAlbum(ctx, source)
		if err != nil {
			return "", 0, err
		}
		return album.URL, album.Year, nil
	})
	return formatAlbumReply(source, results), nil
}

// targets returns the platforms to translate link to: all others that can
// search for its kind.
func (b *Bot) targets(link platform.Link) []platform.Platform {
	var targets []platform.Platform
	for _, p := range b.platforms {
		if p == link.Platform {
			continue
		}
		if _, ok := p.(platform.AlbumSearcher); link.Kind == platform.KindAlbum && !ok {
			continue
		}
		targets = append(targets, p)
	}
	return targets
}

// searchAll runs search on all platforms concurrently. search returns the
// link and year of what it found. title is only used for logging.
func searchAll(ctx context.Context, platforms []platform.Platform, title string, search func(platform.Platform) (url string, year int, err error)) []result {
	results := make([]result, len(platforms))
	var wg sync.WaitGroup
	for i, p := range platforms {
		results[i].platform = p
		wg.Add(1)
		go func(r *result) {
			defer wg.Done()
			url, year, err := search(r.platform)
			if err != nil {
				if !errors.Is(err, platform.ErrNotFound) {
					log.Printf("Searching %s for %q: %v", r.platform.Name(), title, err)
					r.err = err
				}
				return
			}
			r.url, r.year = url, year
		}(&results[i])
	}
	wg.Wait()
	return results
}
