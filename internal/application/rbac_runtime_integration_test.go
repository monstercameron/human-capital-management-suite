package application

// TestRBACRuntime is the runtime RBAC suite: one composed serve cell (see
// rbac_runtime_fixture_test.go), a table of {user, operation, subject,
// expectation} cases, and a ratchet. Every expectation is the TARGET
// behaviour, each justified by the rule and its source in a comment; a case
// the running system does not meet yet is listed in rbacKnownGaps against the
// todo that closes it. A listed gap that starts passing fails the suite so
// the list only ever shrinks.

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// rbacOutcome is one expected or observed authorization outcome.
type rbacOutcome string

const (
	rbacAllow   rbacOutcome = "ALLOW"
	rbacDeny    rbacOutcome = "DENY" // PermissionDenied / NotFound / HTTP 403 or a redirect away
	rbacUnauth  rbacOutcome = "UNAUTHENTICATED"
	rbacRefused rbacOutcome = "REFUSED" // DENY or UNAUTHENTICATED: either refusal is correct
	rbacFieldOn rbacOutcome = "FIELD_VISIBLE"
	rbacFieldNo rbacOutcome = "FIELD_HIDDEN" // empty or masked, or the row/call itself withheld
	rbacRowOn   rbacOutcome = "ROW_VISIBLE"
	rbacRowNo   rbacOutcome = "ROW_HIDDEN" // not listed, or the call itself refused
	rbacError   rbacOutcome = "ERROR"      // any other failure: never a pass
)

// rbacResult is what one operation observed.
type rbacResult struct {
	class  rbacOutcome // rbacAllow, rbacDeny, rbacUnauth or rbacError
	detail string
	row    *bool // the subject row is present (list operations)
	field  *bool // the governed field is disclosed (field operations)
}

type rbacOp func(h *rbacHarness, user, subject string) rbacResult

type rbacCase struct {
	ID      string
	User    string
	Op      string
	Subject string
	Expect  rbacOutcome
}

// rbacKnownGaps maps every case the running system does not yet meet to the
// todo that closes it. Populated from observed failures; never from guesses.
var rbacKnownGaps = map[string]string{
	// GetRoleAccess still trusts the credential's tenant-scoped role without
	// resolving the principal against this cell's durable assignments.
	"A-05": "RBAC-RT-010",
	// hr_partner is granted pay by unit visibility, not an HR-partner relationship.
	"C-12": "RBAC-RT-007",
	// payroll_manager's directory is its own unit, not the org boundary.
	"C-14": "RBAC-RT-008",
	// IntentService and ListJourneys authorize by tenant, not by relationship.
	"G-02": "RBAC-RT-003",
	"G-03": "RBAC-RT-003",
	"G-05": "RBAC-RT-003",
	"G-06": "RBAC-RT-003",
	"G-08": "RBAC-RT-003",
	"G-09": "RBAC-RT-003",
	"G-11": "RBAC-RT-003",
	"K-04": "RBAC-RT-003",
	"K-05": "RBAC-RT-003",
	"K-06": "RBAC-RT-003",
	"F-08": "RBAC-RT-003",
	// Workflow inspection has the tenant and coarse action checks but does not
	// yet authorize the requested instance against its business subject.
	"J-03": "RBAC-RT-003",
	"J-04": "RBAC-RT-003",
	"J-05": "RBAC-RT-003",
	"K-09": "RBAC-RT-003",
	// hcm_admin and auditor are refused the inspection that carries diagnostics.
	"H-04": "RBAC-RT-005",
	"H-06": "RBAC-RT-005",
	// A roleless principal lists work items (an empty page, not a refusal).
	"K-07": "RBAC-RT-004",
	// Gate B (rbac_runtime_pages_test.go).
	// worker_self is granted self-service pages whose data read refuses it.
	"B-10": "RBAC-RT-020",
	// Served calls with no page/feature/action gate; a note written under view.
	"P1-04": "RBAC-RT-016",
	"P1-06": "RBAC-RT-016",
	"P1-07": "RBAC-RT-016",
	// An inactive role still grants; built-in roles can be renamed/deactivated.
	"P3-04": "RBAC-RT-017",
	"P3-05": "RBAC-RT-017",
	"P3-06": "RBAC-RT-017",
	"P3-07": "RBAC-RT-017",
	// Role administration follows token roles; no self-escalation or lockout guard.
	"P4-01": "RBAC-RT-018",
	"P4-02": "RBAC-RT-018",
	"P4-03": "RBAC-RT-018",
	// A page grant creates no feature rows; a page revoke leaves them.
	"P5-02": "RBAC-RT-019",
	"P5-04": "RBAC-RT-019",
	// Navigation lists a page whose content feature is revoked.
	"P6-03": "RBAC-RT-020",
	// A permission change leaves no ledger event or revision row.
	"P7-01": "RBAC-RT-021",
}

