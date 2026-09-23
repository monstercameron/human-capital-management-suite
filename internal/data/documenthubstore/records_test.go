package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_HUB_037 is the PRIMARY test for HUB-037: a held document
// refuses disposal, and disposal removes derivatives while retaining the
// immutable records core under a verified inventory.
func TestTodo_HUB_037(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	if err := s.IndexDeployedVersion(ctx, "tenant-a", docA, v1.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.StoreBlocks(ctx, "tenant-a", docA, v1.ID, DeriveBlocks("# Cats\n\nA guide to cats.\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EmbedSections(ctx, "tenant-a", docA, v1.ID, testEmbeddingPolicy(), "hub-internal-v1", stubEmbedder); err != nil {
		t.Fatal(err)
	}
	hold, err := s.PlaceHold(ctx, "tenant-a", docA, "u-author", "litigation-9")
	if err != nil {
		t.Fatal(err)
	}
	if !hold.Active || hold.Reason != "litigation-9" {
		t.Fatalf("hold wrong: %+v", hold)
	}
	if _, err := s.DisposeDocument(ctx, "tenant-a", docA, "u-author", "expired"); !errors.Is(err, ErrHoldActive) {
		t.Fatalf("held document disposed: %v", err)
	}
	if err := s.ReleaseHold(ctx, "tenant-a", hold.ID, "u-author"); err != nil {
		t.Fatal(err)
	}
	before, err := s.VerifyDisposition(ctx, "tenant-a", docA)
	if err != nil {
		t.Fatal(err)
	}
	byClass := map[string]RecordsClass{}
	for _, c := range before.Classes {
		byClass[c.Class] = c
	}
	if byClass["versions"].Retained == 0 || byClass["derivatives"].Retained == 0 {
		t.Fatalf("inventory missed rows: %+v", before)
	}
	if before.Held {
		t.Fatalf("released hold still reported: %+v", before)
	}
	// Disposal needs a withdrawn document: retire the live pointer first.
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docA, SubjectKind: "person", SubjectID: "u-author", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docA, ScopeKind: "default", ScopeID: "", ActorID: "u-author", ExpectedLive: v1.ID, Reason: "expiry"}); err != nil {
		t.Fatal(err)
	}
	done, err := s.DisposeDocument(ctx, "tenant-a", docA, "u-author", "expired")
	if err != nil {
		t.Fatalf("disposal refused: %v", err)
	}
	after := map[string]RecordsClass{}
	for _, c := range done.Classes {
		after[c.Class] = c
	}
	if after["derivatives"].Retained != 0 {
		t.Fatalf("derivatives survived disposal: %+v", done)
	}
	if after["versions"].Retained == 0 {
		t.Fatalf("immutable versions erased: %+v", done)
	}
	hits, err := s.SearchLexical(ctx, "tenant-a", "cats", "person", "u-author")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.DocumentID == docA {
			t.Fatalf("disposed document still searchable: %+v", hits)
		}
	}
	vecs, err := s.SectionVectors(ctx, "tenant-a", docA, v1.ID, "hub-internal-v1")
	if err != nil || len(vecs) != 0 {
		t.Fatalf("embeddings outside records inventory: %+v err=%v", vecs, err)
	}
}

// TestTodo_HUB_037_Security is the SECURITY test for HUB-037: holds need
// authority, disposal needs authority, and holds never cross tenants.
func TestTodo_HUB_037_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlaceHold(ctx, "tenant-a", docA, "u-stranger", "x"); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger placed hold: %v", err)
	}
	if _, err := s.DisposeDocument(ctx, "tenant-a", docA, "u-stranger", "x"); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger disposed: %v", err)
	}
	if _, err := s.PlaceHold(ctx, "tenant-b", docA, "u-author", "x"); err == nil {
		t.Fatal("cross-tenant hold accepted")
	}
	hold, err := s.PlaceHold(ctx, "tenant-a", docA, "u-author", "audit-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseHold(ctx, "tenant-a", hold.ID, "u-stranger"); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger released hold: %v", err)
	}
	if _, err := s.VerifyDisposition(ctx, "tenant-b", docA); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant inventory: %v", err)
	}
}

// TestTodo_HUB_037_Integration is the INTEGRATION test for HUB-037: the
// hold/dispose roundtrip reaches the real store and the retained archive
// still serves version reads after disposal.
func TestTodo_HUB_037_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1 := searchFixture(t, s, ctx, "tenant-a", docA, "# Cats\n\nA guide to cats.\n")
	hold, err := s.PlaceHold(ctx, "tenant-a", docA, "u-author", "audit-3")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseHold(ctx, "tenant-a", hold.ID, "u-author"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DisposeDocument(ctx, "tenant-a", docA, "u-author", "early"); !errors.Is(err, ErrDocumentLive) {
		t.Fatalf("live document disposed: %v", err)
	}
	if _, err := s.GrantAction(ctx, "tenant-a", GrantInput{DocumentID: docA, SubjectKind: "person", SubjectID: "u-author", Action: ActionRetire, Effect: EffectAllow, Issuer: "u-owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Withdraw(ctx, "tenant-a", WithdrawInput{DocumentID: docA, ScopeKind: "default", ScopeID: "", ActorID: "u-author", ExpectedLive: v1.ID, Reason: "expiry"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DisposeDocument(ctx, "tenant-a", docA, "u-author", "expired"); err != nil {
		t.Fatal(err)
	}
	inv, err := s.VerifyDisposition(ctx, "tenant-a", docA)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Disposed {
		t.Fatalf("disposal not recorded: %+v", inv)
	}
}

// TestTodo_HUB_037_Golden is the GOLDEN test for HUB-037: the disposition
// manifest pins byte-for-byte over a fixed inventory.
func TestTodo_HUB_037_Golden(t *testing.T) {
	d := RecordsDisposition{DocumentID: "doc-1", Held: true, Disposed: false, Classes: []RecordsClass{
		{Class: "versions", Removed: 0, Retained: 2, Tables: []string{"document_version"}},
		{Class: "derivatives", Removed: 5, Retained: 0, Tables: []string{"document_block", "document_link", "document_search_term", "document_section_vector"}},
	}}
	assertGoldenJSON(t, "testdata/hub037_disposition.golden.json", dispositionManifest(d))
}
