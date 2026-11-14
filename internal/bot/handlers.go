package bot

import (
	"log"
	"slices"
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
	// With a single platform there is nothing to translate to.
	if len(b.platforms) < 2 {
		return
	}

	// Links are independent, so they are resolved concurrently, but the
	// replies are sent in the order the links appear.
	// Album links are skipped while no other platform can search for albums.
	links := slices.DeleteFunc(platform.FindLinks(m.Content, b.platforms, maxLinksPerMessage),
		func(link platform.Link) bool { return len(b.targets(link)) == 0 })
	replies := make([]string, len(links))
	errs := make([]error, len(links))
	var wg sync.WaitGroup
	for i, link := range links {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replies[i], errs[i] = b.translate(link)
		}()
	}
	wg.Wait()

	sentRateLimits := map[string]bool{}
	for i, link := range links {
		reply, err := replies[i], errs[i]
		if err != nil {
			log.Printf("Looking up %s %s %s: %v", link.Platform.Name(), kindName(link.Kind), link.ID, err)

			// Tell users why the bot didn't answer, with at most one such
			// notice per API and message. Replies to other links may still
			// mention the same rate limit for their searches. Other errors
			// stay silent, since a reply without links isn't useful.
			rateLimitErr, ok := platform.AsRateLimitError(err)
			if !ok || sentRateLimits[rateLimitErr.API] {
				continue
			}
			sentRateLimits[rateLimitErr.API] = true
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

func kindName(k platform.Kind) string {
	if k == platform.KindAlbum {
		return "album"
	}
	return "track"
}
