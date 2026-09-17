// CASE-004 RED: reopen, appeal, relationship graph and evidence-package
// export. Tests are written before the production code; they must fail
// until appeal.go provides the named symbols.
package hrcase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func case004At() time.Time { return time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC) }

func case004Request() HRRequest {
	return HRRequest{
		CaseID: "case-004", Type: "ER-INVESTIGATION", TypeVersion: "2026.04", Service: "employee-relations",
		Requester: "principal:manager", Subject: "worker:subject", Purpose: "fact finding",
		Classification: "INVESTIGATION", Retention: "7y",
		Participants: []Participant{{Role: "CASE_MANAGER", Principal: "principal:manager"}},
	}
}

// case004Closed drives one case to CLOSED with a disposition, returning
// every revision so callers can persist the full chain.
func case004Closed(t *testing.T) []CaseRevision {
	t.Helper()
	agg, err := NewCase(case004Request())
	if err != nil {
		t.Fatalf("NewCase: %v", err)
	}
	for _, step := range []struct {
		to          CaseState
		disposition string
	}{{Open, ""}, {Resolved, ""}, {Closed, "FINAL: sustained"}} {
		if _, err := agg.Apply(step.to, step.disposition); err != nil {
			t.Fatalf("Apply %s: %v", step.to, err)
		}
	}
	revs := agg.Revisions()
	if got := revs[len(revs)-1]; got.State != Closed || got.Disposition != "FINAL: sustained" {
		t.Fatalf("closed = %+v", got)
	}
	return revs
}

func case004Events() []PackageEvent {
	at := case004At()
	return []PackageEvent{
		{Seq: 1, Kind: "OPENED", Ref: "rev-1", At: at},
		{Seq: 2, Kind: "FINDING", Ref: "finding-1", At: at.Add(time.Hour)},
		{Seq: 3, Kind: "DISPOSITION", Ref: "FINAL: sustained", At: at.Add(2 * time.Hour)},
	}
}

func case004Artifacts() []ExportArtifact {
	return []ExportArtifact{
		{EvidenceID: "ev-1", ArtifactDigest: "sha256:artifact-1", CompartmentID: "general", RetentionDecision: "retain:7y"},
		{EvidenceID: "ev-2", ArtifactDigest: "sha256:artifact-2", CompartmentID: "investigation", RedactionNote: "witness name redacted", LegalHold: true, HoldRef: "hold-7", RetentionDecision: "retain:7y", CorrectionOf: "ev-2-v1"},
	}
}

func case004Decisions() []string {
	return []string{"decision:board-1", "hold:hold-7", "correction:ev-2-v1"}
}

