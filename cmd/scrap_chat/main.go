package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/xorvus/scrap-chat/pkg/scrapchat"
)

var version = "dev"

const (
	outputLog  = "log"
	outputFile = "file"

	formatDefault = "default"
	formatJSON    = "json"
	formatCustom  = "custom"

	msgTypeLiveChat = "livechat"
	msgTypeComments = "comments"
	msgTypeInfo     = "info"

	liveOutputFile     = "livechat_output.json"
	commentsOutputFile = "comments_output.json"
	infoOutputFile     = "info_output"

	filePermission = 0644
)

type Config struct {
	ShowVersion  bool
	Verbose      bool
	MsgType      string
	Output       string
	Format       string
	CustomOutput string
	URL          string
}

func main() {
	config := parseFlags()

	if config.ShowVersion {
		fmt.Println("Version:", version)
		return
	}

	if err := run(config); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func parseFlags() *Config {
	config := &Config{}

	flag.BoolVar(&config.ShowVersion, "version", false, "Display program version")
	flag.BoolVar(&config.Verbose, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&config.Verbose, "v", false, "Enable verbose logging (short form)")
	flag.StringVar(&config.MsgType, "type", "", "Type of scrap [livechat, comments, info]")
	flag.StringVar(&config.MsgType, "t", "", "Type of scrap [livechat, comments, info] (short form)")
	flag.StringVar(&config.Output, "output", outputLog, "Output result destination [log, file]")
	flag.StringVar(&config.Output, "o", outputLog, "Output result destination [log, file] (short form)")
	flag.StringVar(&config.Format, "format", formatDefault, "Format of result [default, json, custom]")
	flag.StringVar(&config.Format, "f", formatDefault, "Format of result [default, json, custom] (short form)")
	flag.StringVar(&config.CustomOutput, "custom-output", "", "Custom output template")
	flag.StringVar(&config.CustomOutput, "co", "", "Custom output template (short form)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <url>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  --version               Display program version\n")
		fmt.Fprintf(os.Stderr, "  -v, --verbose           Enable verbose logging\n")
		fmt.Fprintf(os.Stderr, "  -t, --type              Type of scrap [livechat, comments, info]\n")
		fmt.Fprintf(os.Stderr, "  -o, --output            Output destination [log, file]\n")
		fmt.Fprintf(os.Stderr, "  -f, --format            Format of result [default, json, custom]\n")
		fmt.Fprintf(os.Stderr, "  -co, --custom-output    Custom output template (for format=custom)\n")
	}

	flag.Parse()

	if flag.NArg() < 1 && !config.ShowVersion {
		fmt.Fprintln(os.Stderr, "Error: Missing URL")
		flag.Usage()
		os.Exit(1)
	}

	if flag.NArg() > 0 {
		config.URL = flag.Arg(0)
	}

	return config
}

func run(config *Config) error {
	chat, err := scrapchat.New("youtube", config.Verbose)
	if err != nil {
		return fmt.Errorf("failed to initialize scraper: %w", err)
	}

	switch strings.ToLower(config.MsgType) {
	case msgTypeLiveChat:
		return handleLive(chat, config)
	case msgTypeComments:
		return handleVideo(chat, config)
	case msgTypeInfo:
		return handleInfo(chat, config)
	default:
		return fmt.Errorf("unknown type. Use -h for help")
	}
}

func handleLive(chat *scrapchat.ScrapChat, config *Config) error {
	liveChat, err := chat.FetchLiveChat(config.URL)
	if err != nil {
		return fmt.Errorf("failed to fetch live chat: %w", err)
	}

	handler := newLiveOutputHandler(config.Output, config.Format)
	if err := handler.initialize(); err != nil {
		return err
	}
	defer handler.close()

	for msg := range liveChat {
		if err := handler.write(msg, config.CustomOutput); err != nil {
			return err
		}
	}

	return nil
}

func handleVideo(chat *scrapchat.ScrapChat, config *Config) error {
	videoComments, err := chat.FetchVideoComments(config.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch video comments: %w", err)
	}

	handler := newVideoOutputHandler(config.Output, config.Format)
	if err := handler.initialize(); err != nil {
		return err
	}
	defer handler.close()

	for msg := range videoComments {
		if err := handler.write(msg, config.CustomOutput); err != nil {
			return err
		}
	}

	return nil
}

func handleInfo(chat *scrapchat.ScrapChat, config *Config) error {
	info, err := chat.FetchChannelInfo(config.URL)
	if err != nil {
		return fmt.Errorf("failed to fetch channel info: %w", err)
	}

	formatted, err := formatChannelInfo(info, config.Format, config.CustomOutput)
	if err != nil {
		return err
	}

	return writeInfoOutput(formatted, config.Output, config.Format)
}

func writeInfoOutput(content, output, format string) error {
	if output == outputFile {
		ext := "txt"
		if format == formatJSON {
			ext = "json"
		}
		filename := fmt.Sprintf("%s.%s", infoOutputFile, ext)

		if err := os.WriteFile(filename, []byte(content), filePermission); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}

		fmt.Printf("Result written to %s\n", filename)
		return nil
	}

	fmt.Println(content)
	return nil
}