func boolPtr(v bool) *bool { return &v }

// grpcResult classifies a gRPC error. tolerateValidation treats a refusal
// that is about the request's content (invalid, stale version, exists) as
// having passed authorization, which is what ALLOW means for a write probe.
func grpcResult(err error, tolerateValidation bool) rbacResult {
	if err == nil {
		return rbacResult{class: rbacAllow, detail: "OK"}
	}
	code := status.Code(err)
	// The owned error's reason names the layer that refused (a
	// journey.feature_action.* reason is the page/feature gate, a
	// journey.<op>.denied reason is the engine).
	named := code.String()
	for _, detail := range status.Convert(err).Details() {
		if reasoned, ok := detail.(interface{ GetReasonRef() string }); ok && reasoned.GetReasonRef() != "" {
			named += " " + reasoned.GetReasonRef()
		}
	}
	switch code {
	case codes.PermissionDenied, codes.NotFound:
		return rbacResult{class: rbacDeny, detail: named}
	case codes.Unauthenticated:
		return rbacResult{class: rbacUnauth, detail: named}
	case codes.InvalidArgument, codes.Aborted, codes.FailedPrecondition, codes.AlreadyExists:
		if tolerateValidation {
			return rbacResult{class: rbacAllow, detail: "authorized, then " + named}
		}
	}
	return rbacResult{class: rbacError, detail: fmt.Sprintf("%s: %v", code, err)}
}

// judge decides whether r meets expect and renders what was observed.
func judge(expect rbacOutcome, r rbacResult) (bool, string) {
	call := fmt.Sprintf("%s (%s)", r.class, r.detail)
	switch expect {
	case rbacAllow, rbacDeny, rbacUnauth:
		return r.class == expect, call
	case rbacRefused:
		return r.class == rbacDeny || r.class == rbacUnauth, call
	case rbacRowOn, rbacRowNo:
		if r.class != rbacAllow {
			return expect == rbacRowNo && (r.class == rbacDeny || r.class == rbacUnauth), call
		}
		observed := rbacRowNo
		if r.row != nil && *r.row {
			observed = rbacRowOn
		}
		return observed == expect, string(observed)
	case rbacFieldOn, rbacFieldNo:
		if r.class != rbacAllow {
			return expect == rbacFieldNo && (r.class == rbacDeny || r.class == rbacUnauth), call
		}
		if r.row != nil && !*r.row {
			return expect == rbacFieldNo, "ROW_HIDDEN"
		}
		observed := rbacFieldNo
		if r.field != nil && *r.field {
			observed = rbacFieldOn
		}
		return observed == expect, fmt.Sprintf("%s (%s)", observed, r.detail)
	}
	return false, "unknown expectation " + string(expect)
}

// sameDecimal reports whether two decimal strings denote the same value.
func sameDecimal(a, b string) bool {
	x, okA := new(big.Rat).SetString(a)
	y, okB := new(big.Rat).SetString(b)
	return okA && okB && x.Cmp(y) == 0
}

