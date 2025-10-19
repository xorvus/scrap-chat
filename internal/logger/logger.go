package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
}

var levelIcons = map[Level]string{
	DEBUG: "🔍",
	INFO:  "ℹ️ ",
	WARN:  "⚠️ ",
	ERROR: "❌",
}

type contextKey int

const (
	correlationIDKey contextKey = iota
)

var correlationCounter uint64

type Logger struct {
	mu        sync.Mutex
	out       io.Writer
	minLevel  Level
	prefix    string
	verbose   bool
	timestamp bool
	showIcons bool
	ctx       context.Context
}

func New(prefix string, verbose bool) *Logger {
	minLevel := INFO
	if verbose {
		minLevel = DEBUG
	}

	return &Logger{
		out:       os.Stdout,
		minLevel:  minLevel,
		prefix:    prefix,
		verbose:   verbose,
		timestamp: true,
		showIcons: false,
		ctx:       context.Background(),
	}
}

func NewWithContext(ctx context.Context, prefix string, verbose bool) *Logger {
	l := New(prefix, verbose)
	l.ctx = ctx
	return l
}

func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out = w
}

func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minLevel = level
}

func (l *Logger) SetShowIcons(show bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.showIcons = show
}

func (l *Logger) SetContext(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ctx = ctx
}

func (l *Logger) log(level Level, format string, v ...interface{}) {
	if level < l.minLevel {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	var msg string
	levelName := levelNames[level]

	if l.showIcons {
		levelName = fmt.Sprintf("%s %s", levelIcons[level], levelName)
	}

	if l.timestamp {
		timestamp := time.Now().Format("2006/01/02 15:04:05")
		msg = fmt.Sprintf("%s [%s]", timestamp, levelName)
	} else {
		msg = fmt.Sprintf("[%s]", levelName)
	}

	if l.prefix != "" {
		msg = fmt.Sprintf("%s [%s]", msg, l.prefix)
	}

	corrID := l.getCorrelationID()
	if corrID != "" {
		msg = fmt.Sprintf("%s [%s]", msg, corrID)
	}

	msg = fmt.Sprintf("%s %s\n", msg, fmt.Sprintf(format, v...))

	if l.out != nil {
		_, _ = l.out.Write([]byte(msg))
	}
}

func (l *Logger) getCorrelationID() string {
	if l.ctx == nil {
		return ""
	}
	if id, ok := l.ctx.Value(correlationIDKey).(string); ok {
		return id
	}
	return ""
}

func (l *Logger) Debug(format string, v ...interface{}) {
	l.log(DEBUG, format, v...)
}

func (l *Logger) Info(format string, v ...interface{}) {
	l.log(INFO, format, v...)
}

func (l *Logger) Warn(format string, v ...interface{}) {
	l.log(WARN, format, v...)
}

func (l *Logger) Error(format string, v ...interface{}) {
	l.log(ERROR, format, v...)
}

func (l *Logger) Fatal(format string, v ...interface{}) {
	l.log(ERROR, format, v...)
	os.Exit(1)
}

func (l *Logger) IsVerbose() bool {
	return l.verbose
}

func (l *Logger) WithPrefix(prefix string) *Logger {
	return &Logger{
		out:       l.out,
		minLevel:  l.minLevel,
		prefix:    prefix,
		verbose:   l.verbose,
		timestamp: l.timestamp,
		showIcons: l.showIcons,
		ctx:       l.ctx,
	}
}

func (l *Logger) WithContext(ctx context.Context) *Logger {
	return &Logger{
		out:       l.out,
		minLevel:  l.minLevel,
		prefix:    l.prefix,
		verbose:   l.verbose,
		timestamp: l.timestamp,
		showIcons: l.showIcons,
		ctx:       ctx,
	}
}

var defaultLogger = &Logger{
	out:       os.Stdout,
	minLevel:  INFO,
	timestamp: true,
}

func Debug(format string, v ...interface{}) {
	defaultLogger.Debug(format, v...)
}

func Info(format string, v ...interface{}) {
	defaultLogger.Info(format, v...)
}

func Warn(format string, v ...interface{}) {
	defaultLogger.Warn(format, v...)
}

func Error(format string, v ...interface{}) {
	defaultLogger.Error(format, v...)
}

func Fatal(format string, v ...interface{}) {
	defaultLogger.Fatal(format, v...)
}

func SetVerbose(verbose bool) {
	if verbose {
		defaultLogger.SetLevel(DEBUG)
	} else {
		defaultLogger.SetLevel(INFO)
	}
}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

func GenerateCorrelationID() string {
	counter := atomic.AddUint64(&correlationCounter, 1)
	return fmt.Sprintf("CID-%d", counter)
}

func init() {
	log.SetFlags(0)
	log.SetOutput(io.Discard)
}
