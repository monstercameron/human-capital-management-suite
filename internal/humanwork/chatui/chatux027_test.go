package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
)

// chatux027Manage draws Manage channel with the given rows open, the way the
// panel does after the person pressed them in that order.
func chatux027Manage(t *testing.T, m Model, open ...string) string {
	t.Helper()
	var local localUI
	local.setDetailGroup(chatux005GroupManage, true)
	for _, id := range open {
		local.toggleSection(chatux027ManageScope(m), id, false)
	}
	built := channelWidgetPartsFor(m, handlers{local: local})
	return renderNode(t, chatux005Manage(m, handlers{local: local}, m.selected(), &built))
}

// manageBody is the markup inside the open Manage channel group.
func chatux027Body(t *testing.T, markup string) string {
	t.Helper()
	at := strings.Index(markup, `id="chat-details-group-manage"`)
	if at < 0 {
		t.Fatalf("no Manage channel block: %s", markup)
	}
	return markup[at:]
}

// Every setting under Manage channel is one disclosure row: the same classes,
// a button with its state and the block it controls; one row open at a time,
// state kept in the local state; help only inside an open row; an error inside
// the row it belongs to, in full, with Try again.
func TestTodo_CHATUX_027(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chatbug048Model()
		m.Locale = locale
		closed := chatux027Body(t, chatux027Manage(t, m))
		rows := regexp.MustCompile(`<button[^>]*class="manage-sec-row"[^>]*>`).FindAllString(closed, -1)
		// Status, Role labels, Project, two filters, Integrations: Translation loads from the server.
		if len(rows) < 6 {
			t.Fatalf("%s: %d section rows, want at least 6: %s", locale, len(rows), closed)
		}
		for _, row := range rows {
			if !strings.Contains(row, `aria-expanded="false"`) || !strings.Contains(row, `aria-controls="chat-manage-`) || !strings.Contains(row, `type="button"`) {
				t.Errorf("%s: a row is not a button with its state: %s", locale, row)
			}
		}
		for _, id := range regexp.MustCompile(`aria-controls="([^"]+)"`).FindAllStringSubmatch(closed, -1) {
			if !strings.Contains(closed, `id="`+id[1]+`"`) {
				t.Errorf("%s: aria-controls names %q and nothing has that id", locale, id[1])
			}
		}
		if strings.Contains(closed, `aria-expanded="true"`) {
			t.Errorf("%s: a row is open before anyone pressed it", locale)
		}
		// Help sits inside an open row only.
		if chatux027ShownHelp(closed) != 0 {
			t.Errorf("%s: help text hangs under a closed row", locale)
		}

		// Two rows opened in turn: only the second stays open.
		turn := chatux027Body(t, chatux027Manage(t, m, "status", "roles"))
		if got := strings.Count(turn, `aria-expanded="true"`); got != 1 {
			t.Fatalf("%s: %d rows open after opening two in turn, want 1", locale, got)
		}
		if !regexp.MustCompile(`(?s)data-id="roles"[^>]*aria-expanded="true"|aria-expanded="true"[^>]*data-id="roles"`).MatchString(turn) {
			t.Errorf("%s: the row that is open is not the second one", locale)
		}
		if chatux027ShownHelp(turn) == 0 || !chatux027BodyHidden(t, turn, "status") || chatux027BodyHidden(t, turn, "roles") {
			t.Errorf("%s: the open row's content or the closed row's form is wrong", locale)
		}
		// The same row pressed again closes it.
		var local localUI
		scope := chatux027ManageScope(m)
		local.toggleSection(scope, "roles", false)
		local.toggleSection(scope, "roles", true)
		if chatux027IsOpen(local, scope, "roles", false) {
			t.Errorf("%s: pressing an open row does not close it", locale)
		}
	}
}

