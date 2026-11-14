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

	results := searchAll(ctx, b.targets(link), source.Title, func(p platform.Platform) (*platform.Track, error) {
		return p.Search(ctx, source)
	})
	return formatReply(source, results), nil
}

func (b *Bot) translateAlbum(ctx context.Context, link platform.Link) (string, error) {
	source, err := link.Platform.(platform.AlbumPlatform).LookupAlbum(ctx, link.ID)
	if err != nil {
		return "", err
	}

	// The reply only needs the link and year of what was found, which an
	// album shares with a track.
	results := searchAll(ctx, b.targets(link), source.Title, func(p platform.Platform) (*platform.Track, error) {
		album, err := p.(platform.AlbumPlatform).SearchAlbum(ctx, source)
		if err != nil {
			return nil, err
		}
		return &platform.Track{Year: album.Year, URL: album.URL}, nil
	})
	return formatAlbumReply(source, results), nil
}

// targets returns the platforms to translate link to: all others that
// support its kind.
func (b *Bot) targets(link platform.Link) []platform.Platform {
	var targets []platform.Platform
	for _, p := range b.platforms {
		if p == link.Platform {
			continue
		}
		if _, ok := p.(platform.AlbumPlatform); link.Kind == platform.KindAlbum && !ok {
			continue
		}
		targets = append(targets, p)
	}
	return targets
}

// searchAll runs search on all platforms concurrently. title is only used
// for logging.
func searchAll(ctx context.Context, platforms []platform.Platform, title string, search func(platform.Platform) (*platform.Track, error)) []result {
	results := make([]result, len(platforms))
	var wg sync.WaitGroup
	for i, p := range platforms {
		results[i].platform = p
		wg.Add(1)
		go func(r *result) {
			defer wg.Done()
			track, err := search(r.platform)
			if err != nil {
				if !errors.Is(err, platform.ErrNotFound) {
					log.Printf("Searching %s for %q: %v", r.platform.Name(), title, err)
					r.err = err
				}
				return
			}
			r.track = track
		}(&results[i])
	}
	wg.Wait()
	return results
}