// rbacOps are the operations the table names.
var rbacOps = map[string]rbacOp{
	"Journey.ListWorkers": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.journey.ListWorkers(h.rpc(user), &journeyv1.ListWorkersRequest{})
		return grpcResult(err, false)
	},
	// row: the subject worker is listed.
	"Journey.ListWorkers/row": func(h *rbacHarness, user, subject string) rbacResult {
		resp, err := h.journey.ListWorkers(h.rpc(user), &journeyv1.ListWorkersRequest{})
		r := grpcResult(err, false)
		if err == nil {
			found := false
			for _, w := range resp.GetWorkers() {
				found = found || w.GetWorkerRef() == subject
			}
			r.row = boolPtr(found)
		}
		return r
	},
	// field: the subject's BasePay or BonusTarget is disclosed unmasked.
	"Journey.ListWorkers/pay": func(h *rbacHarness, user, subject string) rbacResult {
		resp, err := h.journey.ListWorkers(h.rpc(user), &journeyv1.ListWorkersRequest{})
		r := grpcResult(err, false)
		if err != nil {
			return r
		}
		want := h.workers[subject]
		r.row = boolPtr(false)
		for _, w := range resp.GetWorkers() {
			if w.GetWorkerRef() != subject {
				continue
			}
			r.row = boolPtr(true)
			r.field = boolPtr(sameDecimal(w.GetBasePay(), want.base) || sameDecimal(w.GetBonusTarget(), want.bonus))
			r.detail = fmt.Sprintf("base_pay=%q bonus_target=%q", w.GetBasePay(), w.GetBonusTarget())
		}
		return r
	},
	// row: the listing holds the subject and nobody else.
	"Journey.ListWorkers/self-only": func(h *rbacHarness, user, subject string) rbacResult {
		resp, err := h.journey.ListWorkers(h.rpc(user), &journeyv1.ListWorkersRequest{})
		r := grpcResult(err, false)
		if err == nil {
			r.row = boolPtr(len(resp.GetWorkers()) == 1 && resp.GetWorkers()[0].GetWorkerRef() == subject)
			r.detail = fmt.Sprintf("%d rows", len(resp.GetWorkers()))
		}
		return r
	},
	"Workspace.GET": func(h *rbacHarness, user, page string) rbacResult {
		code, err := h.page(user, page)
		if err != nil {
			return rbacResult{class: rbacError, detail: err.Error()}
		}
		detail := fmt.Sprintf("HTTP %d", code)
		switch {
		case code == http.StatusOK:
			return rbacResult{class: rbacAllow, detail: detail}
		case code == http.StatusUnauthorized:
			return rbacResult{class: rbacUnauth, detail: detail}
		case code == http.StatusForbidden || (code >= 300 && code < 400):
			return rbacResult{class: rbacDeny, detail: detail}
		}
		// Every page the table names is a registered route, so a 404 is a
		// wrong path in this suite, never a refusal: it must not pass as DENY.
		return rbacResult{class: rbacError, detail: detail}
	},
	"Journey.GetRoleAccess": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.journey.GetRoleAccess(h.rpc(user), &journeyv1.GetRoleAccessRequest{})
		return grpcResult(err, false)
	},
	// A re-save of the subject's current durable assignment at its current
	// version: the least harmful write that still exercises the gate.
	"Journey.SaveWorkerRoleAssignment": func(h *rbacHarness, user, subject string) rbacResult {
		version, roles := h.assignment(subject)
		_, err := h.journey.SaveWorkerRoleAssignment(h.rpc(user), &journeyv1.SaveWorkerRoleAssignmentRequest{
			Assignment: &journeyv1.WorkerRoleAssignment{Version: version, WorkerRef: subject, RoleIds: roles}})
		return grpcResult(err, true)
	},
	// Creates the inert rbac_probe role nobody holds.
	"Journey.SaveAccessRole": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.journey.SaveAccessRole(h.rpc(user), &journeyv1.SaveAccessRoleRequest{
			Role: &journeyv1.AccessRole{RoleId: "rbac_probe", Name: "RBAC probe", Description: "Inert role the RBAC runtime suite writes.", Active: true}})
		return grpcResult(err, true)
	},
	// Sets rbac_probe's directory boundary to OWN_UNIT.
	"Journey.SaveRoleOrganizationVisibility": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.journey.SaveRoleOrganizationVisibility(h.rpc(user), &journeyv1.SaveRoleOrganizationVisibilityRequest{
			Policy: &journeyv1.RoleOrganizationVisibilityPolicy{Version: h.visibilityVersion("rbac_probe"), RoleId: "rbac_probe", Mode: roleaccess.VisibilityOwnUnit}})
		return grpcResult(err, true)
	},
	"Journey.ProposeJourney(fixture)": func(h *rbacHarness, _, _ string) rbacResult {
		return grpcResult(h.proposeErr, false)
	},
	"Journey.ListJourneys/row": func(h *rbacHarness, user, _ string) rbacResult {
		resp, err := h.journey.ListJourneys(h.rpc(user), &journeyv1.ListJourneysRequest{})
		r := grpcResult(err, false)
		if err == nil {
			found := false
			for _, j := range resp.GetJourneys() {
				found = found || j.GetIntentId() == h.intentID
			}
			r.row = boolPtr(found)
		}
		return r
	},
	"Journey.ListJourneys": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.journey.ListJourneys(h.rpc(user), &journeyv1.ListJourneysRequest{})
		return grpcResult(err, false)
	},
	"Journey.InspectJourney": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.journey.InspectJourney(h.rpc(user), &journeyv1.InspectJourneyRequest{IntentId: h.intentID})
		return grpcResult(err, false)
	},
	// field: the diagnostics disclosure (nodes, transitions, evidence ids,
	// planned writes, ledger) is present.
	"Journey.InspectJourney/diagnostics": func(h *rbacHarness, user, _ string) rbacResult {
		resp, err := h.journey.InspectJourney(h.rpc(user), &journeyv1.InspectJourneyRequest{IntentId: h.intentID})
		r := grpcResult(err, false)
		if err == nil {
			d := resp.GetDetail()
			r.field = boolPtr(len(d.GetNodes()) > 0 || len(d.GetTransitions()) > 0 || len(d.GetEvidenceIds()) > 0 || len(d.GetPlannedWrites()) > 0 || d.GetLedger() != nil)
			r.detail = fmt.Sprintf("nodes=%d transitions=%d evidence=%d planned=%d", len(d.GetNodes()), len(d.GetTransitions()), len(d.GetEvidenceIds()), len(d.GetPlannedWrites()))
		}
		return r
	},
	"Intent.GetIntent": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.intents.GetIntent(h.rpc(user), &intentsv1.GetIntentRequest{IntentId: h.intentID})
		return grpcResult(err, false)
	},
	"Intent.ListIntents": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.intents.ListIntents(h.rpc(user), &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: 100}})
		return grpcResult(err, false)
	},
	"Intent.ListIntents/row": func(h *rbacHarness, user, _ string) rbacResult {
		resp, err := h.intents.ListIntents(h.rpc(user), &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: 100}})
		r := grpcResult(err, false)
		if err == nil {
			found := false
			for _, i := range resp.GetIntents() {
				found = found || i.GetIntentId() == h.intentID
			}
			r.row = boolPtr(found)
		}
		return r
	},
	"Intent.ListIntentTimeline": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.intents.ListIntentTimeline(h.rpc(user), &intentsv1.ListIntentTimelineRequest{IntentId: h.intentID})
		return grpcResult(err, false)
	},
	"Admin.GetReleaseManifest": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.admin.GetReleaseManifest(h.rpc(user), &adminv1.GetReleaseManifestRequest{})
		return grpcResult(err, false)
	},
	"Admin.ListCapabilityProfiles": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.admin.ListCapabilityProfiles(h.rpc(user), &adminv1.ListCapabilityProfilesRequest{})
		return grpcResult(err, false)
	},
	"Work.ListWorkItems": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.work.ListWorkItems(h.rpc(user), &humanworkv1.ListWorkItemsRequest{})
		return grpcResult(err, false)
	},
	"Work.ListWorkItems/row": func(h *rbacHarness, user, _ string) rbacResult {
		resp, err := h.work.ListWorkItems(h.rpc(user), &humanworkv1.ListWorkItemsRequest{})
		r := grpcResult(err, false)
		if err == nil {
			found := false
			for _, item := range resp.GetWorkItems() {
				found = found || item.GetWorkItemId() == h.financeItemID
			}
			r.row = boolPtr(found)
		}
		return r
	},
	"Work.GetWorkItem": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.work.GetWorkItem(h.rpc(user), &humanworkv1.GetWorkItemRequest{WorkItemId: h.financeItemID})
		return grpcResult(err, false)
	},
	"Workflow.GetWorkflow": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.workflows.GetWorkflow(h.rpc(user), &workflowv1.GetWorkflowRequest{InstanceId: h.instanceID})
		return grpcResult(err, false)
	},
	"Workflow.ListNodeExecutions": func(h *rbacHarness, user, _ string) rbacResult {
		_, err := h.workflows.ListNodeExecutions(h.rpc(user), &workflowv1.ListNodeExecutionsRequest{InstanceId: h.instanceID})
		return grpcResult(err, false)
	},
}

