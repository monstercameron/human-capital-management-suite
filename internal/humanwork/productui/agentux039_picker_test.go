package productui

import (
	"strings"
	"testing"
)

// The picker in the Agent setup editor read as broken while it was idle: a
// "Try again" button showed beside "Search by title to add a document." and
// the field looked disabled. Nothing had failed to load.
func TestTodo_AGENTUX_039(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	idle := personaAdminRender(t, AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{ID: "picker", Available: true, State: AgentDocumentPickerIdle}))
	// Idle: an enabled field, the idle sentence once, and the retry button
	// carrying the hidden attribute.
	if !strings.Contains(idle, `role="combobox"`) || strings.Contains(idle, " disabled") {
		t.Fatalf("the idle picker's search field is missing or disabled: %s", idle)
	}
	if strings.Count(idle, ">Search by title to add a document.<") != 1 {
		t.Fatalf("the idle status is not rendered exactly once: %s", idle)
	}
	if !strings.Contains(idle, `<button class="button secondary compact" data-agentdoc-retry="true" hidden type="button">Try again</button>`) {
		t.Fatalf("the retry button is not hidden while nothing has failed: %s", idle)
	}
	// The counter carries its noun and shows only when there is something to count.
	if !strings.Contains(idle, `data-agentdoc-count="true" hidden>0 of 8 documents<`) {
		t.Fatalf("the reference counter is unlabelled or shown at zero: %s", idle)
	}

	// A picker drawn in the failed state says so as an alert and shows the retry
	// button, with one status line.
	failed := personaAdminRender(t, AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{ID: "picker", Available: true, State: AgentDocumentPickerFailed}))
	if !strings.Contains(failed, `role="alert"`) || strings.Count(failed, "data-agentdoc-status") != 1 || strings.Contains(failed, " disabled") {
		t.Fatalf("a failed picker does not report one alert: %s", failed)
	}
	if strings.Contains(failed, `data-agentdoc-retry="true" hidden`) {
		t.Fatalf("a failed search offers no retry: %s", failed)
	}

	// The picker says what went wrong, in every language, without codes.
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(language)
		markup := personaAdminRender(t, AgentDocumentReferencePicker(resolved, AgentDocumentReferencePickerModel{ID: "picker", Available: true}))
		seen := map[string]bool{}
		for _, key := range []string{"failed", "failed_timeout", "failed_denied", "failed_unavailable"} {
			text := agentDocumentPickerText(resolved, key)
			if text == "" || text == key || seen[text] {
				t.Fatalf("%s: failure sentence %q is missing or repeats another", language, key)
			}
			seen[text] = true
			for _, code := range []string{"NOT_FOUND", "PERMISSION_DENIED", "DEADLINE", "document.unavailable", "rpc"} {
				if strings.Contains(text, code) {
					t.Fatalf("%s: failure sentence %q shows an internal code", language, key)
				}
			}
		}
		for _, attribute := range []string{`data-agentdoc-failed="`, `data-agentdoc-failed-timeout="`, `data-agentdoc-failed-denied="`, `data-agentdoc-failed-unavailable="`} {
			if !strings.Contains(markup, attribute) {
				t.Fatalf("%s: the picker does not carry %s for the browser", language, attribute)
			}
		}
	}

	// A document the editor cannot read stays listed by its safe label and can
	// be removed; the readable ones keep their link and version choice.
	references := []PersonaAdminDocumentReference{
		{DocumentID: "doc-policy", Title: "Paid time off policy", Label: "Paid time off policy", VersionMode: "PINNED", PinnedVersion: 1, Readable: true},
		{DocumentID: "doc-gone", Label: "Restricted handbook", VersionMode: "PINNED", PinnedVersion: 2, Readable: false},
	}
	mixed := personaAdminRender(t, AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{ID: "picker", Available: true, References: references}))
	for _, want := range []string{`href="/workspace/app/docs?document=doc-policy"`, "Restricted handbook", "Cannot be read by you", "Remove Restricted handbook", `role="combobox"`, "2 of 8 documents"} {
		if !strings.Contains(mixed, want) {
			t.Fatalf("a picker holding one unreadable document is missing %q: %s", want, mixed)
		}
	}
	if strings.Contains(mixed, " disabled") || strings.Contains(mixed, `href="/workspace/app/docs?document=doc-gone"`) {
		t.Fatalf("one unreadable document disabled the picker or was linked: %s", mixed)
	}
}

// "Edit" opens the editor in the card, in page flow, and the picker's rules are
// in the stylesheet the browser accepts.
func TestTodo_AGENTUX_039_Browser(t *testing.T) {
	sheet := Stylesheet()
	for _, want := range []string{
		agentUX039PickerStyles,
		`.agent-page-frame [hidden]{display:none!important}`,
		`.persona-admin-page .agentdoc-picker-combobox input,.persona-admin-page .agentdoc-picker-row select{background:var(--hcm-color-surface)}`,
		`.persona-admin-page .persona-admin-version-editor[hidden]{display:none}`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("the product stylesheet is missing %.90q", want)
		}
	}
	// The surface fill must come after the canvas fill it corrects.
	canvas := strings.Index(sheet, `.persona-admin-page .agentdoc-picker-combobox input,.persona-admin-page .agentdoc-picker-row select{box-sizing:border-box`)
	surface := strings.Index(sheet, `.persona-admin-page .agentdoc-picker-combobox input,.persona-admin-page .agentdoc-picker-row select{background:var(--hcm-color-surface)}`)
	if canvas < 0 || surface < canvas {
		t.Fatal("the picker field's surface fill does not override the canvas fill")
	}
	// The editor is a sibling of the card heading in normal flow: no absolute or
	// fixed positioning rule applies to it.
	for _, rule := range strings.Split(sheet, "}") {
		if strings.Contains(rule, ".persona-admin-version-editor{") && (strings.Contains(rule, "position:absolute") || strings.Contains(rule, "position:fixed")) {
			t.Fatalf("the version editor is positioned out of page flow: %s", rule)
		}
	}
	persona := agentUXSetup2Persona()
	card := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, agentUXSetup2Snapshot(persona)))
	for _, want := range []string{`class="persona-admin-version-editor"`, `data-persona-editor-toggle="persona-admin-version-policy-helper"`, `aria-controls="persona-admin-version-policy-helper"`, `data-agentdoc-picker=`, `>Cancel<`} {
		if !strings.Contains(card, want) {
			t.Fatalf("the in-place editor is missing %q", want)
		}
	}
	if strings.Contains(card, ` style="`) {
		t.Fatal("the card carries an inline style")
	}
}
