build:
	@echo VERSION is: $(shell git describe --tags)
	@go build -ldflags "-X main.version=$(shell git describe --tags)" -o scrap-chat cmd/scrap_chat/*.go

example-live:
	@go build examples/get_live_chat/get_live_chat.go
	@./get_live_chat "https://www.youtube.com/@LofiGirl"

example-id:
	@go build examples/get_channel_id/get_channel_id.go
	@./get_channel_id


lint:
	@printf "[Lint: golangci-lint run] \n"
	@golangci-lint run

cyclo:
	@printf "[Cyclo: gocyclo -over 15 .] \n"
	@out="$$(gocyclo -over 15 .)"; \
	if [ -z "$$out" ]; then echo 0; else printf "%s\n" "$$out"; fi

check: lint cyclo build