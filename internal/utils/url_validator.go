package utils

import (
	"net/url"
	"strings"
)

// IsValidYouTubeURL validates if the given string is a valid YouTube URL or handle.
func IsValidYouTubeURL(urlStr string) bool {
	if urlStr == "" {
		return false
	}

	if strings.HasPrefix(urlStr, "@") {
		return true
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	if parsedURL.Host == "" {
		return false
	}

	validHosts := []string{
		"youtube.com",
		"www.youtube.com",
		"m.youtube.com",
		"youtu.be",
	}

	host := strings.ToLower(parsedURL.Host)
	for _, validHost := range validHosts {
		if host == validHost || strings.HasSuffix(host, "."+validHost) {
			return true
		}
	}

	return false
}

// NormalizeYouTubeURL normalizes a YouTube URL by adding https:// prefix if missing.
func NormalizeYouTubeURL(urlStr string) string {
	urlStr = strings.TrimSpace(urlStr)

	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		if strings.HasPrefix(urlStr, "@") {
			return urlStr
		}
		urlStr = "https://" + urlStr
	}

	return urlStr
}
