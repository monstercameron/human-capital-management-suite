package main

import (
	"os"
	"strings"
	"testing"
)

// The announced-sidebar read takes a newer revision, and this tab's next draft
// write rides on it. Another tab's draft in that read must therefore be
// compared with this tab's own (TestIssue3953_AnotherTabsDraftIsKeptWhenThisTabReadsTheAccount
// shows loadDrafts keeps both); the browser-only handler is checked here by its
// source because it cannot run natively. Served proof: the live spec
// tools/uxqual/browser/issue-drafts-1.live.spec.mjs (#3953).
func TestIssue3953_AnnouncedSidebarReadComparesDrafts(t *testing.T) {
	raw, err := os.ReadFile("chatside001_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func chatside001SidebarAnnounced(")
	if start < 0 {
		t.Fatal("chatside001SidebarAnnounced not found")
	}
	body := source[start:]
	revision := strings.Index(body, "chatRecipientBrowser.sidebarRevision = side.GetSidebar().GetRevision()")
	drafts := strings.Index(body, "chatBrowser.loadDrafts(layout.Drafts)")
	if revision < 0 || drafts < 0 || drafts < revision {
		t.Fatalf("the announced read takes a new revision (at %d) without comparing drafts (at %d): the next draft write would replace another tab's text silently", revision, drafts)
	}
}
