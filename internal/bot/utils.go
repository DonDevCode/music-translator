package bot

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/DonDevCode/music-translator/internal/platform"
)

// translate looks up the linked track, searches for it on all other
// platforms and returns the reply text.
func (b *Bot) translate(link platform.Link) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), linkTimeout)
	defer cancel()

	source, err := link.Platform.Lookup(ctx, link.ID)
	if err != nil {
		return "", err
	}

	var results []result
	for _, p := range b.platforms {
		if p != link.Platform {
			results = append(results, result{platform: p})
		}
	}

	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(r *result) {
			defer wg.Done()
			track, err := r.platform.Search(ctx, source)
			if err != nil {
				if !errors.Is(err, platform.ErrNotFound) {
					log.Printf("Searching %s for %q: %v", r.platform.Name(), source.Title, err)
					r.err = err
				}
				return
			}
			r.track = track
		}(&results[i])
	}
	wg.Wait()

	return formatReply(source, results), nil
}
