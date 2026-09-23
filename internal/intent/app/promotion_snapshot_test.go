package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// rev00601Principal is a compensation administrator proposing under the
// compensation-review purpose: the BOOTSTRAP policy grants base salary and
// bonus target to this combination.
func rev00601Principal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: fixtures.Tenant, Subject: "user-rev00601", SubjectKind: trust.SubjectKindHuman,
		Roles:                []string{string(authz.RoleCompAdmin)},
		Purposes:             []string{authz.PurposeCompensationReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-rev00601", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "credential-digest-rev00601",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func rev00601AsOf(t *testing.T) people.AsOf {
	t.Helper()
	effective, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	known, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return people.AsOf{EffectiveOn: effective, KnownAt: known}
}

func rev00601Subject(t *testing.T) values.EntityRef {
	t.Helper()
	ref, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("WorkerRef: %v", err)
	}
	return ref
}

func rev00601Proposed(t *testing.T) rewards.CompensationSnapshot {
	t.Helper()
	base, err := fixtures.Money("98000.00", "USD")
	if err != nil {
		t.Fatalf("Money: %v", err)
	}
	effective, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	return rewards.CompensationSnapshot{
		Base: values.Value(base), PayBasis: rewards.PayBasisAnnualSalary, EffectiveDate: effective,
	}
}

// rev00601Authorize runs the same authorization the resolve path evaluates
// and loads the governed worker facts it reads under.
func rev00601Authorize(t *testing.T, ctx context.Context, inputs *FixtureInputs, principal *trust.Principal, subject values.EntityRef, asOf people.AsOf) (authorizationResult, people.FactSet) {
	t.Helper()
	read := mergeFields(peopleFields(promotion.RequiredWorkerFields()), peopleFields(promosnapshot.WorkerFactFields()))
	decision, err := authorizeRead(principal, authz.PurposeCompensationReview, authorizationRequest{
		Subject:     subject,
		EvaluatedAt: asOf.KnownAt.Instant(),
		Gate:        []authz.FieldID{authz.FieldBaseSalary, authz.FieldBonusTarget},
		Read:        read,
	})
	if err != nil {
		t.Fatalf("authorizeRead: %v", err)
	}
	facts, err := inputs.read(ctx, subject.Tenant, subject, asOf, people.FieldFTE)
	if err != nil {
		t.Fatalf("governed worker read: %v", err)
	}
	return decision, facts
}

func rev00601Input(t *testing.T, decision authorizationResult, subject values.EntityRef, asOf people.AsOf, facts people.FactSet) promotionSnapshotInput {
	t.Helper()
	return promotionSnapshotInput{
		Subject:   subject,
		Target:    promotionTargetPlacement{JobCode: "OPS-HRBP3", Grade: "P3", OrgUnit: "people-ops", PositionID: "POS-HRBP-301", PayZone: "US-EAST"},
		Proposed:  rev00601Proposed(t),
		Effective: asOf.EffectiveOn,
		AsOf:      asOf,
		Decision:  decision,
		Facts:     facts,
		Budget:    &promotionBudgetAuthority{},
	}
}

