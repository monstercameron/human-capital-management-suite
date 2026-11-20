package main

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func issue3953State(t *testing.T) (*chatState, journeyclient.Config, uint64) {
	t.Helper()
	state := newChatStateForTest(t)
	state.mutate(func(m *chatui.Model) {
		m.SelectedID, m.Locale = "ch-general", "en-US"
		m.Conversations = chatbug068Rooms()
	})
	_, _, epoch := state.draftsForWrite()
	return state, journeyclient.Config{Tenant: "northwind", Subject: "avery", Locale: "en-US"}, epoch
}

// persist stands for a debounced write that reached the account.
func issue3953Persist(state *chatState, cfg journeyclient.Config) {
	drafts, revision, epoch := state.draftsForWrite()
	_ = drafts
	state.markDraftsPersisted(revision, cfg, epoch)
}

// Tab A saved its draft; tab B saved a different one for the same conversation.
// When A reads the account again it keeps its own text, as before, and now also
// keeps B's version where the person can choose, instead of dropping it.
func TestIssue3953_AnotherTabsDraftIsKeptWhenThisTabReadsTheAccount(t *testing.T) {
	state, cfg, _ := issue3953State(t)
	state.setDraft("ch-general", "A text")
	issue3953Persist(state, cfg)

	state.loadDrafts(map[string]string{"ch-general": "B text"})

	model := state.snapshot()
	if model.Draft != "A text" || state.draft("ch-general") != "A text" {
		t.Fatalf("the composer changed: draft %q stored %q", model.Draft, state.draft("ch-general"))
	}
	if got := model.DraftConflicts["ch-general"]; got != "B text" {
		t.Fatalf("the other tab's draft was not kept: %+v", model.DraftConflicts)
	}
	state.resolveDraftConflict("ch-general")
	if got := state.snapshot().DraftConflicts; len(got) != 0 {
		t.Fatalf("a settled conflict is still offered: %+v", got)
	}
}

// A's write loses the race (revision conflict) after B saved: the rebase keeps
// A's text, as the earlier fix did, and now B's version too.
func TestIssue3953_RevisionConflictKeepsBothVersions(t *testing.T) {
	state, cfg, epoch := issue3953State(t)
	state.setDraft("ch-general", "A1")
	issue3953Persist(state, cfg)
	state.setDraft("ch-general", "A2")

	drafts, _, current := state.rebaseDrafts(map[string]string{"ch-general": "B text"}, cfg, epoch)
	if !current || drafts["ch-general"] != "A2" {
		t.Fatalf("this tab's text was replaced: current %t %+v", current, drafts)
	}
	if got := state.snapshot().DraftConflicts["ch-general"]; got != "B text" {
		t.Fatalf("the other tab's draft was dropped silently: %q", got)
	}
}

// A conflict is not invented: the account's copy that is this tab's own older
// write, another tab clearing or sending the draft, or a change to something
// other than this conversation's draft are all ordinary.
func TestIssue3953_NoConflictForTheTabsOwnOlderWriteOrAClear(t *testing.T) {
	state, cfg, epoch := issue3953State(t)
	state.setDraft("ch-general", "A1")
	issue3953Persist(state, cfg)
	state.setDraft("ch-general", "A2")
	state.rebaseDrafts(map[string]string{"ch-general": "A1"}, cfg, epoch)
	if got := state.snapshot().DraftConflicts; len(got) != 0 {
		t.Fatalf("this tab's own older write was reported as another tab's: %+v", got)
	}
	state.rebaseDrafts(map[string]string{}, cfg, epoch)
	if got := state.snapshot().DraftConflicts; len(got) != 0 {
		t.Fatalf("another tab clearing the draft was reported as a conflict: %+v", got)
	}
	if state.draft("ch-general") != "A2" {
		t.Fatalf("this tab's text was lost: %q", state.draft("ch-general"))
	}
}

