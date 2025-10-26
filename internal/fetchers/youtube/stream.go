package youtube

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/xorvus/scrap-chat/internal/utils"
)

func (y *Youtube) longPolling(param func(string)) {
	y.log.Info("Starting long polling with enhanced recovery...")

	y.updateStreamState(StreamStateConnecting, "Starting long polling connection")

	connectionAttempt := 0
	for {
		connectionAttempt++
		y.log.Debug("Connection attempt #%d", connectionAttempt)

		if err := y.establishConnection(param); err != nil {
			y.log.Warn("Connection attempt #%d failed: %v", connectionAttempt, err)

			if !shouldRetryConnection(err) {
				y.log.Error("Connection error is not retryable, terminating")
				return
			}
			y.handleConnectionError(err)
			continue
		}

		y.log.Info("Successfully established connection on attempt #%d", connectionAttempt)
	}
}

func (y *Youtube) establishConnection(param func(string)) error {
	y.log.Debug("Establishing connection to streaming server")

	y.resetStreamHealth()

	url := y.buildSignalerURL()
	y.log.Debug("Building signaler URL: %s", url)

	resp, err := y.executeWithRetry(url, "GET", nil)
	if err != nil {
		err := fmt.Errorf("HTTP connection failed: %w", err)
		y.log.Error("%v", err)
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			y.log.Error("Error closing response body: %v", err)
		}
	}()

	y.log.Debug("Response received with status: %d", resp.StatusCode)

	y.updateStreamState(StreamStateConnected, "HTTP connection established")
	y.log.Info("Connected to YouTube signaling server")

	y.log.Debug("Starting to process stream data")

	return y.processStreamData(resp.Body, param)
}

func (y *Youtube) buildSignalerURL() string {
	y.log.Debug("[Flow 4] Building long polling URL - gsessionid=%s, SID=%s", y.gsessionID, y.sid)
	return fmt.Sprintf("https://signaler-pa.youtube.com/punctual/multi-watch/channel?VER=8&gsessionid=%s&key=%s&RID=rpc&SID=%s&AID=0&CI=0&TYPE=xmlhttp&zx=%s&t=1",
		y.gsessionID, y.config.API_KEY, y.sid, utils.GenerateZX())
}

func (y *Youtube) processStreamData(body io.ReadCloser, param func(string)) error {
	y.log.Debug("Starting to process stream data")

	reader := bufio.NewReader(body)
	y.updateStreamState(StreamStateReading, "Starting to read from stream")

	lastRefreshTime := time.Now()
	refreshCount := 0
	lineCount := 0

	for {
		lineCount++
		line, readTime, err := y.readStreamWithTimeout(reader)
		if err != nil {
			y.log.Error("Error reading stream line %d: %v", lineCount, err)
			return y.handleStreamReadError(err, readTime)
		}

		y.log.Debug("Successfully read line %d in %v", lineCount, readTime)

		if y.processStreamLine(line, readTime, param) {
			if time.Since(lastRefreshTime) > credRefreshInterval {
				y.log.Debug("Refreshing credentials after %v of inactivity", time.Since(lastRefreshTime))
				y.refreshCreds()
				lastRefreshTime = time.Now()
				refreshCount++

				if refreshCount >= maxCredRefreshes {
					y.log.Debug("Resetting SID after %d credential refreshes", refreshCount)
					y.getSID()
					refreshCount = 0
				}
			}
		}
	}
}

func (y *Youtube) processStreamLine(line string, readTime time.Duration, param func(string)) bool {
	originalLine := line
	line = strings.TrimSpace(line)
	bytesRead := len(originalLine)

	y.logStreamRead(line, readTime, bytesRead)
	y.resetConsecutiveErrors()

	if !y.isValidResponse(line) {
		y.log.Debug("Skipping invalid response")
		return false
	}

	param(line)
	return true
}

func (y *Youtube) readStreamWithTimeout(reader *bufio.Reader) (string, time.Duration, error) {
	y.log.Debug("Reading stream line with timeout: %v", streamReadTimeout)

	ctx, cancel := context.WithTimeout(context.Background(), streamReadTimeout)
	defer cancel()

	startTime := time.Now()
	resultChan := make(chan readResult, 1)

	go func() {
		y.log.Debug("Starting async read operation")
		line, err := reader.ReadString('\n')
		resultChan <- readResult{line: line, err: err}
		y.log.Debug("Async read operation completed")
	}()

	select {
	case result := <-resultChan:
		readTime := time.Since(startTime)
		y.log.Debug("Successfully read line in %v", readTime)
		return result.line, readTime, result.err
	case <-ctx.Done():
		readTime := time.Since(startTime)
		err := fmt.Errorf("stream read timeout after %v: %w", readTime, ctx.Err())
		y.log.Warn("%v", err)
		return "", readTime, err
	}
}

