package productui

import (
	"strings"
	"testing"
)

// TestTodo_UXAUDIT_013_Browser is the BROWSER matrix test for
// planning/todos.md's UXAUDIT-013. By this repo's testing convention (see
// TestTodo_UX_002_Browser) it is a Go-side check of the served document, so
// it needs no JS engine to run: every support path Help advertises must
// resolve to a destination the browser can use on its own -- a same-origin
// link it can follow or a form it can submit -- rather than a control that
// only works once the wasm client hydrates. Article results stay owned by
// the governed help service; the document carries the address, never the
// records.
func TestTodo_UXAUDIT_013_Browser(t *testing.T) {
	doc, err := Render(testView(PageHelp))
	if err != nil {
		t.Fatal(err)
	}
	// The knowledge-search and support-request entries are real same-origin
	// anchors, not spans or buttons waiting for a client handler.
	for _, action := range []struct {
		name string
		mark string
		href string
	}{
		{"knowledge search", `hcm-help-action="knowledge-search"`, "/workspace/app/help/knowledge-search"},
		{"support request", `>Request HR support</a>`, "/workspace/app/help/hr-service-request"},
	} {
		if !anchorToSameOriginHref(doc, action.mark, action.href) {
			t.Errorf("Help %s entry is not a followable same-origin link to %q", action.name, action.href)
		}
	}
	// The task guidance beside the support entries links to the durable
	// task destinations, so Help never strands the reader at a dead end.
	for _, href := range []string{"/workspace/app/myself", "/workspace/app/organization", "/workspace/app/people"} {
		if !strings.Contains(doc, `href="`+href) {
			t.Errorf("Help task guidance omits a followable link to %q", href)
		}
	}

	// The knowledge-search page works without script: with no Navigate
	// handler the form still GETs the named query to the governed route.
	unenhanced := testView(PageKnowledgeSearch)
	unenhanced.Query = "pay statement"
	unenhanced.Navigate = nil
	searchDoc, err := Render(unenhanced)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<form`, `role="search"`, `action="/workspace/app/help/knowledge-search`, `method="get"`, `name="q"`, `value="pay statement"`, `type="submit"`} {
		if !strings.Contains(searchDoc, want) {
			t.Errorf("unenhanced knowledge search missing %q", want)
		}
	}

	// The support-request page POSTs its fields to the governed route, so
	// request details never travel in a query string or browser history.
	requestDoc, err := Render(testView(PageHRServiceRequest))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`action="/workspace/app/help/hr-service-request`, `method="post"`, `name="category"`, `name="details"`, `type="submit"`} {
		if !strings.Contains(requestDoc, want) {
			t.Errorf("support-request form missing %q", want)
		}
	}
}

// anchorToSameOriginHref reports whether mark appears inside an anchor
// element whose href starts at the expected same-origin path. A marker on a
// span, button, or client-only control -- or an href escaping to another
// origin -- fails, because the browser could not follow it on its own.
func anchorToSameOriginHref(doc, mark, href string) bool {
	index := strings.Index(doc, mark)
	if index < 0 {
		return false
	}
	open := strings.LastIndex(doc[:index], "<a")
	if open < 0 || strings.Contains(doc[open:index], ">") {
		return false
	}
	end := strings.Index(doc[open:], ">")
	if end < 0 {
		return false
	}
	tag := doc[open : open+end]
	hrefIndex := strings.Index(tag, `href="`+href)
	if hrefIndex < 0 {
		return false
	}
	rest := tag[hrefIndex+len(`href="`+href):]
	if rest == "" {
		return false
	}
	switch rest[0] {
	case '"', '?', '#':
		return true
	default:
		return false
	}
}
