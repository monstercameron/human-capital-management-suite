package chatui

import (
	"strings"
	"testing"
)

func issue3790Model(locale string) (Model, Message) {
	m, msg := chatbug064Model()
	m.Locale = locale
	m.MessageWork = MessageWorkState{Enabled: true, Open: func(string) {}, Links: map[string][]MessageWorkLink{
		"m1": {{PostID: "m1", ProjectID: "ops", TaskID: "t-1", Title: "Swap Dana's laptop", Status: "To do", OwnerID: "walt", DueDate: "2026-12-04"}},
	}}
	return m, msg
}

// Issue 3790: the message menu offers Create task where the server can make
// tasks, and nowhere else, so there is never a button that cannot work.
func TestIssue3790_MenuOffersCreateTaskOnlyWhereItWorks(t *testing.T) {
	m, msg := issue3790Model("en-US")
	menu := renderNode(t, spanOf(chatux022MenuItems(m, msg)))
	if !strings.Contains(menu, `data-action="work-open"`) || !strings.Contains(menu, "Create task") {
		t.Fatalf("menu has no Create task row: %s", menu)
	}
	for name, mutate := range map[string]func(*Model){
		"server cannot make tasks": func(m *Model) { m.MessageWork.Enabled = false },
		"no form to open":          func(m *Model) { m.MessageWork.Open = nil },
		"signed out":               func(m *Model) { m.CurrentUser = "" },
	} {
		off, offMsg := issue3790Model("en-US")
		mutate(&off)
		if strings.Contains(renderNode(t, spanOf(chatux022MenuItems(off, offMsg))), "work-open") {
			t.Errorf("%s: Create task is offered", name)
		}
	}
}

func TestIssue3790_ChoosingCreateTaskOpensTheFormAndTheMenuCloses(t *testing.T) {
	var calls []string
	m, _ := issue3790Model("en-US")
	m.MessageWork.Open = func(id string) { calls = append(calls, "work:"+id) }
	m.Callbacks.OpenMenu = func(id string) { calls = append(calls, "menu:"+id) }
	m.actWith("work-open", "m1", "")
	if strings.Join(calls, ",") != "work:m1,menu:" {
		t.Fatalf("calls=%v", calls)
	}
}

// The message shows what it became: kind, title, owner, status and due date,
// as a link to the task, named for a screen reader as one sentence.
func TestIssue3790_MessageShowsTheTaskItBecame(t *testing.T) {
	m, msg := issue3790Model("en-US")
	chips := renderNode(t, spanOf(chatworkChips(m, msg)))
	for _, want := range []string{"Task created", "Swap Dana&#39;s laptop", "Owner: You", "Status: To do", "Due: 2026-12-04", `href="/workspace/app/project?project=ops&amp;task=t-1"`, `role="group"`, "Work from this message"} {
		if !strings.Contains(chips, want) {
			t.Errorf("chip lacks %q: %s", want, chips)
		}
	}
	other := Message{ID: "m2"}
	if got := chatworkChips(m, other); len(got) != 0 {
		t.Errorf("a message with no work shows chips: %d", len(got))
	}
}

// An owner is named only when the viewer can be told who it is; an id is never
// shown as a name.
func TestIssue3790_ChipNamesOwnersWithoutLeakingIDs(t *testing.T) {
	m, msg := issue3790Model("en-US")
	m.MessageWork.Links["m1"][0].OwnerID = "u-4411"
	chips := renderNode(t, spanOf(chatworkChips(m, msg)))
	if strings.Contains(chips, "u-4411") || strings.Contains(chips, "Owner:") {
		t.Fatalf("unknown owner id shown: %s", chips)
	}
	m.MessageWork.Links["m1"][0].OwnerID = "ben"
	if chips := renderNode(t, spanOf(chatworkChips(m, msg))); !strings.Contains(chips, "Owner: Ben") {
		t.Fatalf("message author not named: %s", chips)
	}
}

