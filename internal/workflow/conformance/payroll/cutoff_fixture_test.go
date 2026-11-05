package payroll

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/effectivedate"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/revalidate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

func fixedDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatalf("ParseLocalDate(%q): %v", text, err)
	}
	return d
}
func oneTimeline(t *testing.T, scenario CutoffScenario) {
	t.Helper()
	interval, err := values.NewLocalDateInterval(scenario.Fixture.Period.Start, scenario.Fixture.Period.End, scenario.Fixture.Calendar.Calendar)
	if err != nil {
		t.Fatalf("NewLocalDateInterval: %v", err)
	}
	known, err := values.NewKnownAt(scenario.ObservedAt)
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	recorded, err := values.NewRecordedAt(scenario.ObservedAt)
	if err != nil {
		t.Fatalf("NewRecordedAt: %v", err)
	}
	timeline, err := effectivedate.NewTimeline([]effectivedate.Coordinate{{
		ID: "promotion-revision-1", Effective: interval, KnownAt: known, RecordedAt: recorded,
	}})
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	winner, found, err := timeline.InForce(scenario.EffectiveOn, known)
	if err != nil {
		t.Fatalf("Timeline.InForce: %v", err)
	}
	if !found || winner.ID != "promotion-revision-1" {
		t.Fatalf("effective-date winner = %+v found=%v, want promotion-revision-1", winner, found)
	}
}

// TestTodo_CONF_008 is the primary promotion conformance proof for the
// payroll cutoff boundary. It compiles the real executable promotion graph,
// checks the required WAIT -> revalidation shape, and verifies that all
// three fixed cutoff fixtures are valid period-bound facts.
func TestTodo_CONF_008(t *testing.T) {
	plan, err := promotionexec.CompileSimulation()
	if err != nil {
		t.Fatalf("CompileSimulation: %v", err)
	}
	// Since WF-TEST-001 the projection retains the two authored promotion
	// writes for the recording simulator; it is P1B but no longer
	// zero-effect.
	if plan.Phase != workflow.PhaseP1B || plan.Effects.ZeroEffect {
		t.Fatalf("simulation plan phase/effects = %s/%+v, want P1B with the retained writes", plan.Phase, plan.Effects)
	}
	waitNode, ok := plan.Node(promotionexec.NodeWaitEffectiveDate)
	if !ok || waitNode.Type != workflow.StepWait || waitNode.Wait == nil || waitNode.Wait.ZoneID != "America/New_York" || waitNode.Wait.ZoneTzdbVersion != "2026a" {
		t.Fatalf("promotion WAIT node = %+v, want pinned New York@2026a wait", waitNode)
	}
	revalidateNode, ok := plan.Node(promotionexec.NodeRevalidate)
	if !ok || revalidateNode.Capability == nil || revalidateNode.Capability.ID == "" {
		t.Fatalf("promotion revalidation node = %+v, want governed capability", revalidateNode)
	}
	if effectivedate.Version() == 0 || schedule.Version() == 0 || !strings.Contains(schedule.Explain(), "schedule") {
		t.Fatalf("engine contracts are not available: effectivedate=%d schedule=%d explain=%q", effectivedate.Version(), schedule.Version(), schedule.Explain())
	}
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatalf("GoldenCutoffScenarios: %v", err)
	}
	if len(scenarios) != 3 {
		t.Fatalf("scenario count = %d, want 3", len(scenarios))
	}
	for _, scenario := range scenarios {
		if err := scenario.Fixture.Validate(); err != nil {
			t.Errorf("%s Validate: %v", scenario.Fixture.Name, err)
		}
		oneTimeline(t, scenario)
	}
}

func TestPromotionEffectiveDateAfterCutoffIsRetroactive(t *testing.T) {
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatal(err)
	}
	scenario := scenarios[0]
	status, expectation, err := scenario.Fixture.Classify(scenario.EffectiveOn, scenario.ObservedAt)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if status != EffectiveDateRetroactive {
		t.Fatalf("status = %s, want %s", status, EffectiveDateRetroactive)
	}
	if expectation != RetroExpectation {
		t.Fatalf("retro expectation = %q, want %q", expectation, RetroExpectation)
	}
	if scenario.ObservedAt.Compare(scenario.Fixture.CutoffAt) <= 0 {
		t.Fatalf("fixture observation %s is not after cutoff %s", scenario.ObservedAt, scenario.Fixture.CutoffAt)
	}
}

