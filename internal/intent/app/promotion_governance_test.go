package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestGovernanceStandingAnswersTheDelegationsCurrentAuthority proves the
// standing carries the cell's control versions and reports an authorized
// delegation as authorized, and a revoked one as refused with its reason
// rather than as an error revalidation could not record.
func TestGovernanceStandingAnswersTheDelegationsCurrentAuthority(t *testing.T) {
	ctx := context.Background()
	h := newStepHarness(t, nil)
	standing, err := h.services.GovernanceStanding(ctx, h.call)
	if err != nil {
		t.Fatalf("GovernanceStanding: %v", err)
	}
	if !standing.Authorized || standing.Refusal != "" {
		t.Fatalf("standing = %+v, want an authorized delegation", standing)
	}
	if standing.Subject != h.call.Delegation.Subject || standing.RequiredRole != "promotion_operator" ||
		standing.Purpose != h.call.Delegation.Purposes[0] || standing.SessionRef != h.call.Delegation.SessionRef {
		t.Fatalf("standing identity = %+v", standing)
	}
	if standing.PolicyBundleDigest == "" || standing.LegalContextDigest == "" || standing.ClassificationDigest == "" ||
		standing.CapabilityDigest == "" || standing.ControlDigest == "" || standing.RiskClass == "" {
		t.Fatalf("standing control versions = %+v, want every version the decision is composed against", standing)
	}
	if standing.ObservedAt.IsZero() {
		t.Fatal("standing carries no observation instant")
	}

	// RBAC-RT-003: intent creation needs a role granting the subject's data
	// domain, so the revoked fixture keeps comp_admin (like the revocation
	// fixture in TestPromotionStepServicesFailClosed) and drops only the
	// promotion_operator delegation the standing reports on.
	revoked := newStepHarness(t, stepRoleAccess{snapshot: roleaccess.Snapshot{Assignments: []roleaccess.Assignment{
		{WorkerRef: h.call.Delegation.Subject, RoleIDs: []string{"intent_author", "comp_admin"}},
	}}})
	refused, err := revoked.services.GovernanceStanding(ctx, revoked.call)
	if err != nil {
		t.Fatalf("GovernanceStanding after revocation: %v", err)
	}
	if refused.Authorized || !strings.Contains(refused.Refusal, "promotion_operator") {
		t.Fatalf("revoked standing = %+v, want an unauthorized standing naming the missing role", refused)
	}
	if refused.ControlDigest == "" {
		t.Fatal("a refused standing must still carry the control versions the decision names")
	}
}

// TestPromotionAggregateCatalogCoversEveryPublishedTarget proves the catalog
// records a job for every published ladder profile, vacancies for every
// target in the organization units that ladder applies to, and a pool per
// organization unit.
func TestPromotionAggregateCatalogCoversEveryPublishedTarget(t *testing.T) {
	recorded := time.Date(2026, 9, 1, 13, 45, 0, 0, time.UTC)
	catalog, err := PromotionAggregateCatalog(recorded)
	if err != nil {
		t.Fatalf("PromotionAggregateCatalog: %v", err)
	}
	if catalog.RecordedAt != recorded || catalog.BudgetAmount != PromotionPoolAmount || catalog.BudgetCurrency == "" {
		t.Fatalf("catalog header = %+v", catalog)
	}
	jobs := map[string]bool{}
	for _, job := range catalog.Jobs {
		if job.Code == "" || job.Grade == "" {
			t.Fatalf("catalog job %+v is incomplete", job)
		}
		jobs[job.Code+"/"+job.Grade] = true
	}
	paths, err := publishedPromotionPaths()
	if err != nil {
		t.Fatal(err)
	}
	vacancies := map[string]bool{}
	for _, vacancy := range catalog.Vacancies {
		vacancies[vacancy.OrgUnit+"|"+vacancy.JobCode+"|"+vacancy.Grade] = true
	}
	for _, path := range paths {
		option := path.Option
		if !jobs[option.TargetJobCode+"/"+option.TargetGrade] {
			t.Fatalf("catalog records no job for published target %s/%s", option.TargetJobCode, option.TargetGrade)
		}
		found := false
		for key := range vacancies {
			if strings.HasSuffix(key, "|"+option.TargetJobCode+"|"+option.TargetGrade) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("catalog records no vacancy for published target %s/%s", option.TargetJobCode, option.TargetGrade)
		}
	}
	units := map[string]bool{}
	for _, unit := range catalog.BudgetOrgUnits {
		if units[unit] {
			t.Fatalf("catalog records the pool for %s twice", unit)
		}
		units[unit] = true
	}
	for _, vacancy := range catalog.Vacancies {
		if !units[vacancy.OrgUnit] {
			t.Fatalf("vacancy in %s has no compensation pool", vacancy.OrgUnit)
		}
	}
}

