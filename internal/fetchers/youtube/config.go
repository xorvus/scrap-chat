package youtube

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/xorvus/scrap-chat/types"
)

func (y *Youtube) getConfig(url string) error {
	y.log.Debug("Starting configuration fetch from URL: %s", url)

	data, err := y.fetchPage(url)
	if err != nil {
		y.log.Error("Failed to fetch page: %v", err)
		return err
	}

	y.log.Debug("Fetched %d bytes of page data", len(data))

	buffer := bufferPool.Get().(*bytes.Buffer)
	defer func() {
		buffer.Reset()
		bufferPool.Put(buffer)
	}()

	buffer.Write(data)

	y.log.Debug("Processing configuration from page data")

	config := &types.YTCgf{}
	foundCfg, ytcfgCont := processConfigRegex(buffer, ytCfgRegex, config)
	foundInitial, cont, reloadCont, vid := processInitialDataRegex(buffer, initialDataRegex)

	if !foundCfg || !foundInitial {
		err := fmt.Errorf("failed to extract configuration")
		y.log.Error("%v", err)
		return err
	}

	y.log.Debug("Configuration extraction successful - Found config: %t, Found initial data: %t", foundCfg, foundInitial)

	y.videoID = strings.Clone(vid)
	y.config = &types.YTCgf{
		INNERTUBE_API_KEY:        config.INNERTUBE_API_KEY,
		API_KEY:                  config.API_KEY,
		INNERTUBE_CONTEXT:        config.INNERTUBE_CONTEXT,
		INNERTUBE_CLIENT_VERSION: config.INNERTUBE_CLIENT_VERSION,
		ID_TOKEN:                 config.ID_TOKEN,
	}

	finalContinuation := cont
	if finalContinuation == "" {
		finalContinuation = y.resolveContinuationFallback(ytcfgCont, reloadCont)
	}

	y.continuation = strings.Clone(finalContinuation)

	y.log.Debug("Successfully stored configuration - Video ID: %s, Continuation: %s", y.videoID, y.continuation)

	return nil
}

func processConfigRegex(buffer *bytes.Buffer, regex *regexp.Regexp, config *types.YTCgf) (bool, string) {
	data := buffer.Bytes()
	match := regex.FindSubmatch(data)
	if len(match) < 2 {
		return false, ""
	}

	jsonBytes := match[1]
	config.INNERTUBE_API_KEY = gjson.GetBytes(jsonBytes, "INNERTUBE_API_KEY").String()
	config.API_KEY = gjson.GetBytes(jsonBytes, "LIVE_CHAT_BASE_TANGO_CONFIG.apiKey").String()
	config.INNERTUBE_CLIENT_VERSION = gjson.GetBytes(jsonBytes, "INNERTUBE_CLIENT_VERSION").String()
	config.ID_TOKEN = gjson.GetBytes(jsonBytes, "ID_TOKEN").String()

	contextResult := gjson.GetBytes(jsonBytes, "INNERTUBE_CONTEXT")
	if !contextResult.Exists() {
		return false, ""
	}

	if err := json.Unmarshal([]byte(contextResult.Raw), &config.INNERTUBE_CONTEXT); err != nil {
		return false, ""
	}

	ytcfgContinuation := gjson.GetBytes(jsonBytes, "LIVE_CHAT_BASE_TANGO_CONFIG.continuation").String()

	return true, ytcfgContinuation
}

func processInitialDataRegex(buffer *bytes.Buffer, regex *regexp.Regexp) (bool, string, string, string) {
	data := buffer.Bytes()
	match := regex.FindSubmatch(data)
	if len(match) < 2 {
		return false, "", "", ""
	}

	jsonBytes := match[1]

	continuation := extractInvalidationContinuation(jsonBytes)
	reloadContinuation := extractReloadContinuation(jsonBytes)
	videoID := extractVideoID(jsonBytes)

	return true, continuation, reloadContinuation, videoID
}

func extractInvalidationContinuation(jsonBytes []byte) string {
	paths := []string{
		"continuationContents.liveChatContinuation.continuations.0.invalidationContinuationData.continuation",
		"continuationContents.liveChatContinuation.continuations.#.invalidationContinuationData.continuation",
	}
	return extractJSONPath(jsonBytes, paths)
}