func TestPromotionEffectiveDateBeforeCutoffIsCurrent(t *testing.T) {
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatal(err)
	}
	scenario := scenarios[1]
	status, expectation, err := scenario.Fixture.Classify(scenario.EffectiveOn, scenario.ObservedAt)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if status != EffectiveDateCurrent || expectation != "" {
		t.Fatalf("status/expectation = %s/%q, want CURRENT/empty", status, expectation)
	}
	if scenario.ObservedAt.Compare(scenario.Fixture.CutoffAt) >= 0 {
		t.Fatalf("fixture observation %s is not before cutoff %s", scenario.ObservedAt, scenario.Fixture.CutoffAt)
	}
}

func TestPromotionEffectiveDateOnACutoffBoundaryIsDeterministic(t *testing.T) {
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatal(err)
	}
	scenario := scenarios[2]
	first, firstExpectation, err := scenario.Fixture.Classify(scenario.EffectiveOn, scenario.ObservedAt)
	if err != nil {
		t.Fatalf("first Classify: %v", err)
	}
	second, secondExpectation, err := scenario.Fixture.Classify(scenario.EffectiveOn, scenario.ObservedAt)
	if err != nil {
		t.Fatalf("second Classify: %v", err)
	}
	if first != EffectiveDateCurrent || second != first || firstExpectation != "" || secondExpectation != "" {
		t.Fatalf("boundary classifications = (%s,%q), (%s,%q), want CURRENT/empty twice", first, firstExpectation, second, secondExpectation)
	}
	if scenario.Fixture.Calendar.Zone.ID != "America/New_York" || scenario.Fixture.Calendar.Zone.TzdbVersion != "2026a" || scenario.Fixture.Calendar.Calendar.Version != "2026.1" {
		t.Fatalf("boundary dataset = %s/%s, want America/New_York@2026a and calendar 2026.1", scenario.Fixture.Calendar.Zone, scenario.Fixture.Calendar.Calendar)
	}
	fact1, err := scenario.Fixture.Fact(scenario.EffectiveOn, scenario.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	fact2, err := scenario.Fixture.Fact(scenario.EffectiveOn, scenario.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if fact1.CutoffAt != fact2.CutoffAt || fact1.EvidenceRef != fact2.EvidenceRef {
		t.Fatalf("boundary fact drifted: first=%+v second=%+v", fact1, fact2)
	}
	node := wait.CompiledWaitNode{
		WorkflowID: promotionexec.WorkflowID, WorkflowVersion: promotionexec.Version, NodeID: promotionexec.NodeWaitEffectiveDate,
		WakeLocalDate: scenario.EffectiveOn, Disambiguation: values.DisambiguationRejectGap,
		Zone: scenario.Fixture.Calendar.Zone, Calendar: scenario.Fixture.Calendar.Calendar,
		Policy: values.ReferenceUpdateReviewRequired,
	}
	requirement, err := wait.ComputeTimerRequirement(node, values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"})
	if err != nil {
		t.Fatalf("ComputeTimerRequirement: %v", err)
	}
	if requirement.Digest == "" || requirement.Reference.Zone.String() != "America/New_York@2026a" {
		t.Fatalf("wait requirement = %+v, want pinned boundary dataset", requirement)
	}
}

func TestPromotionRevalidationRefusesAnInvalidPayrollDate(t *testing.T) {
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatal(err)
	}
	scenario := scenarios[0]
	invalidDate := fixedDate(t, "2027-01-02")
	fact, err := scenario.Fixture.Fact(invalidDate, scenario.ObservedAt)
	if err != nil {
		t.Fatalf("Fact: %v", err)
	}
	if fact.Status != EffectiveDateInvalid {
		t.Fatalf("invalid date status = %s, want %s", fact.Status, EffectiveDateInvalid)
	}
	historicalFacts := baseRevalidationFacts()
	historical := baseHistoricalApproval(t, historicalFacts)
	result, err := revalidatePromotionAt(scenario.ObservedAt, historical, historicalFacts, fact)
	if err != nil {
		t.Fatalf("Revalidate with payroll cutoff fact: %v", err)
	}
	if result.Confirmed {
		t.Fatalf("invalid payroll date was CONFIRMED: %+v", result)
	}
	if result.Requirement != revalidate.RequirementBlock || result.RecomposedState != decision.Deny {
		t.Fatalf("invalid payroll date result = %+v, want BLOCK/DENY", result)
	}
	if !slices.Contains(result.ChangedInputs, revalidate.ChangedConflict) {
		t.Fatalf("changed inputs = %v, want payroll cutoff carried as CONFLICT", result.ChangedInputs)
	}
}

func TestTodo_CONF_008_Golden(t *testing.T) {
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		status      EffectiveDateStatus
		expectation string
	}{
		"after-cutoff":    {EffectiveDateRetroactive, RetroExpectation},
		"before-cutoff":   {EffectiveDateCurrent, ""},
		"cutoff-boundary": {EffectiveDateCurrent, ""},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Fixture.Name, func(t *testing.T) {
			wantCase, ok := want[scenario.Fixture.Name]
			if !ok {
				t.Fatalf("no golden for %q", scenario.Fixture.Name)
			}
			fact, err := scenario.Fixture.Fact(scenario.EffectiveOn, scenario.ObservedAt)
			if err != nil {
				t.Fatal(err)
			}
			if fact.Status != wantCase.status {
				t.Fatalf("status = %s, want %s", fact.Status, wantCase.status)
			}
			_, expectation, err := scenario.Fixture.Classify(scenario.EffectiveOn, scenario.ObservedAt)
			if err != nil {
				t.Fatal(err)
			}
			if expectation != wantCase.expectation || fact.EvidenceRef == "" {
				t.Fatalf("expectation/evidence = %q/%q, want %q/non-empty", expectation, fact.EvidenceRef, wantCase.expectation)
			}
		})
	}
}

