package chatui

import (
	"os"
	"strings"
	"testing"
)

// TestIssue3989_RecenterDecision: the target 4,170 px above the viewport (what a
// cold cross-channel open showed) must be pulled back; one that is in view must
// be left alone.
func TestIssue3989_RecenterDecision(t *testing.T) {
	cases := []struct {
		name                           string
		rowTop, rowBottom, top, bottom float64
		want                           bool
	}{
		{"far above after landing on newest", -4170, -4080, 100, 700, true},
		{"far below", 2000, 2090, 100, 700, true},
		{"centered", 349, 438, 100, 700, false},
		{"partly cut at the top", 80, 160, 100, 700, true},
		{"partly cut at the bottom", 650, 740, 100, 700, true},
		{"taller than the timeline, covering the middle", 50, 900, 100, 700, false},
		{"taller than the timeline, scrolled past", -900, 500, 100, 700, false},
		{"taller than the timeline, below the middle", 500, 1500, 100, 700, true},
		{"timeline not laid out yet", -4170, -4080, 0, 0, false},
	}
	for _, c := range cases {
		if got := issue3989NeedsRecenter(c.rowTop, c.rowBottom, c.top, c.bottom); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestIssue3989_BrowserHalfWatchesTargetUntilSettled pins that the browser half
// does not look for the row once: it keeps retrying until the row exists, puts
// it back in view when the room switch or late layout moved it, stops when the
// reader scrolls, and reports a row that never arrived.
func TestIssue3989_BrowserHalfWatchesTargetUntilSettled(t *testing.T) {
	raw, err := os.ReadFile("search_focus_js.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, want := range []string{
		"issue3989SettleFrames", "issue3989MissingFrames", "issue3989NeedsRecenter",
		`"wheel"`, `"touchstart"`, `"keydown"`, "FocusSearchMessageOrElse",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("search_focus_js.go does not use %s", want)
		}
	}
	for _, path := range []string{"../../../tools/uxqual/cmd/journeywasm/chat_wasm.go"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(raw), "chatui.FocusSearchMessageOrElse(") < 2 {
			t.Errorf("%s: both open paths must report a hit that never arrives", path)
		}
	}
}
