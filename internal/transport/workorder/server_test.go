package workorder

import (
	"strings"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	workorderv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workorder/v1"
	domain "github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	workflowworkorder "github.com/monstercameron/human-capital-management-suite/internal/workflow/workorder"
)

func TestDecimalWireRoundTripPreservesScaleAndSign(t *testing.T) {
	for _, text := range []string{"0.00", "12.34", "-0.05"} {
		t.Run(text, func(t *testing.T) {
			want, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decimalValue(decimalMessage(want))
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != want.String() {
				t.Fatalf("round trip = %q, want %q", got.String(), want.String())
			}
		})
	}
}

func TestPageSizeIsBounded(t *testing.T) {
	if got, err := pageSize(nil); err != nil || got != defaultPageSize {
		t.Fatalf("default page size = %d, %v", got, err)
	}
	if got, err := pageSize(&commonv1.PageRequest{PageSize: 100}); err != nil || got != 100 {
		t.Fatalf("maximum page size = %d, %v", got, err)
	}
	if _, err := pageSize(&commonv1.PageRequest{PageSize: 101}); err == nil {
		t.Fatal("oversized page accepted")
	}
}

func TestWorkEntryDurationRejectsSilentTruncation(t *testing.T) {
	if _, err := durationMinutes(workorderv1.WorkEntryKind_WORK_ENTRY_KIND_LABOR, 59); err == nil {
		t.Fatal("fractional minute duration accepted")
	}
	if _, err := durationMinutes(workorderv1.WorkEntryKind_WORK_ENTRY_KIND_LABOR, 0); err == nil {
		t.Fatal("zero labor duration accepted")
	}
	if got, err := durationMinutes(workorderv1.WorkEntryKind_WORK_ENTRY_KIND_LABOR, 1800); err != nil || got != 30 {
		t.Fatalf("whole minute conversion=(%d,%v)", got, err)
	}
}

func TestSubmitRequestEnforcesKindAndCopiesBoundedFormValues(t *testing.T) {
	if err := validateDetailsKind(domain.RequestApproval, domain.RequestBudget); err == nil {
		t.Fatal("budget detail accepted under approval kind")
	}
	if err := validateDetailsKind(domain.RequestMaterial, domain.RequestMaterial); err != nil {
		t.Fatalf("matching typed request rejected: %v", err)
	}
	input := map[string]string{"location": "Riverside", "quantity": "4.00"}
	copied, err := copyFormValues(input)
	if err != nil {
		t.Fatal(err)
	}
	input["location"] = "forged after conversion"
	if copied["location"] != "Riverside" {
		t.Fatalf("converted form map aliased caller input: %v", copied)
	}
	if _, err := copyFormValues(map[string]string{"x": strings.Repeat("v", 4097)}); err == nil {
		t.Fatal("oversized form value accepted")
	}
	if _, err := spendDisposition(workorderv1.SpendDisposition_SPEND_DISPOSITION_COMMITMENT); err != nil {
		t.Fatalf("commitment disposition rejected: %v", err)
	}
	if _, err := spendDisposition(workorderv1.SpendDisposition_SPEND_DISPOSITION_UNSPECIFIED); err == nil {
		t.Fatal("unspecified spend disposition accepted")
	}
}

