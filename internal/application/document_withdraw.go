// Recoverable removal for DOCS-07: WithdrawDocument clears the document's
// live default-scope pointer (documenthubstore Store.Withdraw, HUB-010)
// without touching any immutable version or record, and RestoreDocument
// puts the withdrawn version back through Store.RestoreWithdrawal. A
// personal document's default-scope pointer was never reviewed to begin
// with (it goes live on share, not on a formal review/deploy), so undoing
// its own withdrawal does not newly require review either; a team/channel
// placement, which did require review to go live (HUB-014), still needs
// fresh evidence to come back, and RestoreWithdrawal enforces that by
// falling back to the ordinary reviewed Deploy path for any non-default
// scope.
package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// defaultWithdrawReason is recorded when the caller (the confirm dialog)
// supplies none; Store.Withdraw requires a non-empty audited reason.
const defaultWithdrawReason = "removed by owner"

// WithdrawDocument withdraws the document's live default-scope deployment
// (the personal-document "library" pointer) and returns the version that
// was live, so the caller can offer an Undo that calls RestoreDocument
// with it. A document with nothing currently live reports a clear
// precondition failure rather than a silent no-op.
func (s documentService) WithdrawDocument(ctx context.Context, tenantID, actorID, documentID, reason string) (string, error) {
	// Authorize before resolving the live deployment: otherwise an
	// unauthorized or cross-tenant caller could tell a document that
	// exists but is not shared (document.not_removable) apart from one
	// that does not exist or is not theirs to withdraw (document
	// .unavailable), which no other document read or write here allows.
	if err := s.store.Authorize(ctx, tenantID, documentID, "person", actorID, documenthubstore.ActionRetire); err != nil {
		if errors.Is(err, documenthubstore.ErrDenied) {
			return "", unavailableDocument()
		}
		return "", err
	}
	live, err := s.store.ResolveDeployment(ctx, tenantID, documentID, "default", "")
	if errors.Is(err, documenthubstore.ErrNoDeployment) {
		return "", envelope.New(envelope.CodeFailedPrecondition, "document.not_removable", "this document is not currently shared, so there is nothing to remove")
	}
	if err != nil {
		return "", err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = defaultWithdrawReason
	}
	withdrawal, err := s.store.Withdraw(ctx, tenantID, documenthubstore.WithdrawInput{
		DocumentID: documentID, ScopeKind: "default", ScopeID: "",
		ActorID: actorID, ExpectedLive: live.VersionID, Reason: reason,
	})
	if errors.Is(err, documenthubstore.ErrDenied) {
		return "", unavailableDocument()
	}
	if errors.Is(err, documenthubstore.ErrStalePointer) {
		return "", envelope.New(envelope.CodeAborted, "document.stale_version", "the document changed; reload it before removing")
	}
	if err != nil {
		return "", err
	}
	return withdrawal.VersionID, nil
}

// RestoreDocument puts versionID back on the default scope, undoing a
// WithdrawDocument. It refuses when something is already live (the
// withdrawal was superseded) and, for the ordinary personal-document case,
// restores directly: it re-establishes the prior authorized state rather
// than publishing new content, so it does not require a fresh review the
// document never needed to go live in the first place. It still reports
// document.restore_unavailable for the one case that legitimately needs
// fresh evidence and lacks it (a team/channel placement withdrawn and not
// yet re-reviewed).
func (s documentService) RestoreDocument(ctx context.Context, tenantID, actorID, documentID, versionID string) error {
	if strings.TrimSpace(versionID) == "" {
		return envelope.New(envelope.CodeInvalidArgument, "document.invalid_version_ref", "a version ID is required")
	}
	if _, err := s.store.ResolveDeployment(ctx, tenantID, documentID, "default", ""); !errors.Is(err, documenthubstore.ErrNoDeployment) {
		if err == nil {
			return envelope.New(envelope.CodeAborted, "document.already_restored", "this document is already shared; reload before restoring")
		}
		return err
	}
	_, err := s.store.RestoreWithdrawal(ctx, tenantID, documenthubstore.RestoreInput{
		DocumentID: documentID, ScopeKind: "default", ScopeID: "", ActorID: actorID, VersionID: versionID,
	})
	if errors.Is(err, documenthubstore.ErrDenied) {
		return unavailableDocument()
	}
	if errors.Is(err, documenthubstore.ErrNoApprovedReview) {
		return envelope.New(envelope.CodeFailedPrecondition, "document.restore_unavailable", "this document can't be restored automatically; ask its owner to share it again")
	}
	if errors.Is(err, documenthubstore.ErrStalePointer) {
		return envelope.New(envelope.CodeAborted, "document.stale_version", "the document changed; reload it before restoring")
	}
	return err
}
