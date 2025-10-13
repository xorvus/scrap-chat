package tests

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/xorvus/scrap-chat/internal/fetchers"
)

func TestNewYoutube(t *testing.T) {
	ctx := context.Background()
	yt := fetchers.NewYoutube(&ctx, false)

	if yt == nil {
		t.Error("NewYoutube() returned nil")
	}
}

func TestNewYoutubeWithVerbose(t *testing.T) {
	ctx := context.Background()
	yt := fetchers.NewYoutube(&ctx, true)

	if yt == nil {
		t.Error("NewYoutube() with verbose returned nil")
	}
}

func TestAddCookiesInvalidFile(t *testing.T) {
	ctx := context.Background()
	yt := fetchers.NewYoutube(&ctx, false)

	err := yt.AddCookies("/nonexistent/cookies.txt")
	if err == nil {
		t.Error("expected error for nonexistent cookie file")
	}
}

func TestAddCookiesEmptyFile(t *testing.T) {
	ctx := context.Background()
	yt := fetchers.NewYoutube(&ctx, false)

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
	yt := fetchers.NewYoutube(&ctx, false)

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
	yt := fetchers.NewYoutube(&ctx, false)

	_, err := yt.FetchChannelInfo("invalid-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestFetchLiveChatInvalidURL(t *testing.T) {
	ctx := context.Background()
	yt := fetchers.NewYoutube(&ctx, false)

	_, err := yt.FetchLiveChat("invalid-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestIsRegexTrue(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected bool
	}{
		{"match digits", `\d+`, "12345", true},
		{"no match", `\d+`, "abcde", false},
		{"empty string", `\d+`, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := regexp.MustCompile(tt.pattern)
			result := fetchers.IsRegexTrue(re, tt.input)
			if result != tt.expected {
				t.Errorf("IsRegexTrue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRegexGetValue(t *testing.T) {
	tests := []struct {
		name          string
		pattern       string
		input         string
		expectMatch   bool
		expectedCount int
	}{
		{"find digits", `\d+`, "abc123def456", true, 2},
		{"no match", `\d+`, "abcdef", false, 0},
		{"empty input", `\d+`, "", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := regexp.MustCompile(tt.pattern)
			ok, matches := fetchers.RegexGetValue(re, tt.input)
			if ok != tt.expectMatch {
				t.Errorf("RegexGetValue() match = %v, want %v", ok, tt.expectMatch)
			}
			if len(matches) != tt.expectedCount {
				t.Errorf("RegexGetValue() count = %d, want %d", len(matches), tt.expectedCount)
			}
		})
	}
}

func BenchmarkNewYoutube(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		fetchers.NewYoutube(&ctx, false)
	}
}
