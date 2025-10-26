package youtube

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/xorvus/scrap-chat/internal/utils"
	"github.com/xorvus/scrap-chat/types"
)

// FetchVideoComments retrieves all comments from a YouTube video.
// The videoID parameter can be a full YouTube video URL or just the video ID.
// If date is provided, only comments posted after that date are returned.
// Returns a channel that emits comments sequentially, including replies.
// The returned channel is closed when all comments have been fetched or an error occurs.
func (y *Youtube) FetchVideoComments(videoID string, date *time.Time) (<-chan *types.ChatMessage, error) {
	y.logCommentsFetchInfo(videoID, date)

	normalizedVideoID := y.normalizeVideoID(videoID)

	if err := y.setupVideoConfig(normalizedVideoID); err != nil {
		return nil, err
	}

	if y.videoID == "" {
		err := fmt.Errorf("failed to extract video ID")
		y.logVerbose("[COMMENTS] %v", err)
		return nil, err
	}

	y.logVerbose("[COMMENTS] Successfully extracted video ID: %s", y.videoID)

	commentsChan := make(chan *types.ChatMessage, defaultChannelBuffer)
	go y.startCommentsFetch(commentsChan, date)

	y.logVerbose("[COMMENTS] Successfully created comments channel")
	return commentsChan, nil
}

func (y *Youtube) logCommentsFetchInfo(videoID string, date *time.Time) {
	y.log.Debug("Starting comment fetch for video: %s", videoID)
	if date != nil {
		y.log.Debug("Filtering comments after date: %v", date)
	}
}

func (y *Youtube) normalizeVideoID(videoID string) string {
	if !strings.HasPrefix(videoID, "http") {
		normalizedID := youtubeBaseURL + "/watch?v=" + videoID
		y.logVerbose("[COMMENTS] Normalized video ID from '%s' to '%s'", videoID, normalizedID)
		return normalizedID
	}
	return videoID
}

func (y *Youtube) setupVideoConfig(videoID string) error {
	y.logVerbose("[COMMENTS] Getting video config")

	if err := y.getConfig(videoID); err != nil {
		err := fmt.Errorf("failed to get video config: %w", err)
		y.logVerbose("[COMMENTS] %v", err)
		return err
	}
	return nil
}

func (y *Youtube) startCommentsFetch(commentsChan chan *types.ChatMessage, date *time.Time) {
	y.logVerbose("[COMMENTS] Starting recursive comment fetch in goroutine")

	defer func() {
		y.logVerbose("Closing comments channel")
		close(commentsChan)
	}()

	if err := y.fetchCommentsRecursive(commentsChan, "", date); err != nil {
		if !errors.Is(err, ErrNoComments) {
			y.log.Error("Error fetching comments: %v", err)
		}
	}
}

func (y *Youtube) fetchCommentsRecursive(commentsChan chan<- *types.ChatMessage, continuation string, date *time.Time) error {
	y.logCommentsFetchStart(continuation)

	payload, err := y.createCommentsPayload(continuation)
	if err != nil {
		return y.logAndWrapError(err, "failed to create payload")
	}

	body, err := y.fetchCommentsResponse(payload)
	if err != nil {
		return err
	}

	comments, err := y.extractComments(body)
	if err != nil {
		y.logVerbose("[COMMENTS] Failed to extract comments: %v", err)
		return err
	}

	y.logVerbose("[COMMENTS] Extracted %d comments from response", len(comments))

	if shouldStop := y.sendCommentsToChannel(commentsChan, comments, date); shouldStop {
		y.logVerbose("[COMMENTS] Stopping recursive fetch due to date filter")
		return nil
	}

	nextContinuation, err := extractContinuationFromComments(body)
	if err != nil {
		y.logVerbose("[COMMENTS] No more comments to fetch")
		return nil
	}

	y.logVerbose("[COMMENTS] Fetched %d comments, continuing with next batch", len(comments))
	return y.fetchCommentsRecursive(commentsChan, nextContinuation, date)
}

