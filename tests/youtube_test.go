package tests

import (
	"context"
	"os"
	"testing"

	"github.com/xorvus/scrap-chat/internal/fetchers/youtube"
)

func TestNewYoutube(t *testing.T) {
	ctx := context.Background()
	yt := youtube.New(&ctx, false)

	if yt == nil {
		t.Error("New() returned nil")
	}
}

func TestNewYoutubeWithVerbose(t *testing.T) {
	ctx := context.Background()
	yt := youtube.New(&ctx, true)

	if yt == nil {
		t.Error("New() with verbose returned nil")
	}
}

func TestAddCookiesInvalidFile(t *testing.T) {
	ctx := context.Background()
	yt := youtube.New(&ctx, false)

	err := yt.AddCookies("/nonexistent/cookies.txt")
	if err == nil {
		t.Error("expected error for nonexistent cookie file")
	}
}

func TestAddCookiesEmptyFile(t *testing.T) {
	ctx := context.Background()
	yt := youtube.New(&ctx, false)

	tempDir := t.TempDir()
	cookieFile := tempDir + "/empty_cookies.txt"

	file, err := os.Create(cookieFile)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	_ = file.Close()

	err = yt.AddCookies(cookieFile)
	if err != nil {
		t.Errorf("AddCookies failed on empty file: %v", err)
	}
}

func TestAddCookiesValidFormat(t *testing.T) {
	ctx := context.Background()
	yt := youtube.New(&ctx, false)

	tempDir := t.TempDir()
	cookieFile := tempDir + "/cookies.txt"

	cookieContent := `.youtube.com	TRUE	/	TRUE	1735689600	test_cookie	test_value
# comment line
.youtube.com	TRUE	/	FALSE	1735689600	another_cookie	another_value`

	err := os.WriteFile(cookieFile, []byte(cookieContent), 0644)
	if err != nil {
		t.Fatalf("failed to write cookie file: %v", err)
	}

	err = yt.AddCookies(cookieFile)
	if err != nil {
		t.Errorf("AddCookies failed: %v", err)
	}
}

func TestYoutubeFetchChannelInfoInvalidURL(t *testing.T) {
	ctx := context.Background()
	yt := youtube.New(&ctx, false)

	_, err := yt.FetchChannelInfo("invalid-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestFetchLiveChatInvalidURL(t *testing.T) {
	ctx := context.Background()
	yt := youtube.New(&ctx, false)

	_, err := yt.FetchLiveChat("invalid-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func BenchmarkNewYoutube(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		youtube.New(&ctx, false)
	}
}
