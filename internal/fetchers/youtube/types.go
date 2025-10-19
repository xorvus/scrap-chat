package youtube

import (
	"bytes"
	"regexp"
	"sync"
	"time"
)

const (
	regexFirstChat   = `\[\[\d+,\[\[null,null,\["([^"]+)"\]\]\]\]`
	regexNoChat      = `\[\[\d*,\[\[\[\[.*\[null,null,\["\d*`
	regexChat        = `\d{16,}`
	regexSession     = `\w{8,}`
	regexYTCfg       = `ytcfg\.set\((\{.*?})\);`
	regexInitialData = `(?s)(?:window\s*\[\s*["']ytInitialData["']\s*\]|ytInitialData)\s*=\s*({.+?})\s*;`

	youtubeBaseURL   = "https://www.youtube.com"
	youtubeAPIURL    = "https://www.youtube.com/youtubei/v1"
	liveChatEndpoint = youtubeAPIURL + "/live_chat/get_live_chat?prettyPrint=false"
	nextEndpoint     = youtubeAPIURL + "/next?prettyPrint=false"

	maxResponseHeaderBytes = 1 << 20
	maxResponseBodyBytes   = 2 << 20
	bufferInitialSize      = 32 * 1024

	defaultHTTPTimeout  = 300 * time.Second
	reconnectDelay      = 500 * time.Millisecond
	credRefreshInterval = 4 * time.Minute
	maxCredRefreshes    = 4

	maxRetries     = 3
	baseRetryDelay = 1 * time.Second
	maxRetryDelay  = 16 * time.Second

	minResponseLength    = 10
	streamReadTimeout    = 120 * time.Second
	maxConsecutiveErrors = 5

	initialMsgCapacity  = 128
	messageCleanupAfter = 30 * time.Minute
	cleanupInterval     = 10 * time.Minute
)

var (
	regFirstChat     = regexp.MustCompile(regexFirstChat)
	regNoChat        = regexp.MustCompile(regexNoChat)
	regChat          = regexp.MustCompile(regexChat)
	regSession       = regexp.MustCompile(regexSession)
	ytCfgRegex       = regexp.MustCompile(regexYTCfg)
	initialDataRegex = regexp.MustCompile(regexInitialData)

	bufferPool = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, bufferInitialSize))
		},
	}
)

type StreamState int

const (
	StreamStateDisconnected StreamState = iota
	StreamStateConnecting
	StreamStateConnected
	StreamStateReading
	StreamStateError
	StreamStateRecovering
)

func (s StreamState) String() string {
	switch s {
	case StreamStateDisconnected:
		return "Disconnected"
	case StreamStateConnecting:
		return "Connecting"
	case StreamStateConnected:
		return "Connected"
	case StreamStateReading:
		return "Reading"
	case StreamStateError:
		return "Error"
	case StreamStateRecovering:
		return "Recovering"
	default:
		return "Unknown"
	}
}

type StreamHealth struct {
	ConnectedAt       time.Time
	LastReadAt        time.Time
	BytesRead         int64
	MessagesRead      int64
	ErrorsCount       int64
	ConsecutiveErrors int
	LastError         error
	AverageReadTime   time.Duration
	MaxReadTime       time.Duration
	readTimeSum       time.Duration
	readCount         int64
}

type readResult struct {
	line string
	err  error
}

type MessageOptions struct {
	Timestamp string
	IsTimeout bool
	IsFirst   bool
}