// Package config loads the bot's configuration from the environment.
package config

import (
	"errors"
	"os"
)

// defaultDataPath is where server settings are stored if DATA_PATH is unset.
const defaultDataPath = "data/settings.json"

// Config holds runtime settings for the bot.
type Config struct {
	DiscordToken  string
	YouTubeAPIKey string // optional; YouTube Music is disabled without it
	DataPath      string // file with the per-server settings
}

// Load reads the configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		DiscordToken:  os.Getenv("DISCORD_TOKEN"),
		YouTubeAPIKey: os.Getenv("YOUTUBE_API_KEY"),
		DataPath:      os.Getenv("DATA_PATH"),
	}

	if cfg.DiscordToken == "" {
		return nil, errors.New("DISCORD_TOKEN is not set")
	}
	if cfg.DataPath == "" {
		cfg.DataPath = defaultDataPath
	}

	return cfg, nil
}
