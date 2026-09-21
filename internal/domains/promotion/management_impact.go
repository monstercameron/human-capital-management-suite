package promotion

// REV-091-02: the live proposal review page must show PROMOUX-005's
// manager, organization and reporting-line impact. Before this file the
// typed ManagementImpact was only ever produced inside PreflightPromotion, so
// a read surface that shows an existing journey had no way to ask the same
// question without re-running the whole preflight.
//
// EvaluateManagementImpact is that question on its own. It is not a second
// implementation: it runs evaluateTargetManagerSelection, the exact function
// PreflightPromotion runs, so the review card and the preflight can never
// disagree about existence, disclosure or cycle safety.

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// EvaluateManagementImpact evaluates one target-manager selection for
// subject against the Organization capability's own facts. It returns the
// typed impact together with any finding the evaluation raised (not found,
// confirmed cycle, or chain not certifiable).
//
// A candidate that does not exist, or that this caller may not learn about,
// returns the zero [ManagementImpact] (Evaluated() is false) and the
// not-found finding, so a caller that renders only an evaluated impact can
// never disclose an unauthorized manager. Only a contract failure (a nil
// reader, a malformed coordinate or a reader error) is returned as an error.
func EvaluateManagementImpact(
	ctx context.Context, tenant values.TenantId, subject values.EntityRef,
	facts org.WorkerFacts, sel TargetManagerSelection,
) (ManagementImpact, []Finding, error) {
	if err := subject.Validate(); err != nil {
		return ManagementImpact{}, nil, fmt.Errorf("%w: subject: %w", ErrRequestInvalid, err)
	}
	findings, impact, err := evaluateTargetManagerSelection(ctx, PreflightRequest{
		Tenant: tenant, Subject: subject, ManagerFacts: facts, TargetManagerSelection: &sel,
	})
	if err != nil {
		return ManagementImpact{}, nil, err
	}
	return impact, findings, nil
}
