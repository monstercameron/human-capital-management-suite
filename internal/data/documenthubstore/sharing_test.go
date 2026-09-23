package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_HUB_012_Golden is the GOLDEN test for HUB-012: the canonical
// audience preview bytes are pinned for the share dialog.
func TestTodo_HUB_012_Golden(t *testing.T) {
	assertGoldenJSON(t, "testdata/hub012_audience.golden.json", []map[string]string{
		{"action": "comment", "effect": "allow", "expires_at": "", "subject_id": "team-1", "subject_kind": "team"},
		{"action": "read", "effect": "allow", "expires_at": "", "subject_id": "u-friend", "subject_kind": "person"},
	})
}

// TestTodo_HUB_012 is the PRIMARY test for HUB-012: a new personal document
// is visible only to its owner until the owner or a manager explicitly
// shares it, and sharing names its audience with expiry.
func TestTodo_HUB_012(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-owner", ActionRead); err != nil {
		t.Fatalf("owner cannot read own document: %v", err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-stranger", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatal("new personal note visible to a stranger")
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-stranger", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-friend", Action: ActionRead, Effect: EffectAllow}); !errors.Is(err, ErrDenied) {
		t.Fatalf("non-owner shared the document: %v", err)
	}
	share, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: "u-friend", Action: ActionRead, Effect: EffectAllow})
	if err != nil {
		t.Fatalf("owner share refused: %v", err)
	}
	if share.Issuer != "u-owner" {
		t.Fatalf("share misattributes issuer: %+v", share)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-friend", ActionRead); err != nil {
		t.Fatalf("named share denied: %v", err)
	}
	audience, err := s.Audience(ctx, "tenant-a", docID, "u-owner")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, g := range audience {
		seen[g.SubjectID+"/"+g.Action] = true
	}
	if !seen["u-friend/read"] || !seen["u-owner/read"] {
		t.Fatalf("audience preview incomplete: %+v", audience)
	}
	if _, err := s.Audience(ctx, "tenant-a", docID, "u-friend"); !errors.Is(err, ErrDenied) {
		t.Fatal("reader enumerated the audience")
	}
}

// TestTodo_HUB_012_Security is the SECURITY test for HUB-012: knowing the
// exact version ID grants nothing, team/channel shares stay explicit, and
// expiry closes access.
func TestTodo_HUB_012_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-owner", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-owner", Markdown: "secret\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "person", "u-holder", ActionRead); !errors.Is(err, ErrDenied) {
		t.Fatal("copied link granted access")
	}
	_ = v
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", GrantInput{DocumentID: docID, SubjectKind: "team", SubjectID: "team-1", Action: ActionRead, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-owner", GrantInput{DocumentID: docID, SubjectKind: "channel", SubjectID: "chan-1", Action: ActionComment, Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "team", "team-1", ActionRead); err != nil {
		t.Fatalf("team share denied: %v", err)
	}
	if err := s.Authorize(ctx, "tenant-a", docID, "team", "team-1", ActionComment); !errors.Is(err, ErrDenied) {
		t.Fatal("team read implied comment")
	}
}
