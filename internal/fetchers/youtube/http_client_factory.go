package youtube

import (
	"net/http"
	"time"
)

type HTTPClientConfig struct {
	Timeout                time.Duration
	MaxResponseHeaderBytes int64
	DisableKeepAlives      bool
}

func newHTTPClientWithConfig(config HTTPClientConfig) *http.Client {
	if config.Timeout == 0 {
		config.Timeout = defaultHTTPTimeout
	}
	if config.MaxResponseHeaderBytes == 0 {
		config.MaxResponseHeaderBytes = maxResponseHeaderBytes
	}

	return &http.Client{
		Transport: &http.Transport{
			MaxResponseHeaderBytes: config.MaxResponseHeaderBytes,
			IdleConnTimeout:        defaultHTTPTimeout,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  defaultHTTPTimeout,
			ExpectContinueTimeout:  1 * time.Second,
			DisableKeepAlives:      config.DisableKeepAlives,
			MaxIdleConns:           10,
			MaxIdleConnsPerHost:    5,
		},
		Timeout: config.Timeout,
	}
}

func newDefaultHTTPClient() *http.Client {
	return newHTTPClientWithConfig(HTTPClientConfig{
		Timeout:                defaultHTTPTimeout,
		MaxResponseHeaderBytes: maxResponseHeaderBytes,
		DisableKeepAlives:      false,
	})
}

func newFetchPageHTTPClient() *http.Client {
	return newHTTPClientWithConfig(HTTPClientConfig{
		Timeout:           defaultHTTPTimeout,
		DisableKeepAlives: true,
	})
}
