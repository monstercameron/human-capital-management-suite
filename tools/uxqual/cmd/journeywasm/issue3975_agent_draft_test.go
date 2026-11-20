package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

// The follow-up used to hand the quoted request to the next page through
// session storage. The browser-state policy refuses that key, so the write was
// dropped and the composer came back blank. The hand-off now fills the composer
// in place; this pins why storage cannot carry it.
func TestIssue3975_SessionStorageCannotCarryTheHandoff(t *testing.T) {
	for _, key := range []string{"hcm-agent-follow-up-context", "hcm-agent-composer-draft"} {
		if err := productclient.ValidateBrowserStorageWrite(key, "> What is the parental leave policy?\n\n"); err == nil {
			t.Fatalf("storage policy accepts %q: the in-memory draft would no longer be required", key)
		}
	}
}

func TestIssue3975_FollowUpKeepsTheRequestAndTheDraft(t *testing.T) {
	if got := agentFollowUpComposerValue("", "What is the parental leave policy?"); got != "> What is the parental leave policy?\n\n" {
		t.Fatalf("blank composer: %q", got)
	}
	got := agentFollowUpComposerValue("and for adoption?", "What is the parental leave policy?")
	if !strings.HasPrefix(got, "> What is the parental leave policy?\n\n") || !strings.HasSuffix(got, "and for adoption?") {
		t.Fatalf("an unsent draft must survive the follow-up: %q", got)
	}
	if again := agentFollowUpComposerValue(got, "What is the parental leave policy?"); again != got {
		t.Fatalf("a second press quoted the request twice: %q", again)
	}
	if got := agentFollowUpComposerValue("keep me", "  "); got != "keep me" {
		t.Fatalf("a task with no request text must not touch the draft: %q", got)
	}
}

func TestIssue3975_DraftSurvivesTheRoundTripButNeverCrossesIdentitiesOrOverwrites(t *testing.T) {
	var store agentComposerDrafts
	store.Remember("t1", "u1", "How many days carry over?")
	if text, ok := store.Restore("t1", "u1", ""); !ok || text != "How many days carry over?" {
		t.Fatalf("empty composer after the round trip: %q %v", text, ok)
	}
	if text, ok := store.Restore("t1", "u1", "  "); !ok || text == "" {
		t.Fatalf("a blank box is empty: %q %v", text, ok)
	}
	if _, ok := store.Restore("t1", "u1", "something newer"); ok {
		t.Fatal("a draft replaced text the person had already typed")
	}
	if _, ok := store.Restore("t1", "u2", ""); ok {
		t.Fatal("a draft crossed to another person")
	}
	if _, ok := store.Restore("t1", "u1", ""); ok {
		t.Fatal("a draft survived a mismatch that should have dropped it")
	}
	store.Remember("t1", "u1", "typed again")
	store.Remember("t1", "u1", "")
	if _, ok := store.Restore("t1", "u1", ""); ok {
		t.Fatal("a cleared box got its old text back")
	}
	store.Remember("t1", "u1", "to send")
	store.Clear()
	if _, ok := store.Restore("t1", "u1", ""); ok {
		t.Fatal("a sent question came back")
	}
}