// A row opens first when it asks to and nothing in its group was chosen; the
// person's own choice wins from then on, and another conversation starts over.
func TestTodo_CHATUX_027_OpenState(t *testing.T) {
	var local localUI
	if chatux027IsOpen(local, "g", "a", false) || !chatux027IsOpen(local, "g", "a", true) {
		t.Error("a row that asks to open first does not, or a plain row does")
	}
	local.toggleSection("g", "b", false)
	if chatux027IsOpen(local, "g", "a", true) || !chatux027IsOpen(local, "g", "b", false) {
		t.Error("choosing a row did not take over from the one that asked to open first")
	}
	local.toggleSection("g", "a", false)
	if chatux027IsOpen(local, "g", "b", false) || !chatux027IsOpen(local, "g", "a", false) {
		t.Error("opening a row did not close the previous one")
	}
	local.toggleSection("other", "b", false)
	if !chatux027IsOpen(local, "g", "a", false) {
		t.Error("a row of another group closed this group's row")
	}
	m := chatbug048Model()
	if chatux027ManageScope(m) == chatux027ManageScope(Model{SelectedID: "elsewhere"}) {
		t.Error("two conversations share their open rows")
	}
}

// A translation setting that could not load or save says so inside its own row,
// in full, with Try again, and not under another row.
func TestTodo_CHATUX_027_ErrorInsideItsRow(t *testing.T) {
	m := chatbug048Model()
	wrap := chatux027Wrap(m, handlers{}, chatux027ManageScope(m), "language", "chatlangadmin-entry")
	failed := renderNode(t, translationChannelRow(TranslationAdminModel{Locale: "en-US", Failed: true}, nil, wrap))
	for _, want := range []string{`data-manage-section="language"`, `data-failed="true"`, `aria-expanded="true"`, "Translation settings could not load or save. Try again.", "Try again", `role="alert"`, "Needs attention"} {
		if !strings.Contains(failed, want) {
			t.Errorf("the failed row misses %q: %s", want, failed)
		}
	}
	// Beside the rest of Manage channel the error is in the language row and in no other.
	manage := chatux027Body(t, chatux027Manage(t, m))
	if strings.Contains(manage, "could not load or save") {
		t.Errorf("an error shows before the setting was asked: %s", manage)
	}
	// A save that failed keeps the choice in the form and puts the error under it.
	saved := TranslationAdminModel{Locale: "en-US", Loaded: true, Failed: true}
	saved.Data.Workspace.Enabled, saved.Data.CanManageChannel = true, true
	saved.Data.Channel = &chatlang.Channel{}
	body := renderNode(t, translationChannelRow(saved, nil, wrap))
	if strings.Count(body, "could not load or save") != 1 || !strings.Contains(body, "chatlangadmin-channel-form") || !strings.Contains(body, "Try again") {
		t.Errorf("a failed save: %s", body)
	}
	if strings.Index(body, "chatlangadmin-channel-form") > strings.Index(body, "could not load or save") {
		t.Errorf("the error is before the form it belongs to: %s", body)
	}
}

