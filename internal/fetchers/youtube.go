package fetchers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tidwall/gjson"
	"github.com/xorvus/scrap-chat/internal/utils"
	"github.com/xorvus/scrap-chat/types"
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

	initialMsgCapacity = 128
)

var (
	regFirstChat     = regexp.MustCompile(regexFirstChat)
	regNoChat        = regexp.MustCompile(regexNoChat)
	regChat          = regexp.MustCompile(regexChat)
	regSession       = regexp.MustCompile(regexSession)
	ytCfgRegex       = regexp.MustCompile(regexYTCfg)
	initialDataRegex = regexp.MustCompile(regexInitialData)
	ErrStreamNotLive = errors.New("stream not live")
	bufferPool       = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, bufferInitialSize))
		},
	}
)

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

type Youtube struct {
	cookies            []*http.Cookie
	config             *types.YTCgf
	continuation       string
	videoID            string
	gsessionID         string
	sid                string
	httpClient         *http.Client
	header             http.Header
	cookieString       string
	timeout            int
	isInvalidationData bool
	session            string
	ctx                *context.Context
	verbose            bool
	streamHealth       *StreamHealth
	streamState        StreamState
	streamMutex        sync.RWMutex
	seenMessageIDs     map[string]time.Time
	lastCleanupTime    time.Time
}

func NewYoutube(ctx *context.Context, verbose bool) *Youtube {
	y := &Youtube{
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxResponseHeaderBytes: maxResponseHeaderBytes,
				IdleConnTimeout:        defaultHTTPTimeout,
				TLSHandshakeTimeout:    10 * time.Second,
				ResponseHeaderTimeout:  defaultHTTPTimeout,
				ExpectContinueTimeout:  1 * time.Second,
				DisableKeepAlives:      false,
				MaxIdleConns:           10,
				MaxIdleConnsPerHost:    5,
			},
			Timeout: defaultHTTPTimeout,
		},
		ctx:             ctx,
		verbose:         verbose,
		streamHealth:    &StreamHealth{},
		streamState:     StreamStateDisconnected,
		seenMessageIDs:  make(map[string]time.Time),
		lastCleanupTime: time.Now(),
	}
	y.header = make(http.Header)
	defaultHeaders(y.header)
	return y
}

func defaultHeaders(h http.Header) {
	headers := map[string]string{
		"accept":                      "*/*",
		"accept-language":             "en-US,en;q=0.9",
		"cache-control":               "no-cache",
		"origin":                      "https://www.youtube.com ",
		"priority":                    "u=1, i",
		"pragma":                      "no-cache",
		"referer":                     "https://www.youtube.com/ ",
		"sec-ch-ua":                   "\"Chromium\";v=\"136\", \"Brave\";v=\"136\", \"Not.A/Brand\";v=\"99\"",
		"sec-ch-ua-arch":              "\"arm\"",
		"sec-ch-ua-bitness":           "\"64\"",
		"sec-ch-ua-full-version-list": "\"Chromium\";v=\"136.0.0.0\", \"Brave\";v=\"136.0.0.0\", \"Not.A/Brand\";v=\"99.0.0.0\"",
		"sec-ch-ua-mobile":            "?0",
		"sec-ch-ua-model":             "\"\"",
		"sec-ch-ua-platform":          "\"macOS\"",
		"sec-ch-ua-platform-version":  "\"15.4.0\"",
		"sec-ch-ua-wow64":             "?0",
		"sec-fetch-dest":              "empty",
		"sec-fetch-mode":              "cors",
		"sec-fetch-site":              "same-site",
		"sec-gpc":                     "1",
		"user-agent":                  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
	}
	for k, v := range headers {
		h.Set(k, v)
	}
}

func (y *Youtube) AddCookies(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open cookie file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Error closing cookie file: %v", err)
		}
	}()
	var cookies []*http.Cookie
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 7 {
			continue
		}
		timestamp, err := strconv.ParseInt(parts[4], 10, 64)
		if err != nil {
			log.Printf("Error parsing cookie time: %v", err)
			continue
		}
		cookies = append(cookies, &http.Cookie{
			Domain:  parts[0],
			Path:    parts[2],
			Name:    parts[5],
			Value:   parts[6],
			Expires: time.Unix(timestamp, 0),
			Secure:  parts[3] == "TRUE",
		})
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading cookie file: %w", err)
	}
	y.cookies = cookies
	y.cookieString = createCookieString(cookies)
	return nil
}

func createCookieString(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, fmt.Sprintf("%s=%s", c.Name, c.Value))
	}
	return strings.Join(parts, "; ")
}

func (y *Youtube) FetchVideoComments(_ string, _ *time.Time) (<-chan *types.ChatMessage, error) {
	return nil, nil
}

