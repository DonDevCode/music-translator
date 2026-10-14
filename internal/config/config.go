// Package config loads the bot's configuration from the environment.
package config

import (
	"errors"
	"os"
)

// Config holds runtime settings for the bot.
type Config struct {
	DiscordToken  string
	YouTubeAPIKey string
}

// Load reads the configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		DiscordToken:  os.Getenv("DISCORD_TOKEN"),
		YouTubeAPIKey: os.Getenv("YOUTUBE_API_KEY"),
	}

	if cfg.DiscordToken == "" {
		return nil, errors.New("DISCORD_TOKEN is not set")
	}
	if cfg.YouTubeAPIKey == "" {
		return nil, errors.New("YOUTUBE_API_KEY is not set")
	}

	return cfg, nil
}
