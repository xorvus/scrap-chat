package tests

import (
	"testing"

	"github.com/xorvus/scrap-chat/internal/utils"
)

func TestFileExists(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"existing file", "generator_test.go", true},
		{"non-existing file", "nonexistent.txt", false},
		{"empty path", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.FileExists(tt.path)
			if result != tt.expected {
				t.Errorf("FileExists(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestDirExists(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"current directory", ".", true},
		{"non-existing directory", "/nonexistent/path", false},
		{"empty path", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.DirExists(tt.path)
			if result != tt.expected {
				t.Errorf("DirExists(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestEnsureDir(t *testing.T) {
	tempDir := t.TempDir()
	testPath := tempDir + "/test_dir"

	err := utils.EnsureDir(testPath)
	if err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}

	if !utils.DirExists(testPath) {
		t.Error("directory was not created")
	}

	err = utils.EnsureDir(testPath)
	if err != nil {
		t.Errorf("EnsureDir should not fail on existing directory: %v", err)
	}
}