func (y *Youtube) FetchLiveChat(path string) (<-chan *types.LiveChatMessage, error) {
	url := path
	if strings.Contains(url, "@") {
		info, err := y.FetchChannelInfo(path)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch channel info: %w", err)
		}
		url = info.URL + "/live"
	}

	if err := y.getConfig(url); err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	_, err := y.sendMessage(&MessageOptions{
		"check", false, true,
	})

	if err != nil {
		return nil, err
	}

	if !y.isInvalidationData && y.timeout == 0 {
		return nil, ErrStreamNotLive
	}

	if y.isInvalidationData {
		y.chooseServer()
		y.getSID()
	}

	msg := make(chan *types.LiveChatMessage)

	go func() {
		defer close(msg)

		y.streamChat(func(params []types.YTChatMessage) {
			for _, param := range params {
				userImage := ""
				if len(param.Author.AuthorImages) > 0 {
					userImage = param.Author.AuthorImages[0].URL
				}

				badges := make([]types.Badge, 0, len(param.Author.Badges))
				for _, badge := range param.Author.Badges {
					iconURL := ""
					if len(badge.LiveChatAuthorBadgeRenderer.CustomThumbnail.Thumbnails) > 0 {
						iconURL = badge.LiveChatAuthorBadgeRenderer.CustomThumbnail.Thumbnails[0].URL
					}
					badges = append(badges, types.Badge{
						Tooltip: badge.LiveChatAuthorBadgeRenderer.Tooltip,
						Label:   badge.LiveChatAuthorBadgeRenderer.Accessibility.AccessibilityData.Label,
						IconURL: iconURL,
					})
				}

				msg <- &types.LiveChatMessage{
					ID:      param.ID,
					Message: param.Message,
					Author: types.Author{
						ID:        param.Author.AuthorID,
						Name:      param.Author.AuthorName,
						Thumbnail: userImage,
						URL:       fmt.Sprintf("https://youtube.com/channel/%s", param.Author.AuthorID),
						Badges:    badges,
						Ranking:   param.Author.Ranking,
					},
					Timestamp: param.Timestamp.Unix(),
				}

				if !y.isInvalidationData {
					time.Sleep(time.Duration(y.timeout/len(params)) * time.Millisecond)
				} else {
					time.Sleep(50 * time.Millisecond)
				}

			}
		})
	}()

	return msg, nil
}

func (y *Youtube) FetchLive() {

}

func (y *Youtube) FetchChannelInfo(path string) (*types.ChannelInfo, error) {
	if !strings.HasPrefix(path, "http") && strings.Contains(path, "@") {
		path = youtubeBaseURL + "/" + path
	}

	info := &types.ChannelInfo{}

	client := &http.Client{Timeout: defaultHTTPTimeout}
	resp, err := client.Get(path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	y.extractChannelMetadata(doc, info)

	if info.URL == "" {
		return nil, fmt.Errorf("could not extract channel URL from page")
	}

	return info, nil
}

func (y *Youtube) extractChannelMetadata(doc *goquery.Document, info *types.ChannelInfo) {
	doc.Find("meta[property='og:title']").Each(func(i int, s *goquery.Selection) {
		info.Name = s.AttrOr("content", "")
	})

	doc.Find("meta[property='og:image']").Each(func(i int, s *goquery.Selection) {
		info.Image = s.AttrOr("content", "")
	})

	doc.Find("meta[property='og:description']").Each(func(i int, s *goquery.Selection) {
		info.Description = s.AttrOr("content", "")
	})

	doc.Find("meta[property='og:url']").Each(func(i int, s *goquery.Selection) {
		info.URL = s.AttrOr("content", "")
		if strings.Contains(info.URL, "/channel/") {
			info.ID = strings.SplitN(info.URL, "/channel/", 2)[1]
		}
	})
}

func (y *Youtube) getConfig(url string) error {
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("error visiting URL: %w", err)
	}

	if resp == nil {
		log.Printf("Response is nil for URL: %s", url)
		return fmt.Errorf("received nil response from URL: %s", url)
	}

	if resp.Body == nil {
		log.Printf("Response body is nil for URL: %s", url)
		return fmt.Errorf("received nil response body from URL: %s", url)
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			if err := resp.Body.Close(); err != nil {
				log.Printf("Error closing response body: %v", err)
			}
		}
	}()

	limited := io.LimitReader(resp.Body, maxResponseBodyBytes)
	buffer := bufferPool.Get().(*bytes.Buffer)

	chunk := make([]byte, 4096)
	defer func() {
		resp = nil
		buffer.Reset()
		bufferPool.Put(buffer)
		chunk = nil
	}()

	foundCfg := false
	foundInitial := false
	config := &types.YTCgf{}

	for {
		n, err := limited.Read(chunk)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		buffer.Write(chunk[:n])

		if !foundCfg {
			foundCfg = processConfigRegex(buffer, ytCfgRegex, config)
		}

		if !foundInitial {
			foundInt, cont, vid := processInitialDataRegex(buffer, initialDataRegex)
			y.continuation = strings.Clone(cont)
			y.videoID = strings.Clone(vid)
			foundInitial = foundInt
		}

		if foundCfg && foundInitial {
			break
		}
	}

	y.config = &types.YTCgf{
		INNERTUBE_API_KEY:        config.INNERTUBE_API_KEY,
		API_KEY:                  config.API_KEY,
		INNERTUBE_CONTEXT:        config.INNERTUBE_CONTEXT,
		INNERTUBE_CLIENT_VERSION: config.INNERTUBE_CLIENT_VERSION,
		ID_TOKEN:                 config.ID_TOKEN,
	}

	config = nil

	return nil
}