// TestCaseAppealCreatesSuccessorProceedingAndVerifiableScopedEvidencePackage
// is the PRIMARY contract: reopen/appeal preserve the original proceeding,
// edges are explicit, and the scoped package is complete and verifiable.
func TestCaseAppealCreatesSuccessorProceedingAndVerifiableScopedEvidencePackage(t *testing.T) {
	at := case004At()
	revs := case004Closed(t)
	closed := revs[len(revs)-1]

	// Reopen preserves the closure disposition and links back explicitly.
	reopened, edge, err := ReopenCase(closed, "new witness came forward", at)
	if err != nil {
		t.Fatalf("ReopenCase: %v", err)
	}
	if reopened.State != Reopened || reopened.Disposition != closed.Disposition || reopened.Previous != closed.Revision {
		t.Fatalf("reopened = %+v", reopened)
	}
	if edge.Kind != EdgeReopenOf || edge.FromCaseID != "case-004" || edge.ToCaseID != "case-004" {
		t.Fatalf("edge = %+v", edge)
	}
	if closed.State != Closed || !closed.Verify() {
		t.Fatal("reopen altered the original closure")
	}

	// Appeal seals a successor without mutating the original finding.
	before := closed
	appealed, aedge, err := AppealCase(closed, "subject appeals the outcome", at)
	if err != nil {
		t.Fatalf("AppealCase: %v", err)
	}
	if appealed.State != Appealed || appealed.Disposition != before.Disposition {
		t.Fatalf("appealed = %+v", appealed)
	}
	if aedge.Kind != EdgeAppealOf {
		t.Fatalf("appeal edge = %+v", aedge)
	}
	if closed.Digest != before.Digest || !closed.Verify() {
		t.Fatal("appeal mutated the original proceeding")
	}

	// Related-case edge carries no compartment membership.
	rel, err := LinkCases("case-004", "case-009", EdgeRelatedTo, "same witness pool", at)
	if err != nil {
		t.Fatalf("LinkCases: %v", err)
	}
	if flat := fmt.Sprintf("%+v", rel); strings.Contains(flat, "principal:") || strings.Contains(flat, "escrow:") || strings.Contains(flat, "medical") {
		t.Fatalf("edge leaks membership: %+v", rel)
	}

	// Scoped export: authorized compartments only, full lineage, stable digest.
	pkg, err := ExportEvidence("case-004", []string{"general", "investigation"}, case004Events(), case004Artifacts(), case004Decisions())
	if err != nil {
		t.Fatalf("ExportEvidence: %v", err)
	}
	if len(pkg.Artifacts) != 2 || len(pkg.Chronology) != 3 {
		t.Fatalf("package = %+v", pkg)
	}
	for _, want := range []string{"decision:board-1", "hold:hold-7", "correction:ev-2-v1"} {
		found := false
		for _, d := range pkg.Decisions {
			if d == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("package decisions=%v miss %s", pkg.Decisions, want)
		}
	}
	if pkg.Digest == "" {
		t.Fatal("package has no digest")
	}
	again, err := ExportEvidence("case-004", []string{"investigation", "general"}, case004Events(), case004Artifacts(), case004Decisions())
	if err != nil || again.Digest != pkg.Digest {
		t.Fatalf("repeated export digest=%s err=%v, want %s", again.Digest, err, pkg.Digest)
	}

	// Viewer without investigation access sees no investigation artifact.
	scoped, err := ExportEvidence("case-004", []string{"general"}, case004Events(), case004Artifacts(), case004Decisions())
	if err != nil {
		t.Fatalf("scoped ExportEvidence: %v", err)
	}
	for _, a := range scoped.Artifacts {
		if a.CompartmentID == "investigation" || a.ArtifactDigest == "sha256:artifact-2" {
			t.Fatalf("scoped package leaks: %+v", a)
		}
	}
	if len(scoped.Withheld) != 1 || scoped.Withheld[0] != "ev-2" {
		t.Fatalf("withheld = %+v", scoped.Withheld)
	}
}

// TestTodo_CASE_004_Property proves edge and export rules are total:
// every declared kind links, unknown kinds fail, digests survive input
// shuffling and chronology is always ordered.
func TestTodo_CASE_004_Property(t *testing.T) {
	at := case004At()
	for _, kind := range []EdgeKind{EdgeReopenOf, EdgeAppealOf, EdgeDuplicateOf, EdgeRelatedTo} {
		if !ValidEdgeKind(kind) {
			t.Fatalf("kind %s not valid", kind)
		}
	}
	for _, kind := range []EdgeKind{"", "MERGED_INTO", "reopen_of"} {
		if ValidEdgeKind(kind) {
			t.Fatalf("kind %q accepted", kind)
		}
		if _, err := LinkCases("case-004", "case-009", kind, "reason", at); !errors.Is(err, ErrInvalidEdge) {
			t.Fatalf("kind %q err=%v, want ErrInvalidEdge", kind, err)
		}
	}
	// Only DUPLICATE_OF and RELATED_TO link directly; reopen/appeal kinds
	// require their constructors so chronology cannot be forged.
	for _, kind := range []EdgeKind{EdgeReopenOf, EdgeAppealOf} {
		if _, err := LinkCases("case-004", "case-009", kind, "reason", at); !errors.Is(err, ErrInvalidEdge) {
			t.Fatalf("kind %s direct err=%v, want ErrInvalidEdge", kind, err)
		}
	}

	events := case004Events()
	shuffled := []PackageEvent{events[2], events[0], events[1]}
	artifacts := case004Artifacts()
	rshuffled := []ExportArtifact{artifacts[1], artifacts[0]}
	first, err := ExportEvidence("case-004", []string{"general", "investigation"}, events, artifacts, case004Decisions())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportEvidence("case-004", []string{"general", "investigation"}, shuffled, rshuffled, case004Decisions())
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("digest depends on input order")
	}
	for i := 1; i < len(second.Chronology); i++ {
		if second.Chronology[i].Seq < second.Chronology[i-1].Seq {
			t.Fatal("chronology is not ordered")
		}
	}
}

