package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xorvus/scrap-chat/types"
)

const (
	filePermissionMode = 0644
	timestampFormat    = "2006/01/02 15:04:05"
)

type OutputHandler interface {
	initialize() error
	close()
}

type JSONFileHandler struct {
	writer         *os.File
	filename       string
	isFirstMessage bool
}

func newJSONFileHandler(filename string) *JSONFileHandler {
	return &JSONFileHandler{
		filename:       filename,
		isFirstMessage: true,
	}
}

func (h *JSONFileHandler) initialize() error {
	writer, err := os.OpenFile(h.filename, os.O_CREATE|os.O_RDWR, filePermissionMode)
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	h.writer = writer
	h.setupSignalHandler()

	return h.prepareJSONFile()
}

func (h *JSONFileHandler) prepareJSONFile() error {
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

	data, err := os.ReadFile(h.filename)
	if err != nil {
		return fmt.Errorf("failed to read existing file: %w", err)
	}

	trimmed := bytes.TrimRight(data, "\n\r ]")

	if err := os.WriteFile(h.filename, trimmed, filePermissionMode); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	if _, err := h.writer.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("failed to seek to end: %w", err)
	}

	h.isFirstMessage = false
	return nil
}

func (h *JSONFileHandler) setupSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		h.closeJSONArray()
		fmt.Println("\nProgram interrupted. Closed JSON array in file.")
		os.Exit(0)
	}()
}

func (h *JSONFileHandler) writeJSONLine(line string) error {
	if !h.isFirstMessage {
		if _, err := h.writer.WriteString(",\n"); err != nil {
			return fmt.Errorf("failed to write separator: %w", err)
		}
	}

	if _, err := h.writer.WriteString(line); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	h.isFirstMessage = false
	return nil
}

func (h *JSONFileHandler) closeJSONArray() {
	if h.writer == nil || h.writer == os.Stdout {
		return
	}

	_, _ = h.writer.WriteString("\n]\n")
	_ = h.writer.Sync()
	_ = h.writer.Close()
}

func (h *JSONFileHandler) close() {
	h.closeJSONArray()
}

type MessageFormatter interface {
	Format(format, customOutput string) (string, error)
	PrintConsole()
}

type LiveChatFormatter struct {
	msg *types.LiveChatMessage
}

func (f *LiveChatFormatter) Format(format, customOutput string) (string, error) {
	return formatLiveChatMessage(f.msg, format, customOutput)
}

func (f *LiveChatFormatter) PrintConsole() {
	timestamp := time.Unix(f.msg.Timestamp, 0).Format(timestampFormat)
	fmt.Printf("[%s] [%s] %s\n", timestamp, f.msg.Author.Name, f.msg.Message)
}

type VideoCommentFormatter struct {
	msg *types.ChatMessage
}

func (f *VideoCommentFormatter) Format(format, customOutput string) (string, error) {
	return formatVideoChatMessage(f.msg, format, customOutput)
}

func (f *VideoCommentFormatter) PrintConsole() {
	timestamp := time.Unix(f.msg.Timestamp, 0).Format(timestampFormat)
	replyIndicator := ""
	if f.msg.Parent != "" {
		replyIndicator = " [REPLY]"
	}
	fmt.Printf("[%s] [%s]%s %s (👍 %d | 💬 %d)\n",
		timestamp, f.msg.Author.Name, replyIndicator, f.msg.Message, f.msg.LikeCount, f.msg.ReplyCount)
}

type GenericOutputHandler struct {
	jsonHandler *JSONFileHandler
	writer      io.Writer
	format      string
	isFile      bool
	isJSON      bool
}

func newGenericOutputHandler(output, format, filename string) *GenericOutputHandler {
	handler := &GenericOutputHandler{
		isFile: output == outputFile,
		isJSON: format == formatJSON,
		format: format,
	}

	if handler.isFile && handler.isJSON {
		handler.jsonHandler = newJSONFileHandler(filename)
	}

	return handler
}

func (h *GenericOutputHandler) initialize() error {
	if h.isFile && h.isJSON {
		return h.jsonHandler.initialize()
	}
	h.writer = os.Stdout
	return nil
}

func (h *GenericOutputHandler) writeMessage(formatter MessageFormatter, customOutput string) error {
	line, err := formatter.Format(h.format, customOutput)
	if err != nil {
		return fmt.Errorf("failed to format message: %w", err)
	}

	if h.isFile && h.isJSON {
		if err := h.jsonHandler.writeJSONLine(line); err != nil {
			return err
		}
		formatter.PrintConsole()
	} else {
		if _, err := fmt.Fprintln(h.writer, line); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
	}

	return nil
}

func (h *GenericOutputHandler) close() {
	if h.jsonHandler != nil {
		h.jsonHandler.close()
	}
}

type LiveChatOutputHandler struct {
	*GenericOutputHandler
}

func newLiveOutputHandler(output, format string) *LiveChatOutputHandler {
	return &LiveChatOutputHandler{
		GenericOutputHandler: newGenericOutputHandler(output, format, liveOutputFile),
	}
}

func (h *LiveChatOutputHandler) write(msg *types.LiveChatMessage, customOutput string) error {
	return h.writeMessage(&LiveChatFormatter{msg: msg}, customOutput)
}

type VideoCommentsOutputHandler struct {
	*GenericOutputHandler
}

func newVideoOutputHandler(output, format string) *VideoCommentsOutputHandler {
	return &VideoCommentsOutputHandler{
		GenericOutputHandler: newGenericOutputHandler(output, format, commentsOutputFile),
	}
}

func (h *VideoCommentsOutputHandler) write(msg *types.ChatMessage, customOutput string) error {
	return h.writeMessage(&VideoCommentFormatter{msg: msg}, customOutput)
}
