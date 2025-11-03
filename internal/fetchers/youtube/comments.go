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

func (y *Youtube) FetchVideoComments(videoID string, date *time.Time) (<-chan *types.ChatMessage, error) {
	y.log.Debug("Starting comment fetch for video: %s", videoID)
	if date != nil {
		y.log.Debug("Filtering comments after date: %v", date)
	}

	normalizedVideoID := y.normalizeVideoID(videoID)

	if err := y.setupVideoConfig(normalizedVideoID); err != nil {
		return nil, err
	}

	if y.videoID == "" {
		return nil, fmt.Errorf("failed to extract video ID")
	}

	commentsChan := make(chan *types.ChatMessage, defaultChannelBuffer)
	go y.startCommentsFetch(commentsChan, date)

	return commentsChan, nil
}

func (y *Youtube) normalizeVideoID(videoID string) string {
	if !strings.HasPrefix(videoID, "http") {
		return youtubeBaseURL + "/watch?v=" + videoID
	}
	return videoID
}

func (y *Youtube) setupVideoConfig(videoID string) error {
	if err := y.getConfig(videoID); err != nil {
		return fmt.Errorf("failed to get video config: %w", err)
	}
	return nil
}

func (y *Youtube) startCommentsFetch(commentsChan chan *types.ChatMessage, date *time.Time) {
	defer close(commentsChan)

	if err := y.fetchCommentsRecursive(commentsChan, "", date); err != nil {
		if !errors.Is(err, ErrNoComments) && !errors.Is(err, ErrNoContinuation) {
			y.log.Error("Error fetching comments: %v", err)
		}
	}
}

func (y *Youtube) fetchCommentsRecursive(commentsChan chan<- *types.ChatMessage, continuation string, date *time.Time) error {
	currentContinuation := continuation

	for iteration := 0; iteration < maxCommentIterations; iteration++ {
		if currentContinuation == "" {
			initialContinuation := y.extractCommentsContinuation()
			if initialContinuation == "" {
				y.log.Debug("Could not extract initial continuation for iteration %d", iteration)
				return fmt.Errorf("could not extract initial continuation")
			}
			currentContinuation = initialContinuation
			y.log.Debug("Starting initial comment fetch (iteration %d)", iteration+1)
		} else {
			y.log.Debug("Fetching comments (iteration %d)", iteration+1)
		}

		payload, err := y.createCommentsPayload(currentContinuation)
		if err != nil {
			return fmt.Errorf("failed to create payload: %w", err)
		}

		// Fetch comments without timeout (let it complete naturally)
		body, err := y.fetchCommentsResponse(payload)
		if err != nil {
			y.log.Error("Failed to fetch comments: %v", err)
			return err
		}

		if len(body) == 0 {
			y.log.Debug("Received empty response body, stopping comment fetch")
			return nil
		}

		comments, err := y.extractComments(body)
		if err != nil {
			y.log.Debug("Error extracting comments: %v", err)
			return err
		}

		commentCount := len(comments)
		y.log.Debug("Extracted %d comments in iteration %d", commentCount, iteration+1)

		if commentCount > 0 {
			if shouldStop := y.sendCommentsToChannel(commentsChan, comments, date); shouldStop {
				y.log.Debug("Date filter stopped comment fetch at iteration %d", iteration+1)
				return nil
			}
		} else {
			y.log.Debug("No comments extracted in iteration %d, checking for continuation", iteration+1)
		}

		nextContinuation, err := extractContinuationFromComments(body)
		if err != nil {
			if err == ErrNoContinuation {
				y.log.Debug("No more continuation found after %d iterations, stopping comment fetch", iteration+1)
				return nil
			}
			y.log.Debug("Error extracting continuation: %v", err)
			return err
		}

		if nextContinuation == "" {
			y.log.Debug("Empty continuation token found after %d iterations", iteration+1)
			return nil
		}

		y.log.Debug("Found next continuation token (first 50 chars): %.50s...", nextContinuation)
		currentContinuation = nextContinuation

		// Add delay before next API call to respect rate limiting
		if iteration < maxCommentIterations-1 {
			time.Sleep(commentAPIDelay)
		}
	}

	// Reached maximum iterations (safety limit)
	y.log.Warn("Reached safety limit of %d iterations, stopping comment fetch", maxCommentIterations)
	return nil
}

func (y *Youtube) createCommentsPayload(continuation string) ([]byte, error) {
	if continuation == "" {
		continuation = y.extractCommentsContinuation()
	}

	payload := map[string]interface{}{
		"context":      y.config.INNERTUBE_CONTEXT,
		"continuation": continuation,
	}
	return json.Marshal(payload)
}

