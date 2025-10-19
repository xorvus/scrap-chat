package youtube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xorvus/scrap-chat/types"
)

func (y *Youtube) FetchLiveChat(path string) (<-chan *types.LiveChatMessage, error) {
	y.logVerbose("[LIVECHAT] Starting live chat fetch for: %s", path)

	url, err := y.resolveLiveChatURL(path)
	if err != nil {
		return nil, err
	}

	if err := y.setupLiveChatConfig(url); err != nil {
		return nil, err
	}

	if err := y.validateStreamIsLive(); err != nil {
		return nil, err
	}

	y.setupInvalidationDataIfNeeded()

	msg := make(chan *types.LiveChatMessage)
	go y.processLiveChatMessages(msg)

	y.logVerbose("[LIVECHAT] Successfully created and returned live chat message channel")
	return msg, nil
}

func (y *Youtube) resolveLiveChatURL(path string) (string, error) {
	if !strings.Contains(path, "@") {
		return path, nil
	}

	y.logVerbose("[LIVECHAT] Path contains @, fetching channel info first")
	info, err := y.FetchChannelInfo(path)
	if err != nil {
		err := fmt.Errorf("failed to fetch channel info: %w", err)
		y.logVerbose("[LIVECHAT] %v", err)
		return "", err
	}

	url := info.URL + "/live"
	y.logVerbose("[LIVECHAT] Resolved channel live URL: %s", url)
	return url, nil
}

func (y *Youtube) setupLiveChatConfig(url string) error {
	y.logVerbose("[LIVECHAT] Getting configuration from: %s", url)

	if err := y.getConfig(url); err != nil {
		err := fmt.Errorf("failed to get config: %w", err)
		y.logVerbose("[LIVECHAT] %v", err)
		return err
	}

	y.logVerbose("[LIVECHAT] Configuration retrieved successfully")
	y.logVerbose("[LIVECHAT] Sending initial check message")
	y.logVerbose("[LIVECHAT] API Key: %s", y.config.INNERTUBE_API_KEY)
	y.logVerbose("[LIVECHAT] Continuation: %s", y.continuation)

	_, err := y.sendMessage(&MessageOptions{"check", false, true})
	if err != nil {
		y.logVerbose("[LIVECHAT] Initial check message failed: %v", err)
		return err
	}

	return nil
}

func (y *Youtube) validateStreamIsLive() error {
	if !y.isInvalidationData && y.timeout == 0 {
		y.logVerbose("[LIVECHAT] %v", ErrStreamNotLive)
		return ErrStreamNotLive
	}
	return nil
}

func (y *Youtube) setupInvalidationDataIfNeeded() {
	if y.isInvalidationData {
		y.logVerbose("[LIVECHAT] Stream uses invalidation data, setting up server connection")
		y.chooseServer()
		y.getSID()
	}
}

func (y *Youtube) processLiveChatMessages(msg chan *types.LiveChatMessage) {
	y.logVerbose("[LIVECHAT] Starting message processing goroutine")

	defer func() {
		y.logVerbose("[LIVECHAT] Closing message channel")
		close(msg)
	}()

	y.streamChat(func(params []types.YTChatMessage) {
		y.logVerbose("[LIVECHAT] Processing %d messages from stream", len(params))
		y.processAndSendMessages(msg, params)
	})
}

func (y *Youtube) processAndSendMessages(msg chan *types.LiveChatMessage, params []types.YTChatMessage) {
	for i, param := range params {
		liveChatMsg := y.convertToLiveChatMessage(param)
		msg <- liveChatMsg

		y.logVerbose("[LIVECHAT] Sent message %d/%d: ID=%s, Author=%s", i+1, len(params), param.ID, param.Author.AuthorName)
		y.sleepBetweenMessages(len(params))
	}
}

