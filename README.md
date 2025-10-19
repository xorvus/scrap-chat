# Scrap Chat

[![Go Version](https://img.shields.io/github/go-mod/go-version/xorvus/scrap-chat)](https://github.com/xorvus/scrap-chat)
[![Go Report Card](https://goreportcard.com/badge/github.com/xorvus/scrap-chat)](https://goreportcard.com/report/github.com/xorvus/scrap-chat)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/xorvus/scrap-chat/blob/main/LICENSE)

Scraping chat messages with no authentication required.

----

## ✨ Features

- Live Chat Youtube
- Video Comments Youtube
- Get Channel Info Youtube


## 📦 Usage

### Command line

```bash
  ./scrap-chat [Options] url

  #Options
  -v --version          Show version
  -t --type             Type of scrap [livechat, comments, info]
  -o --output           Output result [log, file]
  -f --format           Format output [default, json, custom]
  -co --custom-output   Custom output template (for format=custom)
```

#### Example usage

**Live Chat:**
```bash
./scrap-chat --type livechat --output file --format json "https://www.youtube.com/watch?v=jfKfPfyJRdk"
```

**Video Comments:**
```bash
./scrap-chat --type comments --output file --format json "https://www.youtube.com/watch?v=jfKfPfyJRdk"
```

**Channel Info:**
```bash
./scrap-chat --type info --format json "https://www.youtube.com/@LofiGirl"
```

**Custom Format:**
```bash
./scrap-chat --type livechat --format custom -co "TIME ID: [AUTHOR_NAME] MESSAGE" "https://www.youtube.com/watch?v=jfKfPfyJRdk"
```

### Golang

Use `go get`:

```bash
go get github.com/xorvus/scrap-chat@latest
go mod tidy
```

#### Fetch Live Chat

```go
package main

import (
    "log"
    "github.com/xorvus/scrap-chat/pkg/scrapchat"
)

func main() {
    chat, err := scrapchat.New("youtube")
    if err != nil {
        log.Fatalf("Failed to create scraper: %v", err)
    }

    liveChat, err := chat.FetchLiveChat("https://www.youtube.com/watch?v=jfKfPfyJRdk")
    if err != nil {
        log.Fatalf("Error fetching live chat: %v", err)
    }

    for msg := range liveChat {
        log.Printf("[%s] %s\n", msg.Author.Name, msg.Message)
    }
}
```

#### Fetch Video Comments

```go
package main

import (
    "log"
    "github.com/xorvus/scrap-chat/pkg/scrapchat"
)

func main() {
    chat, err := scrapchat.New("youtube")
    if err != nil {
        log.Fatalf("Failed to create scraper: %v", err)
    }

    comments, err := chat.FetchVideoComments("https://www.youtube.com/watch?v=jfKfPfyJRdk", nil)
    if err != nil {
        log.Fatalf("Error fetching video comments: %v", err)
    }

    for comment := range comments {
        log.Printf("[%s] %s (👍 %d | 💬 %d)\n",
            comment.Author.Name,
            comment.Message,
            comment.LikeCount,
            comment.ReplyCount)
    }
}
```

#### Fetch Channel Info

```go
package main

import (
    "log"
    "github.com/xorvus/scrap-chat/pkg/scrapchat"
)

func main() {
    chat, err := scrapchat.New("youtube")
    if err != nil {
        log.Fatalf("Failed to create scraper: %v", err)
    }

    info, err := chat.FetchChannelInfo("https://www.youtube.com/@LofiGirl")
    if err != nil {
        log.Fatalf("Error fetching channel info: %v", err)
    }

    log.Printf("Channel: %s (%s)\n", info.Name, info.ID)
    log.Printf("Description: %s\n", info.Description)
}
```