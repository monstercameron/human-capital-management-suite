package chatui

import (
	"errors"
	"strings"
	"testing"
)

func issue3778Loaded() ChatRetentionPreviewModel {
	return ChatRetentionPreviewModel{Locale: "en-US", Loaded: true, Data: ChatRetentionPreviewData{
		Configured: true, Mode: "BEFORE_DATE", Scanned: 5200, Total: 4250, HeldSkipped: 3, UnknownSkipped: 2,
		WouldRemove: []ChatRetentionPreviewKind{{Kind: "FILE", Count: 38}, {Kind: "POST", Count: 4212}, {Kind: "MYSTERY", Count: 0}},
	}}
}

func TestIssue3778_PreviewShowsCountsByKindHeldAndSkipped(t *testing.T) {
	markup := renderNode(t, ChatRetentionPreviewSettings(issue3778Loaded()))
	for _, want := range []string{
		"Preview what retention would remove", "it would remove 4,250 records out of 5,200 checked",
		`<th scope="col"`, "Kind of record", "Would remove", "Messages", "4,212", "Files", "38", "MYSTERY",
		"3 records are under a legal hold and would be skipped", "2 records would be skipped because every copy",
		"nothing has been or will be deleted", `id="chatretpreview-run"`, `aria-live="polite"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("preview lacks %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "<script") {
		t.Fatal("markup contains a script")
	}
}

func TestIssue3778_PreviewStates(t *testing.T) {
	budget := issue3778Loaded()
	budget.Data.UsageUnknown, budget.Data.Mode = true, "SIZE_BUDGET"
	none := ChatRetentionPreviewModel{Locale: "en-US", Loaded: true}
	nothing := ChatRetentionPreviewModel{Locale: "en-US", Loaded: true, Data: ChatRetentionPreviewData{Configured: true, Scanned: 12}}
	cases := map[string]struct {
		m    ChatRetentionPreviewModel
		want string
	}{
		"idle":        {ChatRetentionPreviewModel{Locale: "en-US"}, "Preview"},
		"loading":     {ChatRetentionPreviewModel{Locale: "en-US", Loading: true}, "Checking what would be removed"},
		"failed":      {ChatRetentionPreviewModel{Locale: "en-US", Failed: true}, "Nothing was changed. Try again."},
		"signed out":  {ChatRetentionPreviewModel{Locale: "en-US", Failed: true, SignedOut: true}, "You were signed out"},
		"denied":      {ChatRetentionPreviewModel{Locale: "en-US", Loaded: true, Denied: true}, "Only a workspace administrator can preview retention."},
		"unavailable": {ChatRetentionPreviewModel{Locale: "en-US", Loaded: true, Unavailable: true}, "not set up on this server"},
		"no policy":   {none, "No policy is saved, so nothing would be removed."},
		"nothing due": {nothing, "Nothing is due for removal under the saved policy. 12 records were checked."},
		"budget":      {budget, "A storage limit cannot be previewed yet"},
	}
	for name, tc := range cases {
		if markup := renderNode(t, ChatRetentionPreviewSettings(tc.m)); !strings.Contains(markup, tc.want) {
			t.Errorf("%s lacks %q: %s", name, tc.want, markup)
		}
	}
	// A budget preview names no counts it cannot stand behind.
	if markup := renderNode(t, ChatRetentionPreviewSettings(budget)); strings.Contains(markup, "4,212") || strings.Contains(markup, "<table") {
		t.Errorf("a storage limit preview promised removals: %s", markup)
	}
	// A refused reader sees no table and no counts.
	denied := issue3778Loaded()
	denied.Denied = true
	if markup := renderNode(t, ChatRetentionPreviewSettings(denied)); strings.Contains(markup, "4,212") || strings.Contains(markup, "<table") {
		t.Errorf("a refused reader saw counts: %s", markup)
	}
	// While the read runs the button stays focusable (aria-disabled, not disabled).
	loading := renderNode(t, ChatRetentionPreviewSettings(ChatRetentionPreviewModel{Locale: "en-US", Loading: true}))
	if !strings.Contains(loading, `aria-disabled="true"`) {
		t.Errorf("the running preview button is not marked busy: %s", loading)
	}
	// With no handler wired the button is disabled, never a button that does nothing.
	if markup := renderNode(t, ChatRetentionPreviewSettings(ChatRetentionPreviewModel{Locale: "en-US"})); !strings.Contains(markup, "disabled") {
		t.Errorf("an unwired Preview button is enabled: %s", markup)
	}
}

func TestIssue3778_PreviewAnswersMapToStates(t *testing.T) {
	data := ChatRetentionPreviewData{Configured: true, Total: 1}
	if got := chatRetentionPreviewAnswered(ChatRetentionPreviewModel{Loading: true}, data, 200, nil); !got.Loaded || got.Loading || got.Data.Total != 1 {
		t.Fatalf("ok: %+v", got)
	}
	if got := chatRetentionPreviewAnswered(ChatRetentionPreviewModel{Loading: true}, ChatRetentionPreviewData{}, 403, errors.New("x")); !got.Denied {
		t.Fatalf("403: %+v", got)
	}
	if got := chatRetentionPreviewAnswered(ChatRetentionPreviewModel{Loading: true}, ChatRetentionPreviewData{}, 503, errors.New("x")); !got.Unavailable {
		t.Fatalf("503: %+v", got)
	}
	if got := chatRetentionPreviewAnswered(ChatRetentionPreviewModel{Loading: true}, ChatRetentionPreviewData{}, 401, errors.New("x")); !got.Failed || !got.SignedOut {
		t.Fatalf("401: %+v", got)
	}
}

func TestIssue3778_PreviewCopyIsTranslatedAndRightToLeft(t *testing.T) {
	for key, values := range chatRetentionPreviewCopy {
		for i, v := range values {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s has no text for column %d", key, i)
			}
		}
	}
	de := issue3778Loaded()
	de.Locale = "de-DE"
	if markup := renderNode(t, ChatRetentionPreviewSettings(de)); !strings.Contains(markup, "Nachrichten") {
		t.Errorf("German preview: %s", markup)
	}
	ar := issue3778Loaded()
	ar.Locale = "ar"
	markup := renderNode(t, ChatRetentionPreviewSettings(ar))
	if !strings.Contains(markup, `dir="rtl"`) || !strings.Contains(markup, "الرسائل") {
		t.Errorf("Arabic preview: %s", markup)
	}
}
