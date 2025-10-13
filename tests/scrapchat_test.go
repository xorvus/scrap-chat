package tests

import (
	"testing"
	"time"

	"github.com/xorvus/scrap-chat/pkg/scrapchat"
	"github.com/xorvus/scrap-chat/types"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name      string
		platform  string
		shouldErr bool
	}{
		{"valid youtube platform", "youtube", false},
		{"invalid platform", "invalid", true},
		{"empty platform", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chat, err := scrapchat.New(tt.platform)
			if (err != nil) != tt.shouldErr {
				t.Errorf("New() error = %v, shouldErr %v", err, tt.shouldErr)
			}
			if !tt.shouldErr && chat == nil {
				t.Error("expected non-nil ScrapChat for valid platform")
			}
		})
	}
}

func TestNewWithVerbose(t *testing.T) {
	chat, err := scrapchat.New("youtube", true)
	if err != nil {
		t.Fatalf("New() with verbose failed: %v", err)
	}
	if chat == nil {
		t.Error("expected non-nil ScrapChat")
	}
}

func TestPlatform(t *testing.T) {
	chat, err := scrapchat.New("youtube")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	platform := chat.Platform()
	if platform != "youtube" {
		t.Errorf("Platform() = %q, want %q", platform, "youtube")
	}
}

func TestAddCookiesInvalidPath(t *testing.T) {
	chat, err := scrapchat.New("youtube")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	err = chat.AddCookies("/invalid/path/cookies.txt")
	if err == nil {
		t.Error("expected error for invalid cookie path")
	}
}

func TestFetchChannelInfoInvalidURL(t *testing.T) {
	chat, err := scrapchat.New("youtube")
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	_, err = chat.FetchChannelInfo("invalid-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

type mockChatFetcher struct{}

func (m *mockChatFetcher) AddCookies(path string) error {
	return nil
}

func (m *mockChatFetcher) FetchLiveChat(streamID string) (<-chan *types.LiveChatMessage, error) {
	ch := make(chan *types.LiveChatMessage)
	close(ch)
	return ch, nil
}

func (m *mockChatFetcher) FetchVideoComments(streamID string, date *time.Time) (<-chan *types.ChatMessage, error) {
	ch := make(chan *types.ChatMessage)
	close(ch)
	return ch, nil
}

func (m *mockChatFetcher) FetchChannelInfo(path string) (*types.ChannelInfo, error) {
	return &types.ChannelInfo{
		ID:   "test-id",
		Name: "Test Channel",
	}, nil
}

func BenchmarkNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		scrapchat.New("youtube")
	}
}
