package governance

import (
	"context"

	privacy "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy"
	privacydispatch "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/dispatch"
	privacyinventory "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory"
)

// PrivacyRuntime is the immutable privacy boundary carried by a composed
// governance result. Keeping these ports on the composition result makes the
// privacy inventory, authority and dispatch packages part of the serving
// binary's selected graph instead of test-only packages.
type PrivacyRuntime struct{}

// NewPrivacyRuntime returns the privacy boundary for one composed process.
// The boundary has no mutable state; policy and evidence remain explicit
// inputs to each operation.
func NewPrivacyRuntime() PrivacyRuntime { return PrivacyRuntime{} }

// ValidateInventory publishes only a validated executable inventory.
func (PrivacyRuntime) ValidateInventory(input privacyinventory.Inventory) (privacyinventory.Executable, error) {
	return privacyinventory.ValidateExecutable(input)
}

// EvaluateAuthority preserves deny-by-default notice and optional-processing
// authority at the composed governance boundary.
func (PrivacyRuntime) EvaluateAuthority(input privacy.AuthorityInput) privacy.AuthorityDecision {
	return privacy.EvaluateAuthority(input)
}

// Dispatch binds processor, transfer and DLP snapshots immediately before an
// external effect, returning the dispatch package's durable receipt.
func (PrivacyRuntime) Dispatch(
	ctx context.Context,
	binding privacydispatch.Binding,
	current privacydispatch.SnapshotSet,
	request privacydispatch.Request,
	policy privacydispatch.DLPDecisioner,
	sender privacydispatch.Sender,
) (privacydispatch.Receipt, error) {
	return privacydispatch.Dispatch(ctx, binding, current, request, policy, sender)
}
