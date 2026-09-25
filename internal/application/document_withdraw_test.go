package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// TestTodo_DOCS_01_Application proves ListDocumentVersions lists a
// document's versions oldest first through the same policy-redacted
// history HUB-017 already uses, marks the last one current, and denies a
// caller with no read grant the same way every other document read does
// (document.unavailable, never a distinguishing error).
func TestTodo_DOCS_01_Application(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner, stranger = "tenant-versions", "owner", "stranger"
	id, v1, err := svc.CreateDocument(ctx, tenant, owner, "Guide", "# One\n")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := svc.CreateDocumentVersion(ctx, tenant, owner, id, v1, "Guide", "# Two\n")
	if err != nil {
		t.Fatal(err)
	}
	versions, err := svc.ListDocumentVersions(ctx, tenant, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].VersionID != v1 || versions[1].VersionID != v2 {
		t.Fatalf("versions not oldest first: %+v", versions)
	}
	if versions[0].IsCurrent || !versions[1].IsCurrent {
		t.Fatalf("current version not marked: %+v", versions)
	}
	if versions[1].Title != "Guide" {
		t.Fatalf("version title missing: %+v", versions[1])
	}

	if _, err := svc.ListDocumentVersions(ctx, tenant, stranger, id); !documentUnavailable(err) {
		t.Fatalf("stranger read the version history: %v", err)
	}
	if _, err := svc.ListDocumentVersions(ctx, "other-tenant", owner, id); !documentUnavailable(err) {
		t.Fatalf("cross-tenant read the version history: %v", err)
	}
}

// TestTodo_DOCS_07_Application proves WithdrawDocument (a) refuses a
// caller with no retire capability the same non-disclosing way every
// other document write does, (b) clears the document's live default
// deployment for an authorized owner and reports nothing left to
// withdraw on a second call, and (c) RestoreDocument puts the document
// back — visible again, with the same content and the reader's grant
// intact — without requiring a review this personal document never
// needed to go live in the first place; it is Undo, not a new
// publication.
func TestTodo_DOCS_07_Application(t *testing.T) {
	ctx := context.Background()
	svc := documentServiceFixture(t)
	const tenant, owner, reader = "tenant-withdraw", "owner", "reader"
	id, v1, err := svc.CreateDocument(ctx, tenant, owner, "Handbook", "# Handbook\n")
	if err != nil {
		t.Fatal(err)
	}
	// Sharing (not a formal review/deploy) is what actually creates the
	// live default-scope pointer a personal document's readers see
	// (documenthubstore.Store.SharePersonalDocumentRole), so this is the
	// realistic setup for a withdrawal.
	if err := svc.ShareDocument(ctx, tenant, owner, id, reader, documenthubstore.RoleViewer); err != nil {
		t.Fatal(err)
	}

	// A viewer holds read, not retire: withdrawal must refuse exactly like
	// any other unauthorized document write, never confirming or denying
	// what withdrawal would have done.
	if _, err := svc.WithdrawDocument(ctx, tenant, reader, id, "wrong version"); !documentUnavailable(err) {
		t.Fatalf("viewer withdrew the document: %v", err)
	}

	withdrawnVersion, err := svc.WithdrawDocument(ctx, tenant, owner, id, "")
	if err != nil {
		t.Fatalf("authorized withdrawal refused: %v", err)
	}
	if withdrawnVersion != v1 {
		t.Fatalf("withdrawal named the wrong version: got %q want %q", withdrawnVersion, v1)
	}

	// Nothing is live any more: a second withdrawal is a clear
	// precondition failure, not a silent no-op or a generic error.
	if _, err := svc.WithdrawDocument(ctx, tenant, owner, id, ""); !documentFailedPrecondition(err, "document.not_removable") {
		t.Fatalf("second withdrawal did not report nothing left to remove: %v", err)
	}

	// This document was shared, never formally reviewed and deployed
	// (the ordinary personal-document case): RestoreDocument still puts
	// it back, because undoing a withdrawal re-establishes the prior
	// authorized state rather than publishing anything new, and that
	// state never needed review either.
	if err := svc.RestoreDocument(ctx, tenant, owner, id, withdrawnVersion); err != nil {
		t.Fatalf("restore of a never-reviewed personal document was refused: %v", err)
	}
	restoredSummary, _, _, err := svc.GetDocument(ctx, tenant, reader, id)
	if err != nil {
		t.Fatalf("document not visible to the reader again after restore: %v", err)
	}
	if restoredSummary.Title != "Handbook" {
		t.Fatalf("restored document lost its content: %+v", restoredSummary)
	}
	// Undo the undo once more, proving this is a repeatable round trip,
	// not a one-shot escape hatch.
	secondWithdraw, err := svc.WithdrawDocument(ctx, tenant, owner, id, "")
	if err != nil {
		t.Fatalf("second withdrawal refused: %v", err)
	}
	if err := svc.RestoreDocument(ctx, tenant, owner, id, secondWithdraw); err != nil {
		t.Fatalf("second restore refused: %v", err)
	}

	// Cross-tenant withdrawal must not confirm the document exists in
	// this tenant either: it is refused the same document.unavailable way
	// as any other cross-tenant document access, never the
	// document.not_removable precondition a real, unshared document gets.
	if _, err := svc.WithdrawDocument(ctx, "other-tenant", owner, id, ""); !documentUnavailable(err) {
		t.Fatalf("cross-tenant withdrawal was not refused as unavailable: %v", err)
	}
}

func documentFailedPrecondition(err error, reason string) bool {
	e, ok := envelope.As(err)
	return ok && e.Code() == envelope.CodeFailedPrecondition && e.ReasonRef() == reason
}
