package intentmanifests

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// conformanceFixtureRegistry returns a small deterministic registry with two
// implemented features in different groups plus one explicitly deferred row.
func conformanceFixtureRegistry() FeatureIntentCoverageRegistry {
	return FeatureIntentCoverageRegistry{
		Version:           "1.0",
		FeatureGroups:     2,
		FeatureCount:      3,
		DispositionCounts: map[FeatureDisposition]int{DispositionReview: 3},
		Features: []FeatureIntentCoverage{
			{
				FeatureID: "worker_create", Group: 1, GroupName: "People",
				CanonicalIdentity:      "hcmnext.people.worker_create",
				Classification:         ClassCreate,
				Role:                   RoleIntentCreator,
				CoverageStatus:         CoverageDefined,
				BoundIntentID:          "hcmnext.people.change_manager/v1",
				Disposition:            DispositionReview,
				DispositionRationale:   "reviewed",
				Owner:                  "people",
				Phase:                  "GATE_A",
				Depth:                  "FULL",
				Actor:                  "manager",
				Channel:                "POST /workers",
				Subject:                "people",
				Resource:               "worker",
				Capability:             "intent:hcmnext.people.change_manager",
				CapabilityVersion:      "1",
				InputSchema:            "properties:worker",
				ResultSchema:           "result:worker",
				ParentChildBehavior:    "independent-until-cataloged",
				GovernanceProfile:      "owner:people",
				EvidenceExpectation:    "request_digest+decision",
				SourceGroupProvenance:  "intake:1",
				SourceFeatureCanonical: "worker_create",
			},
			{
				FeatureID: "payband_evaluate", Group: 2, GroupName: "Rewards",
				CanonicalIdentity:      "hcmnext.rewards.payband_evaluate",
				Classification:         ClassConsume,
				Role:                   RoleIntentConsumer,
				CoverageStatus:         CoverageDefined,
				BoundIntentID:          "hcmnext.rewards.evaluate_pay_band_position/v1",
				Disposition:            DispositionReview,
				DispositionRationale:   "reviewed",
				Owner:                  "rewards",
				Phase:                  "GATE_A",
				Depth:                  "FULL",
				Actor:                  "analyst",
				Channel:                "GET /pay-bands/position",
				Subject:                "rewards",
				Resource:               "pay_band",
				Capability:             "intent:hcmnext.rewards.evaluate_pay_band_position",
				CapabilityVersion:      "1",
				InputSchema:            "properties:payband",
				ResultSchema:           "result:payband",
				ParentChildBehavior:    "independent-until-cataloged",
				GovernanceProfile:      "owner:rewards",
				EvidenceExpectation:    "input_snapshot+decision",
				SourceGroupProvenance:  "intake:2",
				SourceFeatureCanonical: "payband_evaluate",
			},
			{
				FeatureID: "merit_future", Group: 2, GroupName: "Rewards",
				CanonicalIdentity:      "hcmnext.rewards.merit_future",
				Classification:         ClassCreate,
				Role:                   RoleIntentCreator,
				CoverageStatus:         CoverageDeferred,
				BoundIntentID:          DeferredIntentBinding,
				Disposition:            DispositionDeferredToIntent,
				DispositionTarget:      "hcmnext.rewards.merit_cycle_manage/v1",
				DispositionRationale:   "deferred to funded intent",
				Owner:                  "rewards",
				Phase:                  "DESIGN",
				Depth:                  "INTAKE",
				Actor:                  "manager",
				Channel:                "governed",
				Subject:                "rewards",
				Resource:               "merit_cycle",
				Capability:             "intent:hcmnext.rewards.merit_cycle_manage",
				CapabilityVersion:      "1",
				InputSchema:            "properties:merit",
				ResultSchema:           "result:merit",
				ParentChildBehavior:    "independent-until-cataloged",
				GovernanceProfile:      "owner:rewards",
				EvidenceExpectation:    "source_provenance",
				SourceGroupProvenance:  "intake:2",
				SourceFeatureCanonical: "merit_future",
			},
		},
	}
}

