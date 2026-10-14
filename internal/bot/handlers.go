package bot

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	log.Printf("Logged in as %s#%s", r.User.Username, r.User.Discriminator)
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}

	log.Printf("Received message from %s: %s", m.Author.Username, m.Content)
	// TODO: detect music links in m.Content and reply with links for other platforms.
}
