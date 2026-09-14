package configbundle

import (
	"errors"
	"fmt"
	"sync"
)

// Control-plane outage, corruption and recovery (CP-010). An OutageCell
// fronts one Activator with a partition switch: while partitioned, reads
// keep serving the last-known-good receipt and every activation fails
// closed, so the cell runs bounded instead of on an unsafe default. A
// corrupt bundle never activates and moves no epoch. Recovery replays the
// known-good lineage on a fenced recovery stack — a separate Activator
// with an independent copy of the trust root — and adopts each epoch
// only when its digest converges with production, so history cannot fork.
//
// The drill is local and deterministic; it proves the contract the
// outage runbook executes, not the network partition itself.

// ErrPartitionedControlPlane refuses activations while the control plane
// is unreachable. Classify with errors.Is.
var ErrPartitionedControlPlane = errors.New("configbundle: control plane is partitioned; activation refused")

// OutageCell fronts an Activator with a partition switch. It is safe for
// concurrent use.
type OutageCell struct {
	mu          sync.Mutex
	activator   *Activator
	partitioned bool
	refused     int
	adoptedNext uint64
}

// NewOutageCell fronts activator with a clear partition. Recovery
// adoption replays the production lineage starting at epoch 1.
func NewOutageCell(activator *Activator) *OutageCell {
	return &OutageCell{activator: activator, adoptedNext: 1}
}

// SetPartitioned raises or clears the partition.
func (c *OutageCell) SetPartitioned(partitioned bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partitioned = partitioned
}

// RefusedActivations counts activations failed closed during partition.
func (c *OutageCell) RefusedActivations() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refused
}

// ServeKnownGood returns the current epoch receipt: the bounded
// last-known-good the cell keeps serving while dark. Reads never fail
// closed; only changes do.
func (c *OutageCell) ServeKnownGood(tenant string) (ActivationReceipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	epoch := c.activator.CurrentEpoch(tenant)
	if epoch == 0 {
		return ActivationReceipt{}, fmt.Errorf("%w: no activated epoch serves %q", ErrPartitionedControlPlane, tenant)
	}
	receipt, ok := c.activator.Receipt(tenant, epoch)
	if !ok {
		return ActivationReceipt{}, fmt.Errorf("%w: epoch %d receipt is missing for %q", ErrPartitionedControlPlane, epoch, tenant)
	}
	return receipt, nil
}

// Activate passes activation through unless partitioned, in which case
// it fails closed and counts the refusal.
func (c *OutageCell) Activate(request ActivationRequest) (ActivationReceipt, error) {
	c.mu.Lock()
	partitioned := c.partitioned
	c.mu.Unlock()
	if partitioned {
		c.mu.Lock()
		c.refused++
		c.mu.Unlock()
		return ActivationReceipt{}, fmt.Errorf("%w: epoch activation is a sensitive action", ErrPartitionedControlPlane)
	}
	return c.activator.Activate(request)
}

// Adoption is one converged recovery epoch with its signed receipt.
type Adoption struct {
	Receipt          ActivationReceipt
	ConvergedEpochs  int
	ProductionDigest string
}

// AdoptRecovery activates one known-good bundle on the fenced recovery
// stack and adopts the epoch only when its digest converges with the
// production receipt at the same epoch. Divergence refuses instead of
// forking history.
func (c *OutageCell) AdoptRecovery(recovery *Activator, request ActivationRequest) (Adoption, error) {
	receipt, err := recovery.Activate(request)
	if err != nil {
		return Adoption{}, err
	}
	epoch := receipt.Epoch
	tenant := request.Scope.TenantID
	if tenant == "" {
		tenant = request.TenantID
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if epoch != c.adoptedNext {
		return Adoption{}, fmt.Errorf("%w: recovery replays the lineage in order; epoch %d is not next after %d adopted",
			ErrPartitionedControlPlane, epoch, c.adoptedNext-1)
	}
	production, ok := c.activator.Receipt(tenant, epoch)
	if !ok {
		return Adoption{}, fmt.Errorf("%w: production holds no epoch %d receipt to converge with", ErrPartitionedControlPlane, epoch)
	}
	if production.BundleDigest != receipt.BundleDigest {
		return Adoption{}, fmt.Errorf("%w: recovery epoch %d digest %q diverges from production %q",
			ErrPartitionedControlPlane, epoch, receipt.BundleDigest, production.BundleDigest)
	}
	c.adoptedNext++
	return Adoption{Receipt: receipt, ConvergedEpochs: int(epoch), ProductionDigest: production.BundleDigest}, nil
}