func processConfigRegex(buffer *bytes.Buffer, regex *regexp.Regexp, config *types.YTCgf) bool {
	data := buffer.Bytes()
	match := regex.FindSubmatch(data)
	if len(match) < 2 {
		return false
	}

	jsonBytes := match[1]
	config.INNERTUBE_API_KEY = gjson.GetBytes(jsonBytes, "INNERTUBE_API_KEY").String()
	config.API_KEY = gjson.GetBytes(jsonBytes, "LIVE_CHAT_BASE_TANGO_CONFIG.apiKey").String()
	config.INNERTUBE_CLIENT_VERSION = gjson.GetBytes(jsonBytes, "INNERTUBE_CLIENT_VERSION").String()
	config.ID_TOKEN = gjson.GetBytes(jsonBytes, "ID_TOKEN").String()

	contextResult := gjson.GetBytes(jsonBytes, "INNERTUBE_CONTEXT")
	if !contextResult.Exists() {
		return false
	}

	if err := json.Unmarshal([]byte(contextResult.Raw), &config.INNERTUBE_CONTEXT); err != nil {
		log.Printf("Error parsing INNERTUBE_CONTEXT: %v", err)
		return false
	}

	return true
}

func processInitialDataRegex(buffer *bytes.Buffer, regex *regexp.Regexp) (bool, string, string) {
	data := buffer.Bytes()
	match := regex.FindSubmatch(data)
	if len(match) < 2 {
		return false, "", ""
	}

	jsonBytes := match[1]
	continuationStr := gjson.GetBytes(jsonBytes, "contents.twoColumnWatchNextResults.conversationBar.liveChatRenderer.header.liveChatHeaderRenderer.viewSelector.sortFilterSubMenuRenderer.subMenuItems.1.continuation.reloadContinuationData.continuation").String()
	videoIdStr := gjson.GetBytes(jsonBytes, "currentVideoEndpoint.watchEndpoint.videoId").String()

	return true, continuationStr, videoIdStr
}

func (y *Youtube) streamChat(param func([]types.YTChatMessage)) {
	if y.isInvalidationData {
		lastTime := time.Now().Unix()
		y.longPolling(func(res string) {
			tempTime := time.Now()
			diff := tempTime.Sub(time.Unix(lastTime, 0))

			if y.verbose && int(diff.Seconds())%30 == 0 {
				y.LogStreamSummary()
			}

			switch {
			case IsRegexTrue(regFirstChat, res):
				log.Printf("[STREAM] Detected first chat message, processing session")
				go func() {
					_, match := RegexGetValue(regSession, res)
					if len(match) == 0 {
						log.Printf("[STREAM] Regex match for session failed. Response length: %d, preview: %.100s", len(res), res)
						return
					}
					y.session = match[0]
					log.Printf("[STREAM] Session extracted: %s", y.session)
				}()

				res, err := y.sendMessage(&MessageOptions{
					Timestamp: "",
					IsTimeout: false,
					IsFirst:   true,
				})
				if err != nil {
					log.Printf("[STREAM] Error sending first chat message: %v", err)
				} else {
					param(res)
				}

			case diff >= 10*time.Second:
				log.Printf("[STREAM] Sending timeout message after %v inactivity", diff)
				res, err := y.sendMessage(&MessageOptions{
					Timestamp: "",
					IsTimeout: true,
					IsFirst:   false,
				})
				if err != nil {
					log.Printf("[STREAM] Error sending timeout message: %v", err)
				} else {
					param(res)
				}

			case IsRegexTrue(regNoChat, res):
				if y.verbose {
					log.Printf("[STREAM] No chat detected")
				}

			case func() bool {
				ok, match := RegexGetValue(regChat, res)
				if ok {
					if y.verbose {
						log.Printf("[STREAM] Chat message detected with timestamp: %s", match[0])
					}
					res, err := y.sendMessage(&MessageOptions{
						Timestamp: match[0],
						IsTimeout: false,
						IsFirst:   false,
					})
					if err != nil {
						log.Printf("[STREAM] Error sending chat message: %v", err)
					} else {
						param(res)
					}
				}
				return ok
			}():

			default:
				y.logStreamError(fmt.Errorf("undefined response pattern"), "response processing")

			if len(res) == 0 {
				log.Printf("[STREAM] Empty response received")
			} else if len(res) < 10 {
				log.Printf("[STREAM] Very short response: '%s' (length: %d)", res, len(res))
			} else if strings.Contains(res, "error") || strings.Contains(res, "ERROR") {
				log.Printf("[STREAM] Error response detected: %s", res)
			} else {
				log.Printf("[STREAM] Undefined response format - Length: %d, Time since last: %v", len(res), diff)
				if y.verbose {
					preview := res
					if len(preview) > 200 {
						preview = preview[:200] + "..."
					}
					log.Printf("[STREAM] Response preview: %s", preview)
				}
			}

				health := y.GetStreamHealth()
				if health.ConsecutiveErrors > 3 {
					log.Printf("[STREAM] Multiple undefined responses detected (%d), connection may be unstable", health.ConsecutiveErrors)
				}
			}

			lastTime = tempTime.Unix()
		})
	} else {
		log.Printf("[STREAM] Using timed continuation mode")
		for {
			time.Sleep(time.Duration(y.timeout) * time.Millisecond)
			res, err := y.sendMessage(&MessageOptions{
				Timestamp: "",
				IsTimeout: false,
				IsFirst:   true,
			})
			if err != nil {
				log.Printf("[STREAM] Error in timed mode: %v", err)
				continue
			}
			go func() {
				param(res)
			}()
		}
	}
}

