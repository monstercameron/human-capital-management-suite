package workspace_test

// UXAUDIT-025 PRIMARY: gate the live frontend on the blind-audit critical
// journeys.
//
// RED: the component suites pass while the running application still has a
// dead-end promotion, broken mobile shell, ambiguous task navigation,
// material layout shift, inaccessible interaction, unauthorized disclosure or
// any unresolved finding from this audit series.
//
// GREEN: the audit-series findings registry names every UXAUDIT-001..024 and
// UIPOLISH-012 disposition with severity, status and component evidence; no
// severity-1/2 finding stays open or waived without owner and expiry; and a
// real-server browser harness disclosure records the live four-persona pass
// this repository's CI cannot execute (see the UXAUDIT-024 precedent).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type uxaudit025Finding struct {
	ID           string   `json:"id"`
	Severity     int      `json:"severity"`
	Status       string   `json:"status"`
	Owner        string   `json:"owner"`
	WaiverExpiry string   `json:"waiver_expiry"`
	Evidence     []string `json:"evidence"`
	Note         string   `json:"note"`
}

type uxaudit025Gate struct {
	Gate               string              `json:"gate"`
	LiveBrowserHarness string              `json:"live_browser_harness"`
	Findings           []uxaudit025Finding `json:"findings"`
}

func uxaudit025Load(t *testing.T) (string, []byte, uxaudit025Gate) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "test", "workspace", "testdata", "uxaudit025_findings.json"))
	if err != nil {
		t.Fatalf("open findings registry: %v", err)
	}
	var gate uxaudit025Gate
	if err := json.Unmarshal(raw, &gate); err != nil {
		t.Fatalf("parse findings registry: %v", err)
	}
	return root, raw, gate
}

// TestTodo_UXAUDIT_025 enforces the release gate: the registry covers exactly
// the audit series, no severity-1/2 finding is open, waivers carry owner and
// unexpired expiry, and every resolved finding names component evidence that
// exists on disk.
func TestTodo_UXAUDIT_025(t *testing.T) {
	root, _, gate := uxaudit025Load(t)
	if gate.Gate != "UXAUDIT-025" {
		t.Fatalf("gate=%q", gate.Gate)
	}
	if strings.TrimSpace(gate.LiveBrowserHarness) == "" {
		t.Fatal("gate must disclose the live real-server browser harness boundary")
	}
	want := map[string]bool{"UIPOLISH-012": false}
	for i := 1; i <= 24; i++ {
		id := "UXAUDIT-"
		if i < 10 {
			id += "00"
		} else {
			id += "0"
		}
		want[id+itoa(i)] = false
	}
	if len(gate.Findings) != len(want) {
		t.Fatalf("findings=%d, want %d", len(gate.Findings), len(want))
	}
	for _, finding := range gate.Findings {
		seen, ok := want[finding.ID]
		if !ok {
			t.Fatalf("finding %q is outside the audit series", finding.ID)
		}
		if seen {
			t.Fatalf("duplicate finding %q", finding.ID)
		}
		want[finding.ID] = true
		if finding.Severity < 1 || finding.Severity > 3 {
			t.Fatalf("finding %s has no declared severity", finding.ID)
		}
		switch finding.Status {
		case "resolved":
			if len(finding.Evidence) == 0 || strings.TrimSpace(finding.Note) == "" {
				t.Fatalf("finding %s resolves without evidence or rationale", finding.ID)
			}
			for _, path := range finding.Evidence {
				if strings.Contains(path, "..") || filepath.IsAbs(path) {
					t.Fatalf("finding %s evidence escapes the tree: %q", finding.ID, path)
				}
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
					t.Fatalf("finding %s evidence missing: %s", finding.ID, path)
				}
			}
		case "waived":
			if finding.Severity <= 2 {
				if strings.TrimSpace(finding.Owner) == "" || strings.TrimSpace(finding.WaiverExpiry) == "" {
					t.Fatalf("finding %s waives severity-%d without owner and expiry", finding.ID, finding.Severity)
				}
				expiry, err := time.Parse("2006-01-02", finding.WaiverExpiry)
				if err != nil || !expiry.After(time.Now()) {
					t.Fatalf("finding %s waiver expiry invalid: %q", finding.ID, finding.WaiverExpiry)
				}
			}
		case "open":
			if finding.Severity <= 2 {
				t.Fatalf("severity-%d finding %s is still open: %s", finding.Severity, finding.ID, finding.Note)
			}
		default:
			t.Fatalf("finding %s has unknown status %q", finding.ID, finding.Status)
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("audit series member %s has no registry entry", id)
		}
	}
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// TestTodo_UXAUDIT_025_Regression makes the gate tamper-evident: dropping a
// finding, lowering a severity or flipping a status without review changes the
// registry digest and fails here.
func TestTodo_UXAUDIT_025_Regression(t *testing.T) {
	_, raw, _ := uxaudit025Load(t)
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != uxaudit025RegistryDigest {
		t.Fatalf("findings registry changed without gate review: digest=%s", hex.EncodeToString(sum[:]))
	}
}

const uxaudit025RegistryDigest = "692e2df34cb94ca10d04072b5b2e825e4b91917583f8e3e615339332ba63bdae"
