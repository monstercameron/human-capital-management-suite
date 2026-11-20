package main

import (
	"regexp"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/clienttelemetry"
)

// Issue 3798: the client counts what it already knows (live-stream state
// transitions, recovered panics) and sends one bounded, PII-free record when
// the page is hidden or first fails. This file is the portable half: counters,
// record building and the send gate. The browser half is
// issue3798_client_telemetry_wasm.go.

// clientTelemetryPath is the beacon endpoint (application.ChatClientTelemetryPath;
// the wasm client cannot import the server package).
const clientTelemetryPath = "/api/chat/telemetry"

// clientTelemetryMaxPageHides caps the beacons one page load sends; a page that
// is restored from the back-forward cache hides again, and the server throttles
// the rest.
const clientTelemetryMaxPageHides = 3

type clientTelemetryCounters struct {
	mu     sync.Mutex
	stream map[string]int
	panics map[string]int
}

// clientTelemetry is the process-wide set of counters.
var clientTelemetry = &clientTelemetryCounters{stream: map[string]int{}, panics: map[string]int{}}

// CountStream counts one live-stream state transition. Names outside the
// server's allow-list are dropped, so a new diagnostic event never makes the
// whole record refusable.
func (c *clientTelemetryCounters) CountStream(name string) {
	if c == nil || !clienttelemetry.StreamKeys[name] {
		return
	}
	c.mu.Lock()
	c.stream[name]++
	c.mu.Unlock()
}

// CountPanic counts one recovered panic or window-level failure at a site.
func (c *clientTelemetryCounters) CountPanic(site string) {
	if c == nil || !telemetrySitePattern.MatchString(site) {
		return
	}
	c.mu.Lock()
	// Bound the table; the server refuses more than MaxPanicSites anyway.
	if _, known := c.panics[site]; known || len(c.panics) < clienttelemetry.MaxPanicSites {
		c.panics[site]++
	}
	c.mu.Unlock()
}

// Snapshot copies the counters.
func (c *clientTelemetryCounters) Snapshot() (stream, panics map[string]int) {
	stream, panics = map[string]int{}, map[string]int{}
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.stream {
		stream[k] = v
	}
	for k, v := range c.panics {
		panics[k] = v
	}
	return
}

var (
	telemetrySitePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)
	telemetryMarkPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

// recoverAt is deferred directly (defer recoverAt("history.push")) in place of
// a bare recover(): the panic is still swallowed, but it now counts against a
// named site instead of vanishing. recover only works when the deferred
// function itself calls it, which is why this is not wrapped further.
func recoverAt(site string) {
	if recover() != nil {
		clientTelemetry.CountPanic(site)
	}
}

// telemetryMarks turns User Timing marks (name -> start in milliseconds) into
// the record's allow-listed form: only "hcm:" marks, prefix removed, names that
// fit the server's pattern, at most MaxMarks of them (earliest first wins ties
// by name so the result is stable).
func telemetryMarks(raw map[string]float64) map[string]int {
	out := map[string]int{}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		short := strings.TrimPrefix(name, bootMarkPrefix)
		if short == name || !telemetryMarkPattern.MatchString(short) || len(out) >= clienttelemetry.MaxMarks {
			continue
		}
		ms := raw[name]
		if ms < 0 {
			ms = 0
		}
		if ms > 3_600_000 {
			ms = 3_600_000
		}
		out[short] = int(ms + 0.5)
	}
	return out
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// buildClientTelemetryRecord assembles a record and reports whether the server
// would accept it. A record that fails its own validation is never sent.
func buildClientTelemetryRecord(build, locale, reason, session string, marks map[string]float64, stream, panics map[string]int) (clienttelemetry.Record, bool) {
	record := clienttelemetry.Record{
		Version: clienttelemetry.Version,
		Build:   build,
		Locale:  locale,
		Reason:  reason,
		Session: session,
		Marks:   telemetryMarks(marks),
		Stream:  nonEmpty(stream),
		Panics:  nonEmpty(panics),
	}
	return record, record.Validate() == nil
}

func nonEmpty(values map[string]int) map[string]int {
	if len(values) == 0 {
		return nil
	}
	return values
}

// telemetryGate decides whether a trigger may send. The first error sends
// once; page hides send up to clientTelemetryMaxPageHides times.
type telemetryGate struct {
	mu        sync.Mutex
	errorSent bool
	hides     int
}

func (g *telemetryGate) Allow(reason string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch reason {
	case clienttelemetry.ReasonError:
		if g.errorSent {
			return false
		}
		g.errorSent = true
		return true
	case clienttelemetry.ReasonPageHide:
		if g.hides >= clientTelemetryMaxPageHides {
			return false
		}
		g.hides++
		return true
	}
	return false
}
