# Scrap Chat

[![Go Version](https://img.shields.io/github/go-mod/go-version/xorvus/scrap-chat)](https://github.com/xorvus/scrap-chat)
[![Go Report Card](https://goreportcard.com/badge/github.com/xorvus/scrap-chat)](https://goreportcard.com/report/github.com/xorvus/scrap-chat)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/xorvus/scrap-chat/blob/main/LICENSE)

Scraping chat messages with no authentication required.

----

## ✨ Features

- Live Chat Youtube
- Get Channel Id Youtube
- Comment Youtube (under development)


## 📦 Usage

### Command line

```bash
  ./scrapchat [Options] url
  
  #Options
  -v --version          Show version
  -t --type             Type of scrap [live, video, info]
  -o --output           Output result [log, file]
  -f --format           Format output [default, json, custom]
  -co --custom-output   Custom output template (for format=custom)
```

#### Example usage

```bash
./scrapchat --type live --format custom -co "TIME ID: [AUTHOR_NAME] MESSAGE" "https://www.youtube.com/watch?v=jfKfPfyJRdk"
```

### Golang 

Use `go get`:

```bash
go get github.com/xorvus/scrap-chat@latest
go mod tidy
```
main.go
```go
func main() {
    chat, err := scrapchat.New("youtube")
    if err != nil {
        log.Fatalf("Failed to create scraper: %v", err)
    }

    data, err := chat.FetchLiveChat("https://www.youtube.com/watch?v=jfKfPfyJRdk")
    if err != nil {
        log.Fatalf("Error fetching live chat: %v", err)
    }

    for msg := range data {
        log.Printf("[%s] %s\n", msg.Author.Name, msg.Message)
    }
}
```