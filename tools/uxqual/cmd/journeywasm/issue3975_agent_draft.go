package main

import (
	"strings"
	"sync"
)

// agentComposerDrafts (#3975) keeps the question a person has typed in the
// Agents composer while they visit another page of the workspace and come back.
// The page is redrawn on that trip, which gave them an empty box.
//
// It lives in memory on purpose. The browser-state policy
// (productclient.ValidateBrowserStorageWrite) allows session storage only for
// bounded presentation records, and typed text is neither bounded nor a
// presentation hint, so nothing here is written to browser storage. A full
// reload of the page therefore starts clean; the in-app round trip does not
// lose the text. The draft is held for one signed-in identity only.
type agentComposerDrafts struct {
	mu      sync.Mutex
	tenant  string
	subject string
	text    string
}

var agentDraftStore agentComposerDrafts

// Remember records what is typed now. Blank text forgets the draft, so a person
// who cleared the box does not get the old text back.
func (d *agentComposerDrafts) Remember(tenant, subject, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if strings.TrimSpace(text) == "" || strings.TrimSpace(tenant) == "" || strings.TrimSpace(subject) == "" {
		d.tenant, d.subject, d.text = "", "", ""
		return
	}
	d.tenant, d.subject, d.text = tenant, subject, text
}

// Restore answers the text to put back into an empty composer. It never
// replaces text that is already there, and never hands one person's draft to
// another identity.
func (d *agentComposerDrafts) Restore(tenant, subject, current string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.text == "" || strings.TrimSpace(current) != "" {
		return "", false
	}
	if d.tenant != tenant || d.subject != subject {
		d.tenant, d.subject, d.text = "", "", ""
		return "", false
	}
	return d.text, true
}

// Clear forgets the draft, for example once the question has been sent.
func (d *agentComposerDrafts) Clear() {
	d.mu.Lock()
	d.tenant, d.subject, d.text = "", "", ""
	d.mu.Unlock()
}

// agentFollowUpComposerValue is the composer text after "Ask a follow-up": the
// quoted earlier request, then whatever the person had already typed. It never
// replaces the draft, and a second press does not quote the request twice.
func agentFollowUpComposerValue(existing, request string) string {
	quote := agentFollowUpContext(request)
	if quote == "" {
		return existing
	}
	if strings.HasPrefix(existing, quote) {
		return existing
	}
	return quote + existing
}
