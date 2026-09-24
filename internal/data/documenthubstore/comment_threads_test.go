package documenthubstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

const threadDoc = "# Leave policy\n\n## Accrual\n\nStaff accrue **six hours** per pay period. Unused hours carry over.\n\n## Requests\n\nSubmit requests two weeks ahead. Unused hours carry over only once.\n"

func TestCommentAnchorLocation(t *testing.T) {
	plain := PlainText(threadDoc)
	if !strings.Contains(plain, "Staff accrue six hours per pay period.") {
		t.Fatalf("plain text = %q", plain)
	}
	// The same quote occurs twice; context picks the second.
	start, end, block, ok := locateAnchor(threadDoc, plain, "Unused hours carry over", "Submit requests two weeks ahead.", "only once", "")
	if !ok || block != "requests" {
		t.Fatalf("second occurrence = %d %d %q %v", start, end, block, ok)
	}
	if got := string([]rune(plain)[start:end]); got != "Unused hours carry over" {
		t.Fatalf("offsets select %q", got)
	}
	first, _, block, ok := locateAnchor(threadDoc, plain, "Unused  hours\ncarry over", "", "", "accrual")
	if !ok || block != "accrual" || first >= start {
		t.Fatalf("hinted first occurrence = %d %q %v", first, block, ok)
	}
	if _, _, _, ok := locateAnchor(threadDoc, plain, "not in the text", "", "", ""); ok {
		t.Fatal("absent quote located")
	}
	if _, _, _, ok := locateAnchor(threadDoc, plain, "  ", "", "", ""); ok {
		t.Fatal("blank quote located")
	}
	if err := validateAnchorInput(CommentInput{Quote: strings.Repeat("x", MaxAnchorQuote+1)}); !errors.Is(err, ErrInvalidAnchor) {
		t.Fatalf("long quote = %v", err)
	}
	if err := validateAnchorInput(CommentInput{Quote: "x", Prefix: strings.Repeat("é", MaxAnchorContext+1)}); !errors.Is(err, ErrInvalidAnchor) {
		t.Fatalf("long prefix = %v", err)
	}
	if err := validateAnchorInput(CommentInput{Suffix: "context without a quote"}); !errors.Is(err, ErrInvalidAnchor) {
		t.Fatalf("context without quote = %v", err)
	}
	if utf8.RuneCountInString(plain) < end {
		t.Fatal("offsets past the text")
	}
}