func TestTodo_CONF_008_Conformance(t *testing.T) {
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range scenarios {
		if scenario.Fixture.Period.Ref.ID == "" || scenario.Fixture.CutoffAt.String() == "" {
			t.Fatalf("scenario %q lacks payroll period or cutoff identity", scenario.Fixture.Name)
		}
		oneTimeline(t, scenario)
	}
}

func TestTodo_CONF_008_Fault(t *testing.T) {
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatal(err)
	}
	invalid := fixedDate(t, "2026-09-30")
	status, expectation, err := scenarios[0].Fixture.Classify(invalid, scenarios[0].ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if status != EffectiveDateInvalid || expectation != "PAYROLL_DATE_OUTSIDE_PERIOD" {
		t.Fatalf("outside-period classification = %s/%q, want INVALID/PAYROLL_DATE_OUTSIDE_PERIOD", status, expectation)
	}
}

func TestTodo_CONF_008_Mutation(t *testing.T) {
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatal(err)
	}
	scenario := scenarios[2]
	current, _, err := scenario.Fixture.Classify(scenario.EffectiveOn, scenario.Fixture.CutoffAt)
	if err != nil {
		t.Fatal(err)
	}
	retro, _, err := scenario.Fixture.Classify(scenario.EffectiveOn, values.NewInstant(scenario.Fixture.CutoffAt.Time().Add(time.Nanosecond)))
	if err != nil {
		t.Fatal(err)
	}
	if current != EffectiveDateCurrent || retro != EffectiveDateRetroactive {
		t.Fatalf("cutoff mutation = %s -> %s, want CURRENT -> RETROACTIVE", current, retro)
	}
}

func TestTodo_CONF_008_Race(t *testing.T) {
	scenarios, err := GoldenCutoffScenarios()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, scenario := range scenarios {
		scenario := scenario
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 32 {
				status, _, classifyErr := scenario.Fixture.Classify(scenario.EffectiveOn, scenario.ObservedAt)
				if classifyErr != nil || status != scenario.WantStatus {
					t.Errorf("%s concurrent classification = %s/%v, want %s", scenario.Fixture.Name, status, classifyErr, scenario.WantStatus)
				}
			}
		}()
	}
	wg.Wait()
}

func baseRevalidationFacts() revalidate.Facts {
	return revalidate.Facts{
		AuthZ:               revalidate.AuthZFact{Effect: decision.Allow, PolicyVersion: "authz.p1a.v1"},
		Session:             revalidate.SessionFact{Effect: decision.Allow, SessionID: "session-promotion-1", Assurance: "AAL2"},
		SourceAuthority:     revalidate.SourceAuthorityFact{Decision: "authority.assignment/v1"},
		FieldClassification: revalidate.FieldClassificationFact{Version: "classification.hr/2026.1"},
		LegalPolicy:         revalidate.LegalPolicyFact{LegalPackVersion: "legal.hr/2026.1", PolicyPackVersion: "policy.promotion/v1"},
		BudgetPosition: revalidate.BudgetPositionFact{
			Budget:   revalidate.ResourceFact{Effect: decision.Allow, ObservationID: "budget-promotion-1"},
			Position: revalidate.ResourceFact{Effect: decision.Allow, ObservationID: "position-promotion-1"},
		},
		Conflict: revalidate.ConflictFact{Effect: decision.Allow, Classification: "COMPATIBLE_MERGE", EvidenceRef: "conflict-promotion-1"},
	}
}

