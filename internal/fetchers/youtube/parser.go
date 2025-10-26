package youtube

import (
	"strings"

	"github.com/xorvus/scrap-chat/types"
)

func (y *Youtube) processChatMessages(chatMsgResp *types.YTChatMessagesResponse) []types.YTChatMessage {
	y.log.Debug("Starting to process chat messages response")

	actions := chatMsgResp.ContinuationContents.LiveChatContinuation.Actions
	chatMessages := make([]types.YTChatMessage, 0, len(actions))

	var textBuilder strings.Builder
	thumbnailsBuffer := make([]string, 0, 2)

	y.log.Debug("Processing %d chat actions", len(actions))

	stats := &processingStats{}

	for i, action := range actions {
		y.log.Debug("Processing action %d/%d", i+1, len(actions))

		msg, ok := y.processSingleAction(&textBuilder, &thumbnailsBuffer, action, stats)
		if !ok {
			continue
		}

		chatMessages = append(chatMessages, msg)
		y.log.Debug("Message from %s: '%s'", msg.Author.AuthorName, msg.Message)
	}

	y.logProcessingStats(len(actions), stats, len(chatMessages))
	return chatMessages
}

type processingStats struct {
	skipped    int
	duplicates int
	empty      int
}

func (y *Youtube) processSingleAction(textBuilder *strings.Builder, thumbnailsBuffer *[]string, action types.YTActions, stats *processingStats) (types.YTChatMessage, bool) {
	renderer := action.AddChatItemAction.Item.LiveChatTextMessageRenderer

	if len(renderer.Message.Runs) == 0 {
		stats.skipped++
		y.log.Debug("Skipping action with no message runs")
		return types.YTChatMessage{}, false
	}

	if y.isDuplicateMessage(renderer.ID) {
		stats.duplicates++
		y.log.Debug("Skipping duplicate message: %s", renderer.ID)
		return types.YTChatMessage{}, false
	}

	textBuilder.Reset()
	*thumbnailsBuffer = (*thumbnailsBuffer)[:0]
	textBuilder.Grow(initialMsgCapacity)

	y.buildMessageText(textBuilder, thumbnailsBuffer, renderer.Message.Runs)
	finalMessage := normalizeMessage(textBuilder.String())

	if finalMessage == "" {
		stats.empty++
		y.log.Debug("Skipping empty message after cleanup")
		return types.YTChatMessage{}, false
	}

	y.markMessageSeen(renderer.ID)

	ranking := ""
	if len(renderer.BeforeContentButtons) > 0 {
		ranking = renderer.BeforeContentButtons[0].ButtonViewModel.Title
	}

	return types.YTChatMessage{
		ID: renderer.ID,
		Author: types.YTAuthor{
			AuthorName:   renderer.AuthorName.SimpleText,
			AuthorID:     renderer.AuthorExternalChannelID,
			AuthorImages: renderer.AuthorPhoto.Thumbnails,
			Badges:       renderer.AuthorBadges,
			Ranking:      ranking,
		},
		Timestamp: parseMicroSeconds(renderer.TimestampUsec),
		Message:   finalMessage,
	}, true
}

func (y *Youtube) logProcessingStats(total int, stats *processingStats, final int) {
	y.log.Debug("Completed processing - Total: %d, Skipped: %d, Duplicates: %d, Empty: %d, Final: %d",
		total, stats.skipped, stats.duplicates, stats.empty, final)
}

func (y *Youtube) buildMessageText(textBuilder *strings.Builder, thumbnailsBuffer *[]string, runs []types.YTRuns) {
	y.log.Debug("Starting to build message text with %d runs", len(runs))

	for i, run := range runs {
		y.log.Debug("Processing run %d/%d", i+1, len(runs))
		y.processRun(textBuilder, thumbnailsBuffer, run, i)
	}

	y.finalizeMessageText(textBuilder)

	finalText := textBuilder.String()
	if len(finalText) > 100 {
		y.log.Debug("Final message (truncated): '%s...'", finalText[:100])
	} else {
		y.log.Debug("Final message: '%s'", finalText)
	}
}

func (y *Youtube) processRun(textBuilder *strings.Builder, thumbnailsBuffer *[]string, run types.YTRuns, index int) {
	switch {
	case run.Text != "":
		y.processTextRun(textBuilder, run.Text, index)
	case run.Emoji.IsCustomEmoji:
		y.processCustomEmojiRun(textBuilder, thumbnailsBuffer, run, index)
	default:
		y.processStandardEmojiRun(textBuilder, run, index)
	}
}

func (y *Youtube) processTextRun(textBuilder *strings.Builder, text string, index int) {
	y.log.Debug("Run %d: Text='%s'", index, text)

	y.addSpacingIfNeeded(textBuilder, index)
	textBuilder.WriteString(text)
}

func (y *Youtube) processCustomEmojiRun(textBuilder *strings.Builder, thumbnailsBuffer *[]string, run types.YTRuns, index int) {
	if images := run.Emoji.Image.Thumbnails; len(images) > 0 {
		y.log.Debug("Run %d: Custom emoji with %d images", index, len(images))

		y.addSpacingIfNeeded(textBuilder, index)
		*thumbnailsBuffer = append(*thumbnailsBuffer, images[len(images)-1].Url)

		for _, url := range *thumbnailsBuffer {
			textBuilder.WriteString(url)
		}
	} else {
		y.log.Debug("Run %d: Custom emoji with no images", index)
	}
}

func (y *Youtube) processStandardEmojiRun(textBuilder *strings.Builder, run types.YTRuns, index int) {
	y.log.Debug("Run %d: Standard emoji ID='%s'", index, run.Emoji.EmojiId)

	y.addSpacingIfNeeded(textBuilder, index)

	if run.Emoji.EmojiId != "" {
		textBuilder.WriteString(run.Emoji.EmojiId)
	}
}

func (y *Youtube) addSpacingIfNeeded(textBuilder *strings.Builder, index int) {
	if shouldAddSpacing(textBuilder, index) {
		textBuilder.WriteString(" ")
	}
}

func (y *Youtube) finalizeMessageText(textBuilder *strings.Builder) {
	finalText := strings.TrimSpace(textBuilder.String())
	if finalText != "" {
		textBuilder.Reset()
		textBuilder.WriteString(finalText)
	}
}
