package main

import (
	"os"
	"strings"
	"testing"
)

func TestIssue3948_CloseStep(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		report, dirty, asking bool
		want                  string
	}{
		{"an empty report closes at once", true, false, false, "close"},
		{"a report with details asks first", true, true, false, "ask"},
		{"Escape on the question keeps the report", true, true, true, "keep"},
		{"another dialog with a note is not asked about", false, true, false, "close"},
	} {
		if got := issue3948CloseStep(tc.report, tc.dirty, tc.asking); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	if issue3948Dirty("", "  \n", false) || !issue3948Dirty("spam", "", false) || !issue3948Dirty("", "salary", false) {
		t.Error("a report is dirty exactly when a reason or a non-blank note is in it")
	}
	if issue3948Dirty("spam", "note", true) {
		t.Error("a report that is being sent must not be asked about")
	}
}

func TestIssue3948_EveryWayOfClosingAsks(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	remove := read("chatremove_wasm.go")
	// Only the cleanup and the two decisions that finish the work close a dialog
	// directly; the person's own ways of leaving (backdrop, Close, Cancel, Escape)
	// go through requestCloseDialog.
	if n := strings.Count(remove, `b.closeKind("dialog")`); n != 3 {
		t.Errorf("chatremove_wasm.go closes a dialog directly in %d places, want 3 (cleanup, quick/apply, resolve)", n)
	}
	for _, want := range []string{"requestCloseDialog()", "data-chatremove-discard"} {
		if !strings.Contains(remove, want) {
			t.Errorf("chatremove_wasm.go misses %s", want)
		}
	}
	wasm := read("issue3948_discard_wasm.go")
	for _, want := range []string{"issue3948CloseStep(", "chatui.ReportDiscardMarkup(", "issue3948Dirty("} {
		if !strings.Contains(wasm, want) {
			t.Errorf("issue3948_discard_wasm.go misses %s", want)
		}
	}
	for _, banned := range []string{"localStorage", "sessionStorage", "indexedDB"} {
		if strings.Contains(wasm, banned) {
			t.Errorf("a report is kept in %s: it must not outlive the dialog", banned)
		}
	}
}

func TestIssue3887_DialogActionAndFrame(t *testing.T) {
	if got := issue3887ActionOf("/api/chat/moderation/page?action=report&post=p"); got != "report" {
		t.Errorf("action = %q", got)
	}
	overlay, err := os.ReadFile("chatmod005_overlay_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(overlay)
	if strings.Contains(text, "LoadingShapeSection, Status: chatui.ModerationText(b.locale, \"loading\")}))") {
		t.Error("a dialog still draws bare placeholder bars into the backdrop")
	}
	for _, want := range []string{"chatui.ModerationDialogPendingMarkup(b.locale, issue3887ActionOf(href), \"\")", "chatui.ModerationDialogPendingMarkup(b.locale, issue3887ActionOf(href), chatremoveErrorKey(err))"} {
		if !strings.Contains(text, want) {
			t.Errorf("chatmod005_overlay_wasm.go misses %s", want)
		}
	}
	remove, err := os.ReadFile("chatremove_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(remove), "data-chatremove-retry") {
		t.Error("the dialog's Try again is not handled")
	}
}