func conformanceFixtureRoutes() []string {
	return []string{"POST /workers", "GET /pay-bands/position"}
}

// TestEveryImplementedMaterialFeatureHasIntentConformance is INTENT-024's
// PRIMARY proof: every implemented registry entry carries all seven
// conformance vectors at its declared depth, every enabled route is
// exercised, and deferred rows are unavailable rather than passing.
func TestEveryImplementedMaterialFeatureHasIntentConformance(t *testing.T) {
	registry := conformanceFixtureRegistry()
	before := fmt.Sprintf("%+v", registry.Features[0])
	suite, err := GenerateFeatureIntentConformanceSuite(registry, conformanceFixtureRoutes())
	if err != nil {
		t.Fatalf("GenerateFeatureIntentConformanceSuite: %v", err)
	}
	if len(suite.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(suite.Entries))
	}
	available := 0
	groups := map[string]bool{}
	for _, entry := range suite.Entries {
		if entry.Availability == ConformanceAvailable {
			available++
			groups[entry.GroupName] = true
			v := entry.Vectors
			for name, value := range map[string]string{
				"behavior":    v.Behavior,
				"denial":      v.DenialVector,
				"idempotency": v.IdempotencyVector,
				"zeroBypass":  v.ZeroBypass,
				"lifecycle":   v.LifecycleOracle,
				"evidence":    v.EvidenceExpectation,
				"depth":       v.DepthAssertion,
			} {
				if value == "" {
					t.Fatalf("feature %s vector %s is empty", entry.FeatureID, name)
				}
			}
			if entry.BoundIntent == "" || entry.BoundIntent == DeferredIntentBinding || entry.BoundIntent == "MISSING" {
				t.Fatalf("available feature %s has no real binding: %q", entry.FeatureID, entry.BoundIntent)
			}
		} else {
			if entry.Vectors != (ConformanceVectors{}) {
				t.Fatalf("unavailable feature %s carries vectors and could be mistaken for implementation", entry.FeatureID)
			}
		}
	}
	if available != 2 {
		t.Fatalf("available = %d, want 2", available)
	}
	if len(groups) != 2 {
		t.Fatalf("representative groups covered = %d, want 2", len(groups))
	}
	if len(suite.UncoveredRoutes) != 0 {
		t.Fatalf("uncovered routes = %v, want none", suite.UncoveredRoutes)
	}
	if after := fmt.Sprintf("%+v", registry.Features[0]); after != before {
		t.Fatal("generator mutated the registry input")
	}
	if suite.Digest == "" {
		t.Fatal("suite digest is empty")
	}
	again, err := GenerateFeatureIntentConformanceSuite(conformanceFixtureRegistry(), conformanceFixtureRoutes())
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if again.Digest != suite.Digest {
		t.Fatalf("digest unstable: %q vs %q", suite.Digest, again.Digest)
	}
}

// TestTodo_INTENT_024_Golden pins the fixture suite digest so a changed
// feature identity or vector cannot hide behind a passing count.
func TestTodo_INTENT_024_Golden(t *testing.T) {
	suite, err := GenerateFeatureIntentConformanceSuite(conformanceFixtureRegistry(), conformanceFixtureRoutes())
	if err != nil {
		t.Fatalf("GenerateFeatureIntentConformanceSuite: %v", err)
	}
	const wantDigest = "sha256:b06f60e4cbaa8762318bb78814c60623455f426d6ca80171ee276bed13974f7a"
	if suite.Digest != wantDigest {
		t.Fatalf("fixture suite digest = %q, want pinned golden %q", suite.Digest, wantDigest)
	}
}

// TestTodo_INTENT_024_Race proves deterministic generation under concurrent
// use.
func TestTodo_INTENT_024_Race(t *testing.T) {
	const workers = 8
	digests := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			suite, err := GenerateFeatureIntentConformanceSuite(conformanceFixtureRegistry(), conformanceFixtureRoutes())
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = suite.Digest
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	for i := 1; i < workers; i++ {
		if digests[i] != digests[0] {
			t.Fatalf("digest mismatch under concurrency: %q vs %q", digests[i], digests[0])
		}
	}
}

