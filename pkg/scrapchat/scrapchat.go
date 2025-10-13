package scrapchat

import (
	"context"
	"errors"
	"time"

	"github.com/xorvus/scrap-chat/internal/fetchers"
	plf "github.com/xorvus/scrap-chat/pkg/platform"
	"github.com/xorvus/scrap-chat/types"
)

const (
	platformYoutube = "youtube"
)

var (
	ErrUnsupportedPlatform = errors.New("platform not supported")
	ErrInvalidOptions      = errors.New("invalid options")
)

type ScrapChat struct {
	platform string
	scrapper plf.ChatFetcher
}

type Options struct {
	Context context.Context
	Verbose bool
}

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
		return fetchers.NewYoutube(&opts.Context, opts.Verbose), nil
	default:
		return nil, ErrUnsupportedPlatform
	}
}

func (s *ScrapChat) AddCookies(path string) error {
	if s.scrapper == nil {
		return errors.New("scrapper not initialized")
	}
	return s.scrapper.AddCookies(path)
}

func (s *ScrapChat) FetchLiveChat(streamID string) (<-chan *types.LiveChatMessage, error) {
	if s.scrapper == nil {
		return nil, errors.New("scrapper not initialized")
	}
	return s.scrapper.FetchLiveChat(streamID)
}

func (s *ScrapChat) FetchVideoComments(streamID string, date *time.Time) (<-chan *types.ChatMessage, error) {
	if s.scrapper == nil {
		return nil, errors.New("scrapper not initialized")
	}
	return s.scrapper.FetchVideoComments(streamID, date)
}

func (s *ScrapChat) FetchChannelInfo(path string) (*types.ChannelInfo, error) {
	if s.scrapper == nil {
		return nil, errors.New("scrapper not initialized")
	}
	return s.scrapper.FetchChannelInfo(path)
}

func (s *ScrapChat) Platform() string {
	return s.platform
}
