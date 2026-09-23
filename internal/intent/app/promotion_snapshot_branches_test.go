package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// deniedSnapshotDecision is an evaluated decision that withholds base pay,
// bonus target and the subject: the mapping must deny every derived field
// with a reason rather than leaking a default allow.
func deniedSnapshotDecision() authorizationResult {
	deny := func(reason string) authz.FieldRuling {
		return authz.FieldRuling{Effect: authz.EffectDenied, Reason: reason}
	}
	return authorizationResult{
		Purpose: authz.PurposeCompensationReview,
		Decision: authz.Decision{
			SubjectDisclosable:  false,
			SubjectDenialReason: "need_to_know",
			Fields: map[authz.FieldID]authz.FieldRuling{
				authz.FieldBaseSalary:  deny("no_grant_for_base_pay"),
				authz.FieldBonusTarget: deny("no_grant_for_bonus_target"),
			},
		},
	}
}

func TestPromotionSnapshotAuthorizationDenials(t *testing.T) {
	mapped := promotionSnapshotAuthorization(deniedSnapshotDecision(), fixtures.Tenant, nil)
	if mapped.BudgetDisclosable {
		t.Fatal("withheld pay discloses the pool")
	}
	if mapped.BudgetDenialReason == "" {
		t.Fatal("pool denial carries no reason")
	}
	if ruling := mapped.Compensation.Fields[rewards.FieldBasePay]; ruling.Effect != people.EffectDeny || ruling.Reason == "" {
		t.Fatalf("base pay ruling=%+v", ruling)
	}
	if ruling := mapped.Compensation.Fields[rewards.FieldComponents]; ruling.Effect != people.EffectDeny || ruling.Reason == "" {
		t.Fatalf("bonus ruling=%+v", ruling)
	}
	if ruling := mapped.PayBand.Fields[rewards.PositionFieldAmount]; ruling.Effect != people.AccessDenied || ruling.Reason == "" {
		t.Fatalf("band ruling=%+v", ruling)
	}
	if err := mapped.Compensation.Validate(); err != nil {
		t.Fatalf("Compensation.Validate: %v", err)
	}
	if err := mapped.PayBand.Validate(); err != nil {
		t.Fatalf("PayBand.Validate: %v", err)
	}
	if decision := mapped.ManagerHop(org.ManagerRelationshipFact{}); decision.SubjectDisclosable {
		t.Fatal("withheld subject discloses the chain")
	}
	other := position.PositionRevision{
		Position: values.EntityRef{Tenant: "other-tenant", Kind: position.KindPosition, Id: "55555555-5555-4555-8555-555555555555"},
	}
	if mapped.Position(other) {
		t.Fatal("cross-tenant position is readable")
	}
}

// createdWorkerRow is a durable created-population row answering to the
// snapshot adapters without a database.
func createdWorkerRow(id, managerRef string) *workforce.WorkerRow {
	workerID := uuid.MustParse(id)
	return &workforce.WorkerRow{
		WorkerID:               workerID,
		WorkerKey:              "created-" + id[:8],
		BasePay:                "88000.00",
		Currency:               "USD",
		PayBasis:               "ANNUAL_SALARY",
		BonusTarget:            "0.1000",
		JobCode:                "OPS-HRBP3",
		Grade:                  "P3",
		PayZone:                "US-EAST",
		ManagerRelationshipRef: managerRef,
		AssignmentID:           "asg-created-" + id[:8],
		RevisionStream:         "workforce.created-" + id[:8],
		RevisionSequence:       3,
		EffectiveFrom:          "2026-01-01",
		KnownAt:                time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		RecordedAt:             time.Date(2026, 2, 1, 1, 0, 0, 0, time.UTC),
	}
}

func createdLocator(rows map[string]*workforce.WorkerRow) WorkerLocator {
	return func(_ context.Context, tenant values.TenantId, ref string) (WorkerLocation, bool, error) {
		for id, row := range rows {
			if ref == id || ref == row.WorkerKey {
				return WorkerLocation{
					Ref:     values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: id},
					Key:     row.WorkerKey,
					Created: row,
				}, true, nil
			}
		}
		return WorkerLocation{}, false, nil
	}
}

func TestPromotionOrgFactsCreatedWalk(t *testing.T) {
	managerID := "77777777-7777-4777-8777-777777777777"
	subjectID := "66666666-6666-4666-8666-666666666666"
	locate := createdLocator(map[string]*workforce.WorkerRow{
		subjectID: createdWorkerRow(subjectID, managerID),
		managerID: createdWorkerRow(managerID, ""),
	})
	adapter, err := newPromotionOrgFacts(locate)
	if err != nil {
		t.Fatalf("newPromotionOrgFacts: %v", err)
	}
	ctx := context.Background()
	known, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	subject := values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker, Id: subjectID}
	set, err := adapter.WorkerFactsAt(ctx, org.WorkerFactsQuery{
		Tenant: fixtures.Tenant, Worker: subject,
		AsOf: values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)), KnownAt: known,
	})
	if err != nil {
		t.Fatalf("WorkerFactsAt: %v", err)
	}
	if !set.Exists || len(set.Relationships) != 1 {
		t.Fatalf("set=%+v", set)
	}
	if got := set.Relationships[0].Manager.Id; got != managerID {
		t.Fatalf("manager=%q", got)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// The top of the created walk has no manager: one more hop ends the
	// chain rather than failing the read.
	manager := values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker, Id: managerID}
	top, err := adapter.WorkerFactsAt(ctx, org.WorkerFactsQuery{
		Tenant: fixtures.Tenant, Worker: manager,
		AsOf: values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)), KnownAt: known,
	})
	if err != nil {
		t.Fatalf("WorkerFactsAt top: %v", err)
	}
	if !top.Exists || len(top.Relationships) != 0 {
		t.Fatalf("top=%+v", top)
	}
	if !set.Watermark.Equal(top.Watermark) || set.PolicyVersion != top.PolicyVersion {
		t.Fatal("one walk answers under two watermarks")
	}
}