// TestAppendCommitMaterialCarriesThePayAndPinnedManager proves the proposal
// carries the approved annualized pay as a write and the unchanged manager as
// an identical current/proposed assertion pair, and that the budget hold the
// revision implies is the raise against the worker's organization unit.
func TestAppendCommitMaterialCarriesThePayAndPinnedManager(t *testing.T) {
	tenant := values.TenantId("harborcare-demo")
	subject := values.EntityRef{Tenant: tenant, Kind: "worker", Id: uuid.NewString()}
	primary := intent.SubjectReference{Kind: "EMPLOYMENT", SubjectID: subject.Id, AuthorityDomain: "PEOPLE"}
	watermark, err := values.NewSequenceRevision("people.worker."+subject.Id, 1)
	if err != nil {
		t.Fatal(err)
	}
	start := values.NewInstant(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	effective, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatal(err)
	}
	money := func(text string) values.Money {
		m, moneyErr := values.NewMoney(text, "USD", 2, values.RoundingExactRequired)
		if moneyErr != nil {
			t.Fatal(moneyErr)
		}
		return m
	}
	result := promotion.SimulationResult{
		CompensationState: promotion.CompensationEvaluated,
		Compensation: rewards.SimulateCompensationResult{
			Current:  rewards.Projection{AnnualizedBase: money("90000.00")},
			Proposed: rewards.Projection{AnnualizedBase: money("98000.00")},
		},
	}
	manager := uuid.NewString()
	spec := intent.ProposalSpec{}
	if err := appendCommitMaterial(&spec, tenant, primary, subject, watermark, effective, result, manager); err != nil {
		t.Fatalf("appendCommitMaterial: %v", err)
	}
	var payWrite *intent.PlannedWrite
	for i := range spec.Writes {
		if spec.Writes[i].FieldPath == CommitPayFieldPath {
			payWrite = &spec.Writes[i]
		}
		if spec.Writes[i].FieldPath == CommitManagerIDFieldPath {
			t.Fatal("an unchanged manager was emitted as a write; the kernel refuses one")
		}
	}
	if payWrite == nil || payWrite.CurrentCanonicalText != "90000.00" || payWrite.ProposedCanonicalText != "98000.00" ||
		payWrite.Operation != intent.WriteOperationUpdate || payWrite.ExpectedRevision != watermark {
		t.Fatalf("pay write = %+v", payWrite)
	}
	pinned := map[string]int{}
	for _, assertion := range append(append([]intent.StateAssertion{}, spec.CurrentState...), spec.ProposedState...) {
		if assertion.FieldPath == CommitManagerIDFieldPath || assertion.FieldPath == CommitRelationshipFieldPath {
			if assertion.CanonicalText != manager {
				t.Fatalf("pinned manager assertion = %+v, want %s", assertion, manager)
			}
			pinned[assertion.FieldPath]++
		}
	}
	if pinned[CommitManagerIDFieldPath] != 2 || pinned[CommitRelationshipFieldPath] != 2 {
		t.Fatalf("pinned manager assertions = %v, want a current and a proposed one for each field", pinned)
	}
	// A promotion with no evaluated compensation carries no pay write, and a
	// worker with no recorded manager pins none.
	bare := intent.ProposalSpec{}
	if err := appendCommitMaterial(&bare, tenant, primary, subject, watermark, effective, promotion.SimulationResult{}, ""); err != nil {
		t.Fatalf("appendCommitMaterial (bare): %v", err)
	}
	if len(bare.Writes) != 0 || len(bare.CurrentState) != 0 || len(bare.ProposedState) != 0 {
		t.Fatalf("bare material = %+v, want nothing asserted", bare)
	}

	revision := intent.ProposalRevision{
		ProposalRevisionID: uuid.NewString(), EffectiveTime: effective, CreatedAt: values.NewInstant(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)),
		Writes: spec.Writes, ProposedState: append(spec.ProposedState, intent.StateAssertion{
			Subject: primary, FieldPath: PlacementFieldPrefix + "assignment.org_unit", CanonicalText: "people-ops",
		}),
	}
	hold, ok, err := proposalReservationFor(revision)
	if err != nil || !ok {
		t.Fatalf("proposalReservationFor = %+v, %t, %v", hold, ok, err)
	}
	if hold.Amount != "8000.00" || hold.OrgUnit != "people-ops" ||
		!hold.EffectiveStart.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)) ||
		!hold.ProducedAt.Equal(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("hold = %+v", hold)
	}
	if hold.ProposalRevisionID.String() != revision.ProposalRevisionID {
		t.Fatalf("hold proposal = %s, want %s", hold.ProposalRevisionID, revision.ProposalRevisionID)
	}
	if _, ok, _ := proposalReservationFor(intent.ProposalRevision{ProposalRevisionID: uuid.NewString(), EffectiveTime: effective}); ok {
		t.Fatal("a revision with no pay write implied a budget hold")
	}
	var _ promotionbudget.ProposalReservation = hold
}

// TestPinnedManagerFromRevisionsNeedsADatabase proves the approval-frozen
// manager read answers "nothing pinned" rather than failing when the cell was
// composed without an execution database or a tenant mapping, and for a
// reference that is not an intent id.
func TestPinnedManagerFromRevisionsNeedsADatabase(t *testing.T) {
	ctx := context.Background()
	if _, ok, err := PinnedManagerFromRevisions(nil, nil)(ctx, values.TenantId("t"), uuid.NewString()); ok || err != nil {
		t.Fatalf("read with no database = %t, %v; want nothing pinned", ok, err)
	}
	read := PinnedManagerFromRevisions(nil, func(values.TenantId) uuid.UUID { return uuid.New() })
	if _, ok, err := read(ctx, values.TenantId("t"), "not-an-intent"); ok || err != nil {
		t.Fatalf("read for a malformed intent id = %t, %v; want nothing pinned", ok, err)
	}
}
