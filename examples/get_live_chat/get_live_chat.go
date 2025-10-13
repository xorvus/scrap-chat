package main

import (
	"fmt"
	"log"
	"os"

	"github.com/xorvus/scrap-chat/pkg/scrapchat"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: program <channel_url>")
		os.Exit(1)
	}

	chat, err := scrapchat.New("youtube", true)
	if err != nil {
		log.Fatalf("Failed to create scraper: %v", err)
	}

	channelURL := os.Args[1]

	data, err := chat.FetchLiveChat(channelURL)
	if err != nil {
		log.Fatalf("Error fetching live chat: %v", err)
	}

	for msg := range data {
		log.Printf("[%s] %s", msg.Author.Name, msg.Message)
	}
}
