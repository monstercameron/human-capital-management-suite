package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// chatStreamLogSize is how many subscription events are kept. The point of the
// buffer is to answer one question from a live run -- why did this room stop
// updating -- and the last fifty open/end/attempt lines answer it. It is
// deliberately small enough to be always on: a give-up that only reproduces on
// a real tunnel is not something a flag should hide.
const chatStreamLogSize = 50

// chatStreamEvent is one line in the subscription log.
type chatStreamEvent struct {
	At           time.Time
	Conversation string
	// Event is open, ended, gave-up, cancelled or resubscribe.
	Event string
	// Cause says why an end happened, in the transport's own words. It is
	// diagnostic, never reader-facing copy.
	Cause    string
	Attempt  int
	Backoff  time.Duration
	Rollover bool
}

// String is the one-line form the browser buffer holds.
func (e chatStreamEvent) String() string {
	line := fmt.Sprintf("%s %s %s", e.At.UTC().Format("15:04:05.000"), e.Event, e.Conversation)
	if e.Cause != "" {
		line += " cause=" + e.Cause
	}
	if e.Attempt > 0 {
		line += fmt.Sprintf(" attempt=%d", e.Attempt)
	}
	if e.Backoff > 0 {
		line += " backoff=" + e.Backoff.String()
	}
	if e.Rollover {
		line += " rollover"
	}
	return line
}

// chatStreamRing is a fixed-size ring of subscription events.
type chatStreamRing struct {
	mu      sync.Mutex
	entries []chatStreamEvent
	next    int
	size    int
}

func newChatStreamRing(size int) *chatStreamRing {
	if size <= 0 {
		size = chatStreamLogSize
	}
	return &chatStreamRing{entries: make([]chatStreamEvent, size), size: size}
}

// Add records one event, overwriting the oldest once the ring is full.
func (r *chatStreamRing) Add(event chatStreamEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.entries[r.next%r.size] = event
	r.next++
	r.mu.Unlock()
}

// Lines returns the log oldest first.
func (r *chatStreamRing) Lines() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	count := r.next
	if count > r.size {
		count = r.size
	}
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		index := (r.next - count + i) % r.size
		out = append(out, r.entries[index].String())
	}
	return out
}

// Text is the whole log as one string, which is what the browser buffer holds
// so a live run can be read without a debugger.
func (r *chatStreamRing) Text() string { return strings.Join(r.Lines(), "\n") }

// chatStreamLog is the process-wide subscription log.
var chatStreamLog = newChatStreamRing(chatStreamLogSize)

// logChatStream records one subscription event and publishes the buffer.
func logChatStream(event chatStreamEvent) {
	if event.At.IsZero() {
		event.At = time.Now()
	}
	chatStreamLog.Add(event)
	publishChatStreamLog(chatStreamLog)
}

// chatStreamMaxAttempts is how many consecutive fruitless reconnects the client
// makes before it may say the conversation is no longer live. A stream that
// delivered anything, or that missed an event and caught up, resets the count.
const chatStreamMaxAttempts = 5

// chatStreamGiveUpFloor is how long a run of failures must last before the
// reader is told the conversation has stopped updating.
//
// Attempts alone were not enough: a room with history reached five of them in
// under a second, and the notice flashed every few seconds for a server that
// was working. A message saying the feed is dead has to have been earned by the
// clock as well as by the count.
const chatStreamGiveUpFloor = 30 * time.Second

// chatStreamShouldGiveUp reports whether the failures have been both numerous
// and long enough to say so.
func chatStreamShouldGiveUp(attempts int, firstFailure, now time.Time) bool {
	if attempts < chatStreamMaxAttempts || firstFailure.IsZero() {
		return false
	}
	return !now.Before(firstFailure.Add(chatStreamGiveUpFloor))
}
