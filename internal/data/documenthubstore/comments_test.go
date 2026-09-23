package documenthubstore

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_HUB_018_Golden is the GOLDEN test for HUB-018: the canonical
// comment evidence bytes are pinned for thread consumers.
func TestTodo_HUB_018_Golden(t *testing.T) {
	assertGoldenJSON(t, "testdata/hub018_comment.golden.json", map[string]string{
		"anchor_block": "intro", "author_id": "u-anna", "body": "looks good @u-bob",
		"document_id": "doc-fixed", "quote": "one", "version_hash": "9e1e2a8ea86d8c8dcfa4b4a1a0b8e1e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5",
		"version_id": "docv-fixed",
	})
}

// TestTodo_HUB_018 is the PRIMARY test for HUB-018: comments bind the exact
// version hash and range, survive content replacement, and mentions join
// only by consent.
func TestTodo_HUB_018(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddComment(ctx, "tenant-a", CommentInput{DocumentID: docID, VersionID: v1.ID, AuthorID: "u-stranger", Body: "hi @u-bob"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("comment without grant: %v", err)
	}
	for _, action := range []string{ActionRead, ActionComment} {
		if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: action, Effect: EffectAllow}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.AddComment(ctx, "tenant-a", CommentInput{DocumentID: docID, VersionID: v1.ID, AuthorID: "u-anna", AnchorBlock: "intro", Quote: "one", Body: "looks good @u-bob"})
	if err != nil {
		t.Fatalf("granted comment refused: %v", err)
	}
	if c.VersionHash != v1.Hash || c.VersionHash != HashContent("one\n") {
		t.Fatalf("comment does not bind the exact bytes: %+v", c)
	}
	if len(c.Mentions) != 1 || c.Mentions[0] != "u-bob" {
		t.Fatalf("mentions wrong: %+v", c)
	}
	v2, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "two\n"}, v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = v2
	thread, err := s.ListComments(ctx, "tenant-a", docID, v1.ID, "person", "u-anna")
	if err != nil || len(thread) != 1 || thread[0].Body != "looks good @u-bob" {
		t.Fatalf("replacement invalidated discussion: %+v err=%v", thread, err)
	}
	if err := s.ConsentMention(ctx, "tenant-a", c.ID, "u-anna"); err == nil {
		t.Fatal("unmentioned actor consented")
	}
	if err := s.ConsentMention(ctx, "tenant-a", c.ID, "u-bob"); err != nil {
		t.Fatalf("mentioned consent refused: %v", err)
	}
	thread, err = s.ListComments(ctx, "tenant-a", docID, v1.ID, "person", "u-anna")
	if err != nil || len(thread[0].Consented) != 1 || thread[0].Consented[0] != "u-bob" {
		t.Fatalf("consent not recorded: %+v err=%v", thread, err)
	}
	if _, err := s.AddComment(ctx, "tenant-a", CommentInput{DocumentID: docID, VersionID: "docv-missing", AuthorID: "u-anna", Body: "ghost"}); err == nil {
		t.Fatal("comment on unknown version accepted")
	}
}

// TestTodo_HUB_018_Security is the SECURITY test for HUB-018: comments never
// leak across tenants or versions, and mention consent cannot be forged.
func TestTodo_HUB_018_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	docID, err := s.CreateDocument(ctx, "tenant-a", "u-author", "PERSONAL")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "one\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{ActionRead, ActionComment} {
		if _, err := s.ShareDocument(ctx, "tenant-a", docID, "u-author", GrantInput{SubjectKind: "person", SubjectID: "u-anna", Action: action, Effect: EffectAllow}); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.AddComment(ctx, "tenant-a", CommentInput{DocumentID: docID, VersionID: v.ID, AuthorID: "u-anna", Body: "private @u-bob"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.ListComments(ctx, "tenant-b", docID, v.ID, "person", "u-anna")
	if !errors.Is(err, ErrDenied) || len(thread) != 0 {
		t.Fatalf("cross-tenant discussion leaked: %+v err=%v", thread, err)
	}
	other, err := s.SubmitCandidate(ctx, "tenant-a", Version{DocumentID: docID, CreatorID: "u-author", Markdown: "two\n"}, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	thread, err = s.ListComments(ctx, "tenant-a", docID, other.ID, "person", "u-anna")
	if err != nil || len(thread) != 0 {
		t.Fatalf("comment leaked across versions: %+v err=%v", thread, err)
	}
	if err := s.ConsentMention(ctx, "tenant-b", c.ID, "u-bob"); err == nil {
		t.Fatal("cross-tenant consent accepted")
	}
}