// Tabs editing different conversations never conflict; the other conversation's
// draft is simply adopted, and a conflict is kept per conversation.
func TestIssue3953_DifferentConversationsDoNotConflict(t *testing.T) {
	state, cfg, epoch := issue3953State(t)
	state.setDraft("ch-general", "A in general")
	issue3953Persist(state, cfg)
	state.rebaseDrafts(map[string]string{"ch-general": "A in general", "ch-random": "B in random"}, cfg, epoch)
	if got := state.snapshot().DraftConflicts; len(got) != 0 {
		t.Fatalf("edits to different conversations conflicted: %+v", got)
	}
	if state.draft("ch-random") != "B in random" {
		t.Fatalf("the other conversation's draft was not adopted: %q", state.draft("ch-random"))
	}
	state.setDraft("ch-random", "A in random")
	issue3953Persist(state, cfg)
	// Another tab then replaced this tab's saved text in a conversation this tab
	// is not showing: the newer text is the draft, and the replaced text waits
	// as the other version for when the conversation is opened.
	state.loadDrafts(map[string]string{"ch-random": "B2 in random"})
	got := state.snapshot().DraftConflicts
	if len(got) != 1 || got["ch-random"] != "A in random" || state.draft("ch-random") != "B2 in random" {
		t.Fatalf("the replaced text was not kept for its own conversation: %+v draft %q", got, state.draft("ch-random"))
	}
	if state.snapshot().DraftConflicts["ch-general"] != "" {
		t.Fatal("the open conversation was given another conversation's text")
	}
}

// A new session (sign out, another person) starts with no other versions.
func TestIssue3953_NewSessionStartsClean(t *testing.T) {
	state, cfg, _ := issue3953State(t)
	state.setDraft("ch-general", "A text")
	issue3953Persist(state, cfg)
	state.loadDrafts(map[string]string{"ch-general": "B text"})
	state.reset(nil, cfg, nil)
	if got := state.snapshot().DraftConflicts; len(got) != 0 {
		t.Fatalf("a new session inherited %+v", got)
	}
}

// Issue 3950: a failed write is said to the person only when there are edits
// that exist nowhere else, and the saying ends when a write reaches the account.
func TestIssue3950_FailedWriteIsSaidUntilTheEditsAreSaved(t *testing.T) {
	state, cfg, _ := issue3953State(t)
	if state.noteDraftSaveFailed() || state.snapshot().DraftUnsaved {
		t.Fatal("a failed write with nothing to lose was reported")
	}
	state.setDraft("ch-general", "typed during the outage")
	if !state.draftsUnsaved() || !state.noteDraftSaveFailed() || !state.snapshot().DraftUnsaved {
		t.Fatal("edits that never reached the account were not reported")
	}
	state.clearDraftSaveFailure()
	if !state.snapshot().DraftUnsaved {
		t.Fatal("the report was withdrawn before the edits were saved")
	}
	issue3953Persist(state, cfg)
	state.clearDraftSaveFailure()
	if state.snapshot().DraftUnsaved || state.draftsUnsaved() {
		t.Fatal("the report stayed after the edits were saved")
	}
}

// Issue 3950: after a failed write the writer tries again by itself, waiting
// longer each time, and a newer write or a success ends the run.
func TestIssue3950_WriterRetriesWithBackoffUntilItSucceeds(t *testing.T) {
	var delays []time.Duration
	var fire []func()
	schedule := func(d time.Duration, f func()) debounceTimer {
		delays = append(delays, d)
		timer := &chatTestTimer{fire: f}
		fire = append(fire, f)
		return timer
	}
	writes := 0
	writer := newChatDraftWriter(schedule, 1500*time.Millisecond, func() { writes++ })
	for i := 0; i < 6; i++ {
		writer.Retry()
		last := fire[len(fire)-1]
		last()
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	if len(delays) != len(want) || writes != 6 {
		t.Fatalf("delays %v writes %d", delays, writes)
	}
	for i := range want {
		if delays[i] != want[i] {
			t.Errorf("retry %d waited %v, want %v", i+1, delays[i], want[i])
		}
	}
	writer.Succeeded()
	if writer.Failures() != 0 {
		t.Fatalf("failures after a success = %d", writer.Failures())
	}
	writer.Retry()
	if delays[len(delays)-1] != 2*time.Second {
		t.Errorf("the wait did not start over after a success: %v", delays[len(delays)-1])
	}
	// A keystroke after a failure replaces the retry rather than doubling it.
	pending := len(fire)
	writer.Schedule()
	writer.Retry()
	if len(fire) != pending+1 {
		t.Errorf("a retry was scheduled on top of a newer write")
	}
}
