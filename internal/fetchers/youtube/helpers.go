package youtube

import (
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func parseMicroSeconds(usecStr string) time.Time {
	usec, err := strconv.ParseInt(usecStr, 10, 64)
	if err != nil {
		return time.Now()
	}
	return time.Unix(0, usec*1000)
}

func extractJSONPath(jsonBytes []byte, paths []string) string {
	for _, path := range paths {
		if strings.Contains(path, ".#.") {
			results := gjson.GetBytes(jsonBytes, path)
			if results.IsArray() {
				for _, item := range results.Array() {
					if item.String() != "" {
						return item.String()
					}
				}
			}
		} else {
			result := gjson.GetBytes(jsonBytes, path)
			if result.Exists() && result.String() != "" {
				return result.String()
			}
		}
	}
	return ""
}

func shouldAddSpacing(textBuilder *strings.Builder, index int) bool {
	if index == 0 || textBuilder.Len() == 0 {
		return false
	}
	lastChar := textBuilder.String()[textBuilder.Len()-1]
	return lastChar != ' ' && lastChar != '\n'
}

func normalizeMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	return strings.Join(strings.Fields(trimmed), " ")
}
