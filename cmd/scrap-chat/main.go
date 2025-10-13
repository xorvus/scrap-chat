package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xorvus/scrap-chat/pkg/scrapchat"
	"github.com/xorvus/scrap-chat/types"
)

var version = "dev"

const (
	outputLog  = "log"
	outputFile = "file"

	formatDefault = "default"
	formatJSON    = "json"
	formatCustom  = "custom"

	msgTypeLive  = "live"
	msgTypeVideo = "video"
	msgTypeInfo  = "info"

	liveOutputFile = "live_output.json"
	infoOutputFile = "info_output"

	filePermission = 0644
	timeFormat     = "2006/01/02 15:04:05"
)

type Config struct {
	ShowVersion  bool
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
	flag.BoolVar(&config.ShowVersion, "v", false, "Display program version (short form)")
	flag.StringVar(&config.MsgType, "type", "", "Type of scrap [live, video, info]")
	flag.StringVar(&config.MsgType, "t", "", "Type of scrap [live, video, info] (short form)")
	flag.StringVar(&config.Output, "output", outputLog, "Output result destination [log, file]")
	flag.StringVar(&config.Output, "o", outputLog, "Output result destination [log, file] (short form)")
	flag.StringVar(&config.Format, "format", formatDefault, "Format of result [default, json, custom]")
	flag.StringVar(&config.Format, "f", formatDefault, "Format of result [default, json, custom] (short form)")
	flag.StringVar(&config.CustomOutput, "custom-output", "", "Custom output template")
	flag.StringVar(&config.CustomOutput, "co", "", "Custom output template (short form)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <url>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(os.Stderr, "  -v, --version           Display program version\n")
		fmt.Fprintf(os.Stderr, "  -t, --type              Type of scrap [live, video, info]\n")
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
	chat, err := scrapchat.New("youtube")
	if err != nil {
		return fmt.Errorf("failed to initialize scraper: %w", err)
	}

	switch strings.ToLower(config.MsgType) {
	case msgTypeLive:
		return handleLive(chat, config)
	case msgTypeVideo:
		return fmt.Errorf("video comments fetching not yet implemented")
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
		if err := handler.write(msg, config.Format, config.CustomOutput); err != nil {
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

type liveOutputHandler struct {
	writer         *os.File
	isFile         bool
	isJSON         bool
	isFirstMessage bool
}

func newLiveOutputHandler(output, format string) *liveOutputHandler {
	return &liveOutputHandler{
		isFile:         output == outputFile,
		isJSON:         format == formatJSON,
		isFirstMessage: true,
	}
}

func (h *liveOutputHandler) initialize() error {
	if h.isFile && h.isJSON {
		writer, err := os.OpenFile(liveOutputFile, os.O_CREATE|os.O_RDWR, filePermission)
		if err != nil {
			return fmt.Errorf("failed to open output file: %w", err)
		}
		h.writer = writer
		h.setupSignalHandler()

		if err := h.prepareJSONFile(); err != nil {
			return err
		}
	} else {
		h.writer = os.Stdout
	}

	return nil
}

func (h *liveOutputHandler) prepareJSONFile() error {
	info, err := h.writer.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if info.Size() == 0 {
		if _, err := h.writer.WriteString("[\n"); err != nil {
			return fmt.Errorf("failed to write array start: %w", err)
		}
		return nil
	}

	data, err := os.ReadFile(liveOutputFile)
	if err != nil {
		return fmt.Errorf("failed to read existing file: %w", err)
	}

	trimmed := bytes.TrimRight(data, "\n\r ]")

	if err := os.WriteFile(liveOutputFile, trimmed, filePermission); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	if _, err := h.writer.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("failed to seek to end: %w", err)
	}

	h.isFirstMessage = false
	return nil
}

func (h *liveOutputHandler) setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		h.closeJSONArray()
		fmt.Println("\nProgram interrupted. Closed JSON array in file.")
		os.Exit(0)
	}()
}

func (h *liveOutputHandler) write(msg *types.LiveChatMessage, format, customOutput string) error {
	line := formatLiveChatMessage(msg, format, customOutput)

	if h.isFile && h.isJSON {
		if err := h.writeJSONLine(line); err != nil {
			return err
		}
		h.printConsoleOutput(msg)
	} else {
		if _, err := fmt.Fprintln(h.writer, line); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
	}

	h.isFirstMessage = false
	return nil
}

func (h *liveOutputHandler) writeJSONLine(line string) error {
	if !h.isFirstMessage {
		if _, err := h.writer.WriteString(",\n"); err != nil {
			return fmt.Errorf("failed to write separator: %w", err)
		}
	}

	if _, err := h.writer.WriteString(line); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	return nil
}

func (h *liveOutputHandler) printConsoleOutput(msg *types.LiveChatMessage) {
	timestamp := time.Unix(msg.Timestamp, 0).Format(timeFormat)
	fmt.Printf("[%s] [%s] %s\n", timestamp, msg.Author.Name, msg.Message)
}

func (h *liveOutputHandler) closeJSONArray() {
	if h.writer == nil || h.writer == os.Stdout {
		return
	}

	_, err := h.writer.WriteString("\n]\n")
	if err != nil {
		return
	}
	err = h.writer.Sync()
	if err != nil {
		return
	}
	err = h.writer.Close()
	if err != nil {
		return
	}
}

func (h *liveOutputHandler) close() {
	if h.isFile && h.isJSON {
		h.closeJSONArray()
	}
}

func formatLiveChatMessage(msg *types.LiveChatMessage, format, customOutput string) string {
	switch format {
	case formatJSON:
		data, err := json.MarshalIndent(msg, "  ", "  ")
		if err != nil {
			log.Fatalf("Failed to marshal JSON: %v", err)
		}
		return string(data)

	case formatCustom:
		if strings.TrimSpace(customOutput) == "" {
			log.Fatal("Custom format requires custom-output template")
		}
		return applyLiveCustomTemplate(customOutput, msg)

	default:
		timestamp := time.Unix(msg.Timestamp, 0).Format(timeFormat)
		return fmt.Sprintf("[%s] [%s] %s", timestamp, msg.Author.Name, msg.Message)
	}
}

func formatChannelInfo(info *types.ChannelInfo, format, customOutput string) (string, error) {
	switch format {
	case formatJSON:
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal JSON: %w", err)
		}
		return string(data), nil

	case formatCustom:
		if customOutput == "" {
			return "", fmt.Errorf("custom format requires custom-output template")
		}
		return applyInfoCustomTemplate(customOutput, info), nil

	default:
		return fmt.Sprintf("%+v", info), nil
	}
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

func applyInfoCustomTemplate(template string, info *types.ChannelInfo) string {
	replacements := map[string]string{
		"ID":    info.ID,
		"NAME":  info.Name,
		"DESC":  info.Description,
		"IMAGE": info.Image,
		"URL":   info.URL,
	}

	result := template
	for key, value := range replacements {
		result = strings.ReplaceAll(result, key, value)
	}

	return result
}

func applyLiveCustomTemplate(template string, msg *types.LiveChatMessage) string {
	badges := extractBadges(msg.Author.Badges)

	replacements := map[string]string{
		"FANS_RANKING":             msg.Author.Ranking,
		"AUTHOR_NAME":              msg.Author.Name,
		"AUTHOR_MEMBERSHIP":        badges.labels,
		"AUTHOR_MEMBERSHIP_BADGES": badges.images,
		"MESSAGE":                  msg.Message,
		"TIME":                     time.Unix(msg.Timestamp, 0).Format("2006-01-02 15:04:05"),
		"TIMESTAMP":                strconv.FormatInt(msg.Timestamp, 10),
		"ID":                       msg.ID,
		"AUTHOR_ID":                msg.Author.ID,
		"AUTHOR_URL":               msg.Author.URL,
		"AUTHOR_THUMBNAIL":         msg.Author.Thumbnail,
	}

	result := template
	for key, value := range replacements {
		result = strings.ReplaceAll(result, key, value)
		result = processConditionalField(result, "{"+key+"}", value)
	}

	return cleanupTemplate(result)
}

type badgeInfo struct {
	labels string
	images string
}

func extractBadges(badges []types.Badge) badgeInfo {
	if len(badges) == 0 {
		return badgeInfo{}
	}

	labels := make([]string, 0, len(badges))
	images := make([]string, 0, len(badges))

	for _, badge := range badges {
		labels = append(labels, badge.Label)
		if badge.IconURL != "" {
			images = append(images, badge.IconURL)
		}
	}

	return badgeInfo{
		labels: strings.Join(labels, ", "),
		images: strings.Join(images, ", "),
	}
}

func processConditionalField(template, fieldName, value string) string {
	pattern := regexp.MustCompile(`\{([^{}]+)\}`)
	result := template

	for {
		matches := pattern.FindAllStringSubmatch(result, -1)
		if len(matches) == 0 {
			break
		}

		changed := false
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}

			fullMatch := match[0]
			content := match[1]

			if strings.Contains(content, fieldName) {
				replacedContent := strings.ReplaceAll(content, fieldName, value)
				replacedContent = removeEmptyBrackets(replacedContent)
				result = strings.ReplaceAll(result, fullMatch, replacedContent)
				changed = true
			}
		}

		if !changed {
			break
		}
	}

	return result
}

func removeEmptyBrackets(s string) string {
	patterns := []string{
		`\{\s*\[\s*\]\s*\}`,
		`\{\s*\(\s*\)\s*\}`,
		`\{\s*\{\s*\}\s*\}`,
		`\(\s*\)`,
		`\[\s*\]`,
		`\{\s*\}`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		s = re.ReplaceAllString(s, "")
	}

	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func cleanupTemplate(s string) string {
	emptyPatterns := []string{
		"{[]}", "{()}", "{{}}", "{}", "()", "[]",
		"{ }", "( )", "[ ]",
	}

	for _, pattern := range emptyPatterns {
		s = strings.ReplaceAll(s, pattern, "")
	}

	bracketReplacements := map[string]string{
		"{[": "[", "]}": "]",
		"{(": "(", ")}": ")",
		"{{": "{", "}}": "}",
		"[{": "[", "}]": "]",
		"({": "(", "})": ")",
	}

	for old, n := range bracketReplacements {
		s = strings.ReplaceAll(s, old, n)
	}

	s = strings.ReplaceAll(s, "  ", " ")
	return strings.TrimSpace(s)
}
