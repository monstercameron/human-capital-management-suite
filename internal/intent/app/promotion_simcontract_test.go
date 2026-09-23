package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// rev00601SimulateCall resolves the position-bound promotion and runs the
// governed explain side, returning the handler inputs the served SIMULATE
// path assembles: the preflight request with its governed worker state and
// the resolve-time simulations.
func rev00601SimulateCall(t *testing.T, ctx context.Context) (*domainHandlers, promotion.PreflightRequest, *PromotionSimulations) {
	t.Helper()
	inputs, err := NewCorpusInputs()
	if err != nil {
		t.Fatalf("NewCorpusInputs: %v", err)
	}
	catalog, err := fixtures.NewMemoryPositionCatalog()
	if err != nil {
		t.Fatalf("NewMemoryPositionCatalog: %v", err)
	}
	inputs.BindPositionReader(catalog)
	principal := rev00601Principal(t)
	req := ResolveRequest{Instance: rev00601Instance(), Principal: principal, Purpose: authz.PurposeCompensationReview}
	call, err := inputs.resolvePromotion(ctx, req, rev00601ResolvePayload(t))
	if err != nil {
		t.Fatalf("resolvePromotion: %v", err)
	}
	if call.Promotion == nil || call.Explain == nil || call.Simulations == nil {
		t.Fatal("resolve produced no promotion request, explain or simulations")
	}
	handlers := &domainHandlers{workers: inputs.Workers(), bands: inputs.Bands()}
	raw, err := handlers.explainWorkerState(ctx, *call.Explain)
	if err != nil {
		t.Fatalf("explainWorkerState: %v", err)
	}
	explanation, ok := raw.(people.Explanation)
	if !ok {
		t.Fatalf("explain_worker_state returned %T", raw)
	}
	request := *call.Promotion
	request.WorkerState = explanation
	return handlers, request, call.Simulations
}

func rev00601Simulate(t *testing.T, ctx context.Context, handlers *domainHandlers, request promotion.PreflightRequest, sims *PromotionSimulations) promotionAnswer {
	t.Helper()
	raw, err := handlers.promoteWorker(ctx, promotionCall{
		Mode: promotionModeSimulate, Request: request, IntentID: rev00601Instance().IntentID,
		Simulations: sims, ControlSnapshotDigest: "sha256:rev00601-control",
		RevalidationRule: "promotion.revalidation/v1",
	})
	if err != nil {
		t.Fatalf("promoteWorker SIMULATE: %v", err)
	}
	answer, ok := raw.(promotionAnswer)
	if !ok {
		t.Fatalf("promote_worker returned %T", raw)
	}
	return answer
}

