package scrapchat

import (
	"context"
	"errors"
	"time"

	"github.com/xorvus/scrap-chat/internal/fetchers/youtube"
	plf "github.com/xorvus/scrap-chat/pkg/platform"
	"github.com/xorvus/scrap-chat/types"
)

// Package scrapchat provides a unified interface for scraping chat messages
// from various live streaming platforms without requiring authentication.

const (
	platformYoutube = "youtube"
)

var (
	ErrUnsupportedPlatform = errors.New("platform not supported")
	ErrInvalidOptions      = errors.New("invalid options")
	ErrNilScrapper         = errors.New("scrapper not initialized")
)

// ScrapChat is the main client for scraping chat messages from streaming platforms.
type ScrapChat struct {
	platform string
	scrapper plf.ChatFetcher
}

// Options configures the behavior of the ScrapChat client.
type Options struct {
	Context context.Context
	Verbose bool
}

// New creates a new ScrapChat client for the specified platform.
// Supported platforms: "youtube"
// Options can be passed as bool (for verbose), context.Context, or *Options.
func New(platform string, opts ...any) (*ScrapChat, error) {
	options := parseOptions(opts...)
	scrapper, err := createScrapper(platform, options)
	if err != nil {
		return nil, err
	}

	return &ScrapChat{
		platform: platform,
		scrapper: scrapper,
	}, nil
}

func parseOptions(opts ...any) *Options {
	options := &Options{
		Context: context.Background(),
		Verbose: false,
	}

	for _, opt := range opts {
		switch v := opt.(type) {
		case bool:
			options.Verbose = v
		case context.Context:
			options.Context = v
		case *Options:
			return v
		}
	}

	return options
}

func createScrapper(platform string, opts *Options) (plf.ChatFetcher, error) {
	switch platform {
	case platformYoutube:
		return youtube.New(&opts.Context, opts.Verbose), nil
	default:
		return nil, ErrUnsupportedPlatform
	}
}

// AddCookies adds authentication cookies from the specified file path.
// This allows access to members-only or restricted streams.
func (s *ScrapChat) AddCookies(path string) error {
	if s.scrapper == nil {
		return ErrNilScrapper
	}
	if path == "" {
		return errors.New("cookie path cannot be empty")
	}
	return s.scrapper.AddCookies(path)
}

// FetchLiveChat fetches live chat messages from an active stream.
// Returns a channel that streams live chat messages as they arrive.
// The channel is closed when the stream ends or an error occurs.
func (s *ScrapChat) FetchLiveChat(streamID string) (<-chan *types.LiveChatMessage, error) {
	if s.scrapper == nil {
		return nil, ErrNilScrapper
	}
	if streamID == "" {
		return nil, errors.New("stream ID cannot be empty")
	}
	return s.scrapper.FetchLiveChat(streamID)
}

// FetchVideoComments fetches comments from a video.
// Optionally filter comments posted after the specified date.
// Returns a channel that streams comments sequentially.
func (s *ScrapChat) FetchVideoComments(streamID string, date *time.Time) (<-chan *types.ChatMessage, error) {
	if s.scrapper == nil {
		return nil, ErrNilScrapper
	}
	if streamID == "" {
		return nil, errors.New("stream ID cannot be empty")
	}
	return s.scrapper.FetchVideoComments(streamID, date)
}

// FetchChannelInfo retrieves basic information about a channel.
// Accepts either a channel URL or handle (e.g., "@channelname").
func (s *ScrapChat) FetchChannelInfo(path string) (*types.ChannelInfo, error) {
	if s.scrapper == nil {
		return nil, ErrNilScrapper
	}
	if path == "" {
		return nil, errors.New("channel path cannot be empty")
	}
	return s.scrapper.FetchChannelInfo(path)
}

// Platform returns the name of the platform this client is configured for.
func (s *ScrapChat) Platform() string {
	return s.platform
}