// TestPromotionOrgFactsIntradayKnownAt is the REV-006-01 journey
// regression: the journey declares a created worker's knowledge cut-off and
// the resolve reads the manager chain under it. The cut-off keeps the row's
// full intraday precision, so a worker created after midnight resolves its
// own chain; a date-truncated (midnight) cut-off still reads stale, which is
// the bitemporal guard working, not a second defect.
func TestPromotionOrgFactsIntradayKnownAt(t *testing.T) {
	managerID := "88888888-8888-4888-8888-888888888888"
	subjectID := "99999999-9999-4999-8999-999999999999"
	intraday := time.Date(2026, 9, 23, 8, 58, 44, 0, time.UTC)
	subject := createdWorkerRow(subjectID, managerID)
	subject.KnownAt = intraday
	subject.RecordedAt = intraday.Add(time.Hour)
	manager := createdWorkerRow(managerID, "")
	manager.KnownAt = time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	manager.RecordedAt = intraday.Add(time.Hour)
	locate := createdLocator(map[string]*workforce.WorkerRow{subjectID: subject, managerID: manager})
	adapter, err := newPromotionOrgFacts(locate)
	if err != nil {
		t.Fatalf("newPromotionOrgFacts: %v", err)
	}
	allow := org.Authorizer(func(org.ManagerRelationshipFact) people.AuthorizationDecision {
		return people.AuthorizationDecision{PolicyVersion: "authz/v1", Purpose: "manager-read",
			SubjectDisclosable: true, Fields: map[people.FieldID]people.FieldRuling{
				people.FieldManagerRelation: {Effect: people.EffectAllow},
			}}
	})
	ctx := context.Background()
	worker := values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker, Id: subjectID}
	asOf := values.NewInstant(intraday.Add(2 * time.Hour))
	resolve := func(known values.KnownAt) org.ManagerResolution {
		t.Helper()
		resolution, err := org.ResolveManagerRelationships(ctx, adapter, org.ManagerResolutionRequest{
			Tenant: fixtures.Tenant, Worker: worker, AsOf: asOf,
			KnownAt: known, MaxDepth: maxManagerChainDepth, Authorize: allow,
		})
		if err != nil {
			t.Fatalf("ResolveManagerRelationships: %v", err)
		}
		return resolution
	}
	full, err := values.NewKnownAt(values.NewInstant(intraday))
	if err != nil {
		t.Fatal(err)
	}
	if resolved := resolve(full); resolved.Status != org.StatusResolved ||
		resolved.Direct == nil || resolved.Direct.Manager.Value.Id != managerID {
		t.Fatalf("intraday cut-off resolves %+v", resolved)
	}
	midnight, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if stale := resolve(midnight); stale.Status != org.StatusStale {
		t.Fatalf("midnight cut-off resolves %+v, want STALE", stale)
	}
}

func TestPromotionCompensationFactsCreated(t *testing.T) {
	subjectID := "66666666-6666-4666-8666-666666666666"
	locate := createdLocator(map[string]*workforce.WorkerRow{
		subjectID: createdWorkerRow(subjectID, ""),
	})
	catalog, err := fixtures.NewMemoryBandCatalog()
	if err != nil {
		t.Fatalf("NewMemoryBandCatalog: %v", err)
	}
	adapter, err := newPromotionCompensationFacts(locate, catalog)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	subject := values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker, Id: subjectID}
	set, err := adapter.CompensationFactsAt(context.Background(), rewards.CompensationFactsQuery{
		Tenant: fixtures.Tenant, Worker: subject,
		AsOf:   values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)),
		Fields: rewards.CompensationFields(),
	})
	if err != nil {
		t.Fatalf("CompensationFactsAt: %v", err)
	}
	if !set.Exists {
		t.Fatal("created worker has no compensation fact")
	}
	if set.Fact.BasePay.String() != "88000.00 USD" {
		t.Fatalf("base=%q", set.Fact.BasePay.String())
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestCorpusBudgetFactsBranches(t *testing.T) {
	pools, err := newCorpusBudgetFacts()
	if err != nil {
		t.Fatalf("newCorpusBudgetFacts: %v", err)
	}
	ctx := context.Background()
	asOf := values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC))
	query := promosnapshot.BudgetQuery{Tenant: fixtures.Tenant, Scope: "cost-center:people-ops", Period: "FY2026", AsOf: asOf}
	ref, exists, err := pools.CompensationBudgetAt(ctx, query)
	if err != nil || !exists {
		t.Fatalf("ref=%+v exists=%v err=%v", ref, exists, err)
	}
	if ref.AvailableQuantity.String() != "50000.00" {
		t.Fatalf("pool=%q", ref.AvailableQuantity.String())
	}
	pools.setPoolOverride("cost-center:people-ops", "FY2026", ref, false)
	if _, exists, err := pools.CompensationBudgetAt(ctx, query); err != nil || exists {
		t.Fatalf("absent pool exists=%v err=%v", exists, err)
	}
	if _, _, err := pools.CompensationBudgetAt(ctx, promosnapshot.BudgetQuery{Tenant: fixtures.Tenant, AsOf: asOf}); err == nil {
		t.Fatal("empty query succeeded")
	}
}
