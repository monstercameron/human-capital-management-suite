package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func typedProjectRef(id string) ChatReference {
	return ChatReference{Kind: ChatReferenceProjectTask, TenantID: "t1", ID: id, Display: "Ship it"}
}

func readyProjectModel() Model {
	return Model{EmbedOrigin: "https://app.example", ProjectTaskPreviews: map[string]ProjectTaskPreview{
		ProjectTaskPreviewKey("p1", "k1"): {ProjectID: "p1", TaskID: "k1", Title: "Ship it", Readable: true, State: "ready", ProjectName: "Launch", Key: "K-1"},
	}}
}

// A link whose origin differs from the viewer's embed origin never unfurled.
// With a typed reference on the message it does, because the card comes from
// data and the URL is only text.
func TestIssue3752_TypedReferenceUnfurlsAcrossOrigins(t *testing.T) {
	body := "please look\nhttps://old-host.example/workspace/app/project?project=p1&task=k1"
	m := readyProjectModel()
	if got := ProjectTaskReferences(body, m.EmbedOrigin); len(got) != 0 {
		t.Fatalf("precondition: foreign origin matched without a typed reference: %#v", got)
	}
	m.renderWorkReferences = []ChatReference{typedProjectRef("p1/k1")}
	refs := ProjectTaskReferencesFor(body, m.EmbedOrigin, m.renderWorkReferences)
	if len(refs) != 1 || refs[0].ProjectID != "p1" || refs[0].TaskID != "k1" || refs[0].Start < 0 {
		t.Fatalf("typed refs = %#v", refs)
	}
	card, err := ui.RenderToString(ui.Fragment(projectPreviewEmbeds(m, body)...))
	if err != nil || !strings.Contains(card, "Ship it") || !strings.Contains(card, `href="/workspace/app/project?project=p1&amp;task=k1"`) {
		t.Fatalf("card from data missing (err=%v):\n%s", err, card)
	}
	inline, _ := ui.RenderToString(ui.Fragment(projectTaskReferenceBody(m, body)...))
	if strings.Contains(inline, "old-host.example") || !strings.Contains(inline, "Ship it") {
		t.Fatalf("raw URL leaked into the body:\n%s", inline)
	}
}

// The card does not depend on the body at all: an author who edits the URL away
// keeps the typed reference, so the card and its backlinks stay.
func TestIssue3752_TypedReferenceNeedsNoUrlInTheBody(t *testing.T) {
	m := readyProjectModel()
	m.renderWorkReferences = []ChatReference{typedProjectRef("p1/k1")}
	card, _ := ui.RenderToString(ui.Fragment(projectPreviewEmbeds(m, "see this")...))
	if !strings.Contains(card, "Ship it") {
		t.Fatalf("card missing without a URL:\n%s", card)
	}
	inline, _ := ui.RenderToString(ui.Fragment(projectTaskReferenceBody(m, "see this")...))
	if !strings.Contains(inline, "see this") {
		t.Fatalf("body text lost:\n%s", inline)
	}
}

// A reference the reader may not open arrives with no id: it draws the same
// locked card and names nothing.
func TestIssue3752_RestrictedTypedReferenceDrawsNothingNamed_Security(t *testing.T) {
	m := Model{EmbedOrigin: "https://app.example"}
	m.renderWorkReferences = []ChatReference{{Kind: ChatReferenceProjectTask, Display: "Restricted project item"}}
	if refs := ProjectTaskReferencesFor("hello", m.EmbedOrigin, m.renderWorkReferences); len(refs) != 0 {
		t.Fatalf("an id-less reference produced %#v", refs)
	}
	if nodes := projectPreviewEmbeds(m, "hello"); len(nodes) != 0 {
		t.Fatalf("an id-less reference drew a card")
	}
}

// A malformed project address used to stay a raw, truncated URL. It now says,
// in words, that the link cannot be opened.
func TestIssue3752_MalformedProjectLinkShowsUnresolvedChip(t *testing.T) {
	m := readyProjectModel()
	for _, body := range []string{
		"https://app.example/workspace/app/project?project=",
		"https://app.example/workspace/app/project?task=k1",
		"/workspace/app/project?project=p1&ta",
	} {
		refs := ProjectTaskReferencesFor(body, m.EmbedOrigin, nil)
		if len(refs) != 1 || !refs[0].Unresolved {
			t.Fatalf("%q: refs = %#v", body, refs)
		}
		markup, _ := ui.RenderToString(ui.Fragment(projectTaskReferenceBody(m, body)...))
		if strings.Contains(markup, "workspace/app/project") || !strings.Contains(markup, "chat-project-task-unresolved") || !strings.Contains(markup, "can’t be opened") {
			t.Fatalf("%q rendered:\n%s", body, markup)
		}
		if nodes := projectPreviewEmbeds(m, body); len(nodes) != 0 {
			t.Fatalf("%q drew a card", body)
		}
	}
	// A foreign host's malformed link is not ours to judge.
	if refs := ProjectTaskReferencesFor("https://elsewhere.example/workspace/app/project?project=", m.EmbedOrigin, nil); len(refs) != 0 {
		t.Fatalf("foreign malformed link claimed: %#v", refs)
	}
}

func TestIssue3752_UnresolvedChipIsTranslated(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		if got := projectUnresolvedLabel(m); strings.TrimSpace(got) == "" {
			t.Fatalf("%s: empty copy", locale)
		}
	}
	if projectUnresolvedLabel(Model{Locale: "de-DE"}) == projectUnresolvedLabel(Model{Locale: "en-US"}) || projectUnresolvedLabel(Model{Locale: "ar"}) == projectUnresolvedLabel(Model{Locale: "en-US"}) {
		t.Fatal("unresolved copy is not translated")
	}
}