func (y *Youtube) convertToLiveChatMessage(param types.YTChatMessage) *types.LiveChatMessage {
	userImage := ""
	if len(param.Author.AuthorImages) > 0 {
		userImage = param.Author.AuthorImages[0].URL
	}

	badges := y.extractLiveChatBadges(param.Author.Badges)

	return &types.LiveChatMessage{
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
}

func (y *Youtube) extractLiveChatBadges(ytBadges []types.YTAuthorBadge) []types.Badge {
	badges := make([]types.Badge, 0, len(ytBadges))
	for _, badge := range ytBadges {
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
	return badges
}

func (y *Youtube) sleepBetweenMessages(messageCount int) {
	if !y.isInvalidationData {
		sleepDuration := time.Duration(y.timeout/messageCount) * time.Millisecond
		y.logVerbose("[LIVECHAT] Sleeping for %v between messages", sleepDuration)
		time.Sleep(sleepDuration)
	} else {
		y.logVerbose("[LIVECHAT] Sleeping for 50ms between messages")
		time.Sleep(50 * time.Millisecond)
	}
}

func (y *Youtube) streamChat(param func([]types.YTChatMessage)) {
	if y.isInvalidationData {
		y.handleInvalidationDataStream(param)
	} else {
		y.handleTimedContinuationStream(param)
	}
}

func (y *Youtube) handleInvalidationDataStream(param func([]types.YTChatMessage)) {
	lastTime := time.Now().Unix()
	y.longPolling(func(res string) {
		tempTime := time.Now()
		diff := tempTime.Sub(time.Unix(lastTime, 0))

		if y.log.IsVerbose() && int(diff.Seconds())%30 == 0 {
			y.LogStreamSummary()
		}

		if y.processStreamResponse(res, diff, param) {
			lastTime = tempTime.Unix()
		}
	})
}

func (y *Youtube) handleTimedContinuationStream(param func([]types.YTChatMessage)) {
	y.log.Info("Using timed continuation mode")
	for {
		time.Sleep(time.Duration(y.timeout) * time.Millisecond)
		res, err := y.sendMessage(&MessageOptions{
			Timestamp: "",
			IsTimeout: false,
			IsFirst:   true,
		})
		if err != nil {
			y.log.Error("Error in timed mode: %v", err)
			continue
		}
		go func() {
			param(res)
		}()
	}
}

func (y *Youtube) processStreamResponse(res string, diff time.Duration, param func([]types.YTChatMessage)) bool {
	switch {
	case isRegexTrue(regFirstChat, res):
		return y.handleFirstChatResponse(res, param)
	case diff >= 10*time.Second:
		return y.handleTimeoutResponse(param)
	case isRegexTrue(regNoChat, res):
		y.handleNoChatResponse()
	case y.hasChatMessage(res):
		return y.handleChatMessageResponse(res, param)
	default:
		y.handleUndefinedResponse(res, diff)
		return true
	}
	return true
}

func (y *Youtube) handleFirstChatResponse(res string, param func([]types.YTChatMessage)) bool {
	y.log.Debug("Detected first chat message, processing session")
	go y.extractSessionFromResponse(res)

	response, err := y.sendMessage(&MessageOptions{
		Timestamp: "",
		IsTimeout: false,
		IsFirst:   true,
	})
	if err != nil {
		y.log.Error("Error sending first chat message: %v", err)
		return false
	}
	param(response)
	return true
}

func (y *Youtube) handleTimeoutResponse(param func([]types.YTChatMessage)) bool {
	y.log.Debug("Sending timeout message after inactivity")
	response, err := y.sendMessage(&MessageOptions{
		Timestamp: "",
		IsTimeout: true,
		IsFirst:   false,
	})
	if err != nil {
		y.log.Error("Error sending timeout message: %v", err)
		return false
	}
	param(response)
	return true
}

func (y *Youtube) handleNoChatResponse() {
	y.log.Debug("No chat detected")
}

func (y *Youtube) hasChatMessage(res string) bool {
	ok, _ := regexGetValue(regChat, res)
	return ok
}

func (y *Youtube) handleChatMessageResponse(res string, param func([]types.YTChatMessage)) bool {
	ok, match := regexGetValue(regChat, res)
	if !ok {
		return false
	}

	y.log.Debug("Chat message detected with timestamp: %s", match[0])

	response, err := y.sendMessage(&MessageOptions{
		Timestamp: match[0],
		IsTimeout: false,
		IsFirst:   false,
	})
	if err != nil {
		y.log.Error("Error sending chat message: %v", err)
		return false
	}
	param(response)
	return true
}

func (y *Youtube) handleUndefinedResponse(res string, diff time.Duration) {
	y.logStreamError(fmt.Errorf("undefined response pattern"), "response processing")
	y.logUndefinedResponseDetails(res, diff)

	health := y.GetStreamHealth()
	if health.ConsecutiveErrors > 3 {
		y.log.Warn("Multiple undefined responses detected (%d), connection may be unstable", health.ConsecutiveErrors)
	}
}

func (y *Youtube) extractSessionFromResponse(res string) {
	_, match := regexGetValue(regSession, res)
	if len(match) == 0 {
		y.log.Warn("Regex match for session failed. Response length: %d", len(res))
		return
	}
	y.session = match[0]
	y.log.Debug("Session extracted: %s", y.session)
}

func (y *Youtube) logUndefinedResponseDetails(res string, diff time.Duration) {
	switch {
	case len(res) == 0:
		y.log.Warn("Empty response received")
	case len(res) < 10:
		y.log.Warn("Very short response: '%s' (length: %d)", res, len(res))
	case strings.Contains(res, "error") || strings.Contains(res, "ERROR"):
		y.log.Error("Error response detected: %s", res)
	default:
		y.log.Warn("Undefined response format - Length: %d, Time since last: %v", len(res), diff)
		preview := res
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		y.log.Debug("Response preview: %s", preview)
	}
}

func (y *Youtube) sendMessage(opts *MessageOptions) ([]types.YTChatMessage, error) {
	y.log.Debug("Starting message send operation - Timestamp: '%s', Timeout: %v, First: %v",
		opts.Timestamp, opts.IsTimeout, opts.IsFirst)

	payload, err := y.createRequestPayload(opts)
	if err != nil {
		y.log.Error("Failed to create request payload: %v", err)
		return nil, err
	}

	y.log.Debug("Request payload created with size: %d bytes", len(payload))

	chatMsgResp, err := y.executeRequestForChat(payload)
	if err != nil {
		err := fmt.Errorf("error executing request: %w", err)
		y.log.Error("%v", err)
		return nil, err
	}

	y.log.Debug("Request executed successfully, handling continuation")

	if err := y.handleContinuation(chatMsgResp, opts); err != nil {
		err := fmt.Errorf("error handling continuation: %w", err)
		y.log.Error("%v", err)
		return nil, err
	}

	y.log.Debug("Continuation handled successfully, processing chat messages")

	chatMessages := y.processChatMessages(chatMsgResp)

	y.log.Debug("Successfully processed %d messages", len(chatMessages))

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

func (y *Youtube) executeRequestForChat(payload []byte) (*types.YTChatMessagesResponse, error) {
	endpoint := fmt.Sprintf("%s&key=%s", liveChatEndpoint, y.config.INNERTUBE_API_KEY)

	y.log.Debug("Endpoint: %s", endpoint)
	y.log.Debug("Payload: %s", string(payload))

	resp, err := y.executeRequest(endpoint, "POST", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			y.log.Error("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != 200 {
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 2048))
		errorDetail := "no response body"
		if readErr == nil && len(bodyBytes) > 0 {
			errorDetail = string(bodyBytes)
		}

		y.log.Error("Status: %d", resp.StatusCode)
		y.log.Error("Response: %s", errorDetail)

		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, errorDetail)
	}

	var chatMsgResp types.YTChatMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatMsgResp); err != nil {
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
