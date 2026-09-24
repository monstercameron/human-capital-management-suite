package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestDocReferencesRecognizeTokenAndCanonicalURL(t *testing.T) {
	origin := "https://hcm.example"
	body := "See doc:policy-42 and https://hcm.example/workspace/app/docs?document=policy-42 for the same page."
	refs := DocReferences(body, origin)
	if len(refs) != 2 {
		t.Fatalf("refs = %#v", refs)
	}
	if refs[0].ID != "policy-42" || refs[1].ID != "policy-42" {
		t.Fatalf("ids = %#v", refs)
	}
	for _, bad := range []string{
		"https://evil.example/workspace/app/docs?document=policy-42",
		"doc:",
		"doc:../etc",
	} {
		if got := DocReferences(bad, origin); len(got) != 0 {
			t.Errorf("accepted %q: %#v", bad, got)
		}
	}
}

func TestDocReferenceURLRoundTrips(t *testing.T) {
	id := "team-blue.doc"
	got := DocReferenceURL(id)
	refs := DocReferences(got, "https://hcm.example")
	if len(refs) != 1 || refs[0].ID != id {
		t.Fatalf("round trip = %#v (url %q)", refs, got)
	}
}

func TestDocLinkReferenceBodyLinksWithoutLeakingUnreadableTitle(t *testing.T) {
	m := Model{EmbedOrigin: "https://hcm.example", DocPreviews: map[string]DocPreview{
		"secret-1": {ID: "secret-1", State: "ready", Readable: false},
		"open-1":   {ID: "open-1", State: "ready", Readable: true, Title: "Hiring Plan"},
	}}
	m.Text = func(key string) string { return "" }
	body := "Ask about doc:secret-1 and doc:open-1 today."
	markup, err := ui.RenderToString(html.P(html.Props{}, docLinkReferenceBody(m, body)...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "secret-1</a>") {
		t.Fatalf("restricted document link leaked its id as a title: %s", markup)
	}
	if !strings.Contains(markup, "Hiring Plan") {
		t.Fatalf("readable document did not show its title: %s", markup)
	}
	if !strings.Contains(markup, `href="/workspace/app/docs?document=secret-1"`) {
		t.Fatalf("restricted link did not still point at the document: %s", markup)
	}
}

func TestDocPreviewEmbedsRenderLoadingReadyAndRestrictedStates(t *testing.T) {
	m := Model{EmbedOrigin: "https://hcm.example"}
	m.Text = func(key string) string { return "" }
	m.DocPreviews = map[string]DocPreview{
		"ready-1": {ID: "ready-1", State: "ready", Readable: true, Title: "Q4 Plan", Owner: "Priya Shah", UpdatedAt: "2026-09-01", Snippet: "Quarterly headcount plan."},
	}
	loading := docPreviewEmbeds(m, "doc:pending-1")
	markup, err := ui.RenderToString(html.Div(html.Props{}, loading...))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-embed-state="loading"`) {
		t.Fatalf("unresolved reference did not render a loading card: %s", markup)
	}
	ready := docPreviewEmbeds(m, "doc:ready-1")
	markup, err = ui.RenderToString(html.Div(html.Props{}, ready...))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Q4 Plan") || !strings.Contains(markup, "Priya Shah") || !strings.Contains(markup, "Quarterly headcount plan.") {
		t.Fatalf("ready card missing owner/title/snippet: %s", markup)
	}
	restricted := docPreviewEmbeds(Model{EmbedOrigin: "https://hcm.example", DocPreviews: map[string]DocPreview{"locked-1": {ID: "locked-1", State: "ready", Readable: false}}, Text: func(string) string { return "" }}, "doc:locked-1")
	markup, err = ui.RenderToString(html.Div(html.Props{}, restricted...))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "locked-1<") || strings.Contains(markup, `href="/workspace/app/docs?document=locked-1"`) {
		t.Fatalf("restricted embed exposed a title or an open link: %s", markup)
	}
}
