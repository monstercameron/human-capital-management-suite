package runstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_DURABLE_001 is the PRIMARY test for the crash inventory: one row
// per multi-step process and one sub-row per step, each answering where its
// state lives, what makes it durable, its idempotency key, who resumes it
// and when, the worst case on death before/during/after, whether a person
// can tell, and the time to resume. Every "nothing does", "stuck" and
// "repeated" must appear as a finding naming the todo that would fix it.
func TestTodo_DURABLE_001(t *testing.T) {
	processes := DurableInventory()
	if len(processes) == 0 {
		t.Fatal("durable inventory is empty")
	}
	wantProcesses := []string{
		"mention-answer",
		"agents-page-task",
		"scheduled-announcement",
		"reminder",
		"ambient-read",
		"offer-accept",
		"support-email",
		"voice-transcription",
		"document-indexing",
		"chat-indexing",
		"version-review",
		"evaluation",
		"publication-provisioning",
		"staged-rollout",
		"channel-gate",
		"feedback-stop-retry",
		"preparation-command",
	}
	byID := make(map[string]DurableProcess, len(processes))
	for _, process := range processes {
		if process.ID == "" || process.Title == "" {
			t.Fatalf("process row without identity: %+v", process)
		}
		if _, dup := byID[process.ID]; dup {
			t.Fatalf("duplicate process row %q", process.ID)
		}
		byID[process.ID] = process
		if len(process.Steps) == 0 {
			t.Fatalf("process %q has no step sub-rows", process.ID)
		}
		for _, step := range process.Steps {
			empty := []string{}
			if step.Name == "" {
				empty = append(empty, "name")
			}
			if step.State == "" {
				empty = append(empty, "state")
			}
			if step.Durability == "" {
				empty = append(empty, "durability")
			}
			if step.IdempotencyKey == "" {
				empty = append(empty, "idempotency key")
			}
			if step.Resumer == "" {
				empty = append(empty, "resumer")
			}
			if step.DieBefore == "" || step.DieDuring == "" || step.DieAfter == "" {
				empty = append(empty, "die before/during/after")
			}
			if step.Visible == "" {
				empty = append(empty, "person-visible")
			}
			if step.ResumeTime == "" {
				empty = append(empty, "resume time")
			}
			if len(empty) > 0 {
				t.Fatalf("process %q step %q leaves unanswered: %s", process.ID, step.Name, strings.Join(empty, ", "))
			}
		}
	}
	for _, want := range wantProcesses {
		if _, ok := byID[want]; !ok {
			t.Fatalf("inventory omits GREEN process %q", want)
		}
	}

	// The mention path names GREEN's own step vocabulary.
	mention := byID["mention-answer"]
	mentionSteps := make(map[string]bool, len(mention.Steps))
	for _, step := range mention.Steps {
		mentionSteps[strings.ToLower(step.Name)] = true
	}
	for _, want := range []string{"admission", "claim", "model call", "tool effect", "continuation", "validation", "delivery", "receipt"} {
		if !mentionSteps[want] {
			t.Fatalf("mention-answer omits GREEN step %q (has %v)", want, mention.Steps)
		}
	}

	// Every "nothing does", "stuck" and "repeated" is a finding with a
	// fixing todo. The fixing todos are the DURABLE research family plus
	// the voice follow-up the record names.
	findings := DurableFindings()
	if len(findings) == 0 {
		t.Fatal("inventory lists no findings")
	}
	allowedFix := map[string]bool{
		"DURABLE-002": true, "DURABLE-003": true, "DURABLE-004": true,
		"DURABLE-005": true, "DURABLE-006": true,
	}
	covered := make(map[string]bool, len(findings))
	for _, finding := range findings {
		if finding.ID == "" || finding.Process == "" || finding.Step == "" || finding.Symptom == "" {
			t.Fatalf("finding without identity: %+v", finding)
		}
		if !allowedFix[finding.FixTodo] {
			t.Fatalf("finding %q names unknown fixing todo %q", finding.ID, finding.FixTodo)
		}
		if _, ok := byID[finding.Process]; !ok {
			t.Fatalf("finding %q names unknown process %q", finding.ID, finding.Process)
		}
		covered[finding.Process+"\x00"+finding.Step] = true
	}
	for _, process := range processes {
		for _, step := range process.Steps {
			needsFinding := step.Durability == DurabilityNone ||
				strings.Contains(strings.ToLower(step.DieBefore), "stuck") ||
				strings.Contains(strings.ToLower(step.DieDuring), "stuck") ||
				strings.Contains(strings.ToLower(step.DieAfter), "stuck") ||
				strings.Contains(strings.ToLower(step.DieBefore), "repeat") ||
				strings.Contains(strings.ToLower(step.DieDuring), "repeat") ||
				strings.Contains(strings.ToLower(step.DieAfter), "repeat")
			if needsFinding && !covered[process.ID+"\x00"+step.Name] {
				t.Fatalf("process %q step %q admits nothing-durable, stuck or repeated with no finding", process.ID, step.Name)
			}
		}
	}

	// The record cross-checks the journal vocabulary it cites: these
	// identifiers must keep existing or the inventory is stale.
	_ = StateReady
	_ = StateRunning
	_ = StateWaiting
	_ = StateReconciling
	_ = ErrLease
	_ = ErrTerminal

	text := RenderDurableInventory()
	for _, want := range []string{
		"mention", "announcement", "reminder", "ambient", "support email",
		"voice", "index", "version review", "evaluation", "publication",
		"rollout", "gate", "preparation", "idempotency", "lease", "checkpoint",
		"outbox", "RECONCILING", "DURABLE-",
	} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Fatalf("rendered inventory omits %q", want)
		}
	}
}

// TestTodo_DURABLE_001_Golden pins the rendered inventory bytes.
func TestTodo_DURABLE_001_Golden(t *testing.T) {
	const golden = "testdata/durable001_inventory.golden"
	want, err := os.ReadFile(filepath.Clean(golden))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got := RenderDurableInventory(); got != string(want) {
		t.Fatalf("rendered inventory differs from %s (%d bytes vs %d)", golden, len(got), len(want))
	}
}
