# Music Translator

A Discord bot that recognizes music links (Spotify, Apple Music, YouTube Music, …) in chat and replies with links to the same track on other platforms.

## Setup

1. Create an application and bot at <https://discord.com/developers/applications> and enable the **Message Content** intent.
2. Copy `.env.example` to `.env` and fill in `DISCORD_TOKEN`.
3. Run with [just](https://github.com/casey/just), which loads `.env` automatically:
   ```sh
   just run
   ```
   Other recipes: `just build`, `just test`, `just lint`, `just tidy`.

## Layout

```
main.go             entry point
internal/config/    environment-based configuration
internal/bot/       Discord session and event handlers
internal/platform/  music platform abstraction
```