func (y *Youtube) fetchCommentsResponse(payload []byte) ([]byte, error) {
	resp, err := y.executeRequest(nextEndpoint, "POST", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			y.log.Error("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var rawResponse json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return rawResponse, nil
}

func (y *Youtube) sendCommentsToChannel(commentsChan chan<- *types.ChatMessage, comments []types.ChatMessage, date *time.Time) bool {
	for _, comment := range comments {
		if date != nil && time.Unix(comment.Timestamp, 0).Before(*date) {
			return true
		}
		commentsChan <- &comment
	}
	return false
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
	continuation := y.findContinuationToken(jsonBytes)
	if continuation != "" {
		return continuation
	}

	return ""
}

func (y *Youtube) findContinuationToken(jsonBytes []byte) string {
	continuationPaths := []string{
		// subMenuItems.1 = Newest First, subMenuItems.0 = Top Comments
		"engagementPanels.#.engagementPanelSectionListRenderer.header.engagementPanelTitleHeaderRenderer.menu.sortFilterSubMenuRenderer.subMenuItems.1.serviceEndpoint.continuationCommand.token",
		"engagementPanels.#.engagementPanelSectionListRenderer.header.engagementPanelTitleHeaderRenderer.menu.sortFilterSubMenuRenderer.subMenuItems.0.serviceEndpoint.continuationCommand.token",
		"contents.twoColumnWatchNextResults.results.results.contents.#.itemSectionRenderer.contents.#.continuationItemRenderer.continuationEndpoint.continuationCommand.token",
		"engagementPanels.#.engagementPanelSectionListRenderer.content.structuredDescriptionContentRenderer.items.#.videoDescriptionHeaderRenderer.commentsSectionButton.buttonRenderer.command.continuationCommand.token",
		"contents.twoColumnWatchNextResults.results.results.contents.-1.itemSectionRenderer.contents.-1.continuationItemRenderer.continuationEndpoint.continuationCommand.token",
		"contents.twoColumnWatchNextResults.results.results.continuationItemRenderer.continuationEndpoint.continuationCommand.token",
	}

	for _, path := range continuationPaths {
		token := y.extractTokenAtPath(jsonBytes, path)
		if token != "" {
			return token
		}
	}

	return ""
}

func (y *Youtube) extractTokenAtPath(jsonBytes []byte, path string) string {
	result := gjson.GetBytes(jsonBytes, path)
	if !result.Exists() {
		return ""
	}

	if result.IsArray() {
		arr := result.Array()
		for _, item := range arr {
			if item.IsArray() {
				innerArr := item.Array()
				for _, innerItem := range innerArr {
					token := innerItem.String()
					if token != "" && token != "undefined" {
						return token
					}
				}
			} else {
				token := item.String()
				if token != "" && token != "undefined" {
					return token
				}
			}
		}
	} else {
		token := result.String()
		if token != "" && token != "undefined" {
			return token
		}
	}

	return ""
}

// ============ PERBAIKAN UTAMA ============

func (y *Youtube) extractComments(data []byte) ([]types.ChatMessage, error) {
	items, err := y.findCommentItems(data)
	if err != nil {
		return nil, err
	}

	// Build reply token map from OLD format (commentThreadRenderer has reply info)
	replyTokenMap := y.buildReplyTokenMap(data)

	comments := make([]types.ChatMessage, 0, 20)
	for _, item := range items {
		itemBytes, _ := json.Marshal(item)
		comment := y.parseCommentItem(itemBytes)
		if comment == nil {
			continue
		}

		comments = append(comments, *comment)

		// Check for and extract replies using token map
		if comment.ReplyCount > 0 {
			if replyToken, exists := replyTokenMap[comment.ID]; exists && replyToken != "" {
				y.log.Debug("Fetching %d replies for comment %s", comment.ReplyCount, comment.ID)
				replies, err := y.fetchRepliesRecursive(comment.ID, replyToken)
				if err == nil && len(replies) > 0 {
					comments = append(comments, replies...)
				}
			}
		}
	}

	return comments, nil
}

// buildReplyTokenMap creates a map of commentId -> replyToken from OLD format
// NEW format (frameworkUpdates) has comment data but NO reply tokens
// OLD format (commentThreadRenderer) has reply tokens
func (y *Youtube) buildReplyTokenMap(data []byte) map[string]string {
	replyTokenMap := make(map[string]string)

	// Try to get commentThreadRenderer items from OLD format paths
	paths := []string{
		"onResponseReceivedEndpoints.1.reloadContinuationItemsCommand.continuationItems",
		"onResponseReceivedEndpoints.0.reloadContinuationItemsCommand.continuationItems",
		"onResponseReceivedEndpoints.1.appendContinuationItemsAction.continuationItems",
		"onResponseReceivedEndpoints.0.appendContinuationItemsAction.continuationItems",
	}

	for _, path := range paths {
		result := gjson.GetBytes(data, path)
		if !result.Exists() || !result.IsArray() {
			continue
		}

		// Found items, process them
		for _, item := range result.Array() {
			// Check if this is a commentThreadRenderer
			if !item.Get("commentThreadRenderer").Exists() {
				continue
			}

			// Extract commentId from commentViewModel
			commentId := item.Get("commentThreadRenderer.commentViewModel.commentViewModel.commentId").String()
			if commentId == "" {
				// Try alternative path
				commentId = item.Get("commentThreadRenderer.comment.commentRenderer.commentId").String()
			}

			if commentId == "" {
				continue
			}

			// Extract reply token
			replyToken := item.Get("commentThreadRenderer.replies.commentRepliesRenderer.subThreads.0.continuationItemRenderer.continuationEndpoint.continuationCommand.token").String()
			if replyToken == "" {
				replyToken = item.Get("commentThreadRenderer.replies.commentRepliesRenderer.contents.0.continuationItemRenderer.continuationEndpoint.continuationCommand.token").String()
			}

			if replyToken != "" && replyToken != "undefined" {
				replyTokenMap[commentId] = replyToken
			}
		}

		// If we found items in this path, don't try other paths
		if len(replyTokenMap) > 0 {
			break
		}
	}

	return replyTokenMap
}

// DIPERBAIKI: Filter continuation items agar tidak diparsing sebagai comment
func (y *Youtube) findCommentItems(data []byte) ([]interface{}, error) {
	// Try NEW format first (frameworkUpdates) - has complete comment data
	newFormatPath := "frameworkUpdates.entityBatchUpdate.mutations"
	result := gjson.GetBytes(data, newFormatPath)
	if result.Exists() {
		var mutations []interface{}
		if err := json.Unmarshal([]byte(result.Raw), &mutations); err == nil && len(mutations) > 0 {
			var commentItems []interface{}
			for _, mutation := range mutations {
				mutationBytes, _ := json.Marshal(mutation)
				if gjson.GetBytes(mutationBytes, "payload.commentEntityPayload").Exists() {
					commentItems = append(commentItems, mutation)
				}
			}
			if len(commentItems) > 0 {
				return commentItems, nil
			}
		}
	}

	// Fallback to OLD format paths
	contentPaths := []string{
		"onResponseReceivedEndpoints.1.reloadContinuationItemsCommand.continuationItems",
		"onResponseReceivedEndpoints.0.reloadContinuationItemsCommand.continuationItems",
		"onResponseReceivedEndpoints.1.appendContinuationItemsAction.continuationItems",
		"onResponseReceivedEndpoints.0.appendContinuationItemsAction.continuationItems",
		"continuationContents.itemSectionContinuation.contents",
		"continuationContents.commentRepliesContinuation.contents", // For reply responses
	}

	for _, path := range contentPaths {
		result := gjson.GetBytes(data, path)
		if !result.Exists() {
			continue
		}

		var items []interface{}
		if err := json.Unmarshal([]byte(result.Raw), &items); err == nil && len(items) > 0 {
			// PERBAIKAN: Filter out continuation items - only return actual comments
			var commentItems []interface{}
			for _, item := range items {
				itemBytes, _ := json.Marshal(item)

				// Skip if this is a continuation item
				if gjson.GetBytes(itemBytes, "continuationItemRenderer").Exists() {
					continue
				}

				// Include if it has comment data
				if gjson.GetBytes(itemBytes, "commentThreadRenderer").Exists() ||
					gjson.GetBytes(itemBytes, "commentRenderer").Exists() {
					commentItems = append(commentItems, item)
				}
			}

			if len(commentItems) > 0 {
				return commentItems, nil
			}
		}
	}

	return nil, ErrNoComments
}

func (y *Youtube) parseCommentItem(data []byte) *types.ChatMessage {
	if gjson.GetBytes(data, "payload.commentEntityPayload").Exists() {
		return y.parseCommentEntityPayload(data)
	}

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

func (y *Youtube) parseCommentEntityPayload(data []byte) *types.ChatMessage {
	payload := gjson.GetBytes(data, "payload.commentEntityPayload")
	if !payload.Exists() {
		return nil
	}

	commentID := payload.Get("properties.commentId").String()
	if commentID == "" {
		return nil
	}

	message := payload.Get("properties.content.content").String()

	authorName := payload.Get("author.displayName").String()
	authorID := payload.Get("author.channelId").String()
	authorThumbnail := payload.Get("author.avatarThumbnailUrl").String()
	isVerified := payload.Get("author.isVerified").Bool()
	isCreator := payload.Get("author.isCreator").Bool()

	timestampText := payload.Get("properties.publishedTime").String()
	timestamp := parseRelativeTime(timestampText)

	likeCountStr := payload.Get("toolbar.likeCountLiked").String()
	if likeCountStr == "" {
		likeCountStr = payload.Get("toolbar.likeCountNotliked").String()
	}
	likes := parseCount(likeCountStr)

	replyCountStr := payload.Get("toolbar.replyCount").String()
	replyCount := parseCount(replyCountStr)

	isPinned := payload.Get("properties.isPinned").Bool()

	replyLevel := payload.Get("properties.replyLevel").Int()
	parent := ""
	if replyLevel > 0 {
		parent = ""
	}

	badges := []types.Badge{}
	if isVerified {
		badges = append(badges, types.Badge{
			Tooltip: "Verified",
			Label:   "Verified",
			IconURL: "CHECK_CIRCLE_THICK",
		})
	}
	if isCreator {
		badges = append(badges, types.Badge{
			Tooltip: "Channel Owner",
			Label:   "Channel Owner",
			IconURL: "OWNER",
		})
	}

	return &types.ChatMessage{
		ID:      commentID,
		Message: message,
		Author: types.Author{
			ID:         authorID,
			Name:       authorName,
			Thumbnail:  authorThumbnail,
			URL:        fmt.Sprintf(youtubeChannelURL, authorID),
			IsUploader: isCreator,
			IsVerified: isVerified,
			Badges:     badges,
		},
		IsPinned:    isPinned,
		IsFavorited: isCreator,
		ReplyCount:  int(replyCount),
		LikeCount:   likes,
		Timestamp:   timestamp,
		Parent:      parent,
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

// DIPERBAIKI: Extract continuation SEBELUM processing items
func (y *Youtube) fetchRepliesRecursive(parentID, continuation string) ([]types.ChatMessage, error) {
	var allReplies []types.ChatMessage
	currentContinuation := continuation

	for iteration := 0; iteration < maxReplyIterations && currentContinuation != ""; iteration++ {
		y.log.Debug("Fetching replies for parent %s (iteration %d)", parentID, iteration+1)

		payload, err := y.createCommentsPayload(currentContinuation)
		if err != nil {
			return allReplies, fmt.Errorf("failed to create replies payload: %w", err)
		}

		// Fetch replies without timeout (let it complete naturally)
		body, err := y.fetchCommentsResponse(payload)
		if err != nil {
			y.log.Error("Failed to fetch replies for parent %s: %v", parentID, err)
			return allReplies, fmt.Errorf("failed to fetch replies: %w", err)
		}

		// PERBAIKAN: Extract continuation FIRST before processing items
		nextContinuation, err := extractContinuationFromComments(body)
		if err != nil && err != ErrNoContinuation {
			y.log.Debug("Error extracting reply continuation: %v", err)
		}

		// Now process the reply items
		items, err := y.findCommentItems(body)
		if err != nil {
			if err == ErrNoComments {
				y.log.Debug("No more reply items found for parent %s", parentID)
				break
			}
			return allReplies, err
		}

		y.log.Debug("Found %d reply items for parent %s (iteration %d)", len(items), parentID, iteration+1)

		for _, item := range items {
			itemBytes, _ := json.Marshal(item)
			reply := y.parseCommentItem(itemBytes)
			if reply != nil {
				reply.Parent = parentID
				allReplies = append(allReplies, *reply)
			}
		}

		// Update continuation for next iteration
		if nextContinuation == "" || err == ErrNoContinuation {
			y.log.Debug("No more reply continuation for parent %s after %d iterations", parentID, iteration+1)
			break
		}
		currentContinuation = nextContinuation

		// Add delay before next API call
		if iteration < maxReplyIterations-1 && currentContinuation != "" {
			time.Sleep(replyAPIDelay)
		}
	}

	y.log.Debug("Total replies fetched for parent %s: %d", parentID, len(allReplies))
	return allReplies, nil
}

// ============ HELPER FUNCTIONS ============

var timeUnitMultipliers = map[string]time.Duration{
	"second": time.Second,
	"minute": time.Minute,
	"hour":   time.Hour,
	"day":    24 * time.Hour,
	"week":   7 * 24 * time.Hour,
	"month":  30 * 24 * time.Hour,
	"year":   365 * 24 * time.Hour,
}

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