// The rows share one style and open is marked by one thing: the chevron turned
// over. No rule tints a closed row or an open one, hover uses the standard
// token, and the panel holds no scroll area of its own.
func TestTodo_CHATUX_027_Styles(t *testing.T) {
	for _, forbidden := range []string{"overflow:auto", "overflow-y:auto", "overflow:scroll", "max-block-size", "max-height", "padding-left", "padding-right", "margin-left", "margin-right", "border-left", "border-right"} {
		if strings.Contains(ChatUX027Styles, forbidden) {
			t.Errorf("the section styles use %q", forbidden)
		}
	}
	if regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(`).MatchString(ChatUX027Styles) {
		t.Error("the section styles hold a raw colour")
	}
	if regexp.MustCompile(`manage-sec-row\[aria-expanded=true\][^{]*\{[^}]*(background:(?:var|#)|font-weight|color:)`).MatchString(ChatUX027Styles) {
		t.Error("an open row is marked by more than the chevron")
	}
	if !strings.Contains(ChatUX027Styles, "button.manage-sec-row:hover{background:var(--soft)}") || !strings.Contains(ChatUX027Styles, "min-block-size:36px") {
		t.Error("the row has no standard hover or no 36 px height")
	}
	if !strings.Contains(ChatLane3Styles, ChatUX027Styles) || !strings.Contains(Stylesheet, ChatLane3Styles) {
		t.Error("the section styles are not in the stylesheet")
	}
}

// CHATUX-021: the purpose editor, the status form and the person pane take the
// panel's one shape: the label over the field, the hint under it, the button
// row with the main button first and Cancel second; the person pane's header is
// the details header with Back and Close.
func TestTodo_CHATUX_021_OneShape(t *testing.T) {
	m := chatux021Model()
	purpose := chatux021Details(t, m, handlers{local: localUI{purposeEditing: true, purposeDraft: "Policy questions"}})
	form := purpose[strings.Index(purpose, `class="details-purpose-form manage-sec-form"`):]
	order := []string{`for="channel-team-purpose"`, `id="channel-team-purpose"`, `id="details-purpose-hint"`, `class="details-purpose-actions manage-sec-actions"`, ">Save<", ">Cancel<"}
	last := -1
	for _, marker := range order {
		at := strings.Index(form, marker)
		if at < 0 || at < last {
			t.Fatalf("the purpose form: %q is missing or out of order in %s", marker, form)
		}
		last = at
	}
	// The status form: Confirm, then Cancel, in the same row class, and Cancel closes the row.
	status := chatux027Body(t, chatux027Manage(t, m, "status"))
	actions := status[strings.Index(status, `class="manage-sec-actions"`):]
	if strings.Index(actions, ">Confirm status change<") < 0 || strings.Index(actions, ">Cancel<") < strings.Index(actions, ">Confirm status change<") || !strings.Contains(actions, `data-action="manage-section"`) {
		t.Errorf("the status form's button row: %s", actions)
	}
	if !strings.Contains(status, `for="chatstate-choice"`) || strings.Index(status, `for="chatstate-choice"`) > strings.Index(status, `id="chatstate-choice"`) {
		t.Errorf("the status field is not label then field: %s", status)
	}
	// The person pane's header is the details header, plus Back.
	mp := m
	mp.ShowDetails = true
	head := renderNode(t, personPaneHeading(mp))
	if !strings.Contains(head, `class="side-heading person-pane-head"`) || !strings.Contains(head, `data-action="person-back"`) || !strings.Contains(head, `data-action="close-person"`) {
		t.Errorf("the person pane header: %s", head)
	}
	if strings.Index(head, `data-action="person-back"`) > strings.Index(head, "person-pane-heading") || strings.Index(head, "person-pane-heading") > strings.Index(head, `data-action="close-person"`) {
		t.Errorf("Back, title, Close are out of order: %s", head)
	}
	panel := chatux021Details(t, m, handlers{})
	if !strings.Contains(panel, `class="side-heading"`) || !strings.Contains(panel, `data-action="close-details"`) {
		t.Errorf("the details header: %s", panel)
	}
}

// chatux027BodyHidden reports whether the block of a row is hidden.
func chatux027BodyHidden(t *testing.T, markup, id string) bool {
	t.Helper()
	at := strings.Index(markup, `id="chat-manage-`+id+`"`)
	if at < 0 {
		t.Fatalf("no block for %s", id)
	}
	start := strings.LastIndex(markup[:at], "<div")
	return strings.Contains(markup[start:at+strings.Index(markup[at:], ">")], " hidden")
}

// chatux027ShownHelp counts the help sentences that sit in a block that is not hidden.
func chatux027ShownHelp(markup string) int {
	shown := 0
	for from := 0; ; {
		at := strings.Index(markup[from:], "manage-sec-help")
		if at < 0 {
			return shown
		}
		at += from
		from = at + 1
		start := strings.LastIndex(markup[:at], `aria-labelledby="chat-manage-`)
		start = strings.LastIndex(markup[:start], "<div")
		if !strings.Contains(markup[start:at], " hidden") {
			shown++
		}
	}
}
