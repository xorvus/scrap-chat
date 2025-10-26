package youtube

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/xorvus/scrap-chat/types"
)

// FetchChannelInfo retrieves basic information about a YouTube channel.
// The path parameter can be a full YouTube URL or a channel handle (e.g., "@channelname").
// Returns channel ID, name, description, image URL, and channel URL.
func (y *Youtube) FetchChannelInfo(path string) (*types.ChannelInfo, error) {
	y.logVerbose("[CHANNEL] Starting channel info fetch for path: %s", path)

	normalizedPath := y.normalizeChannelPath(path)
	doc, err := y.fetchChannelDocument(normalizedPath)
	if err != nil {
		return nil, err
	}

	info := &types.ChannelInfo{}
	y.extractChannelMetadata(doc, info)

	y.logVerbose("[CHANNEL] Extracted channel metadata: Name='%s', ID='%s', URL='%s'", info.Name, info.ID, info.URL)

	if info.URL == "" {
		err := fmt.Errorf("could not extract channel URL from page")
		y.logVerbose("[CHANNEL] %v", err)
		return nil, err
	}

	y.logVerbose("[CHANNEL] Successfully fetched channel info for: %s", info.Name)
	return info, nil
}

func (y *Youtube) normalizeChannelPath(path string) string {
	if !strings.HasPrefix(path, "http") && strings.Contains(path, "@") {
		originalPath := path
		path = youtubeBaseURL + "/" + path
		y.logVerbose("[CHANNEL] Normalized path from '%s' to '%s'", originalPath, path)
	}
	return path
}

func (y *Youtube) fetchChannelDocument(path string) (*goquery.Document, error) {
	y.logVerbose("[CHANNEL] Making HTTP request to: %s", path)

	client := newDefaultHTTPClient()
	resp, err := client.Get(path)
	if err != nil {
		y.logVerbose("[CHANNEL] HTTP request failed: %v", err)
		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			y.log.Error("Error closing response body: %v", err)
		}
	}()

	y.logVerbose("[CHANNEL] HTTP response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		y.logVerbose("[CHANNEL] Failed to parse HTML response: %v", err)
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	y.logVerbose("[CHANNEL] Successfully parsed HTML document")
	return doc, nil
}

func (y *Youtube) extractChannelMetadata(doc *goquery.Document, info *types.ChannelInfo) {
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
			parts := strings.SplitN(info.URL, "/channel/", 2)
			if len(parts) == 2 {
				info.ID = parts[1]
			}
		}
	})
}
