// Package bot wires the Discord session to the bot's handlers.
package bot

import (
	"fmt"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/config"
)

// Bot wraps a Discord session.
type Bot struct {
	session *discordgo.Session
}

// New creates a Bot from the given configuration.
func New(cfg *config.Config) (*Bot, error) {
	s, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("creating discord session: %w", err)
	}

	// Reading message content requires the privileged Message Content intent,
	// which must also be enabled in the Discord developer portal.
	s.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent

	b := &Bot{session: s}
	s.AddHandler(b.onReady)
	s.AddHandler(b.onMessageCreate)

	return b, nil
}

// Start opens the connection to Discord.
func (b *Bot) Start() error {
	return b.session.Open()
}

// Stop closes the connection to Discord.
func (b *Bot) Stop() error {
	return b.session.Close()
}
