package youtube

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/xorvus/scrap-chat/internal/logger"
	"github.com/xorvus/scrap-chat/types"
)

type Youtube struct {
	cookies            []*http.Cookie
	config             *types.YTCgf
	continuation       string
	videoID            string
	gsessionID         string
	sid                string
	httpClient         *http.Client
	header             http.Header
	signalerHeaders    http.Header // Cached signaler headers to avoid repeated allocation
	cookieString       string
	timeout            int
	isInvalidationData bool
	session            string
	ctx                *context.Context
	log                *logger.Logger
	streamHealth       *StreamHealth
	streamState        StreamState
	streamMutex        sync.RWMutex
	seenMessageIDs     map[string]time.Time
	lastCleanupTime    time.Time
}

func New(ctx *context.Context, verbose bool) *Youtube {
	log := logger.New("YOUTUBE", verbose)
	log.Debug("Starting YouTube fetcher initialization with verbose mode enabled")

	y := &Youtube{
		httpClient:      nil,
		ctx:             ctx,
		log:             log,
		streamHealth:    &StreamHealth{},
		streamState:     StreamStateDisconnected,
		seenMessageIDs:  make(map[string]time.Time),
		lastCleanupTime: time.Now(),
	}

	log.Debug("Creating HTTP client")
	y.httpClient = y.newHTTPClient()

	log.Debug("Setting up default headers")
	y.header = newDefaultHeaders()
	y.signalerHeaders = buildSignalerHeaders()

	log.Debug("YouTube fetcher initialized successfully")

	return y
}

func (y *Youtube) logVerbose(format string, args ...interface{}) {
	y.log.Debug(format, args...)
}

func (y *Youtube) updateStreamState(state StreamState, reason string) {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	oldState := y.streamState
	y.streamState = state

	if state == StreamStateError {
		y.log.Error("State changed: %s -> %s (%s)", oldState, state, reason)
	} else {
		y.log.Debug("State changed: %s -> %s (%s)", oldState, state, reason)
	}
}

func (y *Youtube) logStreamRead(line string, readTime time.Duration, bytesRead int) {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	y.streamHealth.LastReadAt = time.Now()
	y.streamHealth.BytesRead += int64(bytesRead)
	y.streamHealth.MessagesRead++
	y.streamHealth.readTimeSum += readTime
	y.streamHealth.readCount++

	if readTime > y.streamHealth.MaxReadTime {
		y.streamHealth.MaxReadTime = readTime
	}

	if y.streamHealth.readCount > 0 {
		y.streamHealth.AverageReadTime = y.streamHealth.readTimeSum / time.Duration(y.streamHealth.readCount)
	}

	y.log.Debug("Read: %d bytes in %v (avg: %v, total: %d bytes, %d messages)",
		bytesRead, readTime, y.streamHealth.AverageReadTime, y.streamHealth.BytesRead, y.streamHealth.MessagesRead)

	if len(line) > 0 {
		preview := line
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		y.log.Debug("Data preview: %s", preview)
	}
}

func (y *Youtube) logStreamError(err error, context string) {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	y.streamHealth.ErrorsCount++
	y.streamHealth.ConsecutiveErrors++
	y.streamHealth.LastError = err

	y.log.Error("Error in %s: %v (consecutive errors: %d, total errors: %d)",
		context, err, y.streamHealth.ConsecutiveErrors, y.streamHealth.ErrorsCount)

	if y.streamHealth.ConsecutiveErrors >= maxConsecutiveErrors {
		y.updateStreamState(StreamStateError, fmt.Sprintf("Too many consecutive errors: %d", maxConsecutiveErrors))
	}
}

func (y *Youtube) resetStreamHealth() {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	now := time.Now()
	y.streamHealth = &StreamHealth{
		ConnectedAt: now,
		LastReadAt:  now,
	}
	y.streamHealth.ConsecutiveErrors = 0

	y.log.Debug("Health metrics reset")
}

