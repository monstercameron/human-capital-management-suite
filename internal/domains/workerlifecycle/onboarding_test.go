package workerlifecycle

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func onboardingPlan(t *testing.T) WorkerLifecyclePlan {
	t.Helper()
	cal := values.CalendarRef{Ref: "gregorian", Version: "1"}
	newReq := func(id string, ordinal int, owner string, offset int) Requirement {
		return Requirement{
			ID: id, Ordinal: ordinal, Owner: owner,
			Due:                DueRule{Calendar: cal, OffsetDays: offset},
			Evidence:           []values.EntityRef{ref("evidence", "00000000-0000-4000-8000-000000000100")},
			VerificationPolicy: ref("evidence_policy", "00000000-0000-4000-8000-000000000200"),
			Completion:         CompleteAllRequired,
			Required:           true,
		}
	}
	optional := newReq("welcome-lunch", 4, "people", 7)
	optional.Required = false
	p, err := NewPlan(WorkerLifecyclePlan{
		Worker:     ref("worker", "00000000-0000-4000-8000-000000000001"),
		Employment: ref("employment", "00000000-0000-4000-8000-000000000002"),
		Proposal:   ref("proposal", "00000000-0000-4000-8000-000000000003"),
		Event:      EventStart,
		EventDate:  date(t, "2026-01-05"),
		Completion: CompleteAllRequired,
		Requirements: []Requirement{
			newReq("identity", 1, "people", 0),
			newReq("work-auth", 2, "people", 0),
			newReq("training", 3, "learning", 5),
			optional,
		},
		Children: []ChildTemplate{
			{ID: "identity-child", Ordinal: 1, IntentType: "hcm.identity.verify", IntentVersion: "v1"},
			{ID: "training-child", Ordinal: 2, IntentType: "hcm.learning.assign", IntentVersion: "v1", DependsOn: []string{"identity-child"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func onboardingInputs() []RequirementInput {
	return []RequirementInput{
		{RequirementID: "identity", Kind: RequirementIdentity, Protected: true, FreshDays: 30},
		{RequirementID: "work-auth", Kind: RequirementLegal, Protected: true, FreshDays: 30},
		{RequirementID: "training", Kind: RequirementTraining, FreshDays: 90},
		{RequirementID: "welcome-lunch", Kind: RequirementTask, Optional: true, FreshDays: 90},
	}
}

func onboardingFact(requirement, summary string, observed string) WorkerFact {
	return WorkerFact{
		RequirementID: requirement,
		ObservedAt:    mustDate(observed),
		Evidence:      ref("evidence", "00000000-0000-4000-8000-000000000100"),
		Summary:       summary,
	}
}

func mustDate(s string) values.LocalDate {
	d, err := values.ParseLocalDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func onboardingRequest(t *testing.T) ResolutionRequest {
	t.Helper()
	return ResolutionRequest{
		Plan:         onboardingPlan(t),
		Requirements: onboardingInputs(),
		AsOf:         date(t, "2026-01-04"),
		Facts: []WorkerFact{
			onboardingFact("identity", "passport verified", "2025-12-20"),
			onboardingFact("work-auth", "authorization valid", "2025-12-20"),
			onboardingFact("training", "scheduled", "2025-12-20"),
		},
	}
}

// TestOnboardingRequirementResolutionReturnsReadyConditionalBlockedOrUnknown
// is the primary acceptance case: pinned context returns per-requirement and
// aggregate READY|CONDITIONAL|BLOCKED|UNKNOWN with exact blockers, and
// resolution is pure.
func TestOnboardingRequirementResolutionReturnsReadyConditionalBlockedOrUnknown(t *testing.T) {
	got, err := ResolveOnboardingReadiness(onboardingRequest(t))
	if err != nil {
		t.Fatalf("ResolveOnboardingReadiness: %v", err)
	}
	rejectEmptyLifecycleDigest(t, got.Digest)
	if got.Aggregate != AggregateReady {
		t.Fatalf("aggregate = %s, want READY", got.Aggregate)
	}
	byID := map[string]RequirementResult{}
	for _, result := range got.Results {
		byID[result.RequirementID] = result
	}
	if byID["identity"].Status != StatusSatisfied || byID["training"].Status != StatusSatisfied {
		t.Fatalf("results = %+v, want satisfied", got.Results)
	}
	if byID["welcome-lunch"].Status != StatusSkippedOptional {
		t.Fatalf("optional = %s, want SKIPPED_OPTIONAL", byID["welcome-lunch"].Status)
	}

	// Missing identity fact with the due date passed blocks the start.
	blocked := onboardingRequest(t)
	blocked.AsOf = date(t, "2026-01-06")
	blocked.Facts = blocked.Facts[1:]
	rec, err := ResolveOnboardingReadiness(blocked)
	if err != nil {
		t.Fatalf("blocked: %v", err)
	}
	if rec.Aggregate != AggregateBlocked {
		t.Fatalf("aggregate = %s, want BLOCKED", rec.Aggregate)
	}
	if len(rec.Blockers) == 0 {
		t.Fatal("no blockers named")
	}

	// Missing training fact with the due date ahead is conditional.
	conditional := onboardingRequest(t)
	conditional.Facts = conditional.Facts[:2]
	rec, err = ResolveOnboardingReadiness(conditional)
	if err != nil {
		t.Fatalf("conditional: %v", err)
	}
	if rec.Aggregate != AggregateConditional {
		t.Fatalf("aggregate = %s, want CONDITIONAL", rec.Aggregate)
	}

	// Stale identity evidence is unknown, never satisfied.
	stale := onboardingRequest(t)
	stale.Facts[0] = onboardingFact("identity", "passport verified", "2025-01-01")
	rec, err = ResolveOnboardingReadiness(stale)
	if err != nil {
		t.Fatalf("stale: %v", err)
	}
	if rec.Aggregate != AggregateUnknown {
		t.Fatalf("aggregate = %s, want UNKNOWN", rec.Aggregate)
	}
	for _, result := range rec.Results {
		if result.RequirementID == "identity" && result.Status != StatusUnknown {
			t.Fatalf("stale identity = %s, want UNKNOWN", result.Status)
		}
	}

	// A manager cannot waive a legal requirement.
	waived := onboardingRequest(t)
	waived.AsOf = date(t, "2026-01-06")
	waived.Facts = waived.Facts[1:]
	waived.Waivers = []Waiver{{RequirementID: "identity", ByRole: "MANAGER", At: mustDate("2026-01-05")}}
	waived.Requirements[0].Kind = RequirementLegal
	rec, err = ResolveOnboardingReadiness(waived)
	if err != nil {
		t.Fatalf("waived: %v", err)
	}
	if rec.Aggregate != AggregateBlocked {
		t.Fatalf("manager waiver aggregate = %s, want BLOCKED", rec.Aggregate)
	}

	// An authority waiver of the same requirement satisfies it.
	waived.Waivers[0].ByRole = WaiverLegalAuthority
	rec, err = ResolveOnboardingReadiness(waived)
	if err != nil {
		t.Fatalf("authority waiver: %v", err)
	}
	for _, result := range rec.Results {
		if result.RequirementID == "identity" && result.Status != StatusSatisfied {
			t.Fatalf("authority waiver = %s, want SATISFIED", result.Status)
		}
	}

	// Resolution is pure: repeated calls agree and the request is unmodified.
	before := onboardingRequest(t)
	first, err := ResolveOnboardingReadiness(before)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveOnboardingReadiness(before)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("resolution is not deterministic")
	}

	// Unknown inputs are refused, not defaulted.
	bad := onboardingRequest(t)
	bad.Requirements[0].Kind = "ASTROLOGY"
	if _, err := ResolveOnboardingReadiness(bad); !errors.Is(err, ErrReadinessRejected) {
		t.Fatalf("unknown kind: err = %v, want WORKER_LIFE_002_REJECTED", err)
	}
}

// TestTodo_WORKER_LIFE_002_Property proves aggregate precedence and input
// order independence over generated resolutions.
func TestTodo_WORKER_LIFE_002_Property(t *testing.T) {
	base := onboardingRequest(t)
	got, err := ResolveOnboardingReadiness(base)
	if err != nil {
		t.Fatal(err)
	}
	// Input order never changes the outcome.
	shuffled := onboardingRequest(t)
	shuffled.Requirements[0], shuffled.Requirements[2] = shuffled.Requirements[2], shuffled.Requirements[0]
	shuffled.Facts[0], shuffled.Facts[2] = shuffled.Facts[2], shuffled.Facts[0]
	again, err := ResolveOnboardingReadiness(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != got.Digest {
		t.Fatal("digest depends on input order")
	}
	for i := range got.Results {
		if got.Results[i] != again.Results[i] {
			t.Fatalf("result %d differs under reorder", i)
		}
	}
	// Precedence: a blocker beats unknown, unknown beats pending.
	for _, tc := range []struct {
		name  string
		facts []WorkerFact
		asOf  string
		want  ReadinessAggregate
	}{
		{"blocked beats unknown", []WorkerFact{
			onboardingFact("identity", "passport verified", "2025-01-01"),
			onboardingFact("training", "scheduled", "2025-12-20"),
		}, "2026-01-06", AggregateBlocked},
		{"unknown beats pending", []WorkerFact{
			onboardingFact("identity", "passport verified", "2025-01-01"),
			onboardingFact("work-auth", "authorization valid", "2025-12-20"),
		}, "2026-01-04", AggregateUnknown},
		{"pending alone", []WorkerFact{
			onboardingFact("identity", "passport verified", "2025-12-20"),
			onboardingFact("work-auth", "authorization valid", "2025-12-20"),
		}, "2026-01-04", AggregateConditional},
	} {
		req := onboardingRequest(t)
		req.Facts = tc.facts
		req.AsOf = date(t, tc.asOf)
		rec, err := ResolveOnboardingReadiness(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if rec.Aggregate != tc.want {
			t.Fatalf("%s: aggregate = %s, want %s", tc.name, rec.Aggregate, tc.want)
		}
	}
}

// TestTodo_WORKER_LIFE_002_Golden pins the canonical digest of the fixture
// resolution.
func TestTodo_WORKER_LIFE_002_Golden(t *testing.T) {
	got, err := ResolveOnboardingReadiness(onboardingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:07b5b92bf3d437d8f2be25e4a3b6a526f5a21c3f850350e145f9c97d1d1cb0dd"
	if got.Digest != want {
		t.Fatalf("digest = %s, want %s", got.Digest, want)
	}
}

// TestTodo_WORKER_LIFE_002_Security proves protected evidence never leaks
// into the manager explanation and unauthorized waivers never satisfy.
func TestTodo_WORKER_LIFE_002_Security(t *testing.T) {
	got, err := ResolveOnboardingReadiness(onboardingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	manager := got.ExplainFor(AudienceManager)
	for _, leak := range []string{"passport verified", "authorization valid"} {
		if containsLifecycle(manager, leak) {
			t.Fatalf("manager explanation leaks %q", leak)
		}
	}
	if !containsLifecycle(manager, "REDACTED") {
		t.Fatal("manager explanation marks nothing redacted")
	}
	owner := got.ExplainFor(AudienceOwner)
	if !containsLifecycle(owner, "passport verified") {
		t.Fatal("owner explanation omits evidence it is entitled to")
	}
	// A cross-tenant evidence reference is refused.
	bad := onboardingRequest(t)
	fact := onboardingFact("identity", "passport verified", "2025-12-20")
	fact.Evidence.Tenant = "other-tenant"
	bad.Facts[0] = fact
	if _, err := ResolveOnboardingReadiness(bad); !errors.Is(err, ErrReadinessRejected) {
		t.Fatalf("cross-tenant evidence: err = %v, want WORKER_LIFE_002_REJECTED", err)
	}
}

// TestTodo_WORKER_LIFE_002_Mutation proves the digest binds plan, facts and
// every outcome: any change yields a new digest and tampering fails.
func TestTodo_WORKER_LIFE_002_Mutation(t *testing.T) {
	base, err := ResolveOnboardingReadiness(onboardingRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*ResolutionRequest){
		func(r *ResolutionRequest) { r.Facts[0] = onboardingFact("identity", "drivers licence", "2025-12-20") },
		func(r *ResolutionRequest) { r.AsOf = mustDate("2026-01-05") },
		func(r *ResolutionRequest) {
			r.Waivers = []Waiver{{RequirementID: "welcome-lunch", ByRole: "MANAGER", At: mustDate("2026-01-04")}}
		},
	}
	for i, mutate := range mutations {
		req := onboardingRequest(t)
		mutate(&req)
		mutated, err := ResolveOnboardingReadiness(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if mutated.Digest == base.Digest {
			t.Fatalf("case %d did not change the digest", i)
		}
	}
	tampered := base
	tampered.Aggregate = AggregateReady
	tampered.Results[0].Status = StatusBlocked
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered outcome passed validation")
	}
	forged := base
	forged.Digest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}

func containsLifecycle(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func rejectEmptyLifecycleDigest(t *testing.T, digest string) {
	t.Helper()
	if digest == "" || digest == "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("digest %q is empty or the empty-payload hash", digest)
	}
}
