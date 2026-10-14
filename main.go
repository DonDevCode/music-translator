// Music Translator runs a Discord bot that translates music links.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/DonDevCode/music-translator/internal/bot"
	"github.com/DonDevCode/music-translator/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	b, err := bot.New(cfg)
	if err != nil {
		log.Fatalf("creating bot: %v", err)
	}

	if err := b.Start(); err != nil {
		log.Fatalf("starting bot: %v", err)
	}
	defer b.Stop()

	log.Println("Music Translator is running. Press Ctrl+C to exit.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down.")
}
