package bot

import (
	"errors"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/platform"
	"github.com/DonDevCode/music-translator/internal/settings"
)

// maxLinksPerMessage limits how many replies a single message can trigger.
const maxLinksPerMessage = 2

// maxVideoLinksPerMessage limits how many video links of a single message
// are looked up. They count separately, since many videos aren't music.
const maxVideoLinksPerMessage = 2 // ToDo: Check which values makes sense here, maybe even cap to 1 link per message

// linkTimeout bounds the time spent resolving one link on all platforms.
const linkTimeout = 10 * time.Second

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("Logged in as %s#%s", r.User.Username, r.User.Discriminator)

	// Servers that had the bot before settings existed keep Auto mode.
	guildIDs := make([]string, len(r.Guilds))
	for i, g := range r.Guilds {
		guildIDs[i] = g.ID
	}
	if err := b.settings.Initialize(guildIDs); err != nil {
		log.Printf("Initializing settings: %v", err)
	}

	b.registerCommands(s, r.User.ID)
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}

	// Detecting links only parses URLs and costs nothing, so it is done
	// before deciding what to do with them.
	links := b.findLinks(m.Content)
	if len(links) == 0 {
		return
	}

	// DMs have no settings and are always answered.
	behavior := settings.Auto
	if m.GuildID != "" {
		behavior = b.behaviorFor(s, m.GuildID, m.ChannelID)
	}

	switch behavior {
	case settings.Auto:
		b.translateAndReply(s, m.ChannelID, m.Reference(), links)
	case settings.OnRequest:
		b.offerTranslation(s, m.Message, links)
	}
}

// findLinks returns the links in content that can be translated.
func (b *Bot) findLinks(content string) []platform.Link {
	// With a single platform there is nothing to translate to.
	if len(b.platforms) < 2 {
		return nil
	}
	// Album links are skipped while no other platform can search for albums.
	return slices.DeleteFunc(platform.FindLinks(content, b.platforms, maxLinksPerMessage, maxVideoLinksPerMessage),
		func(link platform.Link) bool { return len(b.targets(link)) == 0 })
}

// behaviorFor returns what the bot does with links in a channel of a server.
func (b *Bot) behaviorFor(s *discordgo.Session, guildID, channelID string) settings.Behavior {
	g := b.settings.Guild(guildID)
	// Only threads in servers with channel settings need the parent channel.
	if len(g.Channels) == 0 {
		return g.Default
	}
	return g.BehaviorFor(channelID, parentChannel(s, channelID))
}

// parentChannel returns the channel a thread is in, or "" if channelID
// isn't a thread or can't be looked up.
func parentChannel(s *discordgo.Session, channelID string) string {
	ch, err := s.State.Channel(channelID)
	if err != nil {
		if ch, err = s.Channel(channelID); err != nil {
			log.Printf("Looking up channel %s: %v", channelID, err)
			return ""
		}
	}
	if !ch.IsThread() {
		return ""
	}
	return ch.ParentID
}

// translateAndReply translates links and replies to the message ref points
// to. It returns how many replies were sent and whether an API's rate limit
// kept the bot from looking up a link, so that it can be retried later.
func (b *Bot) translateAndReply(s *discordgo.Session, channelID string, ref *discordgo.MessageReference, links []platform.Link) (sent int, rateLimited bool) {
	// Links are independent, so they are resolved concurrently, but the
	// replies are sent in the order the links appear.
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
		if sent >= maxLinksPerMessage {
			break
		}
		reply, err := replies[i], errs[i]
		// Videos that aren't music, or that no other platform has, are
		// expected and ignored.
		if errors.Is(err, platform.ErrNotMusic) || errors.Is(err, errNoMatch) {
			continue
		}
		if err != nil {
			log.Printf("Looking up %s %s %s: %v", link.Platform.Name(), kindName(link.Kind), link.ID, err)

			// A video that couldn't be looked up may well not be music, so
			// users aren't told, not even about rate limits.
			if link.Kind == platform.KindVideo {
				continue
			}

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
			rateLimited = true
		}

		_, err = s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
			Content:   reply,
			Reference: ref,
			// Reply without pinging the author or anyone named in a title.
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		})
		if err != nil {
			log.Printf("Replying to message %s: %v", ref.MessageID, err)
			continue
		}
		sent++
	}
	return sent, rateLimited
}

func (b *Bot) onGuildDelete(s *discordgo.Session, g *discordgo.GuildDelete) {
	// Unavailable means a Discord outage, not that the bot was removed.
	if g.Unavailable {
		return
	}
	if err := b.settings.DeleteGuild(g.ID); err != nil {
		log.Printf("Deleting settings of server %s: %v", g.ID, err)
	}
}

func (b *Bot) onChannelDelete(s *discordgo.Session, c *discordgo.ChannelDelete) {
	if c.GuildID == "" {
		return
	}
	if _, err := b.settings.RemoveChannel(c.GuildID, c.ID); err != nil {
		log.Printf("Removing settings of channel %s: %v", c.ID, err)
	}
}

func (b *Bot) onThreadDelete(s *discordgo.Session, t *discordgo.ThreadDelete) {
	if t.GuildID == "" {
		return
	}
	if _, err := b.settings.RemoveChannel(t.GuildID, t.ID); err != nil {
		log.Printf("Removing settings of thread %s: %v", t.ID, err)
	}
}

func kindName(k platform.Kind) string {
	switch k {
	case platform.KindAlbum:
		return "album"
	case platform.KindVideo:
		return "video"
	}
	return "track"
}
