package youtube

import (
	"bytes"
	"regexp"
	"sync"
	"time"
)

const (
	// Regular expressions for parsing YouTube responses
	regexFirstChat   = `\[\[\d+,\[\[null,null,\["([^"]+)"\]\]\]\]`
	regexNoChat      = `\[\[\d*,\[\[\[\[.*\[null,null,\["\d*`
	regexChat        = `\d{16,}`
	regexSession     = `\w{8,}`
	regexYTCfg       = `ytcfg\.set\((\{.*?})\);`
	regexInitialData = `(?s)(?:window\s*\[\s*["']ytInitialData["']\s*\]|ytInitialData)\s*=\s*({.+?})\s*;`

	// YouTube API endpoints
	youtubeBaseURL       = "https://www.youtube.com"
	youtubeAPIURL        = "https://www.youtube.com/youtubei/v1"
	youtubeSignalerURL   = "https://signaler-pa.youtube.com"
	youtubeChannelURL    = "https://youtube.com/channel/%s"
	liveChatEndpoint     = youtubeAPIURL + "/live_chat/get_live_chat?prettyPrint=false"
	nextEndpoint         = youtubeAPIURL + "/next?prettyPrint=false"
	liveChatPageURL      = youtubeBaseURL + "/live_chat?continuation=%s"
	chooseServerURL      = youtubeSignalerURL + "/punctual/v1/chooseServer?key=%s"
	refreshCredsURL      = youtubeSignalerURL + "/punctual/v1/refreshCreds?key=%s&gsessionid=%s"
	multiWatchChannelFmt = youtubeSignalerURL + "/punctual/multi-watch/channel?VER=8&gsessionid=%s&key=%s&RID=rpc&SID=%s&AID=0&CI=0&TYPE=xmlhttp&zx=%s&t=1"
	getSIDURL            = youtubeSignalerURL + "/punctual/multi-watch/channel?VER=8&gsessionid=%s&key=%s&RID=6167&CVER=22&zx=%s&t=1"

	// User agent strings
	defaultUserAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
	signalerUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

	// HTTP configuration
	maxResponseHeaderBytes = 1 << 20
	maxResponseBodyBytes   = 2 << 20
	bufferInitialSize      = 32 * 1024

	// Timing configuration
	defaultHTTPTimeout    = 300 * time.Second
	reconnectDelay        = 500 * time.Millisecond
	credRefreshInterval   = 4 * time.Minute
	maxCredRefreshes      = 4
	maxConnectionDuration = 4*time.Minute + 58*time.Second // Reconnect 2 seconds before YouTube 5-min timeout

	// Retry configuration
	maxRetries     = 3
	baseRetryDelay = 1 * time.Second
	maxRetryDelay  = 16 * time.Second

	// Stream configuration
	minResponseLength    = 10
	streamReadTimeout    = 120 * time.Second
	maxConsecutiveErrors = 5

	// Message handling
	initialMsgCapacity   = 128
	messageCleanupAfter  = 30 * time.Minute
	cleanupInterval      = 5 * time.Minute // Cleanup more frequently for smaller batches and less lag
	maxSeenMessageIDs    = 10000           // Maximum size of seenMessageIDs map to prevent unbounded growth
	defaultChannelBuffer = 100             // Buffered channel for high-traffic streams (prevents message drops)
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
