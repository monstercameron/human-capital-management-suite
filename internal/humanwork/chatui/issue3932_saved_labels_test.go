package chatui

import (
	"reflect"
	"strings"
	"testing"
)

// Issue 3932: private labels on saved messages.

func issue3932Rows() []SavedMessageRow {
	rows := chatsave002Rows()
	rows[0].Labels = []string{"Open enrollment", "Hiring"}
	rows[1].Labels = []string{"hiring"}
	return rows
}

func issue3932View(locale string) SavedMessagesView {
	view := chatsave002View("all", issue3932Rows())
	view.Locale = locale
	labelRows := [][]string{}
	for _, row := range view.Rows {
		labelRows = append(labelRows, row.Labels)
	}
	view.Labels.Options, view.Labels.UnlabelledCount = SavedLabelOptions(labelRows)
	return view
}

func TestIssue3932_OptionsCountEachLabelOncePerMessage(t *testing.T) {
	options, none := SavedLabelOptions([][]string{{"Hiring", "hiring"}, {"hiring", "Benefits"}, nil, {}})
	want := []SavedLabelOption{{"Benefits", 1}, {"Hiring", 2}}
	if !reflect.DeepEqual(options, want) || none != 2 {
		t.Fatalf("options=%+v none=%d", options, none)
	}
}

func TestIssue3932_FilterRowAndItemLabels(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3932View(locale)
		markup := renderNode(t, RenderSavedMessages(view))
		for _, want := range []string{
			`data-saved-labels="true"`,
			chatsave002Text(locale, "lbl_all"),
			chatsave002Text(locale, "lbl_none"),
			`data-saved-action="label-filter"`,
			`data-saved-label="Open enrollment"`,
			`data-saved-action="label-drop"`,
			`data-saved-action="label-edit"`,
			chatsave002Text(locale, "lbl_drop", "label", "Hiring"),
		} {
			if want == "" || !strings.Contains(markup, want) {
				t.Errorf("%s: missing %q", locale, want)
			}
		}
		if strings.Contains(markup, "lbl_") || strings.Contains(markup, "⟦") {
			t.Errorf("%s: an unresolved copy key is on screen", locale)
		}
		if got := strings.Count(markup, `aria-pressed="true"`); got != 1 {
			t.Errorf("%s: exactly the All labels choice is pressed, got %d", locale, got)
		}
	}
	// A phone-width panel and a right-to-left one keep the same markup, with the panel's own direction.
	if markup := renderNode(t, RenderSavedMessages(issue3932View("ar"))); !strings.Contains(markup, `dir="rtl"`) {
		t.Error("the Arabic panel is not right to left")
	}
}

func TestIssue3932_CopyIsTranslated(t *testing.T) {
	for key, values := range chatsave3932Table {
		for i, text := range values {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s has no text for language %d", key, i)
			}
		}
		if values[0] == values[1] || values[0] == values[2] {
			t.Errorf("%s is not translated", key)
		}
		if strings.Contains(values[0], "{label}") != strings.Contains(values[1], "{label}") || strings.Contains(values[0], "{label}") != strings.Contains(values[2], "{label}") {
			t.Errorf("%s: placeholders differ between languages", key)
		}
	}
}

func TestIssue3932_DoneMessageKeepsLabelsAndEditor(t *testing.T) {
	view := chatsave002View("all", chatsave002Rows())
	view.Rows[2].Labels = []string{"Shift handover"}
	markup := renderNode(t, RenderSavedMessages(view))
	done := markup[strings.Index(markup, `id="chatsave-item-p3"`):]
	done = done[:strings.Index(done, "</li>")]
	if !strings.Contains(done, "Shift handover") || !strings.Contains(done, `data-saved-action="label-edit"`) || !strings.Contains(done, "is-done") && !strings.Contains(markup, "chatsave-item is-done") {
		t.Errorf("a done message lost its labels or the way to change them: %s", done)
	}
}

func TestIssue3932_FilteredAndUnlabelledViews(t *testing.T) {
	view := issue3932View("en-US")
	view.Labels.Filter = "hiring"
	view.Rows = view.Rows[:2]
	markup := renderNode(t, RenderSavedMessages(view))
	if !strings.Contains(markup, `id="chatsave-label-rename"`) || !strings.Contains(markup, `id="chatsave-label-remove"`) {
		t.Error("a chosen label offers no rename or remove")
	}
	view.Rows = nil
	view.Labels.Filter = "Nothing"
	if markup = renderNode(t, RenderSavedMessages(view)); !strings.Contains(markup, chatsave002Text("en-US", "lbl_empty")) {
		t.Errorf("an empty label view says nothing: %s", markup)
	}
	view.Labels.Filter, view.Labels.OnlyUnlabelled = "", true
	markup = renderNode(t, RenderSavedMessages(view))
	if !strings.Contains(markup, chatsave002Text("en-US", "lbl_empty_none")) || !strings.Contains(markup, `id="chatsave-labelf-none"`) {
		t.Errorf("the unlabelled view is not drawn: %s", markup)
	}
}