func IsRegexTrue(r *regexp.Regexp, str string) bool {
	return r.MatchString(str)
}

func RegexGetValue(re *regexp.Regexp, data string) (bool, []string) {
	match := re.FindAllString(data, -1)
	if len(match) > 0 {
		return true, match
	}
	return false, nil
}

func (y *Youtube) copyHeaders(req *http.Request, h http.Header) {
	for k, vv := range h {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Cookie", y.cookieString)
}

func (y *Youtube) updateStreamState(state StreamState, reason string) {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	oldState := y.streamState
	y.streamState = state

	if y.verbose || state == StreamStateError {
		log.Printf("[STREAM] State changed: %s -> %s (%s)", oldState, state, reason)
	}
}

func (y *Youtube) logStreamRead(line string, readTime time.Duration, bytesRead int) {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	y.streamHealth.LastReadAt = time.Now()
	y.streamHealth.BytesRead += int64(bytesRead)
	y.streamHealth.MessagesRead++
	y.streamHealth.readTimeSum += readTime
	y.streamHealth.readCount++

	if readTime > y.streamHealth.MaxReadTime {
		y.streamHealth.MaxReadTime = readTime
	}

	if y.streamHealth.readCount > 0 {
		y.streamHealth.AverageReadTime = y.streamHealth.readTimeSum / time.Duration(y.streamHealth.readCount)
	}

	if y.verbose {
		log.Printf("[STREAM] Read: %d bytes in %v (avg: %v, total: %d bytes, %d messages)",
			bytesRead, readTime, y.streamHealth.AverageReadTime, y.streamHealth.BytesRead, y.streamHealth.MessagesRead)

		if len(line) > 0 {
			preview := line
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			log.Printf("[STREAM] Data preview: %s", preview)
		}
	}
}

func (y *Youtube) logStreamError(err error, context string) {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	y.streamHealth.ErrorsCount++
	y.streamHealth.ConsecutiveErrors++
	y.streamHealth.LastError = err

	log.Printf("[STREAM] Error in %s: %v (consecutive errors: %d, total errors: %d)",
		context, err, y.streamHealth.ConsecutiveErrors, y.streamHealth.ErrorsCount)

	if y.streamHealth.ConsecutiveErrors >= maxConsecutiveErrors {
		y.updateStreamState(StreamStateError, fmt.Sprintf("Too many consecutive errors: %d", maxConsecutiveErrors))
	}
}

func (y *Youtube) resetStreamHealth() {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	now := time.Now()
	y.streamHealth = &StreamHealth{
		ConnectedAt: now,
		LastReadAt:  now,
	}
	y.streamHealth.ConsecutiveErrors = 0

	log.Printf("[STREAM] Health metrics reset")
}

func (y *Youtube) isValidResponse(response string) bool {
	if len(response) < minResponseLength {
		if y.verbose {
			log.Printf("[STREAM] Skipping short response: %d bytes", len(response))
		}
		return false
	}

	if strings.TrimSpace(response) == "" {
		if y.verbose {
			log.Printf("[STREAM] Skipping empty/whitespace response")
		}
		return false
	}

	lowerResponse := strings.ToLower(response)
	errorPatterns := []string{"error", "not found", "forbidden", "unauthorized"}
	for _, pattern := range errorPatterns {
		if strings.Contains(lowerResponse, pattern) && len(response) < 100 {
			if y.verbose {
				log.Printf("[STREAM] Skipping error response: %s", pattern)
			}
			return false
		}
	}

	return true
}

func (y *Youtube) GetStreamHealth() *StreamHealth {
	y.streamMutex.RLock()
	defer y.streamMutex.RUnlock()

	health := *y.streamHealth
	return &health
}

func (y *Youtube) GetStreamState() StreamState {
	y.streamMutex.RLock()
	defer y.streamMutex.RUnlock()
	return y.streamState
}

func (y *Youtube) LogStreamSummary() {
	health := y.GetStreamHealth()
	state := y.GetStreamState()

	uptime := time.Since(health.ConnectedAt)
	if uptime < time.Second {
		uptime = 0
	}

	log.Printf("[STREAM SUMMARY] State: %s, Uptime: %v, Messages: %d, Bytes: %d, Errors: %d, Avg Read Time: %v",
		state, uptime.Round(time.Second), health.MessagesRead, health.BytesRead, health.ErrorsCount, health.AverageReadTime.Round(time.Millisecond))

	if health.LastError != nil {
		log.Printf("[STREAM SUMMARY] Last Error: %v (%d consecutive)", health.LastError, health.ConsecutiveErrors)
	}
}
func (y *Youtube) createStreamContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), streamReadTimeout)
}