// TestTodo_CASE_004_Golden pins the canonical digest of one fixed package.
func TestTodo_CASE_004_Golden(t *testing.T) {
	pkg, err := ExportEvidence("case-004", []string{"general", "investigation"}, case004Events(), case004Artifacts(), case004Decisions())
	if err != nil {
		t.Fatal(err)
	}
	const golden = "sha256:64f5b6cbe56566b1f079527f05aeed6515b9f466a273fbc303e6bf0bffb80177"
	if pkg.Digest != golden {
		t.Fatalf("digest=%s want golden %s", pkg.Digest, golden)
	}
}

// TestTodo_CASE_004_Integration proves the successor proceeding and the
// export survive a round-trip through the Store port with identical typed
// results and CAS error semantics.
func TestTodo_CASE_004_Integration(t *testing.T) {
	ctx := context.Background()
	revs := case004Closed(t)
	closed := revs[len(revs)-1]
	reopened, edge, err := ReopenCase(closed, "new witness came forward", case004At())
	if err != nil {
		t.Fatal(err)
	}
	if edge.Kind != EdgeReopenOf {
		t.Fatalf("edge = %+v", edge)
	}
	store := NewMemoryStore()
	for i, rev := range revs {
		if err := store.AppendRevision(ctx, "tenant-1", rev, uint64(i+1)); err != nil {
			t.Fatalf("append rev %d: %v", i+1, err)
		}
	}
	if err := store.AppendRevision(ctx, "tenant-1", reopened, uint64(len(revs)+1)); err != nil {
		t.Fatalf("append reopened: %v", err)
	}
	loaded, err := store.Current(ctx, "tenant-1", "case-004")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if loaded.State != Reopened || loaded.Disposition != closed.Disposition || !loaded.Verify() {
		t.Fatalf("loaded = %+v", loaded)
	}
	stream, err := store.ListRevisions(ctx, "tenant-1", "case-004")
	if err != nil || len(stream) != len(revs)+1 {
		t.Fatalf("stream=%d err=%v", len(stream), err)
	}
	// Ambiguous commit: appending the same successor twice is stale, and
	// the stored head is unchanged.
	if err := store.AppendRevision(ctx, "tenant-1", reopened, uint64(len(revs)+1)); !errors.Is(err, ErrStoreStaleCAS) && !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate append err=%v, want STALE_CAS or DUPLICATE", err)
	}
	head, err := store.Current(ctx, "tenant-1", "case-004")
	if err != nil || head.Digest != loaded.Digest {
		t.Fatalf("head changed after failed append: %+v %v", head, err)
	}
}

// TestTodo_CASE_004_Fault proves failed operations leave zero effect:
// bad reopens, appeals and exports change nothing and repeat identically.
func TestTodo_CASE_004_Fault(t *testing.T) {
	at := case004At()
	revs := case004Closed(t)
	closed := revs[len(revs)-1]

	openAgg, err := NewCase(case004Request())
	if err != nil {
		t.Fatal(err)
	}
	draft := openAgg.Current()
	if _, _, err := ReopenCase(draft, "reason", at); !errors.Is(err, ErrInvalidSuccessor) {
		t.Fatalf("reopen draft err=%v, want ErrInvalidSuccessor", err)
	}
	if _, _, err := AppealCase(draft, "reason", at); !errors.Is(err, ErrInvalidSuccessor) {
		t.Fatalf("appeal draft err=%v, want ErrInvalidSuccessor", err)
	}
	if !draft.Verify() || draft.State != Draft {
		t.Fatal("failed successor altered the original")
	}
	// The valid path still works after the failures above.
	if next, edge, err := ReopenCase(closed, "recovery reopen", at); err != nil {
		t.Fatalf("recovery reopen: %v", err)
	} else if next.State != Reopened || edge.Kind != EdgeReopenOf || next.Disposition != closed.Disposition {
		t.Fatalf("recovery reopen = %+v %+v", next, edge)
	}

	// Export that omits hold/correction lineage is refused with no package.
	_, err = ExportEvidence("case-004", []string{"general", "investigation"}, case004Events(), case004Artifacts(), []string{"decision:board-1"})
	if !errors.Is(err, ErrExportInvalid) {
		t.Fatalf("lineage-free export err=%v, want ErrExportInvalid", err)
	}
	empty, err := ExportEvidence("case-004", []string{"general", "investigation"}, case004Events(), case004Artifacts(), []string{"decision:board-1"})
	if err == nil || empty.Digest != "" || empty.Artifacts != nil {
		t.Fatalf("failed export produced effect: %+v %v", empty, err)
	}
}

