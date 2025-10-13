package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	defaultHTTPTimeout = 30 * time.Second
	filePermission     = 0644
	bufferSize         = 32 * 1024
)

func DownloadFile(url, outputPath string) error {
	return DownloadFileWithContext(context.Background(), url, outputPath)
}

func DownloadFileWithContext(ctx context.Context, url, outputPath string) error {
	client := &http.Client{
		Timeout: defaultHTTPTimeout,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Warning: failed to close response body: %v\n", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: status %s", resp.Status)
	}

	out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePermission)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() {
		if err := out.Close(); err != nil {
			fmt.Printf("Warning: failed to close output file: %v\n", err)
		}
	}()

	buf := make([]byte, bufferSize)
	_, err = io.CopyBuffer(out, resp.Body, buf)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if err := out.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	return nil
}
