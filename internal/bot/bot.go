// Package bot wires the Discord session to the bot's handlers.
package bot

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/config"
	"github.com/DonDevCode/music-translator/internal/platform"
	"github.com/DonDevCode/music-translator/internal/platform/applemusic"
	"github.com/DonDevCode/music-translator/internal/platform/youtubemusic"
)

// Bot wraps a Discord session.
type Bot struct {
	session   *discordgo.Session
	platforms []platform.Platform
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

	httpClient := &http.Client{Timeout: 10 * time.Second}
	platforms := []platform.Platform{applemusic.New(httpClient)}
	if cfg.YouTubeAPIKey != "" {
		platforms = append(platforms, youtubemusic.New(httpClient, cfg.YouTubeAPIKey))
	} else {
		log.Println("YOUTUBE_API_KEY is not set, YouTube Music is disabled")
	}

	b := &Bot{session: s, platforms: platforms}
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
