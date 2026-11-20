package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

func issue3789Model(locale string) Model {
	m := chatux007Model(locale)
	m.State = StateReady
	m.Preferences.QuietTimezone = "UTC"
	m.CurrentUser = "me"
	m.Members = []Member{{ID: "me", Name: "Me Myself"}, {ID: "priya", Name: "Priya Shah"}, {ID: "dana", Name: "Dana Cole"}, {ID: "bot", Name: "Helper", Agent: true}}
	m.Away = AwayView{Loaded: true, Revision: 1}
	m.Callbacks.SaveAway = func(AwayEdit) {}
	return m
}

func issue3789Render(t *testing.T, m Model) string {
	t.Helper()
	return renderNode(t, html.Div(html.Props{}, chatbug045PanelRows(m, handlers{})...))
}

func issue3789Soon() time.Time {
	return time.Now().UTC().Add(5 * 24 * time.Hour).Truncate(24 * time.Hour)
}

func TestIssue3789_RowIsOfferedOnlyWhereItCanSave(t *testing.T) {
	m := issue3789Model("en-US")
	m.Callbacks.SaveAway = nil
	if strings.Contains(issue3789Render(t, m), `data-prefs-section="away"`) {
		t.Fatal("an Away row with nothing to save it was drawn")
	}
	if !strings.Contains(issue3789Render(t, issue3789Model("en-US")), `data-prefs-section="away"`) {
		t.Fatal("the Away row is missing from Chat preferences")
	}
}

func TestIssue3789_LoadingAndUnavailable(t *testing.T) {
	m := issue3789Model("en-US")
	m.Away = AwayView{}
	got := issue3789Render(t, m)
	if !strings.Contains(got, "Loading your away setting") || strings.Contains(got, `data-action="away-save"`) {
		t.Fatalf("loading: %s", got)
	}
	m.Away = AwayView{Failed: true}
	got = issue3789Render(t, m)
	if !strings.Contains(got, "could not be loaded") || strings.Contains(got, `data-action="away-save"`) {
		t.Fatalf("unavailable state must not offer controls that do nothing: %s", got)
	}
}

