package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/xorvus/scrap-chat/internal/utils"
	"github.com/xorvus/scrap-chat/types"
)

const timeFormat = "2006/01/02 15:04:05"

func formatLiveChatMessage(msg *types.LiveChatMessage, format, customOutput string) string {
	switch format {
	case formatJSON:
		return marshalJSON(msg)
	case formatCustom:
		return formatCustomLiveChat(msg, customOutput)
	default:
		return formatDefaultLiveChat(msg)
	}
}

func formatVideoChatMessage(msg *types.ChatMessage, format, customOutput string) string {
	switch format {
	case formatJSON:
		return marshalJSON(msg)
	case formatCustom:
		return formatCustomVideoChat(msg, customOutput)
	default:
		return formatDefaultVideoChat(msg)
	}
}

func formatChannelInfo(info *types.ChannelInfo, format, customOutput string) (string, error) {
	switch format {
	case formatJSON:
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal JSON: %w", err)
		}
		return string(data), nil
	case formatCustom:
		if customOutput == "" {
			return "", fmt.Errorf("custom format requires custom-output template")
		}
		return applyInfoCustomTemplate(customOutput, info), nil
	default:
		return fmt.Sprintf("%+v", info), nil
	}
}

func marshalJSON(v interface{}) string {
	data, err := json.MarshalIndent(v, "  ", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %v", err)
	}
	return string(data)
}

func formatDefaultLiveChat(msg *types.LiveChatMessage) string {
	timestamp := time.Unix(msg.Timestamp, 0).Format(timeFormat)
	return fmt.Sprintf("[%s] [%s] %s", timestamp, msg.Author.Name, msg.Message)
}

func formatDefaultVideoChat(msg *types.ChatMessage) string {
	timestamp := time.Unix(msg.Timestamp, 0).Format(timeFormat)
	replyStr := ""
	if msg.Parent != "" {
		replyStr = " [REPLY]"
	}
	return fmt.Sprintf("[%s] [%s]%s %s (👍 %d | 💬 %d)",
		timestamp, msg.Author.Name, replyStr, msg.Message, msg.LikeCount, msg.ReplyCount)
}

func formatCustomLiveChat(msg *types.LiveChatMessage, customOutput string) string {
	if strings.TrimSpace(customOutput) == "" {
		log.Fatal("Custom format requires custom-output template")
	}
	return applyLiveCustomTemplate(customOutput, msg)
}

func formatCustomVideoChat(msg *types.ChatMessage, customOutput string) string {
	if strings.TrimSpace(customOutput) == "" {
		log.Fatal("Custom format requires custom-output template")
	}
	return applyVideoCustomTemplate(customOutput, msg)
}

func applyInfoCustomTemplate(template string, info *types.ChannelInfo) string {
	return utils.NewTemplateProcessor(template).
		Set("ID", info.ID).
		Set("NAME", info.Name).
		Set("DESC", info.Description).
		Set("IMAGE", info.Image).
		Set("URL", info.URL).
		Process()
}

func applyLiveCustomTemplate(template string, msg *types.LiveChatMessage) string {
	badges := utils.ExtractBadgeInfo(msg.Author.Badges)

	return utils.NewTemplateProcessor(template).
		Set("FANS_RANKING", msg.Author.Ranking).
		Set("AUTHOR_NAME", msg.Author.Name).
		Set("AUTHOR_MEMBERSHIP", badges.Labels).
		Set("AUTHOR_MEMBERSHIP_BADGES", badges.Images).
		Set("MESSAGE", msg.Message).
		Set("TIME", time.Unix(msg.Timestamp, 0).Format("2006-01-02 15:04:05")).
		Set("TIMESTAMP", strconv.FormatInt(msg.Timestamp, 10)).
		Set("ID", msg.ID).
		Set("AUTHOR_ID", msg.Author.ID).
		Set("AUTHOR_URL", msg.Author.URL).
		Set("AUTHOR_THUMBNAIL", msg.Author.Thumbnail).
		Process()
}

func applyVideoCustomTemplate(template string, msg *types.ChatMessage) string {
	badges := utils.ExtractBadgeInfo(msg.Author.Badges)

	return utils.NewTemplateProcessor(template).
		Set("AUTHOR_NAME", msg.Author.Name).
		Set("AUTHOR_MEMBERSHIP", badges.Labels).
		Set("AUTHOR_MEMBERSHIP_BADGES", badges.Images).
		Set("MESSAGE", msg.Message).
		Set("TIME", time.Unix(msg.Timestamp, 0).Format("2006-01-02 15:04:05")).
		Set("TIMESTAMP", strconv.FormatInt(msg.Timestamp, 10)).
		Set("ID", msg.ID).
		Set("PARENT_ID", msg.Parent).
		Set("AUTHOR_ID", msg.Author.ID).
		Set("AUTHOR_URL", msg.Author.URL).
		Set("AUTHOR_THUMBNAIL", msg.Author.Thumbnail).
		Set("LIKE_COUNT", strconv.Itoa(msg.LikeCount)).
		Set("REPLY_COUNT", strconv.Itoa(msg.ReplyCount)).
		Set("IS_PINNED", strconv.FormatBool(msg.IsPinned)).
		Set("IS_FAVORITED", strconv.FormatBool(msg.IsFavorited)).
		Process()
}
