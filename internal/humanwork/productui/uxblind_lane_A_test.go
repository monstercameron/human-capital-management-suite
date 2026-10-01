package productui

import (
	"strings"
	"testing"
)

func TestTodo_UXBLIND_038(t *testing.T) {
	view := testView(PageWorkerIDs)
	view.WorkerIDPolicy = WorkerIDPolicy{
		Version: 1, Prefix: "HC", Separator: "-", SequenceDigits: 5,
		StartAt: 21001, NextSequence: 21061, IncrementBy: 1, ZeroPad: true,
		YearFormat: "NONE", CheckDigit: "NONE", Previews: []string{"HC-21061", "HC-21062"},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HC-21061", `>21061<`, "Preview of the current format"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("worker ID page missing %q", want)
		}
	}
	if strings.Contains(doc, "CARE") || strings.Contains(doc, "current year") {
		t.Fatal("worker ID preview still describes an unrelated sample unit or year")
	}
}

func TestTodo_UXBLIND_038_Browser(t *testing.T) {
	css := workerIDStylesStylesheet()
	start := strings.Index(css, ".worker-id-actions{")
	end := strings.Index(css[start:], ".worker-id-actions p{")
	if start < 0 || end < 0 {
		t.Fatal("worker ID action style rule is missing")
	}
	actionsCSS := css[start : start+end]
	if strings.Contains(actionsCSS, "position:fixed") || strings.Contains(actionsCSS, "position:sticky") {
		t.Fatal("worker ID action styles can cover form content")
	}
	view := testView(PageWorkerIDs)
	view.WorkerIDPolicy = WorkerIDPolicy{Version: 1, Prefix: "HC", Separator: "-", SequenceDigits: 5, StartAt: 21001, NextSequence: 21061, IncrementBy: 1, ZeroPad: true, YearFormat: "NONE", CheckDigit: "NONE", Previews: []string{"HC-21061"}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="worker-id-actions sticky-actions"`) || !strings.Contains(doc, "Format preview") {
		t.Fatal("worker ID action and preview regions are not rendered")
	}
}