func (y *Youtube) handleStreamReadError(err error, readTime time.Duration) error {
	y.logStreamError(err, "stream reading")

	switch {
	case err == io.EOF:
		y.log.Info("Stream closed gracefully by server")
		y.updateStreamState(StreamStateDisconnected, "Server closed connection")
		return fmt.Errorf("stream closed by server")

	case strings.Contains(err.Error(), "context deadline exceeded"):
		return y.handleStreamTimeout(readTime)

	default:
		y.log.Error("Stream read error: %v", err)
		return fmt.Errorf("stream read error: %w", err)
	}
}

func (y *Youtube) handleStreamTimeout(readTime time.Duration) error {
	y.log.Warn("Stream read timeout after %v, attempting recovery", readTime)
	y.updateStreamState(StreamStateRecovering, "Stream timeout, attempting recovery")

	y.streamMutex.Lock()
	y.streamHealth.ConsecutiveErrors++
	y.streamMutex.Unlock()

	if y.streamHealth.ConsecutiveErrors >= maxConsecutiveErrors {
		y.log.Error("Too many consecutive timeouts (%d), giving up", y.streamHealth.ConsecutiveErrors)
		return fmt.Errorf("too many consecutive timeouts")
	}

	return nil
}

func (y *Youtube) handleConnectionError(err error) {
	y.updateStreamState(StreamStateRecovering, "Retrying connection after error")
	y.log.Warn("Connection error: %v", err)
	time.Sleep(reconnectDelay)
}

func (y *Youtube) isValidResponse(response string) bool {
	if len(response) < minResponseLength {
		return false
	}

	if strings.TrimSpace(response) == "" {
		return false
	}

	lowerResponse := strings.ToLower(response)
	errorPatterns := []string{"error", "not found", "forbidden", "unauthorized"}
	for _, pattern := range errorPatterns {
		if strings.Contains(lowerResponse, pattern) && len(response) < 100 {
			return false
		}
	}

	return true
}

func isRegexTrue(r *regexp.Regexp, str string) bool {
	return r.MatchString(str)
}

func regexGetValue(re *regexp.Regexp, data string) (bool, []string) {
	match := re.FindAllString(data, -1)
	if len(match) > 0 {
		return true, match
	}
	return false, nil
}

func (y *Youtube) getSID() {
	y.log.Debug("[Flow 3] getSID - Starting with gsessionid=%s, videoID=%s", y.gsessionID, y.videoID)
	reqURL := fmt.Sprintf("https://signaler-pa.youtube.com/punctual/multi-watch/channel?VER=8&gsessionid=%s&key=%s&RID=6167&CVER=22&zx=%s&t=1",
		y.gsessionID, y.config.API_KEY, utils.GenerateZX())

	jsonData := fmt.Sprintf(`[[["1",[null,null,null,[9,5],null,[["youtube_live_chat_web"],[1],[[["chat~%s"]]]],null,null,1],null,3]]]`, y.videoID)
	encodedData := fmt.Sprintf("count=1&ofs=0&req0___data__=%s", url.QueryEscape(jsonData))
	y.log.Debug("[Flow 3] getSID payload: %s", encodedData)

	req, err := http.NewRequest("POST", reqURL, strings.NewReader(encodedData))
	if err != nil {
		y.log.Error("getSID: failed to create request: %v", err)
		return
	}

	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("x-webchannel-content-type", "application/json+protobuf")
	y.copyHeaders(req)

	resp, err := y.httpClient.Do(req)
	if err != nil {
		y.log.Error("getSID: HTTP request failed: %v", err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			y.log.Error("Error closing response body: %v", err)
		}
	}()

	limited := io.LimitReader(resp.Body, 1<<20)
	body, err := io.ReadAll(limited)
	if err != nil {
		y.log.Error("getSID read error: %v", err)
		return
	}

	y.extractSIDFromResponse(body)
}

