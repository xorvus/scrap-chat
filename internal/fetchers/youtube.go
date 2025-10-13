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
	// Regex patterns
	REG_FIRST_CHAT = `\[\[\d+,\[\[null,null,\["([^"]+)"\]\]\]\]`
	REG_NO_CHAT    = `\[\[\d*,\[\[\[\[.*\[null,null,\["\d*`
	REG_CHAT       = `\d{16,}`
	REG_SESSION    = `\w{8,}`

	// YouTube URLs
	youtubeBaseURL     = "https://www.youtube.com"
	youtubeSignalerURL = "https://signaler-pa.youtube.com"
	youtubeAPIURL      = "https://www.youtube.com/youtubei/v1"

	maxResponseHeaderBytes = 1 << 20
	maxResponseBodyBytes   = 2 << 20
	bufferInitialSize      = 32 * 1024
	readChunkSize          = 64 * 1024

	defaultHTTPTimeout  = 5 * time.Second
	reconnectDelay      = 500 * time.Millisecond
	chatMessageDelay    = 50 * time.Millisecond
	credRefreshInterval = 4 * time.Minute
	maxCredRefreshes    = 4
	avgMessageSize      = 64

	// File format
	cookieFileSeparator = "\t"
	cookieFileFields    = 7
)

var (
	regFirstChat     = regexp.MustCompile(REG_FIRST_CHAT)
	regNoChat        = regexp.MustCompile(REG_NO_CHAT)
	regChat          = regexp.MustCompile(REG_CHAT)
	regSession       = regexp.MustCompile(REG_SESSION)
	ytCfgRegex       = regexp.MustCompile(`ytcfg\.set\((\{.*?\})\);`)
	initialDataRegex = regexp.MustCompile(`(?s)(?:window\s*\[\s*["']ytInitialData["']\s*\]|ytInitialData)\s*=\s*({.+?})\s*;`)
	ErrStreamNotLive = errors.New("stream not live")
	bufferPool       = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, bufferInitialSize))
		},
	}
)

type Youtube struct {
	cookies                        []*http.Cookie
	config                         *types.YTCgf
	continuation                   string
	videoId                        string
	gsessionID                     string
	sid                            string
	httpClient                     *http.Client
	header                         http.Header
	cookieString                   string
	timeout                        int
	isInvalidationContinuationData bool
	session                        string
	ctx                            *context.Context
	verbose                        bool
}

func NewYoutube(ctx *context.Context, verbose bool) *Youtube {
	y := &Youtube{
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxResponseHeaderBytes: 1 << 20,
			},
		},
		ctx:     ctx,
		verbose: verbose,
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
	defer file.Close()
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