// assignment reads subject's current durable assignment.
func (h *rbacHarness) assignment(subject string) (int64, []string) {
	h.t.Helper()
	snapshot, err := h.composed.Cell().RoleAccess.Load(context.Background(), kernelvalues.TenantId(h.cfg.Tenant), rbacOrgScope)
	if err != nil {
		h.t.Fatalf("load role access: %v", err)
	}
	for _, a := range snapshot.Assignments {
		if a.WorkerRef == subject {
			return a.Version, a.RoleIDs
		}
	}
	h.t.Fatalf("no durable assignment for %s", subject)
	return 0, nil
}

// visibilityVersion is role's current visibility policy version, or 0.
func (h *rbacHarness) visibilityVersion(role string) int64 {
	h.t.Helper()
	snapshot, err := h.composed.Cell().RoleAccess.Load(context.Background(), kernelvalues.TenantId(h.cfg.Tenant), rbacOrgScope)
	if err != nil {
		h.t.Fatalf("load role access: %v", err)
	}
	for _, p := range snapshot.Policies {
		if p.RoleID == role {
			return p.Version
		}
	}
	return 0
}

// rbacCases is the table. Sources cited:
//
//	[POL]  internal/trust/authz/policy.go PolicyTable / administrativeRoleOrder
//	[PAGE] internal/experience/roleaccess/roleaccess.go DefaultPagePermissions
//	[VIS]  roleaccess.PoliciesForRoles (unconfigured role => OWN_UNIT) and
//	       transport/journey withManagedReports (a manager sees their chain)
//	[ASG]  roleaccess.AssignedRoles: the durable assignment, when present,
//	       is the principal's role set
//	[ADM]  transport/journey requireRoleAdministrator (hcm_admin, comp_admin)
//	[OPS]  operations/admin RequireOperator (hcmnext.trust.role.operator)
//	[DIAG] roleaccess.PageJourneyDiagnostics + transport/journey
//	       diagnosticsAuthorized (hcm_admin, comp_admin; auditor per this todo)
//	[AUTH] trust.HMACVerifier: signature, issuer, audience, validity window;
//	       the cell serves ServeConfig.Tenant only
var rbacCases = []rbacCase{
	// A. Authentication [AUTH].
	{"A-01", "expired", "Journey.ListWorkers", "", rbacUnauth},                  // exp in the past is no credential
	{"A-02", "forged", "Journey.ListWorkers", "", rbacUnauth},                   // signed under another key
	{"A-03", "otherTenant", "Journey.ListWorkers", "", rbacRefused},             // tenant claim != the cell's tenant
	{"A-04", "forged", "Intent.GetIntent", "", rbacUnauth},                      // same gate on the intent surface
	{"A-05", "otherTenant", "Journey.GetRoleAccess", "", rbacRefused},           // another tenant's hcm_admin administers nothing here
	{"A-06", "otherTenant", "Workspace.GET", "admin/roles", rbacRefused},        // the browser surface binds the tenant too
	{"A-07", "otherTenant", "Journey.ListWorkers/pay", "rbac-eli", rbacFieldNo}, // no pay crosses tenants

	// B. Directory rows [VIS][PAGE].
	{"B-01", "dana", "Journey.ListWorkers/row", "rbac-eli", rbacRowOn},  // direct report
	{"B-02", "dana", "Journey.ListWorkers/row", "rbac-fay", rbacRowOn},  // direct report
	{"B-03", "dana", "Journey.ListWorkers/row", "rbac-otto", rbacRowOn}, // same unit: manager OWN_UNIT default
	{"B-04", "dana", "Journey.ListWorkers/row", "rbac-ivy", rbacRowNo},  // unit B, outside dana's chain
	{"B-05", "hana", "Journey.ListWorkers/row", "rbac-eli", rbacRowNo},  // unit A, outside hana's chain
	{"B-06", "hana", "Journey.ListWorkers/row", "rbac-ivy", rbacRowOn},  // direct report
	{"B-07", "gus", "Journey.ListWorkers/row", "rbac-eli", rbacRowOn},   // skip-level report
	{"B-08", "gus", "Journey.ListWorkers/row", "rbac-ivy", rbacRowOn},   // skip-level report in another unit
	{"B-09", "admin", "Journey.ListWorkers/row", "rbac-ivy", rbacRowOn}, // hcm_admin sees the tenant
	// RBAC-RT-020: the self-service pages granted to worker_self (home,
	// myself) read ListWorkers, so the target is scoped data, not a refusal:
	// eli's listing holds exactly eli.
	{"B-10", "eli", "Journey.ListWorkers/self-only", "rbac-eli", rbacRowOn},
	{"B-11", "financePartner", "Journey.ListWorkers", "", rbacDeny}, // finance_partner reaches no directory [PAGE]
	{"B-12", "auditor", "Journey.ListWorkers", "", rbacDeny},        // auditor is no product role; no page grant [PAGE]

	// C. Directory pay fields: visible only to self, the management chain,
	// comp_admin/hcm_admin and the administrative pay roles [POL].
	{"C-01", "dana", "Journey.ListWorkers/pay", "rbac-eli", rbacFieldOn},           // p1a.manager.compensation, manager of eli
	{"C-02", "dana", "Journey.ListWorkers/pay", "rbac-fay", rbacFieldOn},           // manager of fay
	{"C-03", "dana", "Journey.ListWorkers/pay", "rbac-otto", rbacFieldNo},          // manager grant is chain-scoped; otto is a peer's report
	{"C-04", "dana", "Journey.ListWorkers/pay", "rbac-gus", rbacFieldNo},           // upward: gus is dana's manager
	{"C-05", "hana", "Journey.ListWorkers/pay", "rbac-ivy", rbacFieldOn},           // manager of ivy
	{"C-06", "gus", "Journey.ListWorkers/pay", "rbac-eli", rbacFieldOn},            // chain (skip level)
	{"C-07", "gus", "Journey.ListWorkers/pay", "rbac-ivy", rbacFieldOn},            // chain across units
	{"C-08", "compAdmin", "Journey.ListWorkers/pay", "rbac-eli", rbacFieldOn},      // p1a.comp_admin.compensation, administrative scope
	{"C-09", "compAdmin", "Journey.ListWorkers/pay", "rbac-ivy", rbacFieldOn},      // administrative scope is the tenant/org, not a unit
	{"C-10", "admin", "Journey.ListWorkers/pay", "rbac-ivy", rbacFieldOn},          // hcm_admin (task rule: comp_admin/hcm_admin)
	{"C-11", "eli", "Journey.ListWorkers/pay", "rbac-fay", rbacFieldNo},            // p1a.worker_self.compensation is self only
	{"C-12", "hrPartner", "Journey.ListWorkers/pay", "rbac-eli", rbacFieldNo},      // p1a.hr_partner is relationship-scoped; no HR-partner relationship exists for eli
	{"C-13", "payroll", "Journey.ListWorkers/pay", "rbac-eli", rbacFieldOn},        // p1a.payroll_manager.compensation under payroll_processing
	{"C-14", "payroll", "Journey.ListWorkers/pay", "rbac-ivy", rbacFieldOn},        // administrativeRoleOrder: scope is the org boundary, not own unit
	{"C-15", "auditor", "Journey.ListWorkers/pay", "rbac-eli", rbacFieldNo},        // p1a.auditor.compensation is REDACTED: raw value never returned
	{"C-16", "financePartner", "Journey.ListWorkers/pay", "rbac-eli", rbacFieldNo}, // finance_partner has no directory [PAGE]; its compensation grant is the routed review
	{"C-17", "hana", "Journey.ListWorkers/pay", "rbac-otto", rbacFieldNo},          // outside hana's chain and unit

	// D. Admin pages fail closed [PAGE].
	{"D-01", "eli", "Workspace.GET", "admin/roles", rbacDeny},
	{"D-02", "eli", "Workspace.GET", "admin", rbacDeny},
	{"D-03", "eli", "Workspace.GET", "studio", rbacDeny},
	{"D-04", "eli", "Workspace.GET", "admin/organization-visibility", rbacDeny},
	{"D-05", "eli", "Workspace.GET", "admin/worker-ids", rbacDeny},
	{"D-06", "noRoles", "Workspace.GET", "admin/roles", rbacDeny},
	{"D-07", "noRoles", "Workspace.GET", "admin", rbacDeny},
	{"D-08", "noRoles", "Workspace.GET", "studio", rbacDeny},
	{"D-09", "noRoles", "Workspace.GET", "admin/organization-visibility", rbacDeny},
	{"D-10", "noRoles", "Workspace.GET", "admin/worker-ids", rbacDeny},
	{"D-11", "admin", "Workspace.GET", "admin/roles", rbacAllow}, // hcm_admin holds every page
	{"D-12", "admin", "Workspace.GET", "admin", rbacAllow},
	{"D-13", "admin", "Workspace.GET", "studio", rbacAllow},
	{"D-14", "admin", "Workspace.GET", "admin/organization-visibility", rbacAllow},
	{"D-15", "admin", "Workspace.GET", "admin/worker-ids", rbacAllow},
	{"D-16", "dana", "Workspace.GET", "admin/roles", rbacDeny}, // manager: no roles page
	{"D-17", "dana", "Workspace.GET", "admin", rbacDeny},
	{"D-18", "eli", "Workspace.GET", "people", rbacDeny},            // worker_self: no people page
	{"D-19", "financePartner", "Workspace.GET", "people", rbacDeny}, // finance_partner: no people page
	{"D-20", "eli", "Workspace.GET", "home", rbacAllow},             // control: worker_self holds home
	{"D-21", "noRoles", "Workspace.GET", "home", rbacDeny},          // no role, no page

	// G. Intents: read authority follows the subject relationship, not the
	// tenant [VIS][PAGE].
	{"G-00", "dana", "Journey.ProposeJourney(fixture)", "rbac-eli", rbacAllow}, // manager of eli; manager holds journeys:create
	{"G-01", "dana", "Intent.GetIntent", "", rbacAllow},                        // the subject's direct manager (and its routed manager approver)
	{"G-02", "eli", "Intent.GetIntent", "", rbacDeny},                          // a peer of the subject
	{"G-03", "hana", "Intent.GetIntent", "", rbacDeny},                         // manager outside the subject's chain
	{"G-04", "dana", "Intent.ListIntents/row", "", rbacRowOn},
	{"G-05", "eli", "Intent.ListIntents/row", "", rbacRowNo},
	{"G-06", "hana", "Intent.ListIntents/row", "", rbacRowNo},
	{"G-07", "dana", "Intent.ListIntentTimeline", "", rbacAllow},
	{"G-08", "eli", "Intent.ListIntentTimeline", "", rbacDeny},
	{"G-09", "hana", "Intent.ListIntentTimeline", "", rbacDeny},
	{"G-10", "dana", "Journey.ListJourneys/row", "", rbacRowOn},
	{"G-11", "hana", "Journey.ListJourneys/row", "", rbacRowNo},
	{"G-12", "eli", "Journey.ListJourneys/row", "", rbacRowNo},

	// H. Journey inspection and the diagnostics disclosure [DIAG].
	{"H-01", "dana", "Journey.InspectJourney", "", rbacAllow},
	{"H-02", "eli", "Journey.InspectJourney", "", rbacDeny},  // worker_self: no journeys/work page
	{"H-03", "hana", "Journey.InspectJourney", "", rbacDeny}, // outside the subject's chain
	{"H-04", "admin", "Journey.InspectJourney/diagnostics", "", rbacFieldOn},
	{"H-05", "compAdmin", "Journey.InspectJourney/diagnostics", "", rbacFieldOn},
	{"H-06", "auditor", "Journey.InspectJourney/diagnostics", "", rbacFieldOn},
	{"H-07", "dana", "Journey.InspectJourney/diagnostics", "", rbacFieldNo},           // a manager who may approve has no diagnostics grant
	{"H-08", "financePartner", "Journey.InspectJourney/diagnostics", "", rbacFieldNo}, // the routed approver has none either

	// I. AdminService is the operator's surface only [OPS].
	{"I-01", "admin", "Admin.GetReleaseManifest", "", rbacDeny}, // hcm_admin is not the platform operator
	{"I-02", "compAdmin", "Admin.GetReleaseManifest", "", rbacDeny},
	{"I-03", "eli", "Admin.GetReleaseManifest", "", rbacDeny},
	{"I-04", "operator", "Admin.GetReleaseManifest", "", rbacAllow},
	{"I-05", "operator", "Admin.ListCapabilityProfiles", "", rbacAllow},
	{"I-06", "dana", "Admin.ListCapabilityProfiles", "", rbacDeny},

	// J. Work and Workflow: an outsider sees nothing of dana's journey;
	// the routed assignee sees its item (workitem.MembershipOf).
	{"J-01", "hana", "Work.ListWorkItems/row", "", rbacRowNo},
	{"J-02", "hana", "Work.GetWorkItem", "", rbacDeny},
	{"J-03", "hana", "Workflow.GetWorkflow", "", rbacDeny},
	{"J-04", "hana", "Workflow.ListNodeExecutions", "", rbacDeny},
	{"J-05", "eli", "Workflow.GetWorkflow", "", rbacDeny},
	{"J-06", "eli", "Work.GetWorkItem", "", rbacDeny},
	{"J-07", "financePartner", "Work.GetWorkItem", "", rbacAllow}, // the routed assignee
	{"J-08", "financePartner", "Work.ListWorkItems/row", "", rbacRowOn},

	// K. A principal with no role reads nothing [PAGE][POL deny-by-default].
	{"K-01", "noRoles", "Journey.ListWorkers", "", rbacDeny},
	{"K-02", "noRoles", "Journey.ListJourneys", "", rbacDeny},
	{"K-03", "noRoles", "Journey.InspectJourney", "", rbacDeny},
	{"K-04", "noRoles", "Intent.GetIntent", "", rbacDeny},
	{"K-05", "noRoles", "Intent.ListIntents", "", rbacDeny},
	{"K-06", "noRoles", "Intent.ListIntentTimeline", "", rbacDeny},
	{"K-07", "noRoles", "Work.ListWorkItems", "", rbacDeny},
	{"K-08", "noRoles", "Work.GetWorkItem", "", rbacDeny},
	{"K-09", "noRoles", "Workflow.GetWorkflow", "", rbacDeny},
	{"K-10", "noRoles", "Journey.GetRoleAccess", "", rbacDeny},
	{"K-11", "noRoles", "Admin.GetReleaseManifest", "", rbacDeny},

	// F. Revocation: the durable assignment (worker_self) governs, not the
	// credential's comp_admin [ASG].
	{"F-01", "revoked", "Journey.GetRoleAccess", "", rbacDeny},
	{"F-02", "revoked", "Journey.SaveWorkerRoleAssignment", "rbac-fay", rbacDeny},
	{"F-03", "revoked", "Journey.SaveAccessRole", "", rbacDeny},
	{"F-04", "revoked", "Workspace.GET", "admin/roles", rbacDeny},
	{"F-05", "revoked", "Workspace.GET", "admin", rbacDeny},
	{"F-06", "revoked", "Journey.ListWorkers/pay", "rbac-eli", rbacFieldNo},
	{"F-07", "revoked", "Journey.InspectJourney", "", rbacDeny},
	{"F-08", "revoked", "Intent.GetIntent", "", rbacDeny},
	{"F-09", "revoked", "Journey.InspectJourney/diagnostics", "", rbacFieldNo},

	// E. Role administration [ADM][PAGE roles]. Writes last: they are inert
	// (a re-save, a role nobody holds) but ordered after every read anyway.
	{"E-01", "dana", "Journey.GetRoleAccess", "", rbacDeny},
	{"E-02", "eli", "Journey.GetRoleAccess", "", rbacDeny},
	{"E-03", "hrPartner", "Journey.GetRoleAccess", "", rbacDeny},
	{"E-04", "admin", "Journey.GetRoleAccess", "", rbacAllow},
	{"E-05", "compAdmin", "Journey.GetRoleAccess", "", rbacAllow},
	{"E-06", "dana", "Journey.SaveWorkerRoleAssignment", "rbac-fay", rbacDeny},
	{"E-07", "eli", "Journey.SaveWorkerRoleAssignment", "rbac-eli", rbacDeny}, // not even for oneself
	{"E-08", "hana", "Journey.SaveWorkerRoleAssignment", "rbac-ivy", rbacDeny},
	{"E-09", "operator", "Journey.SaveWorkerRoleAssignment", "rbac-fay", rbacDeny}, // platform operator is not an HCM role administrator
	{"E-10", "admin", "Journey.SaveWorkerRoleAssignment", "rbac-fay", rbacAllow},
	{"E-11", "dana", "Journey.SaveAccessRole", "", rbacDeny},
	{"E-12", "admin", "Journey.SaveAccessRole", "", rbacAllow},
	{"E-13", "dana", "Journey.SaveRoleOrganizationVisibility", "", rbacDeny},
	{"E-14", "admin", "Journey.SaveRoleOrganizationVisibility", "", rbacAllow},
}