func TestTodo_REV_006_01(t *testing.T) {
	ctx := context.Background()
	inputs, err := NewFixtureInputs()
	if err != nil {
		t.Fatalf("NewFixtureInputs: %v", err)
	}
	principal := rev00601Principal(t)
	subject := rev00601Subject(t)
	asOf := rev00601AsOf(t)
	decision, facts := rev00601Authorize(t, ctx, inputs, principal, subject, asOf)

	out, err := inputs.buildPromotionSnapshot(ctx, rev00601Input(t, decision, subject, asOf, facts))
	if err != nil {
		t.Fatalf("buildPromotionSnapshot: %v", err)
	}
	if !strings.HasPrefix(out.Snapshot.Digest, "sha256:") || len(out.Snapshot.Inputs()) != len(promosnapshot.InputNames()) {
		t.Fatalf("out=%+v", out)
	}
	for _, probe := range []struct{ name, contains string }{
		{promosnapshot.InputSubjectWorkerFacts, "lifecycle_status=active"},
		{promosnapshot.InputCurrentPlacement, "job_code=OPS-HRBP2"},
		{promosnapshot.InputManagerChain, "rel_mgr_1002"},
		{promosnapshot.InputTargetPositionCapacity, promosnapshot.CapacityAvailable},
		{promosnapshot.InputPayBandPositionCurrent, "annualized=93000.00 USD"},
		{promosnapshot.InputPayBandPositionDesired, "annualized=98000.00 USD"},
		{promosnapshot.InputBudgetAvailability, "available=50000.00"},
	} {
		text, ok := out.Snapshot.Disclosed(probe.name)
		if !ok {
			t.Fatalf("input %s is not disclosed:\n%s", probe.name, out.Snapshot.Explain())
		}
		if !strings.Contains(text, probe.contains) {
			t.Fatalf("input %s reads %q, want it to contain %q", probe.name, text, probe.contains)
		}
	}
	// The kernel baseline carries the governed digest, not the
	// payload-pinned marker.
	baseline := out.Snapshot.BaselineSnapshot()
	if baseline.SnapshotID != out.Snapshot.Digest {
		t.Fatalf("baseline snapshot id = %q, want the governed digest %q", baseline.SnapshotID, out.Snapshot.Digest)
	}
	// The builder returns the pool observation and the kept manager the
	// simulations bind to.
	if out.Budget.AvailableQuantity.String() != "50000.00" {
		t.Fatalf("pool available = %q", out.Budget.AvailableQuantity.String())
	}
	noor, err := fixtures.WorkerRef("noor-haddad")
	if err != nil {
		t.Fatalf("WorkerRef: %v", err)
	}
	if out.Manager != noor {
		t.Fatalf("kept manager = %s, want %s", out.Manager, noor)
	}
	// Both simulations run executable over the governed snapshot, bound to
	// one candidate digest that carries the snapshot digest.
	sims, err := runPromotionSims(out, promotionSimInput{
		Target:   promotionTargetPlacement{JobCode: "OPS-HRBP3", Grade: "P3", OrgUnit: "people-ops", PositionID: "POS-HRBP-301", PayZone: "US-EAST"},
		AsOf:     asOf,
		Decision: decision,
		Facts:    facts,
	})
	if err != nil {
		t.Fatalf("runPromotionSims: %v", err)
	}
	if !sims.Assignment.Executable() || !sims.Compensation.Executable() {
		t.Fatalf("sims=%+v", sims)
	}
	if !strings.HasPrefix(sims.CandidateDigest, "sha256:") || sims.CandidateRevision == "" {
		t.Fatalf("sims=%+v", sims)
	}
	if sims.Assignment.SnapshotDigest != out.Snapshot.Digest || sims.Compensation.SnapshotDigest != out.Snapshot.Digest {
		t.Fatal("simulations are not bound to the governed snapshot")
	}
	// A pool that covers existence but not the raise refuses at simulation
	// level, still before any commit runs.
	thin, err := NewFixtureInputs()
	if err != nil {
		t.Fatalf("NewFixtureInputs: %v", err)
	}
	thousand, err := values.NewDecimal("1000.00", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewDecimal: %v", err)
	}
	thinPools, err := newCorpusBudgetFacts()
	if err != nil {
		t.Fatalf("newCorpusBudgetFacts: %v", err)
	}
	thinPools.setPoolOverride("cost-center:people-ops", "FY2026", budget.BudgetAuthorityRef{
		BudgetType: budget.CompensationPool, OwnerSystem: corpusPoolOwner,
		Scope: "cost-center:people-ops", Period: "FY2026", Currency: "USD", Unit: budget.UnitMoney,
		BaselineVersion: corpusPoolBaselineVersion, AvailableQuantity: thousand,
		Evidence: budget.ObservationEvidence{
			ObservationID: "obs_thin", SourceWatermark: values.NewInstant(corpusObservationWatermark),
			RetrievedAt: values.NewInstant(corpusObservationRetrieved), Digest: "sha256:thin",
		},
	}, true)
	thin.budgetPools = thinPools
	thinOut, err := thin.buildPromotionSnapshot(ctx, rev00601Input(t, decision, subject, asOf, facts))
	if err != nil {
		t.Fatalf("thin pool built no snapshot: %v", err)
	}
	if _, err := runPromotionSims(thinOut, promotionSimInput{
		Target:   promotionTargetPlacement{JobCode: "OPS-HRBP3", Grade: "P3", OrgUnit: "people-ops", PositionID: "POS-HRBP-301", PayZone: "US-EAST"},
		AsOf:     asOf,
		Decision: decision,
		Facts:    facts,
	}); err == nil {
		t.Fatal("raise beyond the pool simulated executable")
	}

	// An exhausted pool refuses the build on the budget input, before any
	// commit path can run.
	exhausted, err := NewFixtureInputs()
	if err != nil {
		t.Fatalf("NewFixtureInputs: %v", err)
	}
	zero, err := values.NewDecimal("0.00", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewDecimal: %v", err)
	}
	pools, err := newCorpusBudgetFacts()
	if err != nil {
		t.Fatalf("newCorpusBudgetFacts: %v", err)
	}
	pools.setPoolOverride("cost-center:people-ops", "FY2026", budget.BudgetAuthorityRef{
		BudgetType: budget.CompensationPool, OwnerSystem: corpusPoolOwner,
		Scope: "cost-center:people-ops", Period: "FY2026", Currency: "USD", Unit: budget.UnitMoney,
		BaselineVersion: corpusPoolBaselineVersion, AvailableQuantity: zero,
		Evidence: budget.ObservationEvidence{
			ObservationID: "obs_exhausted", SourceWatermark: values.NewInstant(corpusObservationWatermark),
			RetrievedAt: values.NewInstant(corpusObservationRetrieved), Digest: "sha256:exhausted",
		},
	}, true)
	exhausted.budgetPools = pools
	if _, err := exhausted.buildPromotionSnapshot(ctx, rev00601Input(t, decision, subject, asOf, facts)); err == nil {
		t.Fatal("exhausted pool built a snapshot")
	} else if got := promosnapshot.InputNameOf(err); got != promosnapshot.InputBudgetAvailability {
		t.Fatalf("exhausted pool refused on %q, want the budget input: %v", got, err)
	}

	// An unknown position refuses rather than resolving to a guess.
	unknown := rev00601Input(t, decision, subject, asOf, facts)
	unknown.Target.PositionID = "POS-HRBP-999"
	if _, err := inputs.buildPromotionSnapshot(ctx, unknown); err == nil {
		t.Fatal("unknown position built a snapshot")
	}

	// A position-less proposal has no snapshot contract: the builder says
	// so plainly instead of minting a digest over nothing.
	positionless := rev00601Input(t, decision, subject, asOf, facts)
	positionless.Target.PositionID = ""
	if _, err := inputs.buildPromotionSnapshot(ctx, positionless); err == nil {
		t.Fatal("position-less proposal built a snapshot")
	}
}