func (y *Youtube) readStreamWithTimeout(reader *bufio.Reader) (string, time.Duration, error) {
	ctx, cancel := y.createStreamContext()
	defer cancel()

	startTime := time.Now()

	resultChan := make(chan readResult, 1)

	go func() {
		line, err := reader.ReadString('\n')
		resultChan <- readResult{line: line, err: err}
	}()

	select {
	case result := <-resultChan:
		readTime := time.Since(startTime)
		return result.line, readTime, result.err
	case <-ctx.Done():
		readTime := time.Since(startTime)
		return "", readTime, fmt.Errorf("stream read timeout after %v: context deadline exceeded (this may be due to network latency or server inactivity)", readTime)
	}
}

type readResult struct {
	line string
	err  error
}

func (y *Youtube) shouldRetryConnection(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	if strings.Contains(errStr, "context deadline exceeded") {
		log.Printf("[STREAM] Connection timeout detected, will retry")
		return true
	}

	if strings.Contains(errStr, "connection reset") {
		log.Printf("[STREAM] Connection reset detected, will retry")
		return true
	}

	if strings.Contains(errStr, "unexpected EOF") {
		log.Printf("[STREAM] Unexpected EOF detected, will retry")
		return true
	}

	return false
}

func (y *Youtube) executeWithRetry(url, method string) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(baseRetryDelay.Milliseconds()*int64(1<<uint(attempt-1))) * time.Millisecond
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}

			if y.verbose {
				log.Printf("Retry attempt %d/%d after %v delay", attempt, maxRetries, delay)
			}
			time.Sleep(delay)
		}

		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			lastErr = fmt.Errorf("request error: %w", err)
			continue
		}

		y.copyHeaders(req, y.header)

		resp, err := y.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP error: %w", err)

			if y.verbose {
				log.Printf("HTTP request failed on attempt %d: %v", attempt+1, err)
			}
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			if err := resp.Body.Close(); err != nil {
				log.Printf("Error closing response body: %v", err)
			}

			if y.verbose {
				log.Printf("Server error on attempt %d: %d", attempt+1, resp.StatusCode)
			}
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries (%d) exceeded, last error: %w", maxRetries, lastErr)
}

func (y *Youtube) longPolling(param func(string)) {
	if y.verbose {
		log.Println("Starting long polling with enhanced recovery...")
	}

	y.updateStreamState(StreamStateConnecting, "Starting long polling connection")
	commentCount := 0

	for {
		y.resetStreamHealth()

		url := fmt.Sprintf("https://signaler-pa.youtube.com/punctual/multi-watch/channel?VER=8&gsessionid=%s&key=%s&RID=rpc&SID=%s&AID=0&CI=0&TYPE=xmlhttp&zx=%s&t=1",
			y.gsessionID, y.config.API_KEY, y.sid, utils.GenerateZX())

		resp, err := y.executeWithRetry(url, "GET")
		if err != nil {
			y.logStreamError(err, "HTTP connection")

			if y.shouldRetryConnection(err) {
				y.updateStreamState(StreamStateRecovering, "Retrying connection after error")
				time.Sleep(reconnectDelay)
				continue
			}

			y.updateStreamState(StreamStateError, "HTTP connection failed permanently")
			log.Printf("[STREAM] HTTP error after retries: %v", err)
			return
		}

		y.updateStreamState(StreamStateConnected, "HTTP connection established")
		log.Printf("[STREAM] Connected to YouTube signaling server")

		reader := bufio.NewReader(resp.Body)
		y.updateStreamState(StreamStateReading, "Starting to read from stream")

		lastTime := time.Now().Unix()

		for {
			line, readTime, err := y.readStreamWithTimeout(reader)
			if err != nil {
				y.logStreamError(err, "stream reading")

				if err == io.EOF {
					log.Printf("[STREAM] Stream closed gracefully by server")
					y.updateStreamState(StreamStateDisconnected, "Server closed connection")
					break
				} else if strings.Contains(err.Error(), "context deadline exceeded") {
					log.Printf("[STREAM] Stream read timeout after %v, attempting recovery", readTime)
					y.updateStreamState(StreamStateRecovering, "Stream timeout, attempting recovery")

					y.streamMutex.Lock()
					y.streamHealth.ConsecutiveErrors++
					y.streamMutex.Unlock()

					if y.streamHealth.ConsecutiveErrors >= maxConsecutiveErrors {
						log.Printf("[STREAM] Too many consecutive timeouts (%d), giving up", y.streamHealth.ConsecutiveErrors)
						break
					}
					continue
				} else {
					log.Printf("[STREAM] Stream read error: %v", err)
				}

				break
			}

			originalLine := line
			line = strings.TrimSpace(line)
			bytesRead := len(originalLine)

			y.logStreamRead(line, readTime, bytesRead)

			if y.streamHealth.ConsecutiveErrors > 0 {
				y.streamMutex.Lock()
				y.streamHealth.ConsecutiveErrors = 0
				y.streamMutex.Unlock()
			}

			if !y.isValidResponse(line) {
				if y.verbose {
					log.Printf("[STREAM] Skipping invalid response")
				}
				continue
			}

			param(line)

			tempTime := time.Now()
			diff := tempTime.Sub(time.Unix(lastTime, 0))

			if diff > credRefreshInterval {
				log.Printf("[STREAM] Refreshing credentials after %v of inactivity", diff)
				y.refreshCreds()
				lastTime = time.Now().Unix()
				commentCount++
			}

			if commentCount >= maxCredRefreshes {
				log.Printf("[STREAM] Resetting SID after %d credential refreshes", commentCount)
				y.getSID()
				commentCount = 0
				break
			}
		}

		if err := resp.Body.Close(); err != nil {
			y.logStreamError(err, "closing response body")
		}

		y.updateStreamState(StreamStateDisconnected, "Connection ended")
		log.Printf("[STREAM] Connection ended, reconnecting in %v...", reconnectDelay)
		time.Sleep(reconnectDelay)
	}
}