// TestPromotionSimulateAssemblesGovernedContract proves the SIMULATE branch
// binds the resolve-time governed simulations to a validated simulation
// contract: the contract cites the intent, the governed snapshot both
// simulations ran over, and the candidate digest, carries the served
// preflight's findings and the evaluated cost, and leaves the legacy
// simulation artifact intact.
func TestPromotionSimulateAssemblesGovernedContract(t *testing.T) {
	ctx := context.Background()
	handlers, request, sims := rev00601SimulateCall(t, ctx)
	answer := rev00601Simulate(t, ctx, handlers, request, sims)

	if answer.Simulation.InputsDigest == "" {
		t.Fatal("legacy simulation artifact is missing")
	}
	contract := answer.Contract
	if !strings.HasPrefix(contract.Digest, "sha256:") {
		t.Fatalf("contract digest = %q, want a digest", contract.Digest)
	}
	if err := contract.Validate(); err != nil {
		t.Fatalf("contract Validate: %v", err)
	}
	if contract.Intent.IntentID != rev00601Instance().IntentID ||
		contract.Intent.IntentType != promotion.IntentType ||
		contract.Intent.IntentVersion != promotion.IntentVersion {
		t.Fatalf("contract intent = %+v", contract.Intent)
	}
	if contract.Snapshot.SnapshotDigest != sims.Assignment.SnapshotDigest ||
		contract.ProposalCandidateDigest != sims.CandidateDigest {
		t.Fatalf("contract binds snapshot %q candidate %q, want %q and %q",
			contract.Snapshot.SnapshotDigest, contract.ProposalCandidateDigest,
			sims.Assignment.SnapshotDigest, sims.CandidateDigest)
	}
	// Every contracted finding is one the served preflight produced, with
	// no duplicate identity surviving the assembly.
	seen := map[promotion.FindingIdentity]int{}
	for _, f := range contract.Findings {
		seen[f.Identity()]++
		found := false
		for _, p := range answer.Preflight.Findings {
			if p.Identity() == f.Identity() {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("contract finding %+v is not a served preflight finding", f)
		}
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("contract finding identity %+v appears %d times", id, n)
		}
	}
	if contract.Cost.State != simcontract.CostEvaluated || contract.Cost.Amount == nil ||
		contract.Cost.Amount.String() != sims.Compensation.BasePay.Delta.String() {
		t.Fatalf("contract cost = %+v, want the evaluated %s", contract.Cost, sims.Compensation.BasePay.Delta)
	}
	// The completion verdict tracks the served preflight: ready with no
	// approvals outstanding is READY, ready behind approvals waits for
	// exactly those, and anything else is BLOCKED with no path through.
	ready := answer.Preflight.Status == promotion.StatusReady
	switch want := contract.Completion.State; {
	case ready && len(sims.Compensation.RequiredApprovals) == 0:
		if want != simcontract.CompletionReady {
			t.Fatalf("completion = %s, want READY", want)
		}
	case ready:
		if want != simcontract.CompletionPendingApproval {
			t.Fatalf("completion = %s, want PENDING_APPROVAL", want)
		}
		if strings.Join(contract.Completion.OutstandingApprovals, ",") !=
			strings.Join(sortedUniqueStrings(sims.Compensation.RequiredApprovals), ",") {
			t.Fatalf("outstanding = %q, want %q",
				contract.Completion.OutstandingApprovals, sims.Compensation.RequiredApprovals)
		}
	default:
		if want != simcontract.CompletionBlocked || len(contract.Completion.OutstandingApprovals) != 0 {
			t.Fatalf("completion = %+v, want BLOCKED with no outstanding approvals", contract.Completion)
		}
	}
	// The derived status agrees with the served preflight's blockers.
	blocked := len(answer.Preflight.Blocking()) > 0
	if want := contract.Status; blocked != (want == simcontract.ResultBlocked) {
		t.Fatalf("status = %s for preflight status %s", want, answer.Preflight.Status)
	}
	if contract.Revalidation.ControlSnapshotDigest != "sha256:rev00601-control" ||
		len(contract.Revalidation.Rules) != 1 || contract.Revalidation.Rules[0] != "promotion.revalidation/v1" {
		t.Fatalf("revalidation = %+v", contract.Revalidation)
	}
}

