// Package bot wires the Discord session to the bot's handlers.
package bot

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/bwmarrin/discordgo"

	"github.com/DonDevCode/music-translator/internal/config"
	"github.com/DonDevCode/music-translator/internal/platform"
	"github.com/DonDevCode/music-translator/internal/platform/applemusic"
	"github.com/DonDevCode/music-translator/internal/platform/youtubemusic"
	"github.com/DonDevCode/music-translator/internal/settings"
)

// Bot wraps a Discord session.
type Bot struct {
	session   *discordgo.Session
	platforms []platform.Platform
	settings  *settings.Store

	// requests holds the IDs of messages whose translation was requested
	// and is in progress, so that simultaneous clicks translate only once.
	requests sync.Map
}

// New creates a Bot from the given configuration.
func New(cfg *config.Config) (*Bot, error) {
	s, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("creating discord session: %w", err)
	}

	// Reading message content requires the privileged Message Content intent,
	// which must also be enabled in the Discord developer portal.
	// Guilds fills the state's channel cache, which is needed to find the
	// channel a thread is in, and reports when the bot leaves a server.
	// Reactions are how users request translations in On Request mode.
	s.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMessageReactions |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent

	store, err := settings.Open(cfg.DataPath)
	if err != nil {
		return nil, err
	}

	// Requests are bounded by linkTimeout through their context.
	httpClient := &http.Client{}
	platforms := []platform.Platform{applemusic.New(httpClient)}
	if cfg.YouTubeAPIKey != "" {
		platforms = append(platforms, youtubemusic.New(httpClient, cfg.YouTubeAPIKey))
	} else {
		log.Println("YOUTUBE_API_KEY is not set, YouTube Music is disabled")
	}

	b := &Bot{session: s, platforms: platforms, settings: store}
	s.AddHandler(b.onReady)
	s.AddHandler(b.onMessageCreate)
	s.AddHandler(b.onMessageReactionAdd)
	s.AddHandler(b.onInteractionCreate)
	s.AddHandler(b.onGuildDelete)
	s.AddHandler(b.onChannelDelete)
	s.AddHandler(b.onThreadDelete)

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
