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

// FetchLiveChat initiates a live chat stream from an active YouTube live stream.
// The path parameter can be a YouTube live stream URL or a channel handle (e.g., "@channelname/live").
// Returns a channel that emits live chat messages in real-time until the stream ends.
// The returned channel is closed when the stream ends or an error occurs.
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

	msg := make(chan *types.LiveChatMessage, defaultChannelBuffer)
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

		// Non-blocking send to prevent goroutine blocking on slow consumer
		select {
		case msg <- liveChatMsg:
			y.logVerbose("[LIVECHAT] Sent message %d/%d: ID=%s, Author=%s", i+1, len(params), param.ID, param.Author.AuthorName)
		default:
			y.log.Warn("[LIVECHAT] Channel full, dropping message %d/%d: ID=%s", i+1, len(params), param.ID)
		}
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
			URL:       fmt.Sprintf(youtubeChannelURL, param.Author.AuthorID),
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

func (y *Youtube) streamChat(param func([]types.YTChatMessage)) {
	if y.isInvalidationData {
		y.handleInvalidationDataStream(param)
	} else {
		y.handleTimedContinuationStream(param)
	}
}

func (y *Youtube) handleInvalidationDataStream(param func([]types.YTChatMessage)) {
	y.longPolling(func(res string) {
		y.processStreamResponse(res, param)
	})
}

func (y *Youtube) handleTimedContinuationStream(param func([]types.YTChatMessage)) {
	y.log.Info("Using timed continuation mode")
	for {
		res, err := y.sendMessage(&MessageOptions{
			Timestamp: "",
			IsTimeout: false,
			IsFirst:   true,
		})
		if err != nil {
			y.log.Error("Error in timed mode: %v", err)
			time.Sleep(time.Duration(y.timeout) * time.Millisecond)
			continue
		}
		param(res)
		time.Sleep(time.Duration(y.timeout) * time.Millisecond)
	}
}

func (y *Youtube) processStreamResponse(res string, param func([]types.YTChatMessage)) {
	switch {
	case isRegexTrue(regFirstChat, res):
		y.log.Debug("[Stream] Pattern matched: FIRST_CHAT")
		y.handleFirstChatResponse(res, param)
	case strings.Contains(res, `["noop"]`):
		y.log.Debug("[Stream] Pattern matched: NOOP - keep-alive signal received, continuing polling")
	case isRegexTrue(regNoChat, res):
		y.log.Debug("[Stream] Pattern matched: NO_CHAT")
		y.handleNoChatResponse()
	case y.hasChatMessage(res):
		y.log.Debug("[Stream] Pattern matched: CHAT_MESSAGE")
		y.handleChatMessageResponse(res, param)
	default:
		y.log.Debug("[Stream] Pattern matched: UNDEFINED/ERROR")
		y.handleUndefinedResponse(res)
	}
}

func (y *Youtube) handleFirstChatResponse(res string, param func([]types.YTChatMessage)) {
	y.log.Debug("Detected first chat message, processing session")
	go y.extractSessionFromResponse(res)

	response, err := y.sendMessage(&MessageOptions{
		Timestamp: "",
		IsTimeout: false,
		IsFirst:   true,
	})
	if err != nil {
		y.log.Error("Error sending first chat message: %v", err)
		return
	}
	param(response)
}

func (y *Youtube) handleNoChatResponse() {
	y.log.Debug("No chat detected")
}

func (y *Youtube) hasChatMessage(res string) bool {
	ok, _ := regexGetValue(regChat, res)
	return ok
}

func (y *Youtube) handleChatMessageResponse(res string, param func([]types.YTChatMessage)) {
	ok, match := regexGetValue(regChat, res)
	if !ok {
		return
	}

	y.log.Debug("[Stream] Chat message detected with invalidation timestamp: %s", match[0])
	y.log.Debug("[Flow 6] Sending request with invalidationPayloadLastPublishAtUsec: %s", match[0])

	response, err := y.sendMessage(&MessageOptions{
		Timestamp: match[0],
		IsTimeout: false,
		IsFirst:   false,
	})
	if err != nil {
		y.log.Error("Error sending chat message: %v", err)
		return
	}
	param(response)
}

func (y *Youtube) handleUndefinedResponse(res string) {
	y.logStreamError(fmt.Errorf("undefined response pattern"), "response processing")
	y.logUndefinedResponseDetails(res)

	health := y.GetStreamHealth()
	if health.ConsecutiveErrors > 3 {
		y.log.Warn("Multiple undefined responses detected (%d), connection may be unstable", health.ConsecutiveErrors)
	}
}

func (y *Youtube) extractSessionFromResponse(res string) {
	y.log.Debug("[Session] Attempting to extract session from first chat response")
	_, match := regexGetValue(regSession, res)
	if len(match) == 0 {
		y.log.Warn("[Session] Regex match for session failed. Response length: %d", len(res))
		return
	}
	y.session = match[0]
	y.log.Debug("[Session] Successfully extracted session: %s", y.session)
}

func (y *Youtube) logUndefinedResponseDetails(res string) {
	switch {
	case len(res) == 0:
		y.log.Warn("Empty response received")
	case len(res) < 10:
		y.log.Warn("Very short response: '%s' (length: %d)", res, len(res))
	case strings.Contains(res, "error") || strings.Contains(res, "ERROR"):
		y.log.Error("Error response detected: %s", res)
	default:
		y.log.Warn("Undefined response format - Length: %d", len(res))
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

	// Use json.Marshal directly instead of buffer pool for simpler memory management
	// The payload is immediately consumed by HTTP request, so no need for buffer pooling here
	payload, err := json.Marshal(ytPayloadMessageLive)
	if err != nil {
		return nil, fmt.Errorf("sendMessage: marshal error: %w", err)
	}

	return payload, nil
}

func (y *Youtube) executeRequestForChat(payload []byte) (*types.YTChatMessagesResponse, error) {
	endpoint := fmt.Sprintf("%s&key=%s", liveChatEndpoint, y.config.INNERTUBE_API_KEY)

	y.log.Debug("[Flow 5/6] Getting live chat messages - continuation: %s", y.continuation)
	y.log.Debug("[Flow 5/6] Endpoint: %s", endpoint)
	y.log.Debug("[Flow 5/6] Payload: %s", string(payload))

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
