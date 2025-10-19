package utils

import (
	"fmt"
	"os"
)

const (
	htmlFilePermission = 0644
)

// FileExists checks if a file exists at the given path.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DirExists checks if a directory exists at the given path.
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// EnsureDir creates a directory at the given path if it doesn't exist.
func EnsureDir(path string) error {
	if DirExists(path) {
		return nil
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}

// SaveHTML saves HTML content to the specified file path.
func SaveHTML(content []byte, outputPath string) error {
	if err := os.WriteFile(outputPath, content, htmlFilePermission); err != nil {
		return fmt.Errorf("failed to save HTML file: %w", err)
	}
	return nil
}