// TestTodo_INTENT_024_Conformance proves every enabled route is exercised and
// an unexercised route is a gap, not a silent pass.
func TestTodo_INTENT_024_Conformance(t *testing.T) {
	registry := conformanceFixtureRegistry()
	if _, err := GenerateFeatureIntentConformanceSuite(registry, conformanceFixtureRoutes()); err != nil {
		t.Fatalf("covered routes: %v", err)
	}
	routes := append(conformanceFixtureRoutes(), "DELETE /workers/{id}")
	if _, err := GenerateFeatureIntentConformanceSuite(registry, routes); !errors.Is(err, ErrConformanceGap) {
		t.Fatalf("uncovered route error = %v, want ErrConformanceGap", err)
	}
}

// TestTodo_INTENT_024_Recovery proves DEFERRED|MISSING rows resolve to
// explicitly unavailable entries and are therefore never served.
func TestTodo_INTENT_024_Recovery(t *testing.T) {
	registry := conformanceFixtureRegistry()
	registry.Features = append(registry.Features, FeatureIntentCoverage{
		FeatureID: "legacy_missing", Group: 1, GroupName: "People",
		CanonicalIdentity: "hcmnext.people.legacy_missing", Classification: ClassObserve,
		Role: RoleIntentObserver, CoverageStatus: CoverageMissing, BoundIntentID: "MISSING",
		Disposition: DispositionReview, DispositionRationale: "no binding",
		Owner: "people", Phase: "DESIGN", Depth: "INTAKE", Actor: "reader",
		Channel: "GET /legacy", EvidenceExpectation: "source_provenance",
	})
	suite, err := GenerateFeatureIntentConformanceSuite(registry, append(conformanceFixtureRoutes(), "GET /legacy"))
	if err != nil {
		t.Fatalf("GenerateFeatureIntentConformanceSuite: %v", err)
	}
	byID := map[string]ConformanceCase{}
	for _, entry := range suite.Entries {
		byID[entry.FeatureID] = entry
	}
	deferred, ok := byID["merit_future"]
	if !ok || deferred.Availability != ConformanceUnavailableDeferred {
		t.Fatalf("deferred entry = %+v, want UNAVAILABLE_DEFERRED", deferred)
	}
	missing, ok := byID["legacy_missing"]
	if !ok || missing.Availability != ConformanceUnavailableMissing {
		t.Fatalf("missing entry = %+v, want UNAVAILABLE_MISSING", missing)
	}
	if byID["worker_create"].Availability != ConformanceAvailable {
		t.Fatalf("implemented entry unavailable: %+v", byID["worker_create"])
	}
}

// TestTodo_INTENT_024_Mutation proves a dropped vector is reported as a gap.
func TestTodo_INTENT_024_Mutation(t *testing.T) {
	registry := conformanceFixtureRegistry()
	registry.Features[0].EvidenceExpectation = ""
	if _, err := GenerateFeatureIntentConformanceSuite(registry, conformanceFixtureRoutes()); !errors.Is(err, ErrConformanceGap) {
		t.Fatalf("missing evidence error = %v, want ErrConformanceGap", err)
	}
	registry = conformanceFixtureRegistry()
	registry.Features[1].Channel = ""
	if _, err := GenerateFeatureIntentConformanceSuite(registry, conformanceFixtureRoutes()); !errors.Is(err, ErrConformanceGap) {
		t.Fatalf("missing channel error = %v, want ErrConformanceGap", err)
	}
	registry = conformanceFixtureRegistry()
	registry.Features[0].Classification = ClassReviewNeeded
	if _, err := GenerateFeatureIntentConformanceSuite(registry, conformanceFixtureRoutes()); !errors.Is(err, ErrConformanceGap) {
		t.Fatalf("review-needed implemented error = %v, want ErrConformanceGap", err)
	}
}