func (y *Youtube) FetchVideoComments(path string, date *time.Time) (<-chan *types.ChatMessage, error) {
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

	if !y.isInvalidationContinuationData && y.timeout == 0 {
		return nil, ErrStreamNotLive
	}

	//check use long poling
	if y.isInvalidationContinuationData {
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
				msg <- &types.LiveChatMessage{
					ID:      param.ID,
					Message: param.Message,
					Author: types.Author{
						ID:        param.Author.AuthorID,
						Name:      param.Author.AuthorName,
						Thumbnail: userImage,
						URL:       fmt.Sprintf("https://youtube.com/channel/%s", param.Author.AuthorID),
					},
					Timestamp: param.Timestamp.Unix(),
				}

				if !y.isInvalidationContinuationData {
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
		path = "https://www.youtube.com/" + path
	}

	info := &types.ChannelInfo{}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(path)
	if err != nil {
		return &types.ChannelInfo{}, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return &types.ChannelInfo{}, err
	}

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

	return info, nil
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

	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxResponseBodyBytes)
	buffer := bufferPool.Get().(*bytes.Buffer)

	chunk := make([]byte, readChunkSize)
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
			y.videoId = strings.Clone(vid)
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
	if y.isInvalidationContinuationData {
		lastTime := time.Now().Unix()
		y.longPooling(func(res string) {
			tempTime := time.Now()
			diff := tempTime.Sub(time.Unix(lastTime, 0))

			switch {
			case IsRegexTrue(regFirstChat, res):
				go func() {
					_, match := RegexGetValue(regSession, res)
					if len(match) == 0 {
						log.Printf("Regex match for session failed. Response: %s\n", res)
						return
					}
					y.session = match[0]
				}()

				res, _ := y.sendMessage(&MessageOptions{
					Timestamp: "",
					IsTimeout: false,
					IsFirst:   true,
				})
				param(res)
			case diff >= 10*time.Second:
				res, _ := y.sendMessage(&MessageOptions{
					Timestamp: "",
					IsTimeout: true,
					IsFirst:   false,
				})
				param(res)
			case IsRegexTrue(regNoChat, res):
				// No chat, do nothing
			case func() bool {
				ok, match := RegexGetValue(regChat, res)
				if ok {
					res, _ := y.sendMessage(&MessageOptions{
						Timestamp: match[0],
						IsTimeout: false,
						IsFirst:   false,
					})
					param(res)
				}
				return ok
			}():
				// handled in the inline func
			default:
				log.Printf("[%dms] Undefined: %s", diff/time.Millisecond, res)
			}

			lastTime = tempTime.Unix()
		})
	} else {
		for {
			time.Sleep(time.Duration(y.timeout) * time.Millisecond)
			res, _ := y.sendMessage(&MessageOptions{
				Timestamp: "",
				IsTimeout: false,
				IsFirst:   true,
			})
			go func() {
				param(res)
			}()
		}
	}
}

// IsRegexTrue returns true if the regex matches the data.
func IsRegexTrue(r *regexp.Regexp, str string) bool {
	return r.MatchString(str)
}

// RegexGetValue returns true and the matches if the regex finds more than one match.
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

func (y *Youtube) longPooling(param func(string)) {
	if y.verbose {
		log.Println("Long pool...")
	}
	commentCount := 0
	for {
		url := fmt.Sprintf("https://signaler-pa.youtube.com/punctual/multi-watch/channel?VER=8&gsessionid=%s&key=%s&RID=rpc&SID=%s&AID=0&CI=0&TYPE=xmlhttp&zx=%s&t=1",
			y.gsessionID, y.config.API_KEY, y.sid, utils.GenerateZX())

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Printf("Request error: %v", err)
			return
		}

		y.copyHeaders(req, y.header)

		resp, err := y.httpClient.Do(req)
		if err != nil {
			log.Printf("HTTP error: %v", err)
			return
		}

		if y.verbose {
			log.Println("Connected, streaming...")
		}
		reader := bufio.NewReader(resp.Body)

		lastTime := time.Now().Unix()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					log.Println("Stream closed by server.")
				} else {
					log.Printf("Error reading stream: %v", err)
				}
				break
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if y.verbose {
				log.Println(line)
			}
			param(line)
			tempTime := time.Now()
			diff := tempTime.Sub(time.Unix(lastTime, 0))
			if diff > credRefreshInterval {
				if y.verbose {
					log.Println("Refresh credentials...")
				}
				y.refreshCreds()
				lastTime = time.Now().Unix()
				commentCount++
			}

			if commentCount >= maxCredRefreshes {
				if y.verbose {
					log.Println("Reset SID...")
				}
				y.getSID()
				commentCount = 0
				break
			}
		}

		resp.Body.Close()

		if y.verbose {
			log.Println("Reconnecting...")
		}
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
		log.Printf("Refersh: %d\n", resp.StatusCode)
	}

	resp.Body.Close()

}