func (y *Youtube) refreshCreds() {
	url := fmt.Sprintf("https://signaler-pa.youtube.com/punctual/v1/refreshCreds?key=%s&gsessionid=%s",
		y.config.API_KEY, y.gsessionID)
	payloadRaw := fmt.Sprintf("[\"%s\"]", y.session)
	payload := strings.NewReader(payloadRaw)
	req, err := http.NewRequest("POST", url, payload)
	if err != nil {
		log.Printf("refresh creds error: %v", err)
		return
	}

	y.copyHeaders(req, y.header)
	req.Header.Set("content-type", "application/json+protobuf")

	resp, err := y.httpClient.Do(req)
	if err != nil {
		log.Printf("refresh creds HTTP error: %v", err)
		return
	}
	if y.verbose {
		log.Printf("Refresh: %d\n", resp.StatusCode)
	}

	if err := resp.Body.Close(); err != nil {
		log.Printf("Error closing response body: %v", err)
	}

}

func (y *Youtube) getSID() {
	url := fmt.Sprintf("https://signaler-pa.youtube.com/punctual/multi-watch/channel?VER=8&gsessionid=%s&key=%s&RID=6167&CVER=22&zx=%s&t=1",
		y.gsessionID, y.config.API_KEY, utils.GenerateZX())
	payloadRaw := fmt.Sprintf("count=1&ofs=0&req0___data__=[[[\"1\",[null,null,null,[9,5],null,[[\"youtube_live_chat_web\"],[1],[[[\"chat~%s\"]]]],null,null,1],null,3]]]", y.videoID)
	payload := strings.NewReader(payloadRaw)

	req, err := http.NewRequest("POST", url, payload)
	if err != nil {
		log.Printf("getSID request error: %v", err)
		return
	}

	y.copyHeaders(req, y.header)
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("x-webchannel-content-type", "application/json+protobuf")

	resp, err := y.httpClient.Do(req)
	if err != nil {
		log.Printf("getSID HTTP error: %v", err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	limited := io.LimitReader(resp.Body, 1<<20)
	body, err := io.ReadAll(limited)
	if err != nil {
		log.Printf("getSID read error: %v", err)
		return
	}

	idx := bytes.Index(body, []byte("[["))
	if idx == -1 {
		log.Println("getSID: JSON array not found")
		return
	}

	jsonPart := make([]byte, len(body)-idx)
	copy(jsonPart, body[idx:])
	body = nil
	decoder := json.NewDecoder(bytes.NewReader(jsonPart))

	t, err := decoder.Token()
	if err != nil {
		log.Printf("getSID JSON decode error: %v", err)
		return
	}
	if delim, ok := t.(json.Delim); !ok || delim != '[' {
		log.Println("getSID: expected top-level array")
		return
	}

	for decoder.More() {
		var elem []interface{}
		if err := decoder.Decode(&elem); err != nil {
			log.Printf("getSID elem decode error: %v", err)
			return
		}
		if len(elem) < 2 {
			continue
		}
		innerArray, ok := elem[1].([]interface{})
		if !ok || len(innerArray) < 2 {
			continue
		}
		if sid, ok := innerArray[1].(string); ok {
			y.sid = sid
			return
		}
	}

	log.Println("getSID: SID not found in the JSON structure")
}

func (y *Youtube) chooseServer() {
	url := fmt.Sprintf("https://signaler-pa.youtube.com/punctual/v1/chooseServer?key=%s", y.config.API_KEY)
	rawPayload := fmt.Sprintf("[[null,null,null,[9,5],null,[[\"youtube_live_chat_web\"],[1],[[[\"chat~%s\"]]]]],null,null,0]", y.videoID)
	payload := strings.NewReader(rawPayload)

	req, err := http.NewRequest("POST", url, payload)
	if err != nil {
		log.Printf("chooseServer request error: %v", err)
		return
	}

	y.copyHeaders(req, y.header)
	req.Header.Set("content-type", "application/json+protobuf")

	resp, err := y.httpClient.Do(req)
	if err != nil {
		log.Printf("chooseServer HTTP error: %v", err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	decoder := json.NewDecoder(resp.Body)
	var result []interface{}
	if err := decoder.Decode(&result); err != nil {
		log.Printf("getSID: decode error: %v", err)
		return
	}

	if len(result) > 0 {
		gsessionID, ok := result[0].(string)
		if ok {
			y.gsessionID = gsessionID
		} else {
			log.Println("chooseServer: gsessionid is not a string")
		}
	}
}

type MessageOptions struct {
	Timestamp string
	IsTimeout bool
	IsFirst   bool
}

func (y *Youtube) sendMessage(opts *MessageOptions) ([]types.YTChatMessage, error) {
	if y.verbose {
		log.Printf("[SEND] Sending message - Timestamp: '%s', Timeout: %v, First: %v",
			opts.Timestamp, opts.IsTimeout, opts.IsFirst)
	}

	payload, err := y.createRequestPayload(opts)
	if err != nil {
		return nil, err
	}

	if y.verbose {
		log.Printf("[SEND] Request payload size: %d bytes", len(payload))
	}
	chatMsgResp, err := y.executeRequest(payload)
	if err != nil {
		log.Printf("[SEND] Error executing request: %v", err)
		return nil, err
	}


	if err := y.handleContinuation(chatMsgResp, opts); err != nil {
		log.Printf("[SEND] Error handling continuation: %v", err)
		return nil, err
	}

	chatMessages := y.processChatMessages(chatMsgResp)

	if y.verbose {
		log.Printf("[SEND] Successfully processed %d messages", len(chatMessages))
		for i, msg := range chatMessages {
			log.Printf("[SEND] Message %d: %s - '%s'", i+1, msg.Author.AuthorName, msg.Message)
		}
	}

	return chatMessages, nil
}

func (y *Youtube) createRequestPayload(opts *MessageOptions) ([]byte, error) {
	ytPayloadMessageLive := types.YTPayloadMessageLive{
		Context:      y.config.INNERTUBE_CONTEXT,
		Continuation: y.continuation,
		WebClientInfo: types.YTWebClientInfo{
			IsDocumentHidden: false,
		},
	}

	if opts.IsTimeout {
		ytPayloadMessageLive.IsInvalidationTimeoutRequest = &opts.IsTimeout
	}
	if !opts.IsFirst && !opts.IsTimeout {
		ytPayloadMessageLive.InvalidationPayloadLastPublishAtUsec = &opts.Timestamp
	}

	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(ytPayloadMessageLive); err != nil {
		bufferPool.Put(buf)
		return nil, fmt.Errorf("sendMessage: marshal error: %w", err)
	}

	payload := make([]byte, buf.Len())
	copy(payload, buf.Bytes())
	bufferPool.Put(buf)

	return payload, nil
}

func (y *Youtube) executeRequest(payload []byte) (*types.YTChatMessagesResponse, error) {
	req, err := http.NewRequest("POST", liveChatEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	y.copyHeaders(req, y.header)
	req.Header.Set("Content-Type", "application/json")

	res, err := y.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	var chatMsgResp types.YTChatMessagesResponse
	if err := json.NewDecoder(res.Body).Decode(&chatMsgResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatMsgResp, nil
}

func (y *Youtube) handleContinuation(chatMsgResp *types.YTChatMessagesResponse, opts *MessageOptions) error {
	continuations := chatMsgResp.ContinuationContents.LiveChatContinuation.Continuations
	if len(continuations) == 0 {
		return fmt.Errorf("sendMessage: no continuation data available")
	}

	cont := continuations[0]

	switch {
	case cont.InvalidationContinuationData != nil:
		y.handleInvalidationContinuation(cont.InvalidationContinuationData, opts)
	case cont.TimedContinuationData != nil:
		y.handleTimedContinuation(cont.TimedContinuationData)
	default:
		return fmt.Errorf("sendMessage: no known continuation data type found")
	}

	return nil
}

func (y *Youtube) handleInvalidationContinuation(data *types.InvalidationContinuationData, opts *MessageOptions) {
	y.timeout = data.TimeoutMs
	if opts.Timestamp != "check" {
		y.continuation = data.Continuation
	}
	y.isInvalidationData = true
}

func (y *Youtube) handleTimedContinuation(data *types.TimedContinuationData) {
	y.timeout = data.TimeoutMs
	y.continuation = data.Continuation
	y.isInvalidationData = false
}

func (y *Youtube) processChatMessages(chatMsgResp *types.YTChatMessagesResponse) []types.YTChatMessage {
	actions := chatMsgResp.ContinuationContents.LiveChatContinuation.Actions
	chatMessages := make([]types.YTChatMessage, 0, len(actions))

	var textBuilder strings.Builder
	thumbnailsBuffer := make([]string, 0, 2)

	if y.verbose {
		log.Printf("[CHAT] Processing %d chat actions", len(actions))
	}

	for _, action := range actions {
		renderer := action.AddChatItemAction.Item.LiveChatTextMessageRenderer
		if len(renderer.Message.Runs) == 0 {
			if y.verbose {
				log.Printf("[CHAT] Skipping action with no message runs")
			}
			continue
		}

		if y.isDuplicateMessage(renderer.ID) {
			if y.verbose {
				log.Printf("[CHAT] Skipping duplicate message: %s", renderer.ID)
			}
			continue
		}

		textBuilder.Reset()
		thumbnailsBuffer = thumbnailsBuffer[:0]
		textBuilder.Grow(initialMsgCapacity)

		y.buildMessageText(&textBuilder, &thumbnailsBuffer, renderer.Message.Runs)
		finalMessage := textBuilder.String()

		finalMessage = strings.TrimSpace(finalMessage)

		for strings.Contains(finalMessage, "  ") {
			finalMessage = strings.ReplaceAll(finalMessage, "  ", " ")
		}

		if finalMessage == "" {
			if y.verbose {
				log.Printf("[CHAT] Skipping empty message after cleanup")
			}
			continue
		}

		ytBadges := renderer.AuthorBadges

	ranking := ""
	if len(renderer.BeforeContentButtons) > 0 {
		ranking = renderer.BeforeContentButtons[0].ButtonViewModel.Title
	}

	chatMsg := types.YTChatMessage{
		ID: renderer.ID,
		Author: types.YTAuthor{
			AuthorName:   renderer.AuthorName.SimpleText,
			AuthorID:     renderer.AuthorExternalChannelID,
			AuthorImages: renderer.AuthorPhoto.Thumbnails,
			Badges:       ytBadges,
			Ranking:      ranking,
		},
		Timestamp: parseMicroSeconds(renderer.TimestampUsec),
		Message:   finalMessage,
	}

		chatMessages = append(chatMessages, chatMsg)
		y.markMessageSeen(renderer.ID)

		if y.verbose {
			log.Printf("[CHAT] Message from %s: '%s' (runs: %d) [Ranking: '%s', Badges: %d]",
				renderer.AuthorName.SimpleText, finalMessage, len(renderer.Message.Runs), ranking, len(ytBadges))
			for i, badge := range ytBadges {
				log.Printf("[CHAT] Badge %d: '%s' - '%s'", i+1, badge.LiveChatAuthorBadgeRenderer.Tooltip, badge.LiveChatAuthorBadgeRenderer.Accessibility.AccessibilityData.Label)
			}
		}
	}

	if y.verbose && len(chatMessages) > 0 {
		log.Printf("[CHAT] Successfully processed %d chat messages", len(chatMessages))
	}

	return chatMessages
}

func (y *Youtube) buildMessageText(textBuilder *strings.Builder, thumbnailsBuffer *[]string, runs []types.YTRuns) {
	if y.verbose {
		log.Printf("[BUILD] Processing message with %d runs", len(runs))
	}

	for i, run := range runs {
		switch {
		case run.Text != "":
			if y.verbose {
				log.Printf("[BUILD] Run %d: Text='%s'", i, run.Text)
			}

			if i > 0 && textBuilder.Len() > 0 {
				lastChar := textBuilder.String()[textBuilder.Len()-1]
				if lastChar != ' ' && lastChar != '\n' && lastChar != '\t' {
					textBuilder.WriteString(" ")
				}
			}
			textBuilder.WriteString(run.Text)

		case run.Emoji.IsCustomEmoji:
			if images := run.Emoji.Image.Thumbnails; len(images) > 0 {
				if y.verbose {
					log.Printf("[BUILD] Run %d: Custom emoji with %d images", i, len(images))
				}

				if i > 0 && textBuilder.Len() > 0 {
					textBuilder.WriteString(" ")
				}
				*thumbnailsBuffer = append(*thumbnailsBuffer, images[len(images)-1].Url)

				for _, url := range *thumbnailsBuffer {
					textBuilder.WriteString(url)
				}
			} else {
				if y.verbose {
					log.Printf("[BUILD] Run %d: Custom emoji with no images", i)
				}
			}

		default:
			if y.verbose {
				log.Printf("[BUILD] Run %d: Standard emoji ID='%s'", i, run.Emoji.EmojiId)
			}

			if i > 0 && textBuilder.Len() > 0 {
				textBuilder.WriteString(" ")
			}

			if run.Emoji.EmojiId != "" {
				textBuilder.WriteString(run.Emoji.EmojiId)
			}
		}
	}

	finalText := strings.TrimSpace(textBuilder.String())
	if finalText != "" {
		textBuilder.Reset()
		textBuilder.WriteString(finalText)
	}

	if y.verbose {
		log.Printf("[BUILD] Final message: '%s'", finalText)
	}
}

func (y *Youtube) isDuplicateMessage(messageID string) bool {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	if _, exists := y.seenMessageIDs[messageID]; exists {
		return true
	}
	return false
}

func (y *Youtube) markMessageSeen(messageID string) {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	y.seenMessageIDs[messageID] = time.Now()

	if time.Since(y.lastCleanupTime) > 10*time.Minute {
		y.cleanupOldMessages()
		y.lastCleanupTime = time.Now()
	}
}

func (y *Youtube) cleanupOldMessages() {
	cutoff := time.Now().Add(-30 * time.Minute)
	for id, timestamp := range y.seenMessageIDs {
		if timestamp.Before(cutoff) {
			delete(y.seenMessageIDs, id)
		}
	}
}

func parseMicroSeconds(timeStampStr string) time.Time {
	tm, err := strconv.ParseInt(timeStampStr, 10, 64)
	if err != nil {
		return time.Time{}
	}
	tm = tm / 1000
	sec := tm / 1000
	msec := tm % 1000
	return time.Unix(sec, msec*int64(time.Millisecond))
}
