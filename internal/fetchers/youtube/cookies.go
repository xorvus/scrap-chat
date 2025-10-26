package youtube

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// AddCookies loads authentication cookies from a Netscape format cookie file.
// This allows access to members-only or restricted YouTube live streams.
// The cookies are automatically included in all subsequent HTTP requests.
func (y *Youtube) AddCookies(path string) error {
	y.logVerbose("[COOKIES] Starting to add cookies from file: %s", path)

	file, err := os.Open(path)
	if err != nil {
		err := fmt.Errorf("failed to open cookie file: %w", err)
		y.logVerbose("[COOKIES] %v", err)
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			y.log.Error("Error closing cookie file: %v", err)
		}
	}()

	cookies, lineCount, processedCount := y.parseCookieFile(file)

	y.logVerbose("[COOKIES] Successfully processed %d out of %d lines, loaded %d cookies", processedCount, lineCount, len(cookies))

	y.cookies = cookies
	y.cookieString = createCookieString(cookies)

	y.logVerbose("[COOKIES] Cookie string created with length: %d", len(y.cookieString))
	return nil
}

func (y *Youtube) parseCookieFile(file *os.File) ([]*http.Cookie, int, int) {
	cookies := make([]*http.Cookie, 0, 16)
	scanner := bufio.NewScanner(file)
	lineCount := 0
	processedCount := 0

	for scanner.Scan() {
		lineCount++
		line := scanner.Text()

		if y.shouldSkipCookieLine(line) {
			continue
		}

		cookie, err := y.parseCookieLine(line, lineCount)
		if err != nil {
			continue
		}

		cookies = append(cookies, cookie)
		processedCount++
		y.logVerbose("[COOKIES] Added cookie: %s=%s", cookie.Name, cookie.Value)
	}

	if err := scanner.Err(); err != nil {
		y.logVerbose("[COOKIES] Error reading cookie file: %v", err)
	}

	return cookies, lineCount, processedCount
}

func (y *Youtube) shouldSkipCookieLine(line string) bool {
	return strings.HasPrefix(line, "#") || strings.TrimSpace(line) == ""
}

func (y *Youtube) parseCookieLine(line string, lineNum int) (*http.Cookie, error) {
	parts := strings.Split(line, "\t")
	if len(parts) != 7 {
		y.logVerbose("[COOKIES] Invalid cookie format on line %d, skipping: %s", lineNum, line)
		return nil, fmt.Errorf("invalid format")
	}

	timestamp, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		y.log.Error("Error parsing cookie time: %v", err)
		return nil, err
	}

	return &http.Cookie{
		Domain:  parts[0],
		Path:    parts[2],
		Name:    parts[5],
		Value:   parts[6],
		Expires: time.Unix(timestamp, 0),
		Secure:  parts[3] == "TRUE",
	}, nil
}

func createCookieString(cookies []*http.Cookie) string {
	if len(cookies) == 0 {
		return ""
	}

	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, fmt.Sprintf("%s=%s", c.Name, c.Value))
	}
	return strings.Join(parts, "; ")
}
