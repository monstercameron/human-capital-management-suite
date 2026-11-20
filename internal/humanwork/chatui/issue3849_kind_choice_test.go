package chatui

import (
	"regexp"
	"strings"
	"testing"
)

func issue3849Model(loc string) Model {
	return Model{Locale: loc, ShowCreate: true, NewKind: KindUnchosen, Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) {}, CloseCreate: func() {}}}
}

// The dialog opens with no type chosen, still shows every type with its
// visibility note, and offers nothing to submit.
func TestIssue3849_DialogOpensWithNoTypeSelected(t *testing.T) {
	got := renderNode(t, createDialog(issue3849Model("en-US"), handlers{}))
	if strings.Contains(got, `aria-checked="true"`) {
		t.Fatalf("a type is already selected: %s", got)
	}
	if n := strings.Count(got, `data-action="create-kind"`); n != 4 {
		t.Fatalf("want four type cards, got %d: %s", n, got)
	}
	for _, want := range []string{"A one-to-one conversation.", "Choose a type to continue"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing: %s", want, got)
		}
	}
	for _, gone := range []string{`id="new-chat-name"`, `id="new-chat-member-search"`} {
		if strings.Contains(got, gone) {
			t.Errorf("%s shown before a type is chosen: %s", gone, got)
		}
	}
	if !regexp.MustCompile(`<button[^>]*disabled[^>]*type="submit"|<button[^>]*type="submit"[^>]*disabled`).MatchString(got) {
		t.Errorf("the submit button is enabled with no type: %s", got)
	}
	// Three cards leave the tab order (-1); the first stays the one tab stop.
	if n := strings.Count(got, `tabIndex="-1"`); n != 3 {
		t.Errorf("the type group must have one tab stop, got %d cards out of the tab order: %s", n, got)
	}
}

// Choosing a type brings up its fields and enables the right submit.
func TestIssue3849_ChoosingATypeShowsItsFields(t *testing.T) {
	m := issue3849Model("en-US")
	channel := renderNode(t, createDialog(m, handlers{local: localUI{createKind: PublicChannel}}))
	if !strings.Contains(channel, `id="new-chat-name"`) || !strings.Contains(channel, `aria-checked="true"`) {
		t.Fatalf("a chosen channel shows no name field: %s", channel)
	}
	dm := renderNode(t, createDialog(m, handlers{local: localUI{createKind: DirectMessage}}))
	if !strings.Contains(dm, `id="new-chat-member-search"`) {
		t.Fatalf("a chosen direct message shows no people search: %s", dm)
	}
}

func TestIssue3849_ArrowsFromNoChoice(t *testing.T) {
	if k, ok := chatissue3877NextKind(KindUnchosen, "ArrowDown", false); !ok || k != createKinds[0].kind {
		t.Errorf("down from nothing = %q, %v", k, ok)
	}
	if k, ok := chatissue3877NextKind(KindUnchosen, "ArrowUp", false); !ok || k != createKinds[len(createKinds)-1].kind {
		t.Errorf("up from nothing = %q, %v", k, ok)
	}
}

func TestIssue3849_PromptLanguages(t *testing.T) {
	en := chatissue3849Prompt(issue3849Model("en-US"))
	for _, loc := range []string{"de-DE", "ar"} {
		m := issue3849Model(loc)
		if got := chatissue3849Prompt(m); got == "" || got == en || strings.Contains(got, "prompt") {
			t.Errorf("%s: %q", loc, got)
		}
		if out := renderNode(t, createDialog(m, handlers{})); !strings.Contains(out, chatissue3849Prompt(m)) {
			t.Errorf("%s: prompt not rendered", loc)
		}
	}
}
