package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-074: the page draft lifecycle. Revisions are immutable
// since WEB-073, but no draft exists anywhere: there is no mutable
// working copy bound to a base revision, no publish step recording a
// new revision, and no guard against publishing onto a moved base. The
// lifecycle needs pure draft mechanics — new from a base revision (or
// from scratch), free working-copy edits that never touch the log,
// publish recording the next revision, discard dropping the draft —
// with a stale base refusing to publish.
func TestTodo_WEB_074(t *testing.T) {
	snap := PageDefinitionSnapshot{Page: "home", Route: "/workspace/app/home", Label: "Home", SearchTerms: []string{"start"}}
	var log PageRevisionLog
	base, err := log.Record(snap.Page, snap, 1)
	if err != nil {
		t.Fatal(err)
	}

	// A draft binds its base revision and starts identical to it.
	draft := NewPageDraft(base)
	if draft.Page != "home" || draft.BaseVersion != 1 || draft.BaseDigest != base.Digest {
		t.Fatalf("draft binds no base: %+v", draft)
	}
	if !reflect.DeepEqual(draft.Snapshot, snap) {
		t.Fatal("draft working copy differs from its base")
	}

	// Working-copy edits never touch the log.
	draft.Snapshot.Label = "Start here"
	draft.Snapshot.SearchTerms = append(draft.Snapshot.SearchTerms, "welcome")
	if stored, _ := log.Revision("home", 1); !reflect.DeepEqual(stored.Snapshot.Label, "Home") {
		t.Fatal("working-copy edit reaches the log")
	}

	// Publishing records the edited snapshot as a new revision.
	published, err := PublishDraft(&log, draft, 2)
	if err != nil {
		t.Fatal(err)
	}
	if published.Version != 2 || published.Snapshot.Label != "Start here" {
		t.Fatalf("publish records %+v", published)
	}
	if latest, _ := log.Latest("home"); latest.Digest != published.Digest {
		t.Fatal("publish does not advance latest")
	}

	// Republishing the identical draft and version is idempotent.
	if _, err := PublishDraft(&log, draft, 2); err != nil {
		t.Fatalf("identical republish fails: %v", err)
	}

	// A draft whose base is no longer latest refuses to publish: someone
	// else's v3 advanced the lineage the stale draft cannot include.
	if _, err := log.Record("home", PageDefinitionSnapshot{Page: "home", Route: "/workspace/app/home", Label: "Raced"}, 3); err != nil {
		t.Fatal(err)
	}
	stale := NewPageDraft(base)
	stale.Snapshot.Label = "Stale edit"
	if _, err := PublishDraft(&log, stale, 4); err == nil {
		t.Fatal("stale-base publish is accepted")
	} else if !strings.Contains(err.Error(), "stale") && !strings.Contains(err.Error(), "base") {
		t.Fatalf("stale-base refusal names no base: %v", err)
	}

	// A from-scratch draft publishes without a base.
	scratch := NewPageDraftFromScratch("people")
	scratch.Snapshot = PageDefinitionSnapshot{Page: "people", Route: "/workspace/app/people", Label: "People"}
	if _, err := PublishDraft(&log, scratch, 1); err != nil {
		t.Fatalf("from-scratch publish fails: %v", err)
	}

	// A discarded draft leaves the log untouched.
	before := log.Pages()
	_ = NewPageDraft(base)
	if after := log.Pages(); !reflect.DeepEqual(before, after) {
		t.Fatal("discarded draft touches the log")
	}

	// A working copy retargeted at another page refuses to publish.
	retargeted := NewPageDraft(base)
	retargeted.Snapshot.Page = "people"
	if _, err := PublishDraft(&log, retargeted, 5); err == nil {
		t.Fatal("retargeted draft publishes")
	}
}