func (y *Youtube) getSID() {
	url := fmt.Sprintf("https://signaler-pa.youtube.com/punctual/multi-watch/channel?VER=8&gsessionid=%s&key=%s&RID=6167&CVER=22&zx=%s&t=1",
		y.gsessionID, y.config.API_KEY, utils.GenerateZX())
	payloadRaw := fmt.Sprintf("count=1&ofs=0&req0___data__=[[[\"1\",[null,null,null,[9,5],null,[[\"youtube_live_chat_web\"],[1],[[[\"chat~%s\"]]]],null,null,1],null,3]]]", y.videoId)
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
	defer resp.Body.Close()

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
	rawPayload := fmt.Sprintf("[[null,null,null,[9,5],null,[[\"youtube_live_chat_web\"],[1],[[[\"chat~%s\"]]]]],null,null,0]", y.videoId)
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
	defer resp.Body.Close()

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
	url := "https://www.youtube.com/youtubei/v1/live_chat/get_live_chat?prettyPrint=false"

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

	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("sendMessage: request error: %w", err)
	}

	y.copyHeaders(req, y.header)
	req.Header.Set("Content-Type", "application/json")

	res, err := y.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sendMessage: HTTP error: %w", err)
	}
	defer res.Body.Close()

	var chatMsgResp types.YTChatMessagesResponse
	decoder := json.NewDecoder(res.Body)
	if err := decoder.Decode(&chatMsgResp); err != nil {
		return nil, fmt.Errorf("sendMessage: unmarshal error: %w", err)
	}

	continuations := chatMsgResp.ContinuationContents.LiveChatContinuation.Continuations
	if len(continuations) == 0 {
		return nil, fmt.Errorf("sendMessage: no continuation data available")
	}

	cont := continuations[0]

	switch {
	case cont.InvalidationContinuationData != nil:
		data := cont.InvalidationContinuationData
		y.timeout = data.TimeoutMs
		if opts.Timestamp != "check" {
			y.continuation = data.Continuation
		}
		y.isInvalidationContinuationData = true

	case cont.TimedContinuationData != nil:
		data := cont.TimedContinuationData
		y.timeout = data.TimeoutMs
		y.continuation = data.Continuation
		y.isInvalidationContinuationData = false

	default:
		return nil, fmt.Errorf("sendMessage: no known continuation data type found")
	}

	actions := chatMsgResp.ContinuationContents.LiveChatContinuation.Actions
	chatMessages := make([]types.YTChatMessage, 0, len(actions))

	var textBuilder strings.Builder
	const avgMessageSize = 128
	thumbnailsBuffer := make([]string, 0, 2)

	for _, action := range actions {
		renderer := action.AddChatItemAction.Item.LiveChatTextMessageRenderer
		if len(renderer.Message.Runs) == 0 {
			continue
		}

		textBuilder.Reset()
		thumbnailsBuffer = thumbnailsBuffer[:0]
		textBuilder.Grow(avgMessageSize)

		for _, run := range renderer.Message.Runs {
			switch {
			case run.Text != "":
				textBuilder.WriteString(run.Text)
			case run.Emoji.IsCustomEmoji:
				if images := run.Emoji.Image.Thumbnails; len(images) > 0 {
					thumbnailsBuffer = append(thumbnailsBuffer, images[len(images)-1].Url)
				}
				for _, url := range thumbnailsBuffer {
					textBuilder.WriteString(" ")
					textBuilder.WriteString(url)
					textBuilder.WriteString(" ")
				}
			default:
				textBuilder.WriteString(run.Emoji.EmojiId)
			}
		}

		chatMessages = append(chatMessages, types.YTChatMessage{
			ID: renderer.ID,
			Author: types.YTAuthor{
				AuthorName:   renderer.AuthorName.SimpleText,
				AuthorID:     renderer.AuthorExternalChannelID,
				AuthorImages: renderer.AuthorPhoto.Thumbnails,
			},
			Timestamp: parseMicroSeconds(renderer.TimestampUsec),
			Message:   textBuilder.String(),
		})
	}

	return chatMessages, nil
}

// parseMicroSeconds parses a microsecond timestamp string to time.Time.
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