func TestTypedRequestProjectionRetainsResourceAndEvidenceDetails(t *testing.T) {
	qty, err := values.NewDecimal("4.00", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	cost, err := values.NewDecimal("28.50", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	got := requestMessage(domain.InitiatorRequest{ID: "r-1", Kind: domain.RequestMaterial, Status: domain.RequestPending, RoleOrItem: "stringer", Quantity: qty, Unit: "piece", Location: "Riverside", DueWindow: "2026-09-25T09:00:00Z", NeededUntil: "2026-09-26T09:00:00Z", EstimatedCost: cost, EstimatedCostSpecified: true, EstimatedCostCurrency: "USD", EvidenceRefs: []string{"e-1", "e-2"}}, "wo-1", 4)
	resource := got.GetResource()
	if resource == nil || resource.GetUnit() != "piece" || resource.GetEstimatedCost().GetCurrencyCode() != "USD" || len(resource.GetEvidence()) != 2 || resource.GetNeededUntil() == nil {
		t.Fatalf("resource request fields lost: %v", got)
	}
}

func TestDeniedCostProjectionOmitsOptionalMoney(t *testing.T) {
	budget := requestMessage(domain.InitiatorRequest{ID: "budget-1", Kind: domain.RequestBudget, Currency: "USD"}, "wo-1", 3).GetBudget()
	if budget == nil || budget.GetRequestedLimit() != nil {
		t.Fatalf("redacted budget amount serialized: %v", budget)
	}
	spend := latestSpend(domain.Snapshot{ID: "wo-1", Spending: []domain.Spend{{ID: "spend-1", Category: "MATERIAL", Disposition: domain.SpendIncurred, Currency: "USD"}}})
	if spend == nil || spend.GetAmount() != nil {
		t.Fatalf("redacted spend amount serialized: %v", spend)
	}
	if got := moneyMessage(values.Decimal{}, "USD"); got != nil {
		t.Fatalf("unset decimal serialized as money: %v", got)
	}
}

func TestWorkEntryProjectionRetainsOtherDescription(t *testing.T) {
	got := workEntryMessage(domain.WorkEntry{ID: "entry-1", WorkerID: "worker-1", Kind: "OTHER", WorkDate: "2026-09-25", TimeZone: "America/New_York", Description: "site support", DurationMinutes: 30}, "wo-1", 7)
	if got.GetKind() != workorderv1.WorkEntryKind_WORK_ENTRY_KIND_OTHER || got.GetDescription() != "site support" {
		t.Fatalf("work-entry description or kind lost: %v", got)
	}
}

func TestRestrictedNoteVisibilitiesFailClosed(t *testing.T) {
	if got, err := noteVisibility(workorderv1.NoteVisibility_NOTE_VISIBILITY_PARTICIPANTS); err != nil || got != "PARTICIPANTS" {
		t.Fatalf("participant visibility=(%q,%v)", got, err)
	}
	for _, visibility := range []workorderv1.NoteVisibility{workorderv1.NoteVisibility_NOTE_VISIBILITY_SUPERVISORS, workorderv1.NoteVisibility_NOTE_VISIBILITY_FINANCE} {
		if got, err := noteVisibility(visibility); err == nil || got != "" {
			t.Fatalf("restricted visibility %v allowed with %q", visibility, got)
		}
	}
	if got := mapError(domain.ErrNoteVisibilityUnsupported); got == nil {
		t.Fatal("unsupported restricted visibility error was not mapped")
	} else if owned, ok := envelope.As(got); !ok || owned.Code() != envelope.CodeFailedPrecondition {
		t.Fatalf("restricted visibility mapping=%v", got)
	}
}

func TestWorkOrderMessageMapsLifecycleAndAuditTimes(t *testing.T) {
	snap := domain.Snapshot{ID: "wo-1", ProjectID: "p-1", Title: "Repair", TemplateID: "field", TemplateVersion: "2", Phase: domain.PhaseAccepted, Revision: 3, Journal: []domain.Event{{At: mustTime("2026-09-25T10:00:00Z")}, {At: mustTime("2026-09-25T11:00:00Z")}}}
	got := workOrderMessage(snap)
	if got.GetStatus() != 4 || got.GetTemplateVersion() != "2" || got.GetCreatedAt().AsTime().Hour() != 10 || got.GetUpdatedAt().AsTime().Hour() != 11 {
		t.Fatalf("incorrect work order projection: %v", got)
	}
}

func TestScopeContextRejectsForgedTenantAndTreatsOrganizationAsSelector(t *testing.T) {
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "worker-1", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-1", IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := scopeContext(&commonv1.ScopeContext{TenantId: "tenant-a", OrganizationScopeId: "forged-project-id"}, p)
	if err != nil || project != "" {
		t.Fatalf("unverified organization scope became project selector=(%q,%v)", project, err)
	}
	if _, err = scopeContext(&commonv1.ScopeContext{TenantId: "tenant-b", OrganizationScopeId: "project-1"}, p); err == nil {
		t.Fatal("foreign tenant scope accepted")
	}
}

func TestOwnedWorkflowErrorsProjectToStableTransportCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want envelope.Code
	}{{"unbound phase", workflowworkorder.ErrPhaseBinding, envelope.CodeFailedPrecondition}, {"stale facts", workflowworkorder.ErrStaleGateFacts, envelope.CodeAborted}, {"denied", workorderaccess.ErrUnauthorized, envelope.CodePermissionDenied}, {"stale revision", domain.ErrStale, envelope.CodeAborted}} {
		t.Run(tc.name, func(t *testing.T) {
			mapped := mapError(tc.err)
			owned, ok := envelope.As(mapped)
			if !ok {
				t.Fatalf("error %T is not owned", mapped)
			}
			if owned.Code() != tc.want {
				t.Fatalf("code=%s want %s", owned.Code(), tc.want)
			}
		})
	}
}

func mustTime(s string) time.Time {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return v
}
