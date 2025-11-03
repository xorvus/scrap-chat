package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tidwall/gjson"
)

func main() {
	data, err := os.ReadFile("/Users/corax/Projects/working/byadtech/scrap-chat/comment.json")
	if err != nil {
		panic(err)
	}

	// Find comment items
	contentPaths := []string{
		"onResponseReceivedEndpoints.#.reloadContinuationItemsCommand.continuationItems",
		"onResponseReceivedEndpoints.#.appendContinuationItemsAction.continuationItems",
		"continuationContents.itemSectionContinuation.contents",
	}

	for _, path := range contentPaths {
		result := gjson.GetBytes(data, path)
		if !result.Exists() {
			continue
		}

		var items []interface{}
		if err := json.Unmarshal([]byte(result.Raw), &items); err == nil && len(items) > 0 {
			fmt.Printf("Found %d items at path: %s\n", len(items), path)

			// Check each item for replies
			for i, item := range items {
				itemBytes, _ := json.Marshal(item)

				hasReplies := gjson.GetBytes(itemBytes, "commentThreadRenderer.replies.commentRepliesRenderer").Exists()
				if hasReplies {
					commentID := gjson.GetBytes(itemBytes, "commentThreadRenderer.comment.commentRenderer.commentId").String()
					fmt.Printf("\nItem %d - Comment ID: %s has replies!\n", i, commentID)

					// Print reply structure
					repliesJSON := gjson.GetBytes(itemBytes, "commentThreadRenderer.replies.commentRepliesRenderer").Raw
					fmt.Printf("Replies structure: %s\n", repliesJSON[:min(500, len(repliesJSON))])
				}
			}
			break
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
