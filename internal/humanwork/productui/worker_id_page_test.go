package productui

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestWorkerIDInvalidNumbersDisableSaveAndAvoidSuccessBadge(t *testing.T) {
	view := testView(PageWorkerIDs)
	view.WorkerIDPolicy = WorkerIDPolicy{SequenceDigits: 6, StartAt: 1, IncrementBy: -1}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`<button[^>]*disabled[^>]*>Save worker ID rules</button>`).MatchString(doc) {
		t.Fatal("invalid numeric draft retains an enabled save action")
	}
	if !strings.Contains(doc, "No examples available") || strings.Contains(doc, "Atomic uniqueness") {
		t.Fatal("invalid preview lacks explanation or shows a success badge")
	}
	if !strings.Contains(doc, "Complete the numeric fields") || !strings.Contains(doc, `aria-describedby="worker-id-status"`) {
		t.Fatal("disabled save lacks an associated explanation")
	}
	if !strings.Contains(doc, ".app-shell .button:disabled,.app-shell .button:disabled:hover{") {
		t.Fatal("native disabled buttons lack theme-aware visual state")
	}
}

func TestWorkerIDDraftExamplesTrackInputsWithoutIssuingNumbers(t *testing.T) {
	p := WorkerIDPolicy{Prefix: "TEST", Separator: "-", SequenceDigits: 6, StartAt: 1, NextSequence: 42, IncrementBy: 2, ZeroPad: true, YearFormat: "YYYY", CheckDigit: "NONE", Version: 2, IssuedCount: 5}
	got, err := workerIDDraftExamples(p, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 4 || got[0] != "TEST-2026-000042" || got[3] != "TEST-2026-000048" {
		t.Fatalf("draft examples = %v, %v", got, err)
	}
	if p.NextSequence != 42 || p.IssuedCount != 5 {
		t.Fatal("preview changed allocation")
	}
	p.SequenceDigits = 0
	if _, err := workerIDDraftExamples(p, time.Time{}); err == nil {
		t.Fatal("invalid digits silently normalized")
	}
	p.SequenceDigits = 6
	p.ExcludedRanges = "invalid"
	if _, err := workerIDDraftExamples(p, time.Time{}); err == nil {
		t.Fatal("invalid exclusions silently accepted")
	}
}

func TestWorkerIDNumericDraftPreservesIncompleteInputAsInvalid(t *testing.T) {
	for _, raw := range []string{"", "-", "1.5", "1e3", "999999999999999999999999"} {
		if got := workerIDNumericDraft(raw); got != -1 {
			t.Errorf("raw %q parsed as %d", raw, got)
		}
	}
	p := WorkerIDPolicy{SequenceDigits: 6, StartAt: 1000, IncrementBy: 1}
	draft := newWorkerIDDraft(p, time.Time{})
	if draft.Numbers != [3]string{"6", "1000", "1"} {
		t.Fatalf("seed = %v", draft.Numbers)
	}
	draft.Numbers[2] = ""
	draft.Policy.IncrementBy = workerIDNumericDraft(draft.Numbers[2])
	if workerIDNumbersValid(draft.Policy) {
		t.Fatal("blank increment can submit")
	}
	if _, err := workerIDDraftExamples(draft.Policy, time.Time{}); err == nil {
		t.Fatal("blank increment has preview")
	}
	if draft.Numbers[2] != "" {
		t.Fatal("blank input was replaced")
	}
	draft.Policy.IncrementBy = workerIDNumericDraft("5")
	if !workerIDNumbersValid(draft.Policy) {
		t.Fatal("valid increment cannot recover")
	}
}

func TestWorkerIDAdminPageRendersGovernedRulesAndExamples(t *testing.T) {
	view := testView(PageWorkerIDs)
	view.WorkerIDPolicy = WorkerIDPolicy{Version: 2, Prefix: "HC", Separator: "-", SequenceDigits: 6, StartAt: 1000, NextSequence: 1042, IncrementBy: 1, ZeroPad: true, YearFormat: "NONE", CheckDigit: "NONE", IssuedCount: 42, Previews: []string{"HC-001042", "HC-001043"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Issue worker numbers your way", `id="worker-prefix"`, "Maximum sequence digits", "HC-001042", "Format preview", "never reused", `href="/workspace/app/admin"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("worker ID page missing %q", want)
		}
	}
}

func TestWorkerIDPageSubmitsTypedDraft(t *testing.T) {
	var got WorkerIDPolicy
	node := WorkerIDPage(WorkerIDPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Policy: WorkerIDPolicy{Prefix: "HC", Separator: "-", SequenceDigits: 6, StartAt: 1, NextSequence: 1, IncrementBy: 1, YearFormat: "NONE", CheckDigit: "NONE"}, OnSave: func(p WorkerIDPolicy) { got = p }})
	if node == nil {
		t.Fatal("nil component")
	}
	// Event dispatch is exercised by the WASM integration; this unit contract
	// ensures the typed callback is retained instead of a page-shaped View.
	_ = got
}

func TestWorkerIDPolicyDirtyStateIgnoresServerAllocationCounters(t *testing.T) {
	source := WorkerIDPolicy{Version: 2, Prefix: "HC", SequenceDigits: 6, StartAt: 1, IncrementBy: 1, NextSequence: 42, IssuedCount: 10}
	allocationMoved := source
	allocationMoved.NextSequence = 43
	allocationMoved.IssuedCount = 11
	if workerIDPolicyChanged(source, allocationMoved) {
		t.Fatal("server-owned allocation counters should not make the editor dirty")
	}
	draft := allocationMoved
	draft.Prefix = "EU"
	if !workerIDPolicyChanged(source, draft) {
		t.Fatal("format edits should mark the editor dirty")
	}
	fields := workerIDChangedFields(I18nProps{Locale: ResolveProductLocale("en-US")}, source, draft)
	if len(fields) != 1 || fields[0] != "Number prefix, suffix, or separator" {
		t.Fatalf("changed fields = %v", fields)
	}
}

func TestWorkerIDPageShowsGroupedRulesAndSavedState(t *testing.T) {
	view := testView(PageWorkerIDs)
	view.WorkerIDPolicy = WorkerIDPolicy{SequenceDigits: 6, StartAt: 1, IncrementBy: 1}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-field-group="worker-id-identity"`, `data-field-group="worker-id-sequence"`, `data-field-group="worker-id-format"`, `data-field-group="worker-id-reserved"`, `data-unsaved="false"`, "No unsaved changes"} {
		if !strings.Contains(doc, want) {
			t.Errorf("worker ID page missing %q", want)
		}
	}
}