func extractReloadContinuation(jsonBytes []byte) string {
	paths := []string{
		"contents.twoColumnWatchNextResults.conversationBar.liveChatRenderer.continuations.0.reloadContinuationData.continuation",
		"contents.twoColumnWatchNextResults.conversationBar.liveChatRenderer.header.liveChatHeaderRenderer.viewSelector.sortFilterSubMenuRenderer.subMenuItems.#.continuation.reloadContinuationData.continuation",
		"contents.twoColumnWatchNextResults.conversationBar.liveChatRenderer.continuations.#.reloadContinuationData.continuation",
	}
	return extractJSONPath(jsonBytes, paths)
}

func extractVideoID(jsonBytes []byte) string {
	paths := []string{
		"currentVideoEndpoint.watchEndpoint.videoId",
		"contents.twoColumnWatchNextResults.results.results.contents.0.videoPrimaryInfoRenderer.videoActions.menuRenderer.topLevelButtons.0.segmentedLikeDislikeButtonRenderer.likeButton.toggleButtonRenderer.defaultServiceEndpoint.performCommentActionEndpoint.clientActions.0.updateCommentVoteAction.videoId",
		"responseContext.webResponseContextExtensionData.ytConfigData.videoId",
	}
	return extractJSONPath(jsonBytes, paths)
}

func extractContinuationFromComments(data []byte) (string, error) {
	paths := []string{
		"onResponseReceivedEndpoints.0.reloadContinuationItemsCommand.continuationItems.-1.continuationItemRenderer.continuationEndpoint.continuationCommand.token",
		"onResponseReceivedEndpoints.1.appendContinuationItemsAction.continuationItems.-1.continuationItemRenderer.continuationEndpoint.continuationCommand.token",
		"continuationContents.itemSectionContinuation.continuations.0.nextContinuationData.continuation",
	}

	result := extractJSONPath(data, paths)
	if result == "" {
		return "", ErrNoContinuation
	}
	return result, nil
}

func (y *Youtube) resolveContinuationFallback(ytcfgCont, reloadCont string) string {
	continuations := []string{ytcfgCont, reloadCont}

	for _, cont := range continuations {
		if cont == "" {
			continue
		}

		y.log.Debug("Attempting to fetch continuation from live_chat endpoint")

		liveChatCont, err := y.fetchLiveChatContinuation(cont)
		if err != nil {
			y.log.Debug("Failed to fetch continuation: %v", err)
			continue
		}

		y.log.Debug("Successfully obtained continuation from live_chat endpoint")
		return liveChatCont
	}

	return ""
}

func (y *Youtube) fetchLiveChatContinuation(reloadContinuation string) (string, error) {
	url := fmt.Sprintf("https://www.youtube.com/live_chat?continuation=%s", reloadContinuation)

	y.log.Debug("Fetching live_chat page from: %s", url)

	data, err := y.fetchPage(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch live_chat page: %w", err)
	}

	if len(data) == 0 {
		return "", fmt.Errorf("received empty response from live_chat page")
	}

	y.log.Debug("Fetched %d bytes from live_chat page", len(data))

	continuation, err := y.extractContinuationFromLiveChatPage(data)
	if err != nil {
		return "", err
	}

	y.log.Debug("Extracted continuation from live_chat page: %s", continuation)

	return continuation, nil
}

func (y *Youtube) extractContinuationFromLiveChatPage(data []byte) (string, error) {
	buffer := bufferPool.Get().(*bytes.Buffer)
	defer func() {
		buffer.Reset()
		bufferPool.Put(buffer)
	}()

	buffer.Write(data)

	match := initialDataRegex.FindSubmatch(buffer.Bytes())
	if len(match) < 2 {
		return "", fmt.Errorf("ytInitialData not found in live_chat page")
	}

	jsonBytes := match[1]
	continuationPath := "continuationContents.liveChatContinuation.continuations.0.invalidationContinuationData.continuation"
	result := gjson.GetBytes(jsonBytes, continuationPath)

	if !result.Exists() || result.String() == "" {
		return "", fmt.Errorf("continuation path not found: %s", continuationPath)
	}

	return result.String(), nil
}
