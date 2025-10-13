package main

import (
	"fmt"
	"log"

	"github.com/xorvus/scrap-chat/pkg/scrapchat"
)

const defaultChannel = "https://www.youtube.com/@LofiGirl"

func main() {
	chat, err := scrapchat.New("youtube")
	if err != nil {
		log.Fatalf("Failed to create scraper: %v", err)
	}

	channelInfo, err := chat.FetchChannelInfo(defaultChannel)
	if err != nil {
		log.Fatalf("Error fetching channel info: %v", err)
	}

	fmt.Printf("Channel ID: %s\n", channelInfo.ID)
	fmt.Printf("Channel Name: %s\n", channelInfo.Name)
	fmt.Printf("Channel URL: %s\n", channelInfo.URL)
	fmt.Printf("Description: %s\n", channelInfo.Description)
}