func TestIssue3932_RenameAndRemoveForms(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3932View(locale)
		view.Labels.Filter = "Hiring"
		view.Labels.Manage = "rename"
		markup := renderNode(t, RenderSavedMessages(view))
		for _, want := range []string{`data-saved-form="label-rename"`, `id="chatsave-label-rename-field"`, `data-chat-value="Hiring"`, `maxLength="40"`, `id="chatsave-label-manage-cancel"`} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s rename: missing %q", locale, want)
			}
		}
		view.Labels.Manage = "remove"
		markup = renderNode(t, RenderSavedMessages(view))
		ask := chatsave002Text(locale, "lbl_remove_ask", "label", "Hiring")
		if !strings.Contains(markup, strings.ReplaceAll(ask, "’", "&#39;")) && !strings.Contains(markup, ask) {
			t.Errorf("%s remove: the confirmation does not say the messages stay: %s", locale, ask)
		}
		if !strings.Contains(markup, `data-saved-action="label-remove-confirm"`) || !strings.Contains(markup, `aria-labelledby="chatsave-label-ask"`) {
			t.Errorf("%s remove: the confirmation is not operable or labelled", locale)
		}
	}
}

func TestIssue3932_EditorIsPrivateLabelledAndSuggestsYourLabels(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3932View(locale)
		view.Labels.EditingItem = SavedItemKey("tenant", "general", "p1")
		markup := renderNode(t, RenderSavedMessages(view))
		for _, want := range []string{
			`data-saved-form="labels"`,
			`id="chatsave-labels-field-p1"`,
			`aria-describedby="chatsave-status chatsave-labels-private-p1"`,
			chatsave002Text(locale, "lbl_private"),
			`id="chatsave-labels-cancel-p1"`,
			chatsave002Text(locale, "lbl_hint"),
		} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s: editor missing %q", locale, want)
			}
		}
	}
	view := issue3932View("en-US")
	view.Labels.EditingItem = SavedItemKey("tenant", "general", "p1")
	markup := renderNode(t, RenderSavedMessages(view))
	editor := markup[strings.Index(markup, `data-saved-form="labels"`):]
	editor = editor[:strings.Index(editor, "</form>")]
	if strings.Contains(editor, `data-saved-action="label-add"`) {
		t.Errorf("the editor suggests labels the message already has: %s", editor)
	}
}

func TestIssue3932_NoAccessMessageKeepsLabelsButNoText(t *testing.T) {
	rows := []SavedMessageRow{{TenantID: "tenant", ConversationID: "gone", PostID: "p9", Availability: "no_access", Body: "SECRET BODY", Labels: []string{"Mine"}}}
	view := chatsave002View("all", rows)
	markup := renderNode(t, RenderSavedMessages(view))
	if strings.Contains(markup, "SECRET BODY") {
		t.Fatal("a message the person lost access to shows its text")
	}
	if !strings.Contains(markup, "Mine") || !strings.Contains(markup, `data-saved-action="label-drop"`) || strings.Contains(markup, `data-saved-action="label-edit"`) {
		t.Errorf("an unavailable message must keep removable labels and no editor: %s", markup)
	}
}

func TestIssue3932_PhoneTargetsAndDirectionStyles(t *testing.T) {
	phone := Chatsave002Styles[strings.Index(Chatsave002Styles, "@media(max-width:760px),(pointer:coarse){"):]
	phone = phone[:strings.Index(phone, "\n}\n")]
	for _, want := range []string{".chatsave-labelchip,.chatsave-labelaction,.chatsave-tag-suggest,.chatsave-tag-add{min-height:44px}", ".chatsave-tag-x{width:44px;height:44px}"} {
		if !strings.Contains(phone, want) {
			t.Errorf("label controls are not 44px on phones: missing %s", want)
		}
	}
	if strings.Contains(Chatsave002Styles, "padding:0 2px 0 8px") {
		t.Error("a physical padding breaks right-to-left")
	}
}

func TestIssue3932_LabelFailureCopy(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		failed := SavedActionFailedText(locale, "labels")
		if failed == "" || failed == SavedActionFailedText(locale, "save") {
			t.Errorf("%s: a refused label change reads as a failed save: %q", locale, failed)
		}
		view := issue3932View(locale)
		view.ActionError, view.Action = "label_limit", "labels"
		if markup := renderNode(t, RenderSavedMessages(view)); !strings.Contains(markup, chatsave002Text(locale, "lbl_limit")) {
			t.Errorf("%s: the label limit is not explained", locale)
		}
	}
}

// A label that sits only on done messages must not make the To do tab claim that
// no saved message has it: it says the tab has none and offers Show All.
func TestIssue3932_EmptyLabelViewOnATabOffersShowAll(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3932View(locale)
		view.Tab, view.Rows = "todo", nil
		view.Labels.Filter = "Hiring"
		markup := renderNode(t, RenderSavedMessages(view))
		if !strings.Contains(markup, chatsave002Text(locale, "lbl_empty_tab")) || !strings.Contains(markup, `data-saved-tab="all"`) {
			t.Errorf("%s: an empty label view on To do does not point to All: %s", locale, markup)
		}
		if strings.Contains(markup, chatsave002Text(locale, "lbl_empty")+"<") {
			t.Errorf("%s: the To do tab claims no saved message has the label", locale)
		}
		view.Tab = "all"
		markup = renderNode(t, RenderSavedMessages(view))
		if !strings.Contains(markup, chatsave002Text(locale, "lbl_empty")) || strings.Contains(markup, chatsave002Text(locale, "lbl_empty_tab")) {
			t.Errorf("%s: on All the label view is simply empty", locale)
		}
	}
}

// A label that is too long is a label problem, not a bad note or reminder time.
func TestIssue3932_InvalidLabelHasItsOwnMessage(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3932View(locale)
		view.ActionError, view.Action = "label_invalid", "labels"
		markup := renderNode(t, RenderSavedMessages(view))
		if !strings.Contains(markup, chatsave002Text(locale, "lbl_invalid")) || strings.Contains(markup, SavedMessagesCopy(locale).Invalid) {
			t.Errorf("%s: an invalid label is explained with the wrong message: %s", locale, markup)
		}
	}
}
