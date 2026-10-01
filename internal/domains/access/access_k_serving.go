package access

import (
	"fmt"
	"time"
)

// ServingContractID identifies the read-only Access contract composed by the
// shipped application cell. It does not grant authority or persist state.
const ServingContractID = "hcmnext.conformance.access/v1"

// ValidateServingContract exercises the authoritative graph and decision
// lifecycle through the same pure symbols used by the Access domain. Startup
// validation is deliberately ephemeral: it never writes a graph, calls a
// provider, or turns an observation into an authoritative record.
func ValidateServingContract() error {
	if accessSchemaVersion <= 0 || LifecycleVersion <= 0 {
		return fmt.Errorf("access: serving contract versions must be positive")
	}
	graph, err := NewGraph("tenant-serving")
	if err != nil {
		return fmt.Errorf("access: serving contract graph: %w", err)
	}
	if graph.Tenant != "tenant-serving" || len(graph.Identities) != 0 || len(graph.Observations) != 0 {
		return fmt.Errorf("access: serving contract graph is not an empty authoritative snapshot")
	}

	request, err := RequestAccess(AccessRequestInput{
		RequestID: "request:serving", Tenant: "tenant-serving", Requester: "requester:serving",
		Beneficiary: "worker:serving", Application: "application:serving", AccountID: "account:serving",
		EntitlementID: "entitlement:serving", Scope: "scope:serving", Purpose: "purpose:serving",
		Justification: "serving contract", Risk: RiskHigh, ProposalDigest: "proposal:serving",
		Snapshot:    LifecycleSnapshot{RiskRevision: "risk:serving", ManagerRevision: "manager:serving", EntitlementRevision: "entitlement:serving"},
		RequestedAt: time.Unix(1, 0).UTC(), IdempotencyKey: "request-key:serving",
	})
	if err != nil {
		return fmt.Errorf("access: serving contract request: %w", err)
	}
	if request.State != StateRequested || request.RequiredQuorum != 1 || request.Digest == "" {
		return fmt.Errorf("access: serving contract request is not fenced")
	}

	lifecycle := NewAccessDecisionLifecycle()
	replayed, err := lifecycle.SubmitRequest(request.AccessRequestInput)
	if err != nil {
		return fmt.Errorf("access: serving contract lifecycle: %w", err)
	}
	if replayed.Digest != request.Digest {
		return fmt.Errorf("access: serving contract lifecycle changed the request digest")
	}
	return nil
}