func TestTodo_REV_006_01_Golden(t *testing.T) {
	ctx := context.Background()
	inputs, err := NewFixtureInputs()
	if err != nil {
		t.Fatalf("NewFixtureInputs: %v", err)
	}
	principal := rev00601Principal(t)
	subject := rev00601Subject(t)
	asOf := rev00601AsOf(t)
	decision, facts := rev00601Authorize(t, ctx, inputs, principal, subject, asOf)
	out, err := inputs.buildPromotionSnapshot(ctx, rev00601Input(t, decision, subject, asOf, facts))
	if err != nil {
		t.Fatalf("buildPromotionSnapshot: %v", err)
	}
	got := out.Snapshot.Explain() + "\n"
	path := filepath.Join("testdata", "rev00601_snapshot_explain.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// rev00601ResolvePayload is a position-bound promotion payload for omar that
// states no server-owned fact: no current side, no budget side.
func rev00601ResolvePayload(t *testing.T) *structValue {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"worker_ref": "omar-reyes",
		"target": map[string]any{
			"job_code": "OPS-HRBP3", "grade": "P3", "org_unit": "people-ops",
			"position_id": "POS-HRBP-301", "pay_zone": "US-EAST",
		},
		"effective_date":  "2026-06-01",
		"evaluation_date": "2026-05-15",
		"current": map[string]any{
			"base": "93000.00", "currency": "USD", "pay_basis": "ANNUAL_SALARY",
			"effective_date": "2026-06-01", "revision_stream": "rewards.package.omar-reyes", "revision_sequence": 1,
		},
		"proposed": map[string]any{
			"base": "98000.00", "currency": "USD", "pay_basis": "ANNUAL_SALARY",
			"effective_date": "2026-06-01", "revision_stream": "rewards.package.omar-reyes", "revision_sequence": 1,
		},
		"business_reason": "Promotion into the senior HRBP role",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload protomap.Struct
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return &payload
}

func rev00601Instance() intent.Instance {
	return intent.Instance{
		IntentID:  "rev00601-intent",
		Tenant:    fixtures.Tenant,
		CreatedAt: values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)),
	}
}

