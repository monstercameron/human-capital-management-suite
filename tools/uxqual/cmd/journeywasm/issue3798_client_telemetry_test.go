package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/clienttelemetry"
)

func TestIssue3798_RecoverAtCountsInsteadOfVanishing(t *testing.T) {
	before := func() int { _, p := clientTelemetry.Snapshot(); return p["test.site"] }
	n := before()
	func() {
		defer recoverAt("test.site")
		panic("boom with a secret message")
	}()
	if got := before(); got != n+1 {
		t.Fatalf("panic count = %d, want %d", got, n+1)
	}
	// A call that does not panic must not count.
	func() { defer recoverAt("test.site") }()
	if got := before(); got != n+1 {
		t.Fatalf("quiet call counted: %d", got)
	}
	_, p := clientTelemetry.Snapshot()
	for site := range p {
		if strings.Contains(site, "secret") {
			t.Fatal("the panic value must never become a site label")
		}
	}
}

func TestIssue3798_CountersDropNamesTheServerWouldRefuse(t *testing.T) {
	c := &clientTelemetryCounters{stream: map[string]int{}, panics: map[string]int{}}
	c.CountStream("open")
	c.CountStream("open")
	c.CountStream("subscribe-skipped")
	c.CountPanic("Bad Site With Spaces")
	c.CountPanic("ok.site")
	stream, panics := c.Snapshot()
	if stream["open"] != 2 || len(stream) != 1 {
		t.Fatalf("stream = %v", stream)
	}
	if panics["ok.site"] != 1 || len(panics) != 1 {
		t.Fatalf("panics = %v", panics)
	}
	for i := 0; i < 100; i++ {
		c.CountPanic("site" + strings.Repeat("a", i%10) + string(rune('a'+i%26)) + string(rune('a'+i/26)))
	}
	if _, p := c.Snapshot(); len(p) > clienttelemetry.MaxPanicSites {
		t.Fatalf("panic table grew to %d", len(p))
	}
}

func TestIssue3798_RecordCarriesBootMarksStreamAndPanicsAndPassesServerValidation(t *testing.T) {
	marks := map[string]float64{"hcm:go-main": 120.4, "hcm:hydrated": 899.6, "hcm:read-Some Name": 5, "other": 9, "hcm:neg": -4}
	record, ok := buildClientTelemetryRecord("abc123", "de-DE", clienttelemetry.ReasonPageHide, "0123456789abcdef", marks, map[string]int{"open": 2}, map[string]int{"history.push": 1})
	if !ok {
		t.Fatalf("record refused by its own validator: %+v", record)
	}
	if record.Marks["go-main"] != 120 || record.Marks["hydrated"] != 900 || record.Marks["neg"] != 0 || len(record.Marks) != 3 {
		t.Fatalf("marks = %v", record.Marks)
	}
	raw, _ := json.Marshal(record)
	if _, err := clienttelemetry.Decode(strings.NewReader(string(raw))); err != nil {
		t.Fatalf("server decode: %v (%s)", err, raw)
	}
}

func TestIssue3798_RecordIsRefusedLocallyWhenInvalid(t *testing.T) {
	if _, ok := buildClientTelemetryRecord("", "en-US", clienttelemetry.ReasonError, "0123456789abcdef", nil, nil, nil); ok {
		t.Fatal("an empty build id must not be sent")
	}
	if _, ok := buildClientTelemetryRecord("b", "en-US", "click", "0123456789abcdef", nil, nil, nil); ok {
		t.Fatal("an unknown reason must not be sent")
	}
}

func TestIssue3798_GateSendsFirstErrorOnceAndBoundsPageHides(t *testing.T) {
	var g telemetryGate
	if !g.Allow(clienttelemetry.ReasonError) || g.Allow(clienttelemetry.ReasonError) {
		t.Fatal("the first error sends exactly once")
	}
	sent := 0
	for i := 0; i < 10; i++ {
		if g.Allow(clienttelemetry.ReasonPageHide) {
			sent++
		}
	}
	if sent != clientTelemetryMaxPageHides {
		t.Fatalf("page hides sent %d, want %d", sent, clientTelemetryMaxPageHides)
	}
	if g.Allow("other") {
		t.Fatal("unknown trigger allowed")
	}
}

func TestIssue3798_StreamLogFeedsTheCounters(t *testing.T) {
	before, _ := clientTelemetry.Snapshot()
	logChatStream(chatStreamEvent{Conversation: "c", Event: "open"})
	logChatStream(chatStreamEvent{Conversation: "c", Event: "ended", Rollover: true})
	logChatStream(chatStreamEvent{Conversation: "c", Event: "gave-up"})
	after, _ := clientTelemetry.Snapshot()
	for key, want := range map[string]int{"open": 1, "ended": 1, "rollover": 1, "gave-up": 1} {
		if after[key]-before[key] != want {
			t.Errorf("%s moved by %d, want %d", key, after[key]-before[key], want)
		}
	}
}

func TestIssue3798_NoBareSilentRecoverRemains(t *testing.T) {
	// Every recover() in the browser files must go through recoverAt (or
	// record the site itself), so a swallowed panic is counted.
	for _, file := range telemetryWasmFilesWithRecover(t) {
		t.Errorf("%s still swallows a panic without a site label", file)
	}
}

// telemetryWasmFilesWithRecover lists browser files holding a recover() that
// neither goes through recoverAt nor counts its site within the next lines.
func telemetryWasmFilesWithRecover(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var bad []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "issue3798_client_telemetry.go" {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "recover()") || strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			end := i + 4
			if end > len(lines) {
				end = len(lines)
			}
			if !strings.Contains(strings.Join(lines[i:end], "\n"), "clientTelemetry.CountPanic(") {
				bad = append(bad, fmt.Sprintf("%s:%d", name, i+1))
			}
		}
	}
	return bad
}