func TestDocumentCommentThreads_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader, viewer, stranger = "tenant-threads", "owner-t", "reader-t", "viewer-t", "stranger-t"
	doc, v1, err := s.CreatePersonalDocument(ctx, tenant, owner, "Leave policy", threadDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, doc, owner, reader, RoleCommenter); err != nil {
		t.Fatal(err)
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, doc, owner, viewer, RoleViewer); err != nil {
		t.Fatal(err)
	}
	add := func(in CommentInput) (Comment, error) {
		in.DocumentID = doc
		if in.VersionID == "" {
			in.VersionID = v1.ID
		}
		return s.AddComment(ctx, tenant, in)
	}

	// Anchors are validated and located by the server.
	anchored, err := add(CommentInput{AuthorID: reader, Body: "Is this pro-rated?", Quote: "six hours per pay period", Prefix: "Staff accrue", Suffix: ". Unused"})
	if err != nil {
		t.Fatal(err)
	}
	if anchored.AnchorBlock != "accrual" || anchored.Start < 0 || string([]rune(PlainText(threadDoc))[anchored.Start:anchored.End]) != "six hours per pay period" {
		t.Fatalf("anchor = %+v", anchored)
	}
	if _, err := add(CommentInput{AuthorID: reader, Body: "x", Quote: "twelve hours"}); !errors.Is(err, ErrInvalidAnchor) {
		t.Fatalf("absent quote = %v", err)
	}
	// Commenting still needs the comment grant.
	if _, err := add(CommentInput{AuthorID: viewer, Body: "viewer", Quote: "six hours"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("viewer commented = %v", err)
	}
	if _, err := add(CommentInput{AuthorID: stranger, Body: "stranger"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger commented = %v", err)
	}

	// Replies are one level deep and carry no anchor.
	reply, err := add(CommentInput{AuthorID: owner, Body: "Yes, for part-time staff.", ParentID: anchored.ID})
	if err != nil || reply.ParentID != anchored.ID || reply.Start != -1 {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
	if _, err := add(CommentInput{AuthorID: reader, Body: "nested", ParentID: reply.ID}); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("reply to reply = %v", err)
	}
	if _, err := add(CommentInput{AuthorID: reader, Body: "anchored reply", ParentID: anchored.ID, Quote: "six hours"}); !errors.Is(err, ErrInvalidAnchor) {
		t.Fatalf("anchored reply = %v", err)
	}
	if _, err := add(CommentInput{AuthorID: reader, Body: "ghost", ParentID: "docc-missing"}); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("missing parent = %v", err)
	}

	// A new draft moves the passage, drops another; the owner reads both
	// comments re-anchored or orphaned, the reader still reads v1.
	gone, err := add(CommentInput{AuthorID: reader, Body: "Two weeks is long.", Quote: "Submit requests two weeks ahead."})
	if err != nil {
		t.Fatal(err)
	}
	v2md := "# Leave policy\n\n## Overview\n\nThis policy was rewritten.\n\n## Accrual\n\nStaff accrue six hours per pay period. Unused hours carry over.\n\n## Requests\n\nSubmit requests one week ahead.\n"
	v2, err := s.CreatePersonalDocumentVersion(ctx, tenant, doc, owner, v1.ID, "Leave policy", v2md)
	if err != nil {
		t.Fatal(err)
	}
	draftNote, err := add(CommentInput{AuthorID: owner, VersionID: v2.ID, Body: "Draft-only note", Quote: "This policy was rewritten."})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.ListThread(ctx, tenant, doc, v2.ID, owner)
	if err != nil || len(thread) != 4 {
		t.Fatalf("owner thread = %+v, %v", thread, err)
	}
	byID := map[string]Comment{}
	for _, c := range thread {
		byID[c.ID] = c
	}
	moved := byID[anchored.ID]
	if moved.Orphaned || moved.Start <= anchored.Start || string([]rune(PlainText(v2md))[moved.Start:moved.End]) != "six hours per pay period" || moved.AnchorBlock != "accrual" {
		t.Fatalf("re-anchored = %+v (was %d)", moved, anchored.Start)
	}
	if orphan := byID[gone.ID]; !orphan.Orphaned || orphan.Start != -1 || orphan.Quote != "Submit requests two weeks ahead." {
		t.Fatalf("orphan = %+v", orphan)
	}
	if byID[reply.ID].ParentID != anchored.ID {
		t.Fatal("reply lost its parent")
	}
	readerThread, err := s.ListThread(ctx, tenant, doc, v1.ID, reader)
	if err != nil || len(readerThread) != 3 {
		t.Fatalf("reader thread = %+v, %v", readerThread, err)
	}
	for _, c := range readerThread {
		if c.ID == draftNote.ID {
			t.Fatal("a quote from an unpublished draft reached a reader")
		}
		if c.Orphaned {
			t.Fatalf("reader sees an orphan on its own version: %+v", c)
		}
	}
	if _, err := s.ListThread(ctx, tenant, doc, v1.ID, stranger); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger thread = %v", err)
	}

	// Resolution: the author or a manager, top-level only, idempotent.
	if err := s.ResolveComment(ctx, tenant, doc, anchored.ID, viewer, true); !errors.Is(err, ErrResolveDenied) {
		t.Fatalf("viewer resolved = %v", err)
	}
	if err := s.ResolveComment(ctx, tenant, doc, reply.ID, owner, true); !errors.Is(err, ErrResolveNotTopic) {
		t.Fatalf("resolved a reply = %v", err)
	}
	if err := s.ResolveComment(ctx, tenant, doc, draftNote.ID, reader, true); !errors.Is(err, ErrUnknownComment) {
		t.Fatalf("reader resolved an invisible comment = %v", err)
	}
	if err := s.ResolveComment(ctx, tenant, doc, anchored.ID, stranger, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("stranger resolved = %v", err)
	}
	if err := s.ResolveComment(ctx, tenant, doc, anchored.ID, reader, true); err != nil {
		t.Fatalf("author resolve = %v", err)
	}
	if err := s.ResolveComment(ctx, tenant, doc, anchored.ID, reader, true); err != nil {
		t.Fatalf("repeat resolve = %v", err)
	}
	if err := s.ResolveComment(ctx, tenant, doc, gone.ID, owner, true); err != nil {
		t.Fatalf("manager resolve = %v", err)
	}
	if err := s.ResolveComment(ctx, tenant, doc, gone.ID, owner, false); err != nil {
		t.Fatalf("manager reopen = %v", err)
	}
	readerThread, _ = s.ListThread(ctx, tenant, doc, v1.ID, reader)
	for _, c := range readerThread {
		if want := c.ID == anchored.ID; c.Resolved != want {
			t.Fatalf("resolved state of %q = %v", c.Body, c.Resolved)
		}
	}
}
