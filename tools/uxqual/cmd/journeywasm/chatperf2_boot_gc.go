package main

import (
	"runtime/debug"
	"sync"
	"time"
)

// CHATBUG-014: while the client starts, its heap is small and grows steadily,
// so Go's collector ran four times before the first page was drawn, each time
// marking everything the client had built so far, on the one thread that was
// drawing the page. Profiled on the review machine that was 0.9 s of a 13 s
// load. Leaving it off until the page is drawn costs about 10 MB of memory.
//
// Two things set the collector while the client starts. The page's loader
// starts the client with it off (GOGC=off; see journeyLoaderSource in
// internal/humanwork/workspace), which covers package initialisation. The UI
// framework then applies its own pacing for an interactive page when it
// initialises (GoWebComponents ui.applyInteractiveGCPacing: collect at four
// times the live heap), which turns the collector back on just as the page is
// hydrated and drawn. Hold turns it off again for that stretch and remembers
// the framework's setting; Resume gives the setting back a moment after the
// first page is on screen, or after chatperf2CollectorLimit whatever happens,
// so a page that never finishes loading still collects.

const (
	// chatperf2CollectorDefault is Go's ordinary setting, used when nothing
	// else set one.
	chatperf2CollectorDefault = 100
	// chatperf2CollectorLimit is the longest the collector stays off.
	chatperf2CollectorLimit = 10 * time.Second
	// chatperf2CollectorAfterHydrate is how long after the shell is hydrated
	// the collector resumes: long enough for the page inside it to be drawn.
	chatperf2CollectorAfterHydrate = 3 * time.Second
	// chatperf2CollectorSettle is how long after the first page is drawn the
	// collector resumes, so its first run does not compete with the reads the
	// page starts at that moment.
	chatperf2CollectorSettle = 1500 * time.Millisecond
)

// chatperf2BootCollector keeps the collector off while the client starts and
// turns it back on, once.
type chatperf2BootCollector struct {
	mu      sync.Mutex
	resumed bool
	// held is the setting the collector had when Hold turned it off; hasHeld
	// is false when it was off already.
	held    int
	hasHeld bool
	// set changes the collector's setting and returns the one it had
	// (debug.SetGCPercent; a negative setting is off). after schedules a call.
	set   func(percent int) int
	after func(time.Duration, func())
}

// Arm bounds how long the collector stays off. It is called when the client
// starts.
func (c *chatperf2BootCollector) Arm() { c.schedule(chatperf2CollectorLimit) }

// Hydrated is called when the page shell is hydrated. A page that reports no
// first paint of its own (every page but Chat) resumes from here.
func (c *chatperf2BootCollector) Hydrated() { c.schedule(chatperf2CollectorAfterHydrate) }

// FirstPaint is called when the first page is on screen.
func (c *chatperf2BootCollector) FirstPaint() { c.schedule(chatperf2CollectorSettle) }

func (c *chatperf2BootCollector) schedule(wait time.Duration) {
	c.mu.Lock()
	resumed, after := c.resumed, c.after
	c.mu.Unlock()
	if resumed || after == nil {
		return
	}
	after(wait, c.Resume)
}

// Hold turns the collector off until Resume, remembering the setting it had.
// It is called once the UI framework has applied its own setting. After Resume
// it does nothing.
func (c *chatperf2BootCollector) Hold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resumed || c.set == nil {
		return
	}
	if previous := c.set(-1); previous >= 0 {
		c.held, c.hasHeld = previous, true
	}
}

// Resume turns the collector on. Later calls do nothing.
//
// A setting somebody made after the hold is kept: it is theirs. Otherwise the
// held setting is given back, or Go's default when the collector was never
// given one.
func (c *chatperf2BootCollector) Resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resumed || c.set == nil {
		return
	}
	c.resumed = true
	switch current := c.set(-1); {
	case current >= 0:
		c.set(current)
	case c.hasHeld:
		c.set(c.held)
	default:
		c.set(chatperf2CollectorDefault)
	}
}

// chatperf2Collector is the client's own.
var chatperf2Collector = chatperf2BootCollector{
	set:   debug.SetGCPercent,
	after: func(wait time.Duration, call func()) { time.AfterFunc(wait, call) },
}
