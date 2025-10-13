package ytdlp

import (
	"fmt"
	"github.com/xorvus/scrap-chat/internal/utils"
	"log"
	"os"
	"os/exec"
	"runtime"
)

const version = "2025.04.30"

type YtDlp struct {
}

func NewYtDlp() *YtDlp {
	return &YtDlp{}
}

func (d *YtDlp) Check() error {
	path := "yt-dlp"

	if !utils.FileExists(path) {
		os := runtime.GOOS
		url := ""
		switch os {
		case "windows":
			url = fmt.Sprintf("https://github.com/yt-dlp/yt-dlp/releases/download/%s/yt-dlp.exe", version)
		case "linux", "darwin":
			url = fmt.Sprintf("https://github.com/yt-dlp/yt-dlp/releases/download/%s/yt-dlp", version)
		default:
			return fmt.Errorf("running on unknown OS: %s", os)
		}

		if url != "" {
			if err := utils.DownloadFile(url, path); err != nil {
				return fmt.Errorf("failed to download yt-dlp: %w", err)
			}
		}
	}

	return nil
}

func (d *YtDlp) DownloadComments(url string) {
	err := d.Check()
	if err != nil {
		log.Fatalln(err)
	}

	outputFile := "comments.json"

	file, err := os.Create(outputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating file: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	cmd := exec.Command("yt-dlp", "--get-comments", "--no-download", "--print", "%(comments)j", url)

	cmd.Stdout = file
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running yt-dlp: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Comments saved to %s\n", outputFile)
}
