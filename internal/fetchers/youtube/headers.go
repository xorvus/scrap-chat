package youtube

import "net/http"

// newDefaultHeaders creates HTTP headers for standard YouTube API requests
func newDefaultHeaders() http.Header {
	h := make(http.Header)
	h.Set("accept", "*/*")
	h.Set("accept-language", "en-US,en;q=0.9")
	h.Set("cache-control", "no-cache")
	h.Set("origin", "https://www.youtube.com ")
	h.Set("priority", "u=1, i")
	h.Set("pragma", "no-cache")
	h.Set("referer", "https://www.youtube.com/ ")
	h.Set("sec-ch-ua", `"Chromium";v="136", "Brave";v="136", "Not.A/Brand";v="99"`)
	h.Set("sec-ch-ua-arch", `"arm"`)
	h.Set("sec-ch-ua-bitness", `"64"`)
	h.Set("sec-ch-ua-full-version-list", `"Chromium";v="136.0.0.0", "Brave";v="136.0.0.0", "Not.A/Brand";v="99.0.0.0"`)
	h.Set("sec-ch-ua-mobile", "?0")
	h.Set("sec-ch-ua-model", `""`)
	h.Set("sec-ch-ua-platform", `"macOS"`)
	h.Set("sec-ch-ua-platform-version", `"15.4.0"`)
	h.Set("sec-ch-ua-wow64", "?0")
	h.Set("sec-fetch-dest", "empty")
	h.Set("sec-fetch-mode", "cors")
	h.Set("sec-fetch-site", "same-site")
	h.Set("sec-gpc", "1")
	h.Set("user-agent", defaultUserAgent)
	return h
}

// buildSignalerHeaders creates HTTP headers specific to YouTube signaler API requests
func buildSignalerHeaders() http.Header {
	h := make(http.Header)
	h.Set("accept", "*/*")
	h.Set("accept-language", "en-US,en;q=0.6")
	h.Set("cache-control", "no-cache")
	h.Set("content-type", "application/json+protobuf")
	h.Set("origin", youtubeBaseURL)
	h.Set("pragma", "no-cache")
	h.Set("priority", "u=1, i")
	h.Set("referer", youtubeBaseURL+"/")
	h.Set("sec-ch-ua", `"Google Chrome";v="141", "Not?A_Brand";v="8", "Chromium";v="141"`)
	h.Set("sec-ch-ua-arch", `"arm"`)
	h.Set("sec-ch-ua-bitness", `"64"`)
	h.Set("sec-ch-ua-full-version-list", `"Google Chrome";v="141.0.0.0", "Not?A_Brand";v="8.0.0.0", "Chromium";v="141.0.0.0"`)
	h.Set("sec-ch-ua-mobile", "?0")
	h.Set("sec-ch-ua-model", `""`)
	h.Set("sec-ch-ua-platform", `"macOS"`)
	h.Set("sec-ch-ua-platform-version", `"15.6.1"`)
	h.Set("sec-ch-ua-wow64", "?0")
	h.Set("sec-fetch-dest", "empty")
	h.Set("sec-fetch-mode", "cors")
	h.Set("sec-fetch-site", "same-site")
	h.Set("sec-gpc", "1")
	h.Set("user-agent", signalerUserAgent)
	return h
}

// setPageFetchHeaders applies browser-like headers for HTML page fetching
func (y *Youtube) setPageFetchHeaders(req *http.Request) {
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("User-Agent", signalerUserAgent)
	req.Header.Set("sec-ch-ua", `"Brave";v="141", "Not?A_Brand";v="8", "Chromium";v="141"`)
	req.Header.Set("sec-ch-ua-arch", `"arm"`)
	req.Header.Set("sec-ch-ua-bitness", `"64"`)
	req.Header.Set("sec-ch-ua-full-version-list", `"Brave";v="141.0.0.0", "Not?A_Brand";v="8.0.0.0", "Chromium";v="141.0.0.0"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-model", `""`)
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-ch-ua-platform-version", `"15.6.1"`)
	req.Header.Set("sec-ch-ua-wow64", "?0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
}

func (y *Youtube) copyHeaders(req *http.Request) {
	for k, vv := range y.header {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	if y.cookieString != "" {
		req.Header.Set("Cookie", y.cookieString)
	}
}
