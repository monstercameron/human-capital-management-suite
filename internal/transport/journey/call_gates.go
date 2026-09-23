package journey

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// servedCallGate is one admissible page, feature and action grant for a
// served call. A call lists every grant that admits it; the caller needs one
// of them. Reads name view grants, writes name the mutating action they
// perform. No journey call performs a deletion, so no entry claims the
// delete action; a future deleting call must add one here, and the
// completeness test fails until it does.
type servedCallGate struct {
	pageID    string
	featureID string
	action    string
}

// servedCallGates maps every served JourneyService method to the grants that
// admit it, including reads. It mirrors the per-handler gates: entries for
// already-gated calls restate the pair the handler enforces, entries for
// newly-gated calls are what the handler enforces through
// requireServedCall. A method missing here denies, so adding an RPC without
// an entry fails closed and fails the completeness test.
var servedCallGates = map[string][]servedCallGate{
	"ListJourneys": {
		{pageID: "journeys", featureID: "journey_list", action: roleaccess.ActionView},
		{pageID: "work", featureID: "assigned_queue", action: roleaccess.ActionView},
	},
	"InspectJourney": {
		{pageID: "journeys", featureID: "journey_detail", action: roleaccess.ActionView},
		{pageID: "work", featureID: "assigned_queue", action: roleaccess.ActionView},
	},
	"WatchJourney": {
		{pageID: "journeys", featureID: "journey_detail", action: roleaccess.ActionView},
		{pageID: "work", featureID: "assigned_queue", action: roleaccess.ActionView},
	},
	"WatchPromotionInvalidations": {
		{pageID: "journeys", featureID: "journey_list", action: roleaccess.ActionView},
		{pageID: "work", featureID: "assigned_queue", action: roleaccess.ActionView},
	},
	"ListWorkers": {
		{pageID: "people", featureID: "directory", action: roleaccess.ActionView},
	},
	"CreateWorker": {
		{pageID: "people", featureID: "directory", action: roleaccess.ActionCreate},
	},
	"ProposeJourney": {
		{pageID: "journeys", featureID: "promotion_request", action: roleaccess.ActionCreate},
	},
	"ProposePromotion": {
		{pageID: "journeys", featureID: "promotion_request", action: roleaccess.ActionCreate},
	},
	"ExecuteJourney": {
		{pageID: "journeys", featureID: "journey_detail", action: roleaccess.ActionUpdate},
	},
	"DecideJourney": {
		{pageID: "work", featureID: "approval_decision", action: roleaccess.ActionUpdate},
	},
	"AcknowledgeJourney": {
		{pageID: "journeys", featureID: "journey_detail", action: roleaccess.ActionUpdate},
	},
	"EditProposal": {
		{pageID: "journeys", featureID: "promotion_request", action: roleaccess.ActionUpdate},
	},
	"PreviewJourneyIntervention": {
		{pageID: "journeys", featureID: "journey_detail", action: roleaccess.ActionView},
	},
	"RequestJourneyIntervention": {
		{pageID: "journeys", featureID: "journey_detail", action: roleaccess.ActionUpdate},
	},
	// AddJourneyNote is a write on the journey detail and needs update
	// there: the work page's assigned_queue is a view-only feature, so no
	// work page grant admits it. The handler wiring follows in the owning
	// session; the table already states the intended gate.
	"AddJourneyNote": {
		{pageID: "journeys", featureID: "journey_detail", action: roleaccess.ActionUpdate},
	},
	"GetProductPreferences": {
		{pageID: "settings", featureID: "content", action: roleaccess.ActionView},
	},
	"SaveUserPreferences": {
		{pageID: "settings", featureID: "actions", action: roleaccess.ActionUpdate},
	},
	"SaveTenantAppearance": {
		{pageID: "appearance", featureID: "actions", action: roleaccess.ActionUpdate},
	},
	"SaveOrganizationVisibility": {
		{pageID: "organization-visibility", featureID: "actions", action: roleaccess.ActionUpdate},
	},
	"RecordWorkflowUse": {
		{pageID: "settings", featureID: "actions", action: roleaccess.ActionUpdate},
	},
	"GetRoleAccess": {
		{pageID: "roles", featureID: "actions", action: roleaccess.ActionView},
	},
	"SaveAccessRole": {
		{pageID: "roles", featureID: "actions", action: roleaccess.ActionCreate},
		{pageID: "roles", featureID: "actions", action: roleaccess.ActionUpdate},
	},
	"SaveWorkerRoleAssignment": {
		{pageID: "roles", featureID: "actions", action: roleaccess.ActionUpdate},
	},
	"SaveRoleOrganizationVisibility": {
		{pageID: "organization-visibility", featureID: "actions", action: roleaccess.ActionUpdate},
	},
	"SaveRolePagePermission": {
		{pageID: "roles", featureID: "actions", action: roleaccess.ActionUpdate},
	},
	"SaveRoleFeaturePermission": {
		{pageID: "roles", featureID: "feature_access", action: roleaccess.ActionUpdate},
	},
	"PreviewRoleAccess": {
		{pageID: "organization-visibility", featureID: "content", action: roleaccess.ActionView},
	},
	"GetWorkerIDPolicy": {
		{pageID: "worker-ids", featureID: "content", action: roleaccess.ActionView},
	},
	"SaveWorkerIDPolicy": {
		{pageID: "worker-ids", featureID: "actions", action: roleaccess.ActionUpdate},
	},
}

// gatesForServedCall returns the table's grants for a JourneyService method.
// An unmapped method denies: the table is the only authority for which grant
// admits a call.
func gatesForServedCall(method string) ([]servedCallGate, bool) {
	gates, ok := servedCallGates[method]
	return gates, ok
}

// requireServedCall enforces the table's grants for method. It is how
// handlers gate from the table instead of restating pairs per handler.
func (s *server) requireServedCall(ctx context.Context, principal *trust.Principal, inv *transport.Invocation, method string) error {
	gates, ok := gatesForServedCall(method)
	if !ok {
		return featureAccessDenied(principal, inv)
	}
	requests := make([]featureAccessRequest, 0, len(gates))
	for _, gate := range gates {
		requests = append(requests, featureAccessRequest{pageID: gate.pageID, featureID: gate.featureID, action: gate.action})
	}
	return s.requireAnyFeatureView(ctx, principal, inv, requests...)
}

// requireSnapshotServedCall enforces the table's grants for method against
// an already-loaded snapshot. Handlers that load the snapshot for their own
// answer use this so the gate costs no second load; the pair evaluated is
// still the table's, never a restated copy.
func (s *server) requireSnapshotServedCall(snapshot roleaccess.Snapshot, principal *trust.Principal, inv *transport.Invocation, method string) error {
	gates, ok := gatesForServedCall(method)
	if !ok {
		return featureAccessDenied(principal, inv)
	}
	roles := roleaccess.AssignedRoles(snapshot, principal.Subject(), principal.Roles())
	permissions := roleaccess.EffectivePagePermissions(snapshot, roles)
	features := roleaccess.EffectiveFeaturePermissions(snapshot, roles)
	for _, gate := range gates {
		if !registryFeatureDeclared(gate.pageID, gate.featureID) {
			continue
		}
		if roleaccess.CanFeatureAction(permissions, features, gate.pageID, gate.featureID, gate.action) {
			return nil
		}
	}
	return featureAccessDenied(principal, inv)
}
