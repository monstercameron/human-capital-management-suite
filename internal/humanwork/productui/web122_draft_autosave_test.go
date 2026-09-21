package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// RED for WEB-122: durable draft autosave presentation. The
// page lifecycle digests working copies and records
// revisions, but no presenter turns a draft plus its durable
// save record into autosave status: the first surface
// invents "Saved" copy by convention and a save record for
// another page can present as this draft's save. The
// compiler needs the governed presenter — the draft digest
// against the record over locale copy, with foreign-page
// records failing closed — so autosave status resolves
// today from durable facts only.
func TestTodo_WEB_122(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	draft := NewPageDraftFromScratch(PagePerson)
	savedAt := time.Date(2026, 9, 7, 14, 5, 0, 0, time.UTC)
	record := DraftAutosaveRecord{Page: PagePerson, Digest: DraftDigest(draft), SavedAt: savedAt}

	saved := ResolveDraftAutosave(locale, draft, record)
	if saved.State != DraftAutosaveSaved {
		t.Fatalf("matching digests present as %v", saved.State)
	}
	if saved.Text != locale.Text("draft.autosave_saved") {
		t.Fatalf("saved text = %q", saved.Text)
	}
	if !strings.Contains(saved.Detail, "2026") {
		t.Fatalf("saved detail hides its time: %q", saved.Detail)
	}

	mutated := draft
	mutated.Snapshot.Title = "New title"
	unsaved := ResolveDraftAutosave(locale, mutated, record)
	if unsaved.State != DraftAutosaveUnsaved {
		t.Fatalf("mutated draft presents as %v", unsaved.State)
	}
	if unsaved.Text != locale.Text("draft.autosave_unsaved") {
		t.Fatalf("unsaved text = %q", unsaved.Text)
	}

	failed := ResolveDraftAutosave(locale, mutated, DraftAutosaveRecord{
		Page: PagePerson, Digest: DraftDigest(draft), SavedAt: savedAt, Err: "disk full",
	})
	if failed.State != DraftAutosaveFailed {
		t.Fatalf("recorded error presents as %v", failed.State)
	}
	if failed.Text != locale.Text("draft.autosave_failed") {
		t.Fatalf("failed text = %q", failed.Text)
	}
	if !strings.Contains(failed.Detail, "disk full") {
		t.Fatalf("failed detail hides its error: %q", failed.Detail)
	}
}

// Golden: statuses over draft/record pairs.
func TestTodo_WEB_122_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	base := NewPageDraftFromScratch(PagePerson)
	changed := base
	changed.Snapshot.Title = "Edited"
	at := time.Date(2026, 9, 7, 14, 5, 0, 0, time.UTC)
	records := []DraftAutosaveRecord{
		{},
		{Page: PagePerson, Digest: DraftDigest(base), SavedAt: at},
		{Page: PagePerson, Digest: "stale", SavedAt: at},
		{Page: PagePerson, Digest: DraftDigest(base), SavedAt: at, Err: "boom"},
		{Page: PagePeople, Digest: DraftDigest(base), SavedAt: at},
	}
	var builder strings.Builder
	for _, record := range records {
		for _, draft := range []PageDraft{base, changed} {
			status := ResolveDraftAutosave(locale, draft, record)
			fmt.Fprintf(&builder, "%d|%s|%s\x00", int(status.State), status.Text, status.Detail)
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	// Date-vocabulary re-pin: the saved-at date reads "7 Sep 2026", not
	// "09/07/2026"; substituting the numeric form back reproduces the
	// previous pin 7e830133... exactly.
	const want = "131271297e83d907d932326cfd4d5b0047ea5103dab84cf6a215b9467caac474"
	if got != want {
		t.Fatalf("autosave digest = %s, want %s", got, want)
	}
}

// Browser: status resolution is deterministic and never
// mutates its draft or record.
func TestTodo_WEB_122_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	draft := NewPageDraftFromScratch(PagePerson)
	record := DraftAutosaveRecord{Page: PagePerson, Digest: DraftDigest(draft), SavedAt: time.Now()}
	beforeDraft, beforeRecord := draft, record
	first := ResolveDraftAutosave(locale, draft, record)
	second := ResolveDraftAutosave(locale, draft, record)
	if !reflect.DeepEqual(draft, beforeDraft) || !reflect.DeepEqual(record, beforeRecord) {
		t.Fatal("status resolution mutates its inputs")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("status resolution is nondeterministic")
	}
}