// Golden: draft digest stability across identical bases.
func TestTodo_WEB_074_Golden(t *testing.T) {
	snapshots := []PageDefinitionSnapshot{
		{Page: "home", Route: "/workspace/app/home", Label: "Home", SearchTerms: []string{"start"}},
		{Page: "people", Route: "/workspace/app/people", Label: "People", SearchTerms: []string{"directory"}},
	}
	var builder strings.Builder
	for _, snap := range snapshots {
		var log PageRevisionLog
		base, err := log.Record(snap.Page, snap, 1)
		if err != nil {
			t.Fatal(err)
		}
		first := NewPageDraft(base)
		second := NewPageDraft(base)
		builder.WriteString(string(snap.Page))
		builder.WriteString("\x00")
		builder.WriteString(DraftDigest(first))
		builder.WriteString("\x00")
		builder.WriteString(DraftDigest(second))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "84a560e174af1276394ddd2d7a1c1fd42b6d8f3b5b599869807cf3ac3646854c"
	if got != want {
		t.Fatalf("draft matrix digest = %s, want %s", got, want)
	}
}

// Browser: every registered definition drafts from v1, edits, and
// publishes v2 — deterministically.
func TestTodo_WEB_074_Browser(t *testing.T) {
	var log PageRevisionLog
	for _, definition := range PageDefinitions() {
		snap := SnapshotPageDefinition(definition)
		base, err := log.Record(definition.ID, snap, 1)
		if err != nil {
			t.Fatal(err)
		}
		draft := NewPageDraft(base)
		draft.Snapshot.Label += " (draft)"
		published, err := PublishDraft(&log, draft, 2)
		if err != nil {
			t.Fatalf("registry page %q does not publish: %v", definition.ID, err)
		}
		latest, ok := log.Latest(definition.ID)
		if !ok || latest.Digest != published.Digest {
			t.Fatalf("registry page %q latest is not the publish", definition.ID)
		}
	}
}

// Conformance: determinism, from-scratch publishing, refusal stability.
func TestTodo_WEB_074_Conformance(t *testing.T) {
	snap := PageDefinitionSnapshot{Page: "home", Route: "/workspace/app/home", Label: "Home"}
	var log PageRevisionLog
	base, err := log.Record(snap.Page, snap, 1)
	if err != nil {
		t.Fatal(err)
	}
	firstDraft, secondDraft := DraftDigest(NewPageDraft(base)), DraftDigest(NewPageDraft(base))
	if firstDraft != secondDraft {
		t.Fatal("draft digests are nondeterministic")
	}
	scratch := NewPageDraftFromScratch("studio")
	if scratch.BaseVersion != 0 || scratch.BaseDigest != "" {
		t.Fatalf("from-scratch draft binds a base: %+v", scratch)
	}
	if _, err := PublishDraft(&log, scratch, 1); err != nil {
		t.Fatalf("from-scratch publish fails: %v", err)
	}
	stale := NewPageDraft(base)
	if _, err := log.Record("home", PageDefinitionSnapshot{Page: "home", Label: "Raced"}, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishDraft(&log, stale, 3); err == nil {
		t.Fatal("moved-base publish is accepted")
	}
	// A from-scratch draft refuses a page that already carries versions:
	// forking lineage silently is never allowed.
	scratchHome := NewPageDraftFromScratch("home")
	if _, err := PublishDraft(&log, scratchHome, 9); err == nil {
		t.Fatal("from-scratch publish onto a versioned page is accepted")
	}
}

// Working-copy edits through the Record-return path must not reach the
// log: NewPageDraft owns its search terms even when the base aliases
// the stored revision backing array.
func TestTodo_WEB_074_DraftOwnsSearchTerms(t *testing.T) {
	var log PageRevisionLog
	base, err := log.Record("home", PageDefinitionSnapshot{Page: "home", SearchTerms: []string{"start"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	draft := NewPageDraft(base)
	draft.Snapshot.SearchTerms[0] = "MUTATED"
	stored, ok := log.Revision("home", 1)
	if !ok {
		t.Fatal("recorded revision missing")
	}
	if !reflect.DeepEqual(stored.Snapshot.SearchTerms, []string{"start"}) {
		t.Fatalf("working-copy edit reaches the log: %q", stored.Snapshot.SearchTerms)
	}
	if stored.Digest != base.Digest {
		t.Fatalf("stored digest moved: %s vs %s", stored.Digest, base.Digest)
	}
}
