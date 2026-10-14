package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/platform"
)

// maxLinksPerMessage limits how many links a single message can trigger.
const maxLinksPerMessage = 3

// lookupTimeout bounds the time spent resolving one link.
const lookupTimeout = 5 * time.Second

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("Logged in as %s#%s", r.User.Username, r.User.Discriminator)
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}

	for _, link := range platform.FindLinks(m.Content, b.platforms, maxLinksPerMessage) {
		track, err := b.lookup(link)
		if err != nil {
			log.Printf("Looking up %s track %s: %v", link.Platform.Name(), link.ID, err)
			continue
		}

		log.Printf("Found %s track: %s", link.Platform.Name(), formatTrack(track))

		// TODO: replace with an embed linking to the other platforms.
		if _, err := s.ChannelMessageSendReply(m.ChannelID, formatTrack(track), m.Reference()); err != nil {
			log.Printf("Replying to message %s: %v", m.ID, err)
		}
	}
}

func (b *Bot) lookup(link platform.Link) (*platform.Track, error) {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	return link.Platform.Lookup(ctx, link.ID)
}

// formatTrack renders a track as "Title – Artist, Artist (Year)".
func formatTrack(t *platform.Track) string {
	s := fmt.Sprintf("%s – %s", t.Title, strings.Join(t.Artists, ", "))
	if t.Year != 0 {
		s += fmt.Sprintf(" (%d)", t.Year)
	}
	return s
}