func (y *Youtube) extractSIDFromResponse(body []byte) {
	idx := strings.Index(string(body), "[[")
	if idx == -1 {
		y.log.Error("getSID: JSON array not found")
		return
	}

	jsonPart := body[idx:]
	var result [][]interface{}
	if err := json.Unmarshal(jsonPart, &result); err != nil {
		y.log.Error("getSID JSON decode error: %v", err)
		return
	}

	for _, elem := range result {
		if len(elem) >= 2 {
			if innerArray, ok := elem[1].([]interface{}); ok && len(innerArray) >= 2 {
				if sid, ok := innerArray[1].(string); ok {
					y.sid = sid
					y.log.Debug("[Flow 3] getSID - Successfully extracted SID: %s", sid)
					return
				}
			}
		}
	}

	y.log.Error("[Flow 3] getSID: SID not found in the JSON structure")
}

func (y *Youtube) chooseServer() {
	y.log.Debug("[Flow 2] chooseServer - Getting gsessionid for video: %s", y.videoID)
	url := fmt.Sprintf("https://signaler-pa.youtube.com/punctual/v1/chooseServer?key=%s", y.config.API_KEY)

	payloadStr := fmt.Sprintf(`[[null,null,null,[9,5],null,[["youtube_live_chat_web"],[1],[[["chat~%s"]]]]],null,null,0]`, y.videoID)
	y.log.Debug("[Flow 2] chooseServer payload: %s", payloadStr)
	payload := strings.NewReader(payloadStr)

	req, err := http.NewRequest("POST", url, payload)
	if err != nil {
		y.log.Error("chooseServer: failed to create request: %v", err)
		return
	}

	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "en-US,en;q=0.6")
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("content-type", "application/json+protobuf")
	req.Header.Set("origin", "https://www.youtube.com")
	req.Header.Set("pragma", "no-cache")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("referer", "https://www.youtube.com/")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="141", "Not?A_Brand";v="8", "Chromium";v="141"`)
	req.Header.Set("sec-ch-ua-arch", `"arm"`)
	req.Header.Set("sec-ch-ua-bitness", `"64"`)
	req.Header.Set("sec-ch-ua-full-version-list", `"Google Chrome";v="141.0.0.0", "Not?A_Brand";v="8.0.0.0", "Chromium";v="141.0.0.0"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-model", `""`)
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-ch-ua-platform-version", `"15.6.1"`)
	req.Header.Set("sec-ch-ua-wow64", "?0")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-site")
	req.Header.Set("sec-gpc", "1")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36")

	resp, err := y.httpClient.Do(req)
	if err != nil {
		y.log.Error("chooseServer: HTTP request failed: %v", err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			y.log.Error("Error closing response body: %v", err)
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		y.log.Error("chooseServer: failed to read response: %v", err)
		return
	}

	y.log.Debug("chooseServer response status: %d", resp.StatusCode)
	y.log.Debug("chooseServer raw response: %s", string(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		y.log.Error("chooseServer: unexpected status code: %d", resp.StatusCode)
		return
	}

	var resultObj map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &resultObj); err == nil {
		y.log.Debug("[Flow 2] Response ChooseServer (object): %v", resultObj)

		if gsessionID, ok := resultObj["gsessionid"].(string); ok {
			y.gsessionID = gsessionID
			y.log.Debug("[Flow 2] Successfully extracted gsessionID from object: %s", gsessionID)
			return
		}
	}

	var resultArr []interface{}
	if err := json.Unmarshal(bodyBytes, &resultArr); err != nil {
		y.log.Error("[Flow 2] chooseServer: decode error (tried both object and array): %v", err)
		return
	}

	y.log.Debug("[Flow 2] Response ChooseServer (array): %v", resultArr)

	if len(resultArr) > 0 {
		if gsessionID, ok := resultArr[0].(string); ok {
			y.gsessionID = gsessionID
			y.log.Debug("[Flow 2] Successfully extracted gsessionID from array: %s", gsessionID)
		}
	}
}

func (y *Youtube) refreshCreds() {
	if y.session == "" {
		y.log.Debug("[refreshCreds] Skipping: session not yet established")
		return
	}

	y.log.Debug("[refreshCreds] Refreshing credentials with session=%s, gsessionid=%s", y.session, y.gsessionID)
	url := fmt.Sprintf("https://signaler-pa.youtube.com/punctual/v1/refreshCreds?key=%s&gsessionid=%s",
		y.config.API_KEY, y.gsessionID)
	payloadRaw := fmt.Sprintf("[\"%s\"]", y.session)
	y.log.Debug("[refreshCreds] Payload: %s", payloadRaw)
	payload := strings.NewReader(payloadRaw)

	resp, err := y.executeRequest(url, "POST", payload)
	if err != nil {
		y.log.Error("[refreshCreds] Error: %v", err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			y.log.Error("Error closing response body: %v", err)
		}
	}()

	y.log.Debug("[refreshCreds] Success - Status: %d", resp.StatusCode)
}