// GetStreamHealth returns a copy of the current stream health metrics.
// This includes statistics like bytes read, messages received, error counts, and timing information.
func (y *Youtube) GetStreamHealth() *StreamHealth {
	y.streamMutex.RLock()
	defer y.streamMutex.RUnlock()

	health := *y.streamHealth
	return &health
}

// GetStreamState returns the current state of the stream connection.
// Possible states: Disconnected, Connecting, Connected, Reading, Error, Recovering.
func (y *Youtube) GetStreamState() StreamState {
	y.streamMutex.RLock()
	defer y.streamMutex.RUnlock()
	return y.streamState
}

// LogStreamSummary logs a summary of the current stream health and state.
// Includes uptime, message count, bytes transferred, error count, and average read time.
func (y *Youtube) LogStreamSummary() {
	health := y.GetStreamHealth()
	state := y.GetStreamState()

	uptime := time.Since(health.ConnectedAt)
	if uptime < time.Second {
		uptime = 0
	}

	y.log.Info("STREAM SUMMARY - State: %s, Uptime: %v, Messages: %d, Bytes: %d, Errors: %d, Avg Read Time: %v",
		state, uptime.Round(time.Second), health.MessagesRead, health.BytesRead, health.ErrorsCount, health.AverageReadTime.Round(time.Millisecond))

	if health.LastError != nil {
		y.log.Warn("Last Error: %v (%d consecutive)", health.LastError, health.ConsecutiveErrors)
	}
}

func (y *Youtube) isDuplicateMessage(messageID string) bool {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	if _, exists := y.seenMessageIDs[messageID]; exists {
		return true
	}
	return false
}

func (y *Youtube) markMessageSeen(messageID string) {
	y.streamMutex.Lock()
	defer y.streamMutex.Unlock()

	// Check if map is at capacity and trigger cleanup if needed
	if len(y.seenMessageIDs) >= maxSeenMessageIDs {
		y.log.Debug("seenMessageIDs map at capacity (%d), triggering cleanup", maxSeenMessageIDs)
		y.cleanupOldMessages()
		y.lastCleanupTime = time.Now()
	}

	y.seenMessageIDs[messageID] = time.Now()

	// Regular cleanup interval
	if time.Since(y.lastCleanupTime) > cleanupInterval {
		y.cleanupOldMessages()
		y.lastCleanupTime = time.Now()
	}
}

func (y *Youtube) cleanupOldMessages() {
	y.log.Debug("Starting cleanup of old message IDs (current size: %d)", len(y.seenMessageIDs))

	cutoff := time.Now().Add(-messageCleanupAfter)
	deletedCount := 0
	for id, timestamp := range y.seenMessageIDs {
		if timestamp.Before(cutoff) {
			delete(y.seenMessageIDs, id)
			deletedCount++
		}
	}

	// If still over 80% capacity after cleanup, remove oldest 50% of remaining entries
	if len(y.seenMessageIDs) > (maxSeenMessageIDs * 4 / 5) {
		y.log.Debug("Still at high capacity after cleanup, removing oldest entries")

		// Collect all entries with timestamps
		type entry struct {
			id   string
			time time.Time
		}
		entries := make([]entry, 0, len(y.seenMessageIDs))
		for id, timestamp := range y.seenMessageIDs {
			entries = append(entries, entry{id, timestamp})
		}

		// Sort by timestamp (oldest first) using stdlib - O(n log n) instead of O(n²)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].time.Before(entries[j].time)
		})

		// Remove oldest 50%
		removeCount := len(entries) / 2
		for i := 0; i < removeCount; i++ {
			delete(y.seenMessageIDs, entries[i].id)
			deletedCount++
		}
	}

	y.log.Debug("Cleanup completed - Removed %d message IDs (remaining: %d)", deletedCount, len(y.seenMessageIDs))
}

func (y *Youtube) resetConsecutiveErrors() {
	if y.streamHealth.ConsecutiveErrors > 0 {
		y.streamMutex.Lock()
		y.streamHealth.ConsecutiveErrors = 0
		y.streamMutex.Unlock()
	}
}