func baseHistoricalApproval(t *testing.T, facts revalidate.Facts) revalidate.HistoricalApproval {
	t.Helper()
	inputs := decision.Inputs{
		ProposalRevisionDigest: "sha256:promotion-revision-1",
		Context: decision.Context{
			Principal: "principal:promotion-manager", Delegation: "none", Capability: "promotion.execute/v1",
			Resource: "worker:promotion-1", Fields: []string{"assignment.job", "payroll.effective_date"},
			CurrentOrganization: "engineering", TargetOrganization: "engineering", Purpose: "promotion.execute", Risk: "LOW",
			Authority: facts.SourceAuthority.Decision, Legal: facts.LegalPolicy.LegalPackVersion,
		},
		ControlSnapshot: decision.ControlSnapshot{
			Digest: "sha256:control-snapshot-1", PolicyBundle: facts.LegalPolicy.PolicyPackVersion,
			LegalContext: facts.LegalPolicy.LegalPackVersion, Classification: facts.FieldClassification.Version,
			ReferenceData: "reference.pay-band/2026.1", SourceWatermark: "people.worker@1",
		},
		RulePackVersions:     []decision.RulePackVersion{{ID: "legal.hr", Version: "2026.1", Digest: "sha256:legal"}},
		ApprovalRequirements: []decision.ApprovalRequirement{{ID: "approval.manager", Version: "1", Satisfaction: decision.ApprovalSatisfied}},
		SoDVerdicts:          []decision.SoDVerdict{{RuleID: "sod.promotion", Version: "1", State: decision.Allow, Satisfied: true}},
		Subdecisions: []decision.Subdecision{
			{ID: "authz", Source: "authz", State: facts.AuthZ.Effect, FiredRules: []decision.RuleRef{{ID: "authz.policy_version", Version: facts.AuthZ.PolicyVersion}}},
			{ID: "session", Source: "session", State: facts.Session.Effect, FiredRules: []decision.RuleRef{{ID: "session.identity", Version: facts.Session.SessionID}, {ID: "session.assurance", Version: facts.Session.Assurance}}},
			{ID: "budget", Source: "budget", State: facts.BudgetPosition.Budget.Effect, FiredRules: []decision.RuleRef{{ID: "budget.observation", Version: facts.BudgetPosition.Budget.ObservationID}}},
			{ID: "position", Source: "position", State: facts.BudgetPosition.Position.Effect, FiredRules: []decision.RuleRef{{ID: "position.observation", Version: facts.BudgetPosition.Position.ObservationID}}},
			{ID: "conflict", Source: "conflict", State: facts.Conflict.Effect, FiredRules: []decision.RuleRef{{ID: "conflict.classification", Version: facts.Conflict.Classification}, {ID: "conflict.evidence", Version: facts.Conflict.EvidenceRef}}},
		},
	}
	composed, err := decision.Compose(inputs)
	if err != nil {
		t.Fatalf("Compose historical approval: %v", err)
	}
	if composed.State != decision.Allow {
		t.Fatalf("historical state = %s, want ALLOW", composed.State)
	}
	return revalidate.HistoricalApproval{Inputs: inputs, Decision: composed, Facts: facts, PlanDigest: "plan-promotion-1"}
}

func revalidatePromotionAt(now values.Instant, historical revalidate.HistoricalApproval, current revalidate.Facts, cutoff PayrollCutoffFact) (revalidate.Result, error) {
	if cutoff.Status == EffectiveDateInvalid {
		// The shared GOVERN-003 revalidator has no payroll-specific category.
		// Carrying the typed cutoff fact through its conflict input preserves
		// the real fail-closed requirement without inventing a second policy
		// engine in this conformance package.
		current.Conflict.Effect = decision.Deny
		current.Conflict.Classification = "PAYROLL_CUTOFF_INVALID"
		current.Conflict.EvidenceRef = cutoff.EvidenceRef
	}
	return revalidate.Revalidate(func() values.Instant { return now }, historical, current, historical.PlanDigest)
}
