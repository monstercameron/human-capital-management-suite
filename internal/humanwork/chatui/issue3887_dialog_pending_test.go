package chatui

import (
	"strings"
	"testing"
)

// Issue #3887: while a report dialog waits for the moderation service its
// placeholder must sit inside a recognizable dialog, with a visible Cancel and a
// way to try again; a failed read keeps the same frame with Try again and Close.
func TestIssue3887_PendingDialogHasFrameCancelAndRetry(t *testing.T) {
	for _, tc := range []struct{ locale, title, cancel, retry string }{
		{"en-US", "Report message", "Cancel", "Try again"},
		{"de-DE", "Nachricht melden", "Abbrechen", "Erneut versuchen"},
		{"ar", "الإبلاغ عن رسالة", chatremoveText("ar", "cancel"), "حاول مجدداً"},
	} {
		markup := ModerationDialogPendingMarkup(tc.locale, "report", "")
		for _, want := range []string{
			`class="chatremove chat-dialog chatremove-dialog-pending"`,
			`id="chatremove-title"`, tc.title,
			`data-chatremove-close`, tc.cancel,
			`data-chat-loading=`, `data-chatremove-retry`, tc.retry,
		} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s loading dialog misses %q in %s", tc.locale, want, markup)
			}
		}
		if tc.cancel == "" {
			t.Errorf("%s has no Cancel copy", tc.locale)
		}
	}
	if !strings.Contains(ModerationDialogPendingMarkup("ar", "report", ""), `dir="rtl"`) {
		t.Error("the Arabic loading dialog is not right to left")
	}
}

func TestIssue3887_FailedDialogKeepsFrameWithRetryAndClose(t *testing.T) {
	markup := ModerationDialogPendingMarkup("en-US", "report", "failed")
	for _, want := range []string{
		`chatremove-dialog-pending`, `role="alert"`, `data-chatremove-retry`, "Try again", `data-chatremove-close`, "Close",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("failed dialog misses %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, `data-chat-loading=`) {
		t.Error("a failed read still draws the loading placeholder")
	}
	if got := ModerationDialogPendingMarkup("en-US", "restore", ""); !strings.Contains(got, ">Restore<") {
		t.Errorf("the restore dialog is not named for what it does: %s", got)
	}
}
