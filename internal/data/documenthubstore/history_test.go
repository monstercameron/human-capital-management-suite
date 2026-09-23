package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_HUB_017 is the PRIMARY test for HUB-017: history needs the
// history capability, and versions more sensitive than current policy
// come back redacted with no title or bytes.
func TestTodo_HUB_017(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docA, CreatorID: "u-author", Title: "Secret plan", Classification: "RESTRICTED", Markdown: "secret\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docA, CreatorID: "u-author", Title: "Public plan", Classification: "INTERNAL", Markdown: "public\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadHistory(ctx, "tenant-a", docA, "person", "u-anna"); !errors.Is(err, ErrDenied) {
		t.Fatalf("history without capability: %v", err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadHistory(ctx, "tenant-a", docA, "person", "u-anna"); !errors.Is(err, ErrDenied) {
		t.Fatalf("history with read but no history grant: %v", err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionHistory, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ReadHistory(ctx, "tenant-a", docA, "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("history wrong: %+v", entries)
	}
	if entries[0].VersionID != v1.ID || !entries[0].Redacted {
		t.Fatalf("sensitive version not redacted: %+v", entries[0])
	}
	if entries[0].Title != "" || entries[0].Hash != "" {
		t.Fatalf("redacted version leaks title or bytes: %+v", entries[0])
	}
	if entries[0].Classification != "RESTRICTED" {
		t.Fatalf("redacted shell lost classification: %+v", entries[0])
	}
	if entries[1].VersionID != v2.ID || entries[1].Redacted || entries[1].Title != "Public plan" || entries[1].Hash != v2.Hash {
		t.Fatalf("current version wrong: %+v", entries[1])
	}
}

// TestTodo_HUB_017_Security is the SECURITY test for HUB-017: revoked
// capability, denied capability and cross-tenant reads all fail closed.
func TestTodo_HUB_017_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docA, CreatorID: "u-author", Markdown: "one\n"}, ""); err != nil {
		t.Fatal(err)
	}
	g, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionHistory, Effect: EffectAllow})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeGrant(ctx, "tenant-a", g.ID, "u-author"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadHistory(ctx, "tenant-a", docA, "person", "u-anna"); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked history capability reads: %v", err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-mallory", Action: ActionHistory, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-mallory", Action: ActionHistory, Effect: EffectDeny}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadHistory(ctx, "tenant-a", docA, "person", "u-mallory"); !errors.Is(err, ErrDenied) {
		t.Fatalf("denied history capability reads: %v", err)
	}
	if _, err := s.ReadHistory(ctx, "tenant-b", docA, "person", "u-anna"); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant history reads: %v", err)
	}
}

// TestTodo_HUB_017_Integration is the INTEGRATION test for HUB-017: the
// history roundtrip reaches the real store and uniform classifications
// show every version in full.
func TestTodo_HUB_017_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docA, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docA, CreatorID: "u-author", Title: "One", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docA, CreatorID: "u-author", Title: "Two", Markdown: "two\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docA, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: ActionHistory, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ReadHistory(ctx, "tenant-a", docA, "person", "u-anna")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Redacted || entries[1].Redacted {
		t.Fatalf("uniform history redacted: %+v", entries)
	}
	if entries[0].Hash != v1.Hash || entries[1].Hash != v2.Hash {
		t.Fatalf("history hashes wrong: %+v", entries)
	}
}

// TestTodo_HUB_017_Golden is the GOLDEN test for HUB-017: the history
// manifest pins byte-for-byte over fixed entries.
func TestTodo_HUB_017_Golden(t *testing.T) {
	entries := []HistoryEntry{
		{VersionID: "docv-1", Classification: "RESTRICTED", Redacted: true},
		{VersionID: "docv-2", Classification: "INTERNAL", Title: "Public plan", Hash: "9e1e", Redacted: false},
	}
	assertGoldenJSON(t, "testdata/hub017_history.golden.json", historyManifest(entries))
}