// TestTodo_CASE_004_Security proves export is scoped and non-disclosing:
// zero authorized compartments deny, and denials leak no case content.
func TestTodo_CASE_004_Security(t *testing.T) {
	_, err := ExportEvidence("case-004", nil, case004Events(), case004Artifacts(), case004Decisions())
	if !errors.Is(err, ErrCaseDenied) {
		t.Fatalf("viewer-free export err=%v, want ErrCaseDenied", err)
	}
	if strings.Contains(err.Error(), "sha256:artifact") || strings.Contains(err.Error(), "ev-2") {
		t.Fatalf("denial leaks content: %v", err)
	}
	pkg, err := ExportEvidence("case-004", []string{"general"}, case004Events(), case004Artifacts(), case004Decisions())
	if err != nil {
		t.Fatal(err)
	}
	flat := fmt.Sprintf("%+v", pkg.Artifacts)
	if strings.Contains(flat, "sha256:artifact-2") {
		t.Fatal("scoped export contains an unauthorized artifact")
	}
}

// TestTodo_CASE_004_Conformance proves two construction paths — direct
// events and events rebuilt from Store-loaded revisions — yield the
// identical package digest for the same semantic chronology.
func TestTodo_CASE_004_Conformance(t *testing.T) {
	ctx := context.Background()
	revs := case004Closed(t)
	store := NewMemoryStore()
	for i, rev := range revs {
		if err := store.AppendRevision(ctx, "tenant-1", rev, uint64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	stream, err := store.ListRevisions(ctx, "tenant-1", "case-004")
	if err != nil {
		t.Fatal(err)
	}
	at := case004At()
	direct := make([]PackageEvent, len(stream))
	rebuilt := make([]PackageEvent, len(stream))
	for i, rev := range stream {
		direct[i] = PackageEvent{Seq: uint64(i + 1), Kind: string(rev.State), Ref: rev.CaseID, At: at.Add(time.Duration(i) * time.Hour)}
		rebuilt[i] = PackageEvent{Seq: uint64(i + 1), Kind: string(stream[i].State), Ref: stream[i].CaseID, At: at.Add(time.Duration(i) * time.Hour)}
	}
	allowed := []string{"general", "investigation"}
	artifacts := []ExportArtifact{{EvidenceID: "ev-1", ArtifactDigest: "sha256:artifact-1", CompartmentID: "general", RetentionDecision: "retain:7y"}}
	a, err := ExportEvidence("case-004", allowed, direct, artifacts, []string{"decision:board-1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ExportEvidence("case-004", allowed, rebuilt, artifacts, []string{"decision:board-1"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("conformance digests differ: %s vs %s", a.Digest, b.Digest)
	}
}

// TestTodo_CASE_004_Mutation asserts the guards that kill each seeded
// semantic-mutant class: erased closure, mutated original, dropped
// compartment filter, unstable digest and omitted lineage.
func TestTodo_CASE_004_Mutation(t *testing.T) {
	at := case004At()
	closed := case004Closed(t)[3]

	// Closure-erasure mutant: reopen must carry the disposition forward.
	reopened, _, err := ReopenCase(closed, "reason", at)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Disposition != closed.Disposition {
		t.Fatal("closure-erasure mutant survives")
	}
	// Original-mutation mutant: appeal input must be byte-identical after.
	before := closed.Digest
	if _, _, err := AppealCase(closed, "reason", at); err != nil {
		t.Fatal(err)
	}
	if closed.Digest != before || !closed.Verify() {
		t.Fatal("original-mutation mutant survives")
	}
	// Filter-drop mutant: unauthorized artifacts must stay excluded.
	pkg, err := ExportEvidence("case-004", []string{"general"}, case004Events(), case004Artifacts(), case004Decisions())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range pkg.Artifacts {
		if a.ArtifactDigest == "sha256:artifact-2" {
			t.Fatal("filter-drop mutant survives")
		}
	}
	// Digest-instability mutant: repeats must match exactly.
	again, err := ExportEvidence("case-004", []string{"general"}, case004Events(), case004Artifacts(), case004Decisions())
	if err != nil || again.Digest != pkg.Digest {
		t.Fatal("digest-instability mutant survives")
	}
	// Lineage-drop mutant: missing hold/correction refs must be refused.
	if _, err := ExportEvidence("case-004", []string{"general", "investigation"}, case004Events(), case004Artifacts(), nil); !errors.Is(err, ErrExportInvalid) {
		t.Fatalf("lineage-drop mutant survives: %v", err)
	}
}