// TestTodo_REV_006_01_Integration resolves a position-bound promotion and
// proves the baseline is the governed snapshot digest; then it exhausts the
// live pool and proves the resolve is refused before any commit runs.
func TestTodo_REV_006_01_Integration(t *testing.T) {
	ctx := context.Background()
	inputs, err := NewFixtureInputs()
	if err != nil {
		t.Fatalf("NewFixtureInputs: %v", err)
	}
	principal := rev00601Principal(t)
	req := ResolveRequest{Instance: rev00601Instance(), Principal: principal, Purpose: authz.PurposeCompensationReview}

	call, err := inputs.resolvePromotion(ctx, req, rev00601ResolvePayload(t))
	if err != nil {
		t.Fatalf("resolvePromotion: %v", err)
	}
	if call.Promotion == nil {
		t.Fatal("resolve produced no promotion preflight")
	}
	if !strings.HasPrefix(call.Baseline.SnapshotID, "sha256:") {
		t.Fatalf("baseline snapshot id = %q, want the governed snapshot digest", call.Baseline.SnapshotID)
	}
	if call.Simulations == nil {
		t.Fatal("resolve produced no governed simulations")
	}
	if !call.Simulations.Assignment.Executable() || !call.Simulations.Compensation.Executable() {
		t.Fatalf("simulations=%+v", call.Simulations)
	}
	if call.Simulations.Assignment.SnapshotDigest != call.Baseline.SnapshotID ||
		call.Simulations.Compensation.SnapshotDigest != call.Baseline.SnapshotID {
		t.Fatal("simulations are not bound to the resolved baseline")
	}

	exhausted, err := NewFixtureInputs()
	if err != nil {
		t.Fatalf("NewFixtureInputs: %v", err)
	}
	zero, err := values.NewDecimal("0.00", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewDecimal: %v", err)
	}
	pools, err := newCorpusBudgetFacts()
	if err != nil {
		t.Fatalf("newCorpusBudgetFacts: %v", err)
	}
	pools.setPoolOverride("cost-center:people-ops", "FY2026", budget.BudgetAuthorityRef{
		BudgetType: budget.CompensationPool, OwnerSystem: corpusPoolOwner,
		Scope: "cost-center:people-ops", Period: "FY2026", Currency: "USD", Unit: budget.UnitMoney,
		BaselineVersion: corpusPoolBaselineVersion, AvailableQuantity: zero,
		Evidence: budget.ObservationEvidence{
			ObservationID: "obs_exhausted", SourceWatermark: values.NewInstant(corpusObservationWatermark),
			RetrievedAt: values.NewInstant(corpusObservationRetrieved), Digest: "sha256:exhausted",
		},
	}, true)
	exhausted.budgetPools = pools
	if _, err := exhausted.resolvePromotion(ctx, req, rev00601ResolvePayload(t)); err == nil {
		t.Fatal("promotion against an exhausted pool resolved")
	} else if got := promosnapshot.InputNameOf(err); got != promosnapshot.InputBudgetAvailability {
		t.Fatalf("exhausted pool refused on %q, want the budget input: %v", got, err)
	}
}