// Every string the feature shows has an en-US, de-DE and ar entry, and the
// languages differ.
func TestIssue3790_CopyCoversEveryLanguage(t *testing.T) {
	for key, english := range chatworkCopy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			got := chatworkCopy[locale][key]
			if strings.TrimSpace(got) == "" {
				t.Errorf("%s has no %s copy", key, locale)
			}
			if locale == "ar" && got == english {
				t.Errorf("%s is not translated to ar", key)
			}
		}
	}
	for locale := range chatworkCopy {
		if len(chatworkCopy[locale]) != len(chatworkCopy["en-US"]) {
			t.Errorf("%s has %d entries, en-US has %d", locale, len(chatworkCopy[locale]), len(chatworkCopy["en-US"]))
		}
	}
	if MessageWorkText("de", cwMenu) != "Aufgabe erstellen" || MessageWorkText("ar-SA", cwMenu) != "إنشاء مهمة" {
		t.Fatal("locale prefixes are not honoured")
	}
	if MessageWorkErrorText("en-US", "permission_denied") == MessageWorkErrorText("en-US", "unavailable") {
		t.Fatal("refusals read the same")
	}
}

func issue3790Dialog() MessageWorkDialogView {
	return MessageWorkDialogView{Locale: "en-US", PostID: "m1", Title: "Swap Dana's laptop", State: "ready",
		Projects: []MessageWorkProject{{ID: "ops", Name: "IT operations"}},
		Owners:   []MessageWorkOwner{{ID: "walt", Label: "Me"}, {ID: "ben", Label: "Ben (wrote the message)"}}, OwnerID: "walt"}
}

// The form is labelled, keyboard operable, says what is and is not saved, and
// keeps the person's text on a refusal.
func TestIssue3790_DialogIsLabelledAndKeepsWhatWasTyped(t *testing.T) {
	v := issue3790Dialog()
	v.Error = "permission_denied"
	html := renderNode(t, RenderMessageWorkDialog(v))
	for _, want := range []string{`role="dialog"`, `aria-modal="true"`, `aria-labelledby="chatwork-title"`, `for="chatwork-field-title"`, `value="Swap Dana&#39;s laptop"`, "IT operations", "Ben (wrote the message)", `type="date"`, `role="alert"`, "You can&#39;t create tasks in that project.", "not copied into the project", `data-chatwork-close`} {
		if !strings.Contains(html, want) {
			t.Errorf("dialog lacks %q: %s", want, html)
		}
	}
	v.Busy = true
	busy := renderNode(t, RenderMessageWorkDialog(v))
	if !strings.Contains(busy, `aria-busy="true"`) || !strings.Contains(busy, "Creating the task") || strings.Count(busy, "disabled") < 4 {
		t.Errorf("busy form can be changed or resubmitted: %s", busy)
	}
}

func TestIssue3790_DialogStatesAndRightToLeft(t *testing.T) {
	loading := renderNode(t, RenderMessageWorkDialog(MessageWorkDialogView{Locale: "de-DE", State: "loading"}))
	if !strings.Contains(loading, "Projekte werden geladen") || !strings.Contains(loading, `role="status"`) {
		t.Errorf("loading: %s", loading)
	}
	empty := renderNode(t, RenderMessageWorkDialog(MessageWorkDialogView{Locale: "en-US", State: "ready"}))
	if !strings.Contains(empty, "can&#39;t create tasks in any project yet") || !strings.Contains(empty, "data-chatwork-close") || strings.Contains(empty, "<form") {
		t.Errorf("no projects: %s", empty)
	}
	failed := renderNode(t, RenderMessageWorkDialog(MessageWorkDialogView{Locale: "en-US", State: "error", Error: "unavailable"}))
	if !strings.Contains(failed, `role="alert"`) || !strings.Contains(failed, "Try again") {
		t.Errorf("error: %s", failed)
	}
	rtl := issue3790Dialog()
	rtl.Locale = "ar"
	rtl.Existing = []MessageWorkLink{{Title: "مهمة", Status: "للتنفيذ"}}
	arabic := renderNode(t, RenderMessageWorkDialog(rtl))
	if !strings.Contains(arabic, `dir="rtl"`) || !strings.Contains(arabic, "إنشاء مهمة من هذه الرسالة") || !strings.Contains(arabic, "تم إنشاؤه سابقًا من هذه الرسالة") || !strings.Contains(arabic, "مهمة (للتنفيذ)") {
		t.Errorf("ar: %s", arabic)
	}
}
