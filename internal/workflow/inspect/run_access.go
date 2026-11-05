// Run-access adapters apply a run's classification and participant wall to
// the inspector and to work-item visibility (WF-EXT-024). This package
// already imports both the runtime policy and the work-item model, so the
// derivation lives here: the inspector renders the run decision, and no
// work item of a walled run admits anyone the run itself would deny.
package inspect

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ReasonRunNotDisclosed is the denial token stamped on every inspector
// decision against a viewer the run wall denies.
const ReasonRunNotDisclosed = "RUN_NOT_DISCLOSED"

// RunAuthorization renders the run-access decision for viewer stating
// purpose as an inspector authorization. A participant sees every section
// and every protected field; anyone else — non-participant, removed,
// revoked, or here for the wrong purpose — receives a decision under which
// the instance is not disclosable at all. Only an unusable policy errors:
// this package renders what the policy decided, including its denials.
func RunAuthorization(policy runtime.RunAccessPolicy, viewer, purpose string) (Authorization, error) {
	if err := policy.Validate(); err != nil {
		return Authorization{}, err
	}
	if err := policy.Authorize(viewer, purpose, runtime.SurfaceInspector); err != nil {
		if errors.Is(err, runtime.ErrRunNotDisclosed) {
			return Authorization{
				PolicyVersion:       policy.Digest(),
				Purpose:             purpose,
				Subject:             viewer,
				InstanceDisclosable: false,
				DenialReason:        ReasonRunNotDisclosed,
			}, nil
		}
		return Authorization{}, err
	}
	return AllowAll(policy.Digest(), purpose, viewer), nil
}

// ConstrainVisibility narrows an item's assignment-derived visibility by the
// run wall (WF-EXT-024 REFACTOR: work-item walls derive from the run
// policy). An open run constrains nothing and the item keeps its derived
// class; a walled run pins every item to assignee-only, since any broader
// class could admit — by scope, governance or candidacy — a principal the
// run itself denies.
func ConstrainVisibility(item workitem.Visibility, policy runtime.RunAccessPolicy) workitem.Visibility {
	if !policy.Walled() {
		return item
	}
	return workitem.VisibilityAssigneeOnly
}

// CheckRunCandidates refuses a candidate set that escapes the run wall. An
// assignee-only item still admits its candidates through membership, so the
// routing edge must prove every candidate is a run participant before it
// routes; an open run admits any candidate. The escapee is named because
// this refusal faces the privileged router, never the candidate.
func CheckRunCandidates(policy runtime.RunAccessPolicy, candidates []string) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if !policy.Walled() {
		return nil
	}
	for _, c := range candidates {
		if _, ok := policy.ParticipantRole(c); !ok {
			return fmt.Errorf("%w: candidate %s stands outside the run wall", runtime.ErrRunAccessInvalid, c)
		}
	}
	return nil
}
