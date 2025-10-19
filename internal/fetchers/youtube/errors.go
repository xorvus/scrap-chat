package youtube

import (
	"errors"
	"fmt"
)

var (
	ErrStreamNotLive      = errors.New("stream not live")
	ErrNoComments         = errors.New("no comments available")
	ErrNoContinuation     = errors.New("no continuation token")
	ErrInvalidResponse    = errors.New("invalid response")
	ErrMaxRetriesExceeded = errors.New("max retries exceeded")
	ErrInvalidURL         = errors.New("invalid URL")
	ErrChannelNotFound    = errors.New("channel not found")
	ErrVideoNotFound      = errors.New("video not found")
)

type YouTubeError struct {
	Op  string
	Err error
	Msg string
}

func (e *YouTubeError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("youtube %s: %s: %v", e.Op, e.Msg, e.Err)
	}
	return fmt.Sprintf("youtube %s: %v", e.Op, e.Err)
}

func (e *YouTubeError) Unwrap() error {
	return e.Err
}
