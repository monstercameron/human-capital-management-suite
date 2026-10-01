package accessdrift

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

// ServingContractID identifies the read-only access-drift contract composed
// by the shipped application cell. It does not execute a repair effect.
const ServingContractID = "hcmnext.conformance.access-drift/v1"

// ValidateServingContract exercises the comparison path with ephemeral data.
// A served cell may validate that drift classification remains available, but
// it must not infer custody, contact a provider, or create a repair plan at
// startup.
func ValidateServingContract() error {
	if Version <= 0 {
		return fmt.Errorf("accessdrift: serving contract version must be positive")
	}
	want := Resource{ID: "account:serving", Kind: KindAccount, Subject: "worker:serving", Value: "account", State: "ACTIVE", Risk: RiskLow}
	report, err := Reconcile(ReconcileRequest{
		Tenant: "tenant:serving", AsOf: time.Unix(1, 0).UTC(), Expected: []Resource{want},
		Observed: []Resource{{ID: want.ID, Kind: want.Kind, Subject: want.Subject, Value: want.Value, State: want.State, Risk: want.Risk,
			ProviderVersion: "provider:serving", ObservedAt: time.Unix(1, 0).UTC(), Freshness: observe.FreshnessFresh, Complete: true}},
	})
	if err != nil {
		return fmt.Errorf("accessdrift: serving contract reconcile: %w", err)
	}
	if !report.Complete || report.Freshness != observe.FreshnessFresh || len(report.Findings) != 1 || report.Findings[0].Status != StatusMatch || report.CanonicalDigest == "" {
		return fmt.Errorf("accessdrift: serving contract produced an invalid match report")
	}
	return nil
}