// rev00601ReadyPayload is the position-bound promotion payload with the two
// sides the served preflight requires: a picker-issued position reference
// and a workforce budget authority observation.
func rev00601ReadyPayload(t *testing.T, positionID string) *structValue {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"worker_ref": "omar-reyes",
		"target": map[string]any{
			"job_code": "OPS-HRBP3", "grade": "P3", "org_unit": "people-ops",
			"position_id": positionID, "pay_zone": "US-EAST",
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
		"budget": map[string]any{
			"available_amount": "50000.00", "currency": "USD",
			"owner_system": "finance.incumbent.erp", "policy_ref": "finance.budget.baseline/2026.09",
			"scope": "cost-center:people-ops", "period": "FY2026",
			"baseline_version": "finance.budget.baseline/2026.09", "observation_id": "obs_rev00601_ready",
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

// TestPromotionSimulateAssemblesExecutableContract proves the executable
// path end to end on served inputs: a picker-issued position reference plus
// a budget observation preflights READY, the governed sims run executable,
// and the assembled contract is EXECUTABLE_AS_SIMULATED with the evaluated
// raise bound as its cost.
func TestPromotionSimulateAssemblesExecutableContract(t *testing.T) {
	ctx := context.Background()
	inputs, err := NewCorpusInputs()
	if err != nil {
		t.Fatalf("NewCorpusInputs: %v", err)
	}
	catalog, err := fixtures.NewMemoryPositionCatalog()
	if err != nil {
		t.Fatalf("NewMemoryPositionCatalog: %v", err)
	}
	inputs.BindPositionReader(catalog)
	target, ok := catalog.PositionRefForCode("POS-HRBP-301")
	if !ok {
		t.Fatal("POS-HRBP-301 is not catalogued")
	}
	effective, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	known, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	revision, exists, err := catalog.PositionRevisionAt(ctx, position.PositionQuery{
		Tenant: fixtures.Tenant, Position: target,
		AsOf: position.AsOf{EffectiveOn: effective, KnownAt: known},
	})
	if err != nil || !exists {
		t.Fatalf("revision=%+v exists=%v err=%v", revision, exists, err)
	}
	picker, err := position.EncodeRevisionRef(target, revision.Revision)
	if err != nil {
		t.Fatalf("EncodeRevisionRef: %v", err)
	}
	principal := rev00601Principal(t)
	call, err := inputs.resolvePromotion(ctx, ResolveRequest{
		Instance: rev00601Instance(), Principal: principal, Purpose: authz.PurposeCompensationReview,
	}, rev00601ReadyPayload(t, picker.String()))
	if err != nil {
		t.Fatalf("resolvePromotion: %v", err)
	}
	handlers := &domainHandlers{workers: inputs.Workers(), bands: inputs.Bands()}
	rawExplain, err := handlers.explainWorkerState(ctx, *call.Explain)
	if err != nil {
		t.Fatalf("explainWorkerState: %v", err)
	}
	explanation, ok := rawExplain.(people.Explanation)
	if !ok {
		t.Fatalf("explain_worker_state returned %T", rawExplain)
	}
	request := *call.Promotion
	request.WorkerState = explanation
	answer := rev00601Simulate(t, ctx, handlers, request, call.Simulations)

	if answer.Preflight.Status != promotion.StatusReady {
		for _, f := range answer.Preflight.Findings {
			t.Logf("finding code=%s severity=%s field=%s msg=%.120s", f.Code, f.Severity, f.Field, f.Message)
		}
		t.Fatalf("preflight status = %s, want READY", answer.Preflight.Status)
	}
	if !answer.Simulation.Executable {
		t.Fatal("READY preflight simulated unexecutable")
	}
	contract := answer.Contract
	if err := contract.Validate(); err != nil {
		t.Fatalf("contract Validate: %v", err)
	}
	if contract.Status != simcontract.ResultExecutableAsSimulated {
		t.Fatalf("contract status = %s, want EXECUTABLE_AS_SIMULATED", contract.Status)
	}
	if contract.Snapshot.SnapshotDigest != call.Simulations.Assignment.SnapshotDigest ||
		contract.ProposalCandidateDigest != call.Simulations.CandidateDigest {
		t.Fatal("contract is not bound to the governed sims")
	}
	if contract.Cost.State != simcontract.CostEvaluated || contract.Cost.Amount == nil ||
		contract.Cost.Amount.String() != "5000.00 USD" {
		t.Fatalf("contract cost = %+v, want the evaluated 5000 USD raise", contract.Cost)
	}
	if len(call.Simulations.Compensation.RequiredApprovals) == 0 {
		if contract.Completion.State != simcontract.CompletionReady {
			t.Fatalf("completion = %s, want READY", contract.Completion.State)
		}
	} else if contract.Completion.State != simcontract.CompletionPendingApproval {
		t.Fatalf("completion = %s, want PENDING_APPROVAL", contract.Completion.State)
	}
}

// TestPromotionResolveBaselineIsKernelReady pins the REV-006-01 kernel
// contract for position-bound resolves: the baseline unions the
// request-level presents the kernel names (employment_ref,
// effective_time, reason_ref, ...) with the snapshot's own disclosed
// inputs, and states no negative states -- every substantive absence
// already refuses the build or the simulations, so only informational
// ones could reach the kernel and those must not force obligations.
func TestPromotionResolveBaselineIsKernelReady(t *testing.T) {
	ctx := context.Background()
	inputs, err := NewCorpusInputs()
	if err != nil {
		t.Fatalf("NewCorpusInputs: %v", err)
	}
	call, err := inputs.resolvePromotion(ctx, ResolveRequest{
		Instance: rev00601Instance(), Principal: rev00601Principal(t), Purpose: authz.PurposeCompensationReview,
	}, rev00601ResolvePayload(t))
	if err != nil {
		t.Fatalf("resolvePromotion: %v", err)
	}
	present := map[string]bool{}
	for _, name := range call.Baseline.PresentInputs {
		present[name] = true
	}
	for _, want := range []string{"employment_ref", "effective_time", "reason_ref",
		"target_position_ref", "proposed_base_pay", "promotion.manager_chain",
		"promotion.target_position_capacity", "promotion.budget_availability"} {
		if !present[want] {
			t.Errorf("baseline presents %q, want %q", call.Baseline.PresentInputs, want)
		}
	}
	if len(call.Baseline.NegativeStates) != 0 {
		t.Fatalf("baseline negatives = %+v, want none", call.Baseline.NegativeStates)
	}
	subject := rev00601Subject(t)
	pin, ok := call.Baseline.Revisions[subject.String()]
	if !ok || !pin.IsSpecified() {
		t.Fatalf("baseline pins no revision for %s", subject)
	}
}

// TestPromotionSimulateWithoutSimsKeepsLegacyPath proves a SIMULATE call
// without resolve-time simulations still answers the direct simulation with
// no governed contract attached.
func TestPromotionSimulateWithoutSimsKeepsLegacyPath(t *testing.T) {
	ctx := context.Background()
	handlers, request, _ := rev00601SimulateCall(t, ctx)
	raw, err := handlers.promoteWorker(ctx, promotionCall{Mode: promotionModeSimulate, Request: request})
	if err != nil {
		t.Fatalf("promoteWorker SIMULATE: %v", err)
	}
	answer, ok := raw.(promotionAnswer)
	if !ok {
		t.Fatalf("promote_worker returned %T", raw)
	}
	if answer.Simulation.InputsDigest == "" {
		t.Fatal("legacy simulation artifact is missing")
	}
	if answer.Contract.Digest != "" {
		t.Fatalf("legacy path assembled contract %q", answer.Contract.Digest)
	}
}

// TestPromotionSimulateRefusesUnassemblableContract proves the SIMULATE
// branch fails closed when the simulations cannot bind an honest contract:
// a missing candidate digest and a missing intent identity both refuse the
// simulation rather than minting a digest over a guess.
func TestPromotionSimulateRefusesUnassemblableContract(t *testing.T) {
	ctx := context.Background()
	handlers, request, sims := rev00601SimulateCall(t, ctx)
	broken := *sims
	broken.CandidateDigest = ""
	if _, err := handlers.promoteWorker(ctx, promotionCall{
		Mode: promotionModeSimulate, Request: request, IntentID: rev00601Instance().IntentID,
		Simulations: &broken, ControlSnapshotDigest: "sha256:rev00601-control",
		RevalidationRule: "promotion.revalidation/v1",
	}); err == nil {
		t.Fatal("simulation without a candidate digest succeeded")
	}
	if _, err := handlers.promoteWorker(ctx, promotionCall{
		Mode: promotionModeSimulate, Request: request,
		Simulations: sims, ControlSnapshotDigest: "sha256:rev00601-control",
		RevalidationRule: "promotion.revalidation/v1",
	}); err == nil {
		t.Fatal("simulation without an intent identity succeeded")
	}
}

// TestProposalPinsSimcontractDigest proves the minted proposal carries the
// governed contract digest alongside the preflight, so the approved
// revision's material binds the contract the simulation certified; a path
// that assembled none pins nothing.
// sortedUniqueStrings is the test's own sorted deduplication for
// comparing outstanding approvals without repeating the assembler.
func sortedUniqueStrings(in []string) []string {
	out := append([]string(nil), in...)
	slices.Sort(out)
	return slices.Compact(out)
}

func TestProposalPinsSimcontractDigest(t *testing.T) {
	at := values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC))
	subject := rev00601Subject(t)
	primary := intent.SubjectReference{Kind: "WORKER", SubjectID: subject.Id, AuthorityDomain: "PEOPLE"}
	watermark, err := values.NewSequenceRevision("people.worker."+subject.Id, 7)
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	propose := func(digest string) intent.ProposalSpec {
		t.Helper()
		spec, err := proposalFor(intent.Instance{IntentID: "rev00601-intent", Tenant: subject.Tenant,
			OrganizationScopeID: "org-1", Subjects: []intent.SubjectReference{primary}, RequestedEffectiveAt: &at},
			intent.Definition{}, promotion.PreflightRequest{Subject: subject}, promotion.SimulationResult{},
			intent.BaselineSnapshot{Revisions: map[string]values.RevisionToken{subject.String(): watermark}},
			intent.ControlSnapshots{}, 1, "", digest)
		if err != nil {
			t.Fatalf("proposalFor: %v", err)
		}
		return spec
	}
	pinned := propose("sha256:rev00601-contract")
	found := false
	for _, a := range pinned.Attachments {
		if a.ArtifactID == "artifact:simcontract:rev00601-intent" {
			found = true
			if a.AlgorithmID != "sha256" || a.Digest != "sha256:rev00601-contract" {
				t.Fatalf("attachment = %+v", a)
			}
		}
	}
	if !found {
		t.Fatalf("attachments = %+v, want the simcontract digest", pinned.Attachments)
	}
	for _, a := range propose("").Attachments {
		if strings.HasPrefix(a.ArtifactID, "artifact:simcontract:") {
			t.Fatalf("attachments = %+v, want no simcontract digest", propose("").Attachments)
		}
	}
}