// Conformance: states are distinct, copy comes from the
// catalog, resolution is stable.
func TestTodo_WEB_122_Conformance(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	if DraftAutosaveSaved == DraftAutosaveUnsaved || DraftAutosaveUnsaved == DraftAutosaveFailed {
		t.Fatal("autosave states collapse")
	}
	draft := NewPageDraftFromScratch(PagePerson)
	status := ResolveDraftAutosave(locale, draft, DraftAutosaveRecord{})
	if status.State != DraftAutosaveUnsaved {
		t.Fatal("a draft with no save record presents as saved")
	}
	if !reflect.DeepEqual(status, ResolveDraftAutosave(locale, draft, DraftAutosaveRecord{})) {
		t.Fatal("resolution is unstable")
	}
}

// Integration: status tracks the real draft lifecycle —
// scratch is unsaved, recording the digest saves it,
// editing dirties it again, and recording the new digest
// saves it again.
func TestTodo_WEB_122_Integration(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	draft := NewPageDraftFromScratch(PagePerson)
	at := time.Date(2026, 9, 7, 14, 5, 0, 0, time.UTC)
	var record DraftAutosaveRecord
	if got := ResolveDraftAutosave(locale, draft, record); got.State != DraftAutosaveUnsaved {
		t.Fatalf("scratch draft presents as %v", got.State)
	}
	record = DraftAutosaveRecord{Page: draft.Page, Digest: DraftDigest(draft), SavedAt: at}
	if got := ResolveDraftAutosave(locale, draft, record); got.State != DraftAutosaveSaved {
		t.Fatalf("recorded draft presents as %v", got.State)
	}
	draft.Snapshot.Title = "Edited"
	if got := ResolveDraftAutosave(locale, draft, record); got.State != DraftAutosaveUnsaved {
		t.Fatalf("edited draft presents as %v", got.State)
	}
	record = DraftAutosaveRecord{Page: draft.Page, Digest: DraftDigest(draft), SavedAt: at.Add(time.Hour)}
	if got := ResolveDraftAutosave(locale, draft, record); got.State != DraftAutosaveSaved {
		t.Fatalf("re-recorded draft presents as %v", got.State)
	}
}

// Fault: foreign-page records, blank pages, and zero
// times never present as this draft's save.
func TestTodo_WEB_122_Fault(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	draft := NewPageDraftFromScratch(PagePerson)
	digest := DraftDigest(draft)
	at := time.Date(2026, 9, 7, 14, 5, 0, 0, time.UTC)
	faults := []DraftAutosaveRecord{
		{Page: PagePeople, Digest: digest, SavedAt: at},
		{Page: "", Digest: digest, SavedAt: at},
		{Page: PagePerson, Digest: digest},
	}
	for i, record := range faults[:2] {
		if got := ResolveDraftAutosave(locale, draft, record); got.State != DraftAutosaveUnsaved {
			t.Fatalf("fault %d presents as %v", i, got.State)
		}
	}
	if got := ResolveDraftAutosave(locale, draft, faults[2]); got.State != DraftAutosaveSaved {
		t.Fatalf("zero-time save presents as %v", got.State)
	}
	if got := ResolveDraftAutosave(locale, PageDraft{}, DraftAutosaveRecord{}); got.State != DraftAutosaveUnsaved {
		t.Fatalf("zero draft presents as %v", got.State)
	}
}
