package subprocessor

import (
	"fmt"
	"time"
)

// ServingContractID identifies the side-effect-free subprocessor governance
// contract exposed to a composed application cell.
const ServingContractID = "hcmnext.conformance.subprocessor-governance/v1"

// CapabilityOwner is the single accountable capability owner for this
// governance contract. Privacy, integration, and tenant controls remain
// required review inputs, but they do not create competing ownership.
const CapabilityOwner = "GOVERNANCE"

// ContractMetadata is the immutable identity a composition root can use to
// discover and attribute the served subprocessor governance path.
type ContractMetadata struct {
	ID      string
	Version int
	Owner   string
}

// Contract returns the stable identity and accountable owner of this path.
func Contract() ContractMetadata {
	return ContractMetadata{ID: ServingContractID, Version: Version(), Owner: CapabilityOwner}
}

// ValidateServingContract exercises the pure governance path used by a
// serving cell. It performs no persistence, notification, or provider call.
func ValidateServingContract() error {
	contract := Contract()
	if contract.ID == "" || contract.Version <= 0 || contract.Owner == "" {
		return fmt.Errorf("subprocessor: serving contract identity is incomplete")
	}

	before := servingInventory(1, "region-a")
	after := servingInventory(2, "region-b")
	change, err := Diff(before, after)
	if err != nil {
		return fmt.Errorf("subprocessor: serving contract diff: %w", err)
	}
	if !change.Material || len(change.AffectedTenants) != 1 || len(change.AffectedFlows) != 1 || len(change.AffectedIntents) != 1 {
		return fmt.Errorf("subprocessor: serving contract did not produce a scoped impact graph")
	}

	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	change.Reviews = Review{Security: true, Privacy: true, Residency: true, Contract: true, Evidence: "serving-contract"}
	change.ContractApproved = true
	change.ExitFallback = true
	change.Notice = Notice{
		ID:              "notice-serving",
		IssuedAt:        now,
		ObjectionOpens:  now,
		ObjectionCloses: now.Add(24 * time.Hour),
		TenantIDs:       []string{"tenant-serving"},
	}
	receipt, err := Activate(change, now.Add(25*time.Hour))
	if err != nil || !receipt.Allowed || receipt.Status != StatusActive {
		return fmt.Errorf("subprocessor: serving contract activation: receipt=%+v err=%v", receipt, err)
	}
	return nil
}

func servingInventory(revision uint64, region string) Inventory {
	return Inventory{
		Revision: revision,
		Processors: []Processor{{
			ID:              "processor-serving",
			Name:            "Serving Processor",
			Regions:         []string{region},
			Purposes:        []string{"serving-validation"},
			DataCategories:  []string{"serving-record"},
			Retention:       "7d",
			ContractVersion: "serving-v1",
			ExitPlan:        "export-revoke-confirm",
		}},
		Flows: []Flow{{
			ID:             "flow-serving",
			TenantID:       "tenant-serving",
			IntentID:       "subprocessor.governance",
			ProcessorID:    "processor-serving",
			Region:         region,
			Purpose:        "serving-validation",
			DataCategories: []string{"serving-record"},
			Retention:      "7d",
		}},
	}
}
