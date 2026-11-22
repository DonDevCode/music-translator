package bot

import (
	"context"
	"errors"
	"log"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/platform"
)

// requestEmoji is the reaction that requests a translation in On Request
// mode. It is a Unicode emoji so that it works in every server.
const requestEmoji = "🎵"

// offerTranslation adds the request reaction to a message with links.
//
// The bot's own reaction marks the message as not translated yet: it is
// removed once someone clicks it. This needs no storage and survives
// restarts.
func (b *Bot) offerTranslation(s *discordgo.Session, m *discordgo.Message, links []platform.Link) {
	if !b.anyMusic(links) {
		return
	}
	if err := s.MessageReactionAdd(m.ChannelID, m.ID, requestEmoji); err != nil {
		// Usually missing Add Reactions or Read Message History permissions,
		// which /translator status reports.
		log.Printf("Adding reaction to message %s: %v", m.ID, err)
	}
}

// anyMusic reports whether a translation of links could find something.
// Many video links aren't music, and reacting to them would be noise, so if
// all links are videos, they are looked up first. A lookup costs little
// quota compared to the searches of a translation.
func (b *Bot) anyMusic(links []platform.Link) bool {
	for _, link := range links {
		if link.Kind != platform.KindVideo {
			return true
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), linkTimeout)
	defer cancel()
	for _, link := range links {
		_, err := link.Platform.(platform.VideoPlatform).LookupVideo(ctx, link.ID)
		if err == nil {
			return true
		}
		if !errors.Is(err, platform.ErrNotMusic) {
			log.Printf("Looking up %s video %s: %v", link.Platform.Name(), link.ID, err)
		}
	}
	return false
}

func (b *Bot) onMessageReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if r.GuildID == "" || r.Emoji.ID != "" || r.Emoji.Name != requestEmoji {
		return
	}
	if r.UserID == s.State.User.ID || (r.Member != nil && r.Member.User != nil && r.Member.User.Bot) {
		return
	}

	// Simultaneous clicks are handled once. Later clicks find the bot's
	// reaction gone and do nothing.
	if _, busy := b.requests.LoadOrStore(r.MessageID, struct{}{}); busy {
		return
	}
	defer b.requests.Delete(r.MessageID)

	// Reaction events don't contain the message. Fetching it also picks up
	// edits, and fails if the message was deleted.
	m, err := s.ChannelMessage(r.ChannelID, r.MessageID)
	if err != nil {
		log.Printf("Fetching message %s for translation request: %v", r.MessageID, err)
		return
	}
	if !hasOwnReaction(m, requestEmoji) {
		return
	}
	if err := s.MessageReactionRemove(r.ChannelID, r.MessageID, requestEmoji, "@me"); err != nil {
		// Without removing it, the message would be translated again on
		// the next click.
		log.Printf("Removing reaction from message %s: %v", r.MessageID, err)
		return
	}

	links := b.findLinks(m.Content)
	if len(links) == 0 {
		return
	}
	m.GuildID = r.GuildID // not set on fetched messages
	if _, rateLimited := b.translateAndReply(s, r.ChannelID, m.Reference(), links); rateLimited {
		// Let users try again once the limit is over.
		if err := s.MessageReactionAdd(r.ChannelID, r.MessageID, requestEmoji); err != nil {
			log.Printf("Adding reaction to message %s: %v", r.MessageID, err)
		}
	}
}

// hasOwnReaction reports whether the bot reacted to m with emoji.
func hasOwnReaction(m *discordgo.Message, emoji string) bool {
	for _, r := range m.Reactions {
		if r.Me && r.Emoji != nil && r.Emoji.ID == "" && r.Emoji.Name == emoji {
			return true
		}
	}
	return false
}
