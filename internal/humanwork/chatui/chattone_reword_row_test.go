package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type chattoneRewordProbeProps struct {
	Locale string
	View   ChattoneRewordState
	Status string
	Busy   bool
}

// chattoneRewordProbe draws the row with real handlers, the way the component
// does, so the buttons are enabled in the markup.
func chattoneRewordProbe(p chattoneRewordProbeProps) ui.Node {
	var h chattoneRewordHandlers
	for i := range h.PersonGeneral {
		h.PersonGeneral[i] = ui.UseEvent(func() {})
	}
	for i := range h.PersonChannel {
		h.PersonChannel[i] = ui.UseEvent(func() {})
	}
	for i := range h.WorkspaceMode {
		h.WorkspaceMode[i] = ui.UseEvent(func() {})
	}
	for i := range h.WorkspaceView {
		h.WorkspaceView[i] = ui.UseEvent(func() {})
	}
	for i := range h.ChannelMode {
		h.ChannelMode[i] = ui.UseEvent(func() {})
	}
	for i := range h.ChannelView {
		h.ChannelView[i] = ui.UseEvent(func() {})
	}
	h.ChannelClear = ui.UseEvent(func() {})
	h.Retry = ui.UseEvent(func() {})
	return RenderChattoneRewordRow(p.Locale, p.View, p.Status, p.Busy, h)
}

func chattoneRewordMarkup(t *testing.T, p chattoneRewordProbeProps) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(chattoneRewordProbe, p))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

var chattoneRewordMember = ChattoneRewordState{Available: true, Channel: ChattoneRewordSetting{Mode: "offered", MembersMayViewOriginal: true}, Workspace: ChattoneRewordSetting{Mode: "offered", MembersMayViewOriginal: true}, ChoiceAllowed: true, General: "reworded"}

// TestTodo_CHATTONE_003_Browser: what each kind of person sees. A member where
// rewording is off sees nothing; where originals are hidden they are told so;
// where they may choose they choose, with their current choice pressed; an
// administrator sees both scopes with a sentence for each current setting.
func TestTodo_CHATTONE_003_Browser(t *testing.T) {
	if got := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: ChattoneRewordState{Available: true}}); strings.Contains(got, "Heated messages") {
		t.Fatalf("a member where rewording is off sees the row: %s", got)
	}
	if got := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: ChattoneRewordState{}}); strings.Contains(got, "Heated messages") {
		t.Fatalf("an unavailable service draws the row: %s", got)
	}

	forced := ChattoneRewordState{Available: true, Channel: ChattoneRewordSetting{Mode: "on", MembersMayViewOriginal: false}}
	got := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: forced})
	if !strings.Contains(got, "heated messages are shown reworded") || strings.Contains(got, "chattone-reword-choice") {
		t.Fatalf("a member where originals are hidden: %s", got)
	}

	got = chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: chattoneRewordMember})
	for _, want := range []string{"Heated messages", "Choose how you read them. The original is always kept.", "In every conversation", "In this conversation", "Reworded", "As written", "Use my choice for every conversation"} {
		if !strings.Contains(got, want) {
			t.Errorf("member view lacks %q: %s", want, got)
		}
	}
	// Their general choice is the pressed one in the first group only.
	general := got[strings.Index(got, `id="chattone-reword-general"`):strings.Index(got, `id="chattone-reword-channel"`)]
	if !strings.Contains(general, `aria-pressed="true"`) || strings.Count(general, `aria-pressed="true"`) != 1 {
		t.Errorf("the general choice is not shown pressed once: %s", general)
	}
	if strings.Contains(got, "Administrator settings") {
		t.Errorf("a member sees administration: %s", got)
	}

	admin := chattoneRewordMember
	admin.CanAdmin = true
	admin.Channel = ChattoneRewordSetting{Mode: "on", MembersMayViewOriginal: false, Override: true}
	admin.ChoiceAllowed = false
	got = chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: admin})
	for _, want := range []string{"Administrator settings", "Reword heated messages", "Whole workspace", "This channel only", "Offered to writers", "Members may view messages as written",
		"A reworded version is made for heated messages; each member chooses which to read.", "Heated messages are shown reworded by default;", "Only the writer can read the original", "This channel has its own choice.", "Use the workspace&#39;s choice here"} {
		if !strings.Contains(got, want) && !strings.Contains(got, strings.ReplaceAll(want, "&#39;", "'")) {
			t.Errorf("administrator view lacks %q: %s", want, got)
		}
	}
	// With no override the way back is not offered.
	admin.Channel.Override = false
	if got = chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: admin}); strings.Contains(got, "This channel has its own choice.") {
		t.Errorf("a channel without an override offers the way back: %s", got)
	}
}

// TestTodo_CHATTONE_003_Accessibility: every control is a button with a name
// and a pressed state, every group is named, the outcome is announced, a busy
// row cannot be pressed twice, and the row reads right to left in Arabic.
func TestTodo_CHATTONE_003_Accessibility(t *testing.T) {
	admin := chattoneRewordMember
	admin.CanAdmin = true
	got := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: admin, Status: "saved"})
	if strings.Count(got, `type="button"`) != strings.Count(got, "<button") {
		t.Errorf("a control is not a plain button: %s", got)
	}
	if strings.Count(got, `role="group"`) != 6 {
		t.Errorf("groups: %s", got)
	}
	for _, id := range []string{"chattone-reword-general", "chattone-reword-channel", "workspace-mode", "workspace-view", "channel-mode", "channel-view"} {
		if !strings.Contains(got, id) {
			t.Errorf("group %s missing", id)
		}
	}
	if !strings.Contains(got, `role="status"`) || !strings.Contains(got, "Saved.") {
		t.Errorf("the outcome is not announced: %s", got)
	}
	failed := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: admin, Status: "failed"})
	if !strings.Contains(failed, `role="alert"`) || !strings.Contains(failed, "This could not be saved. Try again.") || !strings.Contains(failed, "Try again") {
		t.Errorf("a failure is not announced with a way to retry: %s", failed)
	}
	busy := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "en-US", View: admin, Busy: true})
	if strings.Count(busy, " disabled") < 8 {
		t.Errorf("a busy row can be pressed again: %s", busy)
	}
	arabic := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "ar", View: admin})
	if !strings.Contains(arabic, `dir="rtl"`) || !strings.Contains(arabic, "الرسائل المتوترة") || strings.Contains(arabic, "Heated messages") {
		t.Errorf("Arabic: %s", arabic)
	}
	german := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: "de-DE", View: admin})
	if !strings.Contains(german, "Aufgeheizte Nachrichten") || strings.Contains(german, "Heated messages") {
		t.Errorf("German: %s", german)
	}
}

// TestTodo_CHATTONE_003_Copy: every key has all three languages, and no
// rendered text is a raw key.
func TestTodo_CHATTONE_003_Copy(t *testing.T) {
	for key, row := range chattoneRewordCopy {
		for i, text := range row {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s has no text in language %d", key, i)
			}
		}
		if row[0] == row[1] && key != "no_help" && len(key) > 3 && row[0] == row[2] {
			t.Errorf("%s is not translated", key)
		}
	}
	if ChattoneRewordText("en-US", "no-such-key") != "" {
		t.Error("an unknown key has text")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		admin := chattoneRewordMember
		admin.CanAdmin = true
		got := chattoneRewordMarkup(t, chattoneRewordProbeProps{Locale: locale, View: admin})
		for key := range chattoneRewordCopy {
			if strings.Contains(got, ">"+key+"<") {
				t.Errorf("%s: raw key %q on the page", locale, key)
			}
		}
	}
}