func (y *Youtube) logCommentsFetchStart(continuation string) {
	if continuation == "" {
		y.log.Debug("Starting initial comment fetch")
	} else {
		y.log.Debug("Fetching comments with continuation: %s", continuation)
	}
}

func (y *Youtube) createCommentsPayload(continuation string) ([]byte, error) {
	if continuation == "" {
		y.logVerbose("[COMMENTS] Creating initial comments payload")
		return y.createInitialCommentsPayload()
	}
	y.logVerbose("[COMMENTS] Creating continuation payload with token: %s", continuation)
	return y.createContinuationPayload(continuation)
}

func (y *Youtube) fetchCommentsResponse(payload []byte) ([]byte, error) {
	y.logVerbose("Sending POST request to %s with payload size: %d bytes", nextEndpoint, len(payload))

	resp, err := y.executeRequest(nextEndpoint, "POST", bytes.NewReader(payload))
	if err != nil {
		return nil, y.logAndWrapError(err, "HTTP request failed")
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			y.log.Error("Error closing response body: %v", err)
		}
	}()

	y.logVerbose("Received response with status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		return nil, y.logAndWrapError(err, "")
	}

	y.logVerbose("Reading response body")
	return y.readResponseBody(resp)
}

func (y *Youtube) sendCommentsToChannel(commentsChan chan<- *types.ChatMessage, comments []types.ChatMessage, date *time.Time) bool {
	for _, comment := range comments {
		if date != nil && time.Unix(comment.Timestamp, 0).Before(*date) {
			y.logVerbose("[COMMENTS] Reached date filter boundary, stopping")
			return true
		}
		commentsChan <- &comment
	}
	return false
}

func (y *Youtube) logAndWrapError(err error, message string) error {
	if message != "" {
		err = fmt.Errorf("%s: %w", message, err)
	}
	y.log.Error("%v", err)
	return err
}

func (y *Youtube) createInitialCommentsPayload() ([]byte, error) {
	payload := map[string]interface{}{
		"context":      y.config.INNERTUBE_CONTEXT,
		"continuation": y.extractCommentsContinuation(),
	}
	return json.Marshal(payload)
}

func (y *Youtube) createContinuationPayload(continuation string) ([]byte, error) {
	payload := map[string]interface{}{
		"context":      y.config.INNERTUBE_CONTEXT,
		"continuation": continuation,
	}
	return json.Marshal(payload)
}

func (y *Youtube) extractCommentsContinuation() string {
	if y.continuation != "" {
		return y.continuation
	}

	url := youtubeBaseURL + "/watch?v=" + y.videoID
	data, err := y.fetchPage(url)
	if err != nil {
		y.log.Error("Failed to fetch page: %v", err)
		return ""
	}

	match := initialDataRegex.FindSubmatch(data)
	if len(match) < 2 {
		return ""
	}

	jsonBytes := match[1]
	continuationPaths := []string{
		"contents.twoColumnWatchNextResults.results.results.contents.#.itemSectionRenderer.contents.#.continuationItemRenderer.continuationEndpoint.continuationCommand.token",
		"engagementPanels.#.engagementPanelSectionListRenderer.content.structuredDescriptionContentRenderer.items.#.videoDescriptionHeaderRenderer.commentsSectionButton.buttonRenderer.command.continuationCommand.token",
	}

	for _, path := range continuationPaths {
		result := gjson.GetBytes(jsonBytes, path)
		if result.Exists() && result.String() != "" {
			return result.String()
		}
	}

	return ""
}

