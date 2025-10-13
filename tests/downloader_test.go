package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/xorvus/scrap-chat/internal/utils"
)

func TestDownloadFile(t *testing.T) {
	content := []byte("test file content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	outputPath := tempDir + "/downloaded.txt"

	err := utils.DownloadFile(server.URL, outputPath)
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}

	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(content))
	}
}

func TestDownloadFileWithContext(t *testing.T) {
	content := []byte("test content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	outputPath := tempDir + "/downloaded.txt"

	ctx := context.Background()
	err := utils.DownloadFileWithContext(ctx, server.URL, outputPath)
	if err != nil {
		t.Fatalf("DownloadFileWithContext failed: %v", err)
	}

	if !utils.FileExists(outputPath) {
		t.Error("file was not created")
	}
}

func TestDownloadFileContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	tempDir := t.TempDir()
	outputPath := tempDir + "/downloaded.txt"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := utils.DownloadFileWithContext(ctx, server.URL, outputPath)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestDownloadFileHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	outputPath := tempDir + "/downloaded.txt"

	err := utils.DownloadFile(server.URL, outputPath)
	if err == nil {
		t.Error("expected error for 404 response, got nil")
	}
}

func TestDownloadFileInvalidPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "content")
	}))
	defer server.Close()

	err := utils.DownloadFile(server.URL, "/invalid/path/file.txt")
	if err == nil {
		t.Error("expected error for invalid path, got nil")
	}
}

func BenchmarkDownloadFile(b *testing.B) {
	content := make([]byte, 1024*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	tempDir := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outputPath := tempDir + "/file_" + string(rune(i)) + ".txt"
		_ = utils.DownloadFile(server.URL, outputPath)
	}
}