// TestRBACRuntime runs every case against one composed cell.
func TestRBACRuntime(t *testing.T) {
	h := rbacCompose(t)
	seen := map[string]bool{}
	for _, c := range rbacCases {
		h.runCase(t, seen, c, nil)
	}
	// The page-visibility and per-page CRUD cases (rbac_runtime_pages_test.go)
	// run after every read above: several of them change grants.
	h.issuePageUsers()
	for _, step := range rbacPageCases {
		h.runCase(t, seen, step.rbacCase, step.setup)
	}
	for id := range rbacKnownGaps {
		if !seen[id] {
			t.Errorf("rbacKnownGaps names %s, which is no case", id)
		}
	}
}

// runCase runs one case under the ratchet. setup, when set, prepares the
// state the case probes and fails the suite if it cannot.
func (h *rbacHarness) runCase(t *testing.T, seen map[string]bool, c rbacCase, setup func(*rbacHarness)) {
	t.Helper()
	if seen[c.ID] {
		t.Fatalf("duplicate case id %s", c.ID)
	}
	seen[c.ID] = true
	op, ok := rbacOps[c.Op]
	if !ok {
		t.Fatalf("case %s names unknown operation %q", c.ID, c.Op)
	}
	t.Run(c.ID, func(t *testing.T) {
		if setup != nil {
			setup(h)
		}
		pass, observed := judge(c.Expect, op(h, c.User, c.Subject))
		todo, known := rbacKnownGaps[c.ID]
		t.Logf("CASE %s | %s | %s %s | expect %s | observed %s", c.ID, c.User, c.Op, c.Subject, c.Expect, observed)
		switch {
		case !pass && known:
			t.Logf("KNOWN GAP %s (%s): expected %s, got %s", c.ID, todo, c.Expect, observed)
		case pass && known:
			t.Errorf("gap %s closed: remove it from rbacKnownGaps", c.ID)
		case !pass:
			t.Errorf("%s: %s %s %s: expected %s, got %s", c.ID, c.User, c.Op, c.Subject, c.Expect, observed)
		}
	})
}
