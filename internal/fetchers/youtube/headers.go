package youtube

import "net/http"

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
	h.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")
	return h
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