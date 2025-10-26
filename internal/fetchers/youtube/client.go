package youtube

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xorvus/scrap-chat/internal/utils"
)

func (y *Youtube) newHTTPClient() *http.Client {
	return newDefaultHTTPClient()
}

func (y *Youtube) executeWithRetry(url, method string, body io.Reader) (*http.Response, error) {
	y.log.Debug("Starting HTTP request with retry: %s %s", method, url)

	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt)
			y.log.Debug("Retry attempt %d/%d after %v delay", attempt, maxRetries, delay)
			time.Sleep(delay)
		}

		y.log.Debug("Executing %s request to %s (attempt %d)", method, url, attempt+1)

		resp, err := y.executeRequest(url, method, body)
		if err != nil {
			lastErr = fmt.Errorf("HTTP error: %w", err)
			y.log.Warn("HTTP request failed on attempt %d: %v", attempt+1, err)
			continue
		}

		y.log.Debug("Received response with status: %d", resp.StatusCode)

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			if err := resp.Body.Close(); err != nil {
				y.log.Error("Error closing response body: %v", err)
			}
			y.log.Warn("Server error on attempt %d: %d", attempt+1, resp.StatusCode)
			continue
		}

		y.log.Debug("Request completed successfully")

		return resp, nil
	}

	y.log.Error("Max retries exceeded after %d attempts", maxRetries)

	return nil, fmt.Errorf("%w: last error: %v", ErrMaxRetriesExceeded, lastErr)
}

func (y *Youtube) executeRequest(url, method string, body io.Reader) (*http.Response, error) {
	y.logVerbose("[CLIENT] Preparing %s request to: %s", method, url)

	bodyBytes, err := y.readRequestBody(body)
	if err != nil {
		return nil, err
	}

	req, err := y.createHTTPRequest(method, url, bodyBytes)
	if err != nil {
		return nil, err
	}

	y.logVerbose("[CLIENT] Executing HTTP request")

	resp, err := y.httpClient.Do(req)
	if err != nil {
		err := fmt.Errorf("failed to execute request: %w", err)
		y.logVerbose("[CLIENT] Request execution failed: %v", err)
		return nil, err
	}

	y.logVerbose("[CLIENT] Request executed successfully, received response")
	return resp, nil
}

func (y *Youtube) readRequestBody(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, nil
	}

	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		err := fmt.Errorf("failed to read body: %w", err)
		y.logVerbose("[CLIENT] %v", err)
		return nil, err
	}

	y.logVerbose("[CLIENT] Request body size: %d bytes", len(bodyBytes))
	return bodyBytes, nil
}

func (y *Youtube) createHTTPRequest(method, url string, bodyBytes []byte) (*http.Request, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		err := fmt.Errorf("request error: %w", err)
		y.logVerbose("[CLIENT] Failed to create request: %v", err)
		return nil, err
	}

	y.logVerbose("[CLIENT] Headers added to request")
	y.copyHeaders(req)

	if method == "POST" && len(bodyBytes) > 0 {
		contentType := y.getContentType(url)
		req.Header.Set("Content-Type", contentType)
		y.logVerbose("[CLIENT] Content-Type set to %s", contentType)
	}

	return req, nil
}

func (y *Youtube) getContentType(url string) string {
	switch {
	case strings.Contains(url, "/chooseServer"):
		return "application/json+protobuf"
	case strings.Contains(url, "/multi-watch/channel"):
		return "application/x-www-form-urlencoded"
	case strings.Contains(url, "/refreshCreds"):
		return "application/json"
	default:
		return "application/json"
	}
}

func (y *Youtube) fetchPage(url string) ([]byte, error) {
	y.log.Debug("Starting page fetch from: %s", url)

	client := newFetchPageHTTPClient()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		err := fmt.Errorf("failed to create request: %w", err)
		y.log.Error("Request creation failed: %v", err)
		return nil, err
	}

	// Apply common browser headers
	y.setPageFetchHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		err := fmt.Errorf("error visiting URL: %w", err)
		y.log.Error("Page fetch failed: %v", err)
		return nil, err
	}

	if resp == nil || resp.Body == nil {
		err := fmt.Errorf("received nil response from URL: %s", url)
		y.log.Error("%v", err)
		return nil, err
	}

	y.log.Debug("HTTP response received with status: %d", resp.StatusCode)

	defer func() {
		if err := resp.Body.Close(); err != nil {
			y.log.Error("Error closing response body: %v", err)
		}
	}()

	limited := io.LimitReader(resp.Body, maxResponseBodyBytes)

	y.log.Debug("Reading response body with limit: %d bytes", maxResponseBodyBytes)

	data, err := io.ReadAll(limited)
	if err != nil {
		err := fmt.Errorf("failed to read response: %w", err)
		y.log.Error("Failed to read response: %v", err)
		return nil, err
	}

	y.log.Debug("Successfully read %d bytes from response", len(data))

	return data, nil
}

func calculateBackoff(attempt int) time.Duration {
	delay := time.Duration(baseRetryDelay.Milliseconds()*int64(1<<uint(attempt-1))) * time.Millisecond
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}

func shouldRetryConnection(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	retryPatterns := []string{
		"context deadline exceeded",
		"connection reset",
		"unexpected EOF",
	}

	for _, pattern := range retryPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// SavePageHTML fetches a YouTube page and saves its HTML content to a file.
// This is useful for debugging and analyzing page structure.
// The HTML is saved exactly as received from the server.
func (y *Youtube) SavePageHTML(url, outputPath string) error {
	y.log.Debug("Saving HTML from URL: %s to: %s", url, outputPath)

	htmlContent, err := y.fetchPage(url)
	if err != nil {
		return fmt.Errorf("failed to fetch page: %w", err)
	}

	if err := utils.SaveHTML(htmlContent, outputPath); err != nil {
		return fmt.Errorf("failed to save HTML: %w", err)
	}

	y.log.Debug("Successfully saved %d bytes to: %s", len(htmlContent), outputPath)

	return nil
}
