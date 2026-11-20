package chatui

import (
	"strings"
	"testing"
)

func TestIssue900002_EachPanelOpensDirectlyUnderItsButton(t *testing.T) {
	m := issue3787Model("en-US")
	m.Callbacks.LoadScheduled = func() {}
	m.SendLater.Loaded = true
	cases := []struct {
		local          localUI
		button, region string
	}{
		{localUI{followedOpen: true}, `id="` + issue3916ToggleID + `"`, `id="issue3916-panel"`},
		{localUI{followUpsOpen: true}, `id="` + issue3787ToggleID + `"`, `id="issue3787-panel"`},
		{localUI{scheduledOpen: true}, `id="` + issue3788ToggleID + `"`, `id="issue3788-panel-tray"`},
	}
	for _, c := range cases {
		markup := renderNode(t, rail(m, handlers{local: c.local}))
		fixedAt, scrollAt := strings.Index(markup, `class="rail-fixed"`), strings.Index(markup, `class="rail-scroll"`)
		button, region := strings.Index(markup, c.button), strings.Index(markup, c.region)
		if button < 0 || region < 0 || fixedAt < 0 {
			t.Fatalf("missing %q or %q: %s", c.button, c.region, markup)
		}
		if !(fixedAt < button && button < region && region < scrollAt) {
			t.Fatalf("%s is not drawn right under its button, inside the fixed block (fixed %d button %d region %d scroll %d)", c.region, fixedAt, button, region, scrollAt)
		}
		// No other entry's row may sit between the button and its panel.
		if between := markup[button:region]; strings.Contains(between, "-control\"") {
			t.Fatalf("another row sits between the button and its panel: %s", between)
		}
	}
}

func TestIssue900002_ClosedRegionsStayAddressable(t *testing.T) {
	m := issue3787Model("en-US")
	m.Callbacks.LoadScheduled = func() {}
	markup := renderNode(t, rail(m, handlers{}))
	for _, id := range []string{"issue3916-panel", "issue3787-panel", "issue3788-panel-tray"} {
		if !strings.Contains(markup, `id="`+id+`"`) {
			t.Fatalf("aria-controls target %s is not in the page", id)
		}
	}
	if scroll := strings.Index(markup, `class="rail-scroll"`); strings.Contains(markup[scroll:], "issue3916-panel") || strings.Contains(markup[scroll:], "issue3787-panel") {
		t.Fatal("a panel region is still in the scrolling conversation list")
	}
}

func TestIssue900002_OpeningOnePanelClosesTheOthers(t *testing.T) {
	for _, keep := range []string{"drafts", "followed", "followups", "scheduled"} {
		u := localUI{draftsOpen: true, followedOpen: true, followUpsOpen: true, scheduledOpen: true, draftsAsk: "x", scheduledEdit: "y"}
		issue900002Exclusive(&u, keep)
		got := map[string]bool{"drafts": u.draftsOpen, "followed": u.followedOpen, "followups": u.followUpsOpen, "scheduled": u.scheduledOpen}
		for name, open := range got {
			if open != (name == keep) {
				t.Fatalf("keep %s: %s open = %v", keep, name, open)
			}
		}
		if keep != "drafts" && u.draftsAsk != "" || keep != "scheduled" && u.scheduledEdit != "" {
			t.Fatalf("keep %s: closing a panel left its pending state behind: %+v", keep, u)
		}
	}
}

func TestIssue900002_EntriesAreSiblingsAndUnreadIsAToggleNotAnInput(t *testing.T) {
	css := Issue900002Styles
	for _, want := range []string{
		".rail-fixed .issue3916-control", ".rail-fixed .issue3787-control", ".rail-fixed .issue3788-control",
		"max-block-size:min(40vh", ".issue3936-toggle{", "border:0", ".issue3936-toggle.on,",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("stylesheet is missing %q", want)
		}
	}
	if strings.Contains(css, "padding:0 .75rem") {
		t.Fatal("entries are indented again")
	}
	for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right"} {
		if strings.Contains(css, physical) {
			t.Fatalf("physical property %s breaks right-to-left", physical)
		}
	}
	// A four-value padding shorthand is physical (left/right), so it is wrong in
	// right-to-left; the Unread row uses padding-inline instead.
	if strings.Contains(css, "padding:0 8px 0 12px") || !strings.Contains(css, "padding-inline:12px 8px") {
		t.Fatal("the Unread row's padding is not logical")
	}
	if !strings.Contains(Stylesheet, Issue900002Styles) {
		t.Fatal("the rules are not part of the page stylesheet")
	}
	if strings.Index(Stylesheet, Issue900002Styles) < strings.Index(Stylesheet, Issue3936Styles) {
		t.Fatal("the rules come before the ones they override")
	}
	m := issue3787Model("en-US")
	markup := renderNode(t, rail(m, handlers{local: localUI{unreadOnly: true}}))
	if !strings.Contains(markup, `aria-pressed="true"`) || !strings.Contains(markup, `<button`) {
		t.Fatal("Unread lost its pressed state")
	}
}