func TestIssue3789_RowShowsTheSettingAndItsLimits(t *testing.T) {
	m := issue3789Model("en-US")
	got := issue3789Render(t, m)
	for _, want := range []string{"You are not away.", `id="chat-away-until"`, `type="date"`, `id="chat-away-cover"`, "Priya Shah", "Dana Cole",
		`data-action="away-save"`, `data-action="away-show"`, `data-action="away-notify"`, "Choose who covers to turn this on.",
		"does not get new access to conversations or any approval authority"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	// Yourself and agents are not offered as cover.
	if strings.Contains(got, "Me Myself</option>") || strings.Contains(got, "Helper") {
		t.Fatalf("an unusable cover was offered: %s", got)
	}
	if strings.Contains(got, `data-action="away-clear"`) {
		t.Fatal("I am back is offered to someone who is not away")
	}
	until := issue3789Soon()
	m.Away = AwayView{Loaded: true, Revision: 3, Active: true, Until: until, CoverID: "priya", NotifyCover: true}
	got = issue3789Render(t, m)
	for _, want := range []string{"You are away until ", "Priya Shah covers.", `data-action="away-clear"`, until.Format("2006-01-02")} {
		if !strings.Contains(got, want) {
			t.Fatalf("active: missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "Choose who covers to turn this on.") {
		t.Fatal("the notice toggle was explained as unavailable while a cover is set")
	}
}

func TestIssue3789_ProfileCardAndMentionHintShowTheAbsence(t *testing.T) {
	until := issue3789Soon()
	m := issue3789Model("en-US")
	m.AwayPeople = map[string]AwayPersonView{"dana": {Until: until, CoverID: "priya"}}
	m.ShowPerson = true
	m.PersonDetails = &PersonDetails{ID: "dana", Name: "Dana Cole", Ready: true}
	card := renderNode(t, personPane(m))
	if !strings.Contains(card, `data-person-away="true"`) || !strings.Contains(card, "Away until ") || !strings.Contains(card, "Priya Shah covers") {
		t.Fatalf("profile card: %s", card)
	}
	// Someone with no visible absence shows nothing, so nothing can tell
	// "hidden" apart from "not away".
	m.PersonDetails = &PersonDetails{ID: "priya", Name: "Priya Shah", Ready: true}
	if strings.Contains(renderNode(t, personPane(m)), "person-away") {
		t.Fatal("an absence line was drawn for someone with none to show")
	}
	// The mention list names the absence and the cover beside the person.
	m.SelectedID = "c1"
	menu := renderNode(t, mentionMenu(m, mentionState{Target: "chat-composer", Open: true}, "chat-composer"))
	if !strings.Contains(menu, "Away until ") || !strings.Contains(menu, "Priya Shah covers") {
		t.Fatalf("mention hint: %s", menu)
	}
	// An ended absence is not shown, even if the page still holds it.
	m.AwayPeople = map[string]AwayPersonView{"dana": {Until: time.Now().Add(-time.Hour), CoverID: "priya"}}
	if strings.Contains(renderNode(t, mentionMenu(m, mentionState{Target: "chat-composer", Open: true}, "chat-composer")), "Away until") {
		t.Fatal("an ended absence is still shown")
	}
}

func TestIssue3789_YourOwnHiddenAbsenceIsStillShownToYouOnly(t *testing.T) {
	m := issue3789Model("en-US")
	m.Away = AwayView{Loaded: true, Revision: 2, Active: true, Until: issue3789Soon(), CoverID: "priya", Hidden: true}
	if line := issue3789Line(m, "me"); !strings.Contains(line, "Away until") {
		t.Fatalf("own absence: %q", line)
	}
	got := issue3789Render(t, m)
	if !strings.Contains(got, "Only you see this while it is hidden.") {
		t.Fatalf("the privacy of a hidden absence is not said: %s", got)
	}
	// The toggle reads as off while hidden.
	if !strings.Contains(got, `aria-pressed="false"`) {
		t.Fatalf("the show toggle does not reflect hiding: %s", got)
	}
}

func TestIssue3789_SaveNotesAreAnnounced(t *testing.T) {
	for note, want := range map[string]string{
		"saved": "Away setting saved", "ended": "Welcome back", "failed": "could not be saved", "conflict": "changed somewhere else",
		"invalid": "not available", "choose_date": "Choose the date", "past_date": "after today",
	} {
		m := issue3789Model("en-US")
		m.Away.Note = note
		got := issue3789Render(t, m)
		role := `role="alert"`
		if note == "saved" || note == "ended" {
			role = `role="status"`
		}
		if !strings.Contains(got, want) || !strings.Contains(got, `data-away-note="`+note+`"`) || !strings.Contains(got, role) {
			t.Errorf("%s: missing %q or %s in %s", note, want, role, got)
		}
	}
}

func TestIssue3789_ClicksSaveTheRightChange(t *testing.T) {
	var got []AwayEdit
	m := issue3789Model("en-US")
	m.Callbacks.SaveAway = func(e AwayEdit) { got = append(got, e) }
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	values := map[string]string{"chat-away-until": "2026-03-12", "chat-away-cover": "priya"}
	read := func(id string) string { return values[id] }

	if !issue3789Apply(m, "away-save", read, now) || len(got) != 1 {
		t.Fatalf("save: %+v", got)
	}
	if e := got[0]; e.Problem != "" || e.CoverID != "priya" || !e.Until.Equal(time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC)) || e.NotifyCover {
		t.Fatalf("save edit: %+v", e)
	}
	// A missing or past date is a problem to say, never a save.
	values["chat-away-until"] = ""
	issue3789Apply(m, "away-save", read, now)
	values["chat-away-until"] = "2026-03-05"
	issue3789Apply(m, "away-save", read, now)
	values["chat-away-until"] = "garbage"
	issue3789Apply(m, "away-save", read, now)
	if got[1].Problem != "choose_date" || got[2].Problem != "past_date" || got[3].Problem != "choose_date" {
		t.Fatalf("problems: %+v", got[1:])
	}
	// Dropping the cover also drops the notices that depended on it.
	m.Away = AwayView{Loaded: true, Active: true, Until: now.Add(48 * time.Hour), CoverID: "priya", NotifyCover: true}
	values["chat-away-until"], values["chat-away-cover"] = "2026-03-12", ""
	issue3789Apply(m, "away-save", read, now)
	if e := got[len(got)-1]; e.CoverID != "" || e.NotifyCover {
		t.Fatalf("notices kept without a cover: %+v", e)
	}
	// Toggles keep everything else.
	issue3789Apply(m, "away-show", read, now)
	if e := got[len(got)-1]; !e.Hidden || e.CoverID != "priya" || !e.NotifyCover || e.Until.IsZero() {
		t.Fatalf("hide: %+v", e)
	}
	issue3789Apply(m, "away-notify", read, now)
	if e := got[len(got)-1]; e.NotifyCover || e.CoverID != "priya" {
		t.Fatalf("notify off: %+v", e)
	}
	// I am back ends the absence and keeps the person's display choice.
	m.Away.Hidden = true
	issue3789Apply(m, "away-clear", read, now)
	if e := got[len(got)-1]; !e.Until.IsZero() || e.CoverID != "" || e.NotifyCover || !e.Hidden {
		t.Fatalf("back: %+v", e)
	}
	// Notices cannot be switched on without a cover or an absence.
	before := len(got)
	m.Away = AwayView{Loaded: true}
	issue3789Apply(m, "away-notify", read, now)
	if len(got) != before {
		t.Fatal("notices were switched on with nothing to cover")
	}
	if issue3789Apply(m, "something-else", read, now) {
		t.Fatal("a foreign click was claimed")
	}
}

func TestIssue3789_CopyInEveryLanguage(t *testing.T) {
	for key := range issue3789Copy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			got := issue3789Copy[locale][key]
			if got == "" || got == issue3789Copy["en-US"][key] {
				t.Errorf("%s has no %s text", key, locale)
			}
		}
	}
	for locale, want := range map[string]string{"de-DE": "Abwesend bis", "ar": "خارج المكتب حتى"} {
		m := issue3789Model(locale)
		got := issue3789Render(t, m)
		if !strings.Contains(got, want) || !strings.Contains(got, `dir="auto"`) {
			t.Fatalf("%s row: %s", locale, got)
		}
	}
	m := issue3789Model("de-DE")
	m.AwayPeople = map[string]AwayPersonView{"dana": {Until: issue3789Soon(), CoverID: "priya"}}
	if line := issue3789Line(m, "dana"); !strings.HasPrefix(line, "Abwesend bis ") || !strings.Contains(line, "Vertretung: Priya Shah") {
		t.Fatalf("German line: %q", line)
	}
	// A cover the page cannot name is "A colleague", never an identifier.
	m.AwayPeople = map[string]AwayPersonView{"dana": {Until: issue3789Soon(), CoverID: "u-9f3a"}}
	if line := issue3789Line(m, "dana"); strings.Contains(line, "u-9f3a") {
		t.Fatalf("an identifier was shown: %q", line)
	}
}

func TestIssue3789_StylesUseLogicalPropertiesAndTouchTargets(t *testing.T) {
	for _, bad := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right", " left:", " right:"} {
		if strings.Contains(Issue3789Styles, bad) {
			t.Errorf("styles use a physical property %q", bad)
		}
	}
	if !strings.Contains(Issue3789Styles, "min-block-size:44px") {
		t.Error("controls are not 44px tall")
	}
	if !strings.Contains(Stylesheet, Issue3789Styles) {
		t.Error("the away styles are not part of the page stylesheet")
	}
}