func (y *Youtube) readResponseBody(resp *http.Response) ([]byte, error) {
	decoder := json.NewDecoder(resp.Body)
	var rawResponse json.RawMessage
	if err := decoder.Decode(&rawResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return rawResponse, nil
}

func (y *Youtube) extractComments(data []byte) ([]types.ChatMessage, error) {
	y.logVerbose("[COMMENTS] Starting comment extraction from %d bytes of data", len(data))

	items, err := y.findCommentItems(data)
	if err != nil {
		return nil, err
	}

	y.logVerbose("[COMMENTS] Processing %d comment items", len(items))
	comments := y.parseCommentItems(items)

	y.logVerbose("[COMMENTS] Successfully extracted %d comments total", len(comments))
	return comments, nil
}

func (y *Youtube) findCommentItems(data []byte) ([]interface{}, error) {
	contentPaths := []string{
		"onResponseReceivedEndpoints.#.reloadContinuationItemsCommand.continuationItems",
		"onResponseReceivedEndpoints.#.appendContinuationItemsAction.continuationItems",
		"continuationContents.itemSectionContinuation.contents",
	}

	for _, path := range contentPaths {
		y.logVerbose("[COMMENTS] Trying content path: %s", path)

		result := gjson.GetBytes(data, path)
		if !result.Exists() {
			continue
		}

		var items []interface{}
		if err := json.Unmarshal([]byte(result.Raw), &items); err == nil && len(items) > 0 {
			y.logVerbose("[COMMENTS] Found %d items using path: %s", len(items), path)
			return items, nil
		}
	}

	y.logVerbose("[COMMENTS] No comment items found in any path, returning ErrNoComments")
	return nil, ErrNoComments
}

func (y *Youtube) parseCommentItems(items []interface{}) []types.ChatMessage {
	comments := make([]types.ChatMessage, 0, 20)

	for i, item := range items {
		y.logVerbose("[COMMENTS] Processing item %d/%d", i+1, len(items))

		itemBytes, _ := json.Marshal(item)
		comment := y.parseCommentItem(itemBytes)
		if comment == nil {
			y.logVerbose("[COMMENTS] Failed to parse item %d", i)
			continue
		}

		y.logVerbose("[COMMENTS] Parsed comment: ID=%s, Author=%s", comment.ID, comment.Author.Name)
		comments = append(comments, *comment)

		replies := y.tryExtractReplies(itemBytes, comment.ID)
		comments = append(comments, replies...)
	}

	return comments
}

func (y *Youtube) tryExtractReplies(itemBytes []byte, commentID string) []types.ChatMessage {
	replies, err := y.extractReplies(itemBytes)
	if err == nil {
		y.logVerbose("[COMMENTS] Found %d replies for comment %s", len(replies), commentID)
		return replies
	}
	return nil
}

func (y *Youtube) parseCommentItem(data []byte) *types.ChatMessage {
	rendererPath := "commentThreadRenderer.comment.commentRenderer"
	if !gjson.GetBytes(data, rendererPath).Exists() {
		rendererPath = "commentRenderer"
		if !gjson.GetBytes(data, rendererPath).Exists() {
			return nil
		}
	}

	commentID := gjson.GetBytes(data, rendererPath+".commentId").String()
	if commentID == "" {
		return nil
	}

	authorName := gjson.GetBytes(data, rendererPath+".authorText.simpleText").String()
	authorID := gjson.GetBytes(data, rendererPath+".authorEndpoint.browseEndpoint.browseId").String()
	authorThumbnail := gjson.GetBytes(data, rendererPath+".authorThumbnail.thumbnails.0.url").String()

	message := y.extractCommentText(data, rendererPath)

	timestampText := gjson.GetBytes(data, rendererPath+".publishedTimeText.runs.0.text").String()
	timestamp := parseRelativeTime(timestampText)

	likeCount := gjson.GetBytes(data, rendererPath+".voteCount.simpleText").String()
	likes := parseCount(likeCount)

	replyCount := gjson.GetBytes(data, rendererPath+".replyCount").Int()

	isPinned := gjson.GetBytes(data, rendererPath+".pinnedCommentBadge").Exists()
	isFavorited := gjson.GetBytes(data, rendererPath+".authorIsChannelOwner").Bool()

	badges := y.extractCommentBadges(data, rendererPath)
	isVerified := utils.IsVerifiedBadge(badges)

	return &types.ChatMessage{
		ID:      commentID,
		Message: message,
		Author: types.Author{
			ID:         authorID,
			Name:       authorName,
			Thumbnail:  authorThumbnail,
			URL:        fmt.Sprintf(youtubeChannelURL, authorID),
			IsUploader: isFavorited,
			IsVerified: isVerified,
			Badges:     badges,
		},
		IsPinned:    isPinned,
		IsFavorited: isFavorited,
		ReplyCount:  int(replyCount),
		LikeCount:   likes,
		Timestamp:   timestamp,
	}
}

func (y *Youtube) extractCommentText(data []byte, basePath string) string {
	runs := gjson.GetBytes(data, basePath+".contentText.runs")
	if !runs.Exists() {
		return ""
	}

	var builder strings.Builder
	runs.ForEach(func(_, value gjson.Result) bool {
		text := value.Get("text").String()
		if text != "" {
			builder.WriteString(text)
		}
		return true
	})

	return strings.TrimSpace(builder.String())
}

func (y *Youtube) extractCommentBadges(data []byte, basePath string) []types.Badge {
	badgesResult := gjson.GetBytes(data, basePath+".authorCommentBadge.authorCommentBadgeRenderer")
	if !badgesResult.Exists() {
		return nil
	}

	badges := make([]types.Badge, 0, 1)
	iconURL := badgesResult.Get("icon.iconType").String()
	tooltip := badgesResult.Get("tooltip").String()

	badges = append(badges, types.Badge{
		Tooltip: tooltip,
		Label:   tooltip,
		IconURL: iconURL,
	})

	return badges
}

func (y *Youtube) extractReplies(data []byte) ([]types.ChatMessage, error) {
	repliesPath := "commentThreadRenderer.replies.commentRepliesRenderer.contents"
	repliesResult := gjson.GetBytes(data, repliesPath)
	if !repliesResult.Exists() {
		return nil, fmt.Errorf("no replies")
	}

	var replies []types.ChatMessage
	repliesResult.ForEach(func(_, value gjson.Result) bool {
		replyBytes := []byte(value.Raw)
		reply := y.parseCommentItem(replyBytes)
		if reply != nil {
			parentID := gjson.GetBytes(data, "commentThreadRenderer.comment.commentRenderer.commentId").String()
			reply.Parent = parentID
			replies = append(replies, *reply)
		}
		return true
	})

	return replies, nil
}

// timeUnitMultipliers maps time unit prefixes to their duration in hours
var timeUnitMultipliers = map[string]time.Duration{
	"second": time.Second,
	"minute": time.Minute,
	"hour":   time.Hour,
	"day":    24 * time.Hour,
	"week":   7 * 24 * time.Hour,
	"month":  30 * 24 * time.Hour,
	"year":   365 * 24 * time.Hour,
}

// parseRelativeTime converts a relative time string (e.g., "2 hours ago") to Unix timestamp
func parseRelativeTime(timeStr string) int64 {
	now := time.Now()
	timeStr = strings.ToLower(strings.TrimSpace(timeStr))

	if timeStr == "" || timeStr == "just now" {
		return now.Unix()
	}

	parts := strings.Fields(timeStr)
	if len(parts) < 2 {
		return now.Unix()
	}

	value, err := strconv.Atoi(parts[0])
	if err != nil {
		return now.Unix()
	}

	duration := findTimeUnitDuration(parts[1])
	if duration == 0 {
		return now.Unix()
	}

	return now.Add(-time.Duration(value) * duration).Unix()
}

// findTimeUnitDuration returns the duration for a given time unit string
func findTimeUnitDuration(unit string) time.Duration {
	for prefix, duration := range timeUnitMultipliers {
		if strings.HasPrefix(unit, prefix) {
			return duration
		}
	}
	return 0
}

func parseCount(countStr string) int {
	countStr = strings.TrimSpace(countStr)
	countStr = strings.ReplaceAll(countStr, ",", "")

	multiplier := 1
	if strings.HasSuffix(countStr, "K") {
		multiplier = 1000
		countStr = strings.TrimSuffix(countStr, "K")
	} else if strings.HasSuffix(countStr, "M") {
		multiplier = 1000000
		countStr = strings.TrimSuffix(countStr, "M")
	}

	value, err := strconv.ParseFloat(countStr, 64)
	if err != nil {
		return 0
	}

	return int(value * float64(multiplier))
}
