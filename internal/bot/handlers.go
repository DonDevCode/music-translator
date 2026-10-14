package bot

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/platform"
)

// maxLinksPerMessage limits how many links a single message can trigger.
const maxLinksPerMessage = 3

// linkTimeout bounds the time spent resolving one link on all platforms.
const linkTimeout = 10 * time.Second

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("Logged in as %s#%s", r.User.Username, r.User.Discriminator)
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}

	for _, link := range platform.FindLinks(m.Content, b.platforms, maxLinksPerMessage) {
		reply, err := b.translate(link)
		if err != nil {
			log.Printf("Looking up %s track %s: %v", link.Platform.Name(), link.ID, err)

			// Tell users why the bot didn't answer. Other errors stay silent,
			// since a reply without links isn't useful.
			var rateLimitErr *platform.RateLimitError
			if !errors.As(err, &rateLimitErr) {
				continue
			}
			reply = rateLimitErr.Error()
		}

		_, err = s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Content:   reply,
			Reference: m.Reference(),
			// Reply without pinging the author or anyone named in a title.
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
		if err != nil {
			log.Printf("Replying to message %s: %v", m.ID, err)
		}
	}
}

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
