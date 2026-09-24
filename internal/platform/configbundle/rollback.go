package configbundle

// Configuration rollback as a new governed activation (CP-009): the exact
// prior bundle is revalidated by digest, re-signed through the caller's
// signing capability, activated at a new epoch with receipts, and history
// is left intact. Rollback never mutates history, never selects a mutable
// label, never revives revoked dependencies and never breaks a pinned
// live-workflow version.

import (
	"strings"
	"time"
)

// PinnedWorkflow names one live workflow's declared bundle version. A
// rollback that would move a pinned workflow off its digest, or that
// lands on a revoked digest, is refused.
type PinnedWorkflow struct {
	WorkflowRef string
	Digest      string
}

// RollbackRequest rolls one tenant back to one exact prior bundle.
type RollbackRequest struct {
	Tenant       string
	Prior        SignedBundle
	PriorDigest  string
	NewEpoch     uint64
	Scope        Scope
	Environment  string
	TrustProfile string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	// Sign re-signs the exact prior bundle bytes for the new epoch. The
	// module never touches private key material: the caller supplies the
	// capability, the module supplies the pinned bytes.
	Sign          func(Bundle) (SignedBundle, error)
	ReceiptSigner ReceiptSigner
	// Revoked names digests revoked since the prior activation: bundles
	// and dependencies alike. Landing on one revives it and is refused.
	Revoked []string
	// Pinned carries the live-workflow version policy the rollback must
	// retain.
	Pinned []PinnedWorkflow
}

// Rollback revalidates the exact prior bundle, re-signs it, activates it
// at the new epoch and proves prior history unchanged.
func Rollback(activator *Activator, request RollbackRequest) (ActivationReceipt, error) {
	if strings.TrimSpace(request.Tenant) == "" || request.Scope.TenantID != request.Tenant || strings.TrimSpace(request.Scope.CellID) == "" {
		return ActivationReceipt{}, refuse("ROLLBACK_SCOPE_MISMATCH", "scope", ErrActivationScopeMismatch, "rollback tenant and tenant/cell scope must identify the same target")
	}
	if !strings.HasPrefix(request.PriorDigest, "sha256:") {
		return ActivationReceipt{}, refuse("MUTABLE_ROLLBACK_LABEL", "", ErrInvalidRequest, "rollback selects the pinned digest %q, never a mutable label", request.PriorDigest)
	}
	if err := request.Prior.Bundle.Verify(); err != nil {
		return ActivationReceipt{}, err
	}
	if !strings.EqualFold(request.Prior.Digest, request.PriorDigest) {
		return ActivationReceipt{}, refuse("ROLLBACK_DIGEST_MISMATCH", "", ErrInvalidSignature, "signed prior digest differs from the pinned digest")
	}
	redigest, err := request.Prior.Bundle.DigestValue()
	if err != nil {
		return ActivationReceipt{}, err
	}
	if !strings.EqualFold(redigest, request.PriorDigest) {
		return ActivationReceipt{}, refuse("ROLLBACK_DIGEST_MISMATCH", "", ErrInvalidSignature, "prior bundle bytes do not reproduce the pinned digest: history would mutate")
	}
	current := activator.CurrentEpoch(request.Tenant)
	if current == 0 {
		return ActivationReceipt{}, refuse("ROLLBACK_NO_ACTIVATION", "", ErrInvalidRequest, "tenant %q has no activation to roll back", request.Tenant)
	}
	before, ok := activator.Receipt(request.Tenant, current)
	if !ok {
		return ActivationReceipt{}, refuse("ROLLBACK_NO_ACTIVATION", "", ErrInvalidRequest, "current receipt for tenant %q is missing", request.Tenant)
	}
	if strings.EqualFold(before.BundleDigest, request.PriorDigest) {
		return ActivationReceipt{}, refuse("ROLLBACK_NOT_NEEDED", "", ErrInvalidRequest, "prior digest is already active at epoch %d", current)
	}
	if request.NewEpoch <= current {
		return ActivationReceipt{}, refuse("ROLLBACK_EPOCH_STALE", "", ErrInvalidRequest, "rollback epoch %d does not advance past %d", request.NewEpoch, current)
	}
	for _, revoked := range request.Revoked {
		if strings.EqualFold(revoked, request.PriorDigest) {
			return ActivationReceipt{}, refuse("REVOKED_ROLLBACK_TARGET", "", ErrRevokedSigningKey, "prior digest %q was revoked", request.PriorDigest)
		}
	}
	for _, pin := range request.Pinned {
		if strings.EqualFold(pin.Digest, before.BundleDigest) {
			return ActivationReceipt{}, refuse("PINNED_VERSION_CONFLICT", "", ErrInvalidRequest, "live workflow %q pins the current digest being rolled back", pin.WorkflowRef)
		}
		for _, revoked := range request.Revoked {
			if strings.EqualFold(revoked, pin.Digest) {
				return ActivationReceipt{}, refuse("PINNED_VERSION_CONFLICT", "", ErrInvalidRequest, "live workflow %q pins revoked digest %q", pin.WorkflowRef, pin.Digest)
			}
		}
	}
	if request.Sign == nil {
		return ActivationReceipt{}, refuse("ROLLBACK_UNSIGNED", "", ErrSigningRefused, "no signing capability for the new epoch")
	}
	resigned, err := request.Sign(request.Prior.Bundle)
	if err != nil {
		return ActivationReceipt{}, err
	}
	if !strings.EqualFold(resigned.Digest, request.PriorDigest) {
		return ActivationReceipt{}, refuse("ROLLBACK_DIGEST_MISMATCH", "", ErrInvalidSignature, "re-signed bundle does not reproduce the pinned digest")
	}
	receipt, err := activator.Activate(ActivationRequest{
		Bundle: resigned, SignedBundle: resigned, Scope: request.Scope, TargetScope: request.Scope,
		TenantID: request.Tenant, Environment: request.Environment, TrustProfile: request.TrustProfile,
		Epoch: request.NewEpoch, ActivationEpoch: request.NewEpoch,
		IssuedAt: request.IssuedAt, ExpiresAt: request.ExpiresAt, ReceiptSigner: request.ReceiptSigner,
	})
	if err != nil {
		return ActivationReceipt{}, err
	}
	after, ok := activator.Receipt(request.Tenant, current)
	if !ok || after != before {
		return ActivationReceipt{}, refuse("ROLLBACK_REWROTE_HISTORY", "", ErrInvalidRequest, "prior epoch %d receipt changed under rollback", current)
	}
	return receipt, nil
}
