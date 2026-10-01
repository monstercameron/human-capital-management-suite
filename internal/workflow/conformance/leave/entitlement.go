// Package leave contains the kernel-pure Leave channel conformance adapter.
// It exercises the same commercial resolver used by Promotion without
// creating a leave intent, work item, message, or other effect.
package leave

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
)

// ServingContractID identifies the Leave entitlement channel contract
// composed by the shipped application cell.
const ServingContractID = "hcmnext.workflow.conformance.leave-entitlement/v1"

// ValidateServingContract proves a suspended commercial contract denies Leave
// without dropping the entitlement fingerprint.
func ValidateServingContract() error {
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	snapshot, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "serving-contract", ContractID: "serving-contract", Revision: 1,
		EffectiveFrom: at, EffectiveTo: at.Add(90 * 24 * time.Hour), Status: commercial.StatusSuspended,
		Capabilities: []string{commercial.LeaveEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 1}, PriceCents: 100, Currency: "USD",
	})
	if err != nil {
		return fmt.Errorf("leave: serving contract snapshot: %w", err)
	}
	gate, err := NewEntitlementGate(snapshot)
	if err != nil {
		return fmt.Errorf("leave: serving contract gate: %w", err)
	}
	request := EntitlementRequest{
		TenantID: "serving-contract", Capability: commercial.LeaveEntitlementCapability,
		At: at, Channel: commercial.ChannelWorkflow, Phase: "resume",
	}
	decision := gate.Decide(request)
	if decision.Code != commercial.CodeContractSuspended || decision.Fingerprint != snapshot.Fingerprint() {
		return fmt.Errorf("leave: serving contract decision = %+v", decision)
	}
	if _, err := gate.Admit(request); err == nil {
		return fmt.Errorf("leave: serving contract admitted suspended entitlement")
	}
	return nil
}

// EntitlementRequest is metadata for one Leave initiation or resume route.
// Route and phase are intentionally not interpreted by the commercial gate.
type EntitlementRequest struct {
	TenantID   string
	Capability string
	At         time.Time
	Channel    commercial.Channel
	Phase      string
}

// EntitlementGate is the Leave adapter over the shared commercial authority.
type EntitlementGate struct {
	gate commercial.Gate
}

// NewEntitlementGate composes the Leave channel with one immutable tenant
// entitlement snapshot.
func NewEntitlementGate(snapshot commercial.EntitlementSnapshot) (EntitlementGate, error) {
	gate, err := commercial.NewGate(snapshot)
	if err != nil {
		return EntitlementGate{}, err
	}
	return EntitlementGate{gate: gate}, nil
}

// Decide resolves the channel-neutral commercial decision for discovery.
func (g EntitlementGate) Decide(request EntitlementRequest) commercial.EntitlementDecision {
	return g.gate.Resolve(commercial.NewEntitlementRequest(request.TenantID, request.Capability, request.At, request.Channel))
}

// Admit returns the pinned decision or the shared typed commercial refusal.
func (g EntitlementGate) Admit(request EntitlementRequest) (commercial.EntitlementDecision, error) {
	return g.gate.Admit(commercial.NewEntitlementRequest(request.TenantID, request.Capability, request.At, request.Channel))
}
