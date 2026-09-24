package releasefixture_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/rulepayload"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/testprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const savedRunRef = "fixture:author-run/promotion@v1"

func publishedPromotionRun(t *testing.T) (*simulate.PromotionSetup, version.CompiledVersion) {
	t.Helper()
	setup, err := simulate.NewPromotionSetup(simulate.PromotionExceedsThresholdPay)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := setup.Env.Registry()
	if err != nil {
		t.Fatal(err)
	}
	references, ok := setup.Options.RulePayloads.(*rulepayload.Store)
	if !ok {
		t.Fatalf("promotion simulator rule resolver has type %T, want *rulepayload.Store", setup.Options.RulePayloads)
	}
	compileOptions, err := workflow.PromotionReferenceV2Options(registry, references)
	if err != nil {
		t.Fatal(err)
	}
	v, err := version.Publish(version.NewRegistry(), workflow.PromotionReferenceV2Definition(), setup.Plan,
		compileOptions, version.PublishMeta{
			SemanticVersion: "1.0.0", PublishedAt: ranAt, PublishedBy: "author:test", FixtureRefs: []string{savedRunRef},
		})
	if err != nil {
		t.Fatal(err)
	}
	return setup, v
}

func promotionReceipt(t *testing.T, setup *simulate.PromotionSetup) simulate.Receipt {
	t.Helper()
	r, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTodo_WF_TEST_009(t *testing.T) {
	setup, v := publishedPromotionRun(t)
	run := promotionReceipt(t, setup)

	profiles, err := testprofile.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	profileRef := testprofile.ProfileRef{ID: "harborcare-promotion", Version: 1}
	profile, err := profiles.ResolveForTenant(profileRef, "harborcare-demo")
	if err != nil {
		t.Fatalf("resolve run profile: %v", err)
	}
	scenario := releasefixture.SavedRun{Outcomes: []releasefixture.OutcomeChoice{{
		NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: string(workflow.OutcomeSucceeded),
	}}}
	saved, err := releasefixture.Capture(savedRunRef, profile, scenario, v, run)
	if err != nil {
		t.Fatalf("Capture(actual simulator receipt) = %v", err)
	}
	if saved.ExpectedResultDigest != "sha256:"+run.Digest() || saved.CompiledPlanDigest != setup.Plan.Digest() {
		t.Fatalf("captured binding = %+v, receipt=%s plan=%s", saved, run.Digest(), setup.Plan.Digest())
	}
	if err := saved.Verify(v); err != nil {
		t.Fatalf("Verify(saved run) = %v", err)
	}

	encoded, err := saved.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := releasefixture.DecodeSavedRun(encoded)
	if err != nil || decoded.FixtureDigest != saved.FixtureDigest || decoded.Verify(v) != nil {
		t.Fatalf("saved run round trip = %+v, %v", decoded, err)
	}

	runner := func(ctx context.Context, fixture releasefixture.SavedRun, resolved testprofile.Profile, compiled version.CompiledVersion) (simulate.Receipt, error) {
		if fixture.Profile != profileRef || resolved.Digest() != fixture.ProfileDigest || compiled.CompiledPlanDigest != setup.Plan.Digest() {
			return simulate.Receipt{}, errors.New("unexpected fixture binding")
		}
		return simulate.Run(ctx, setup.Plan, setup.Inputs, setup.Options)
	}
	if err := releasefixture.ReproduceSavedRunWithProfiles(context.Background(), decoded, v, profiles, "harborcare-demo", runner); err != nil {
		t.Fatalf("ReproduceSavedRun(actual run) = %v", err)
	}

	differentInputs, err := simulate.NewPromotionSetup(simulate.PromotionWithinThresholdPay)
	if err != nil {
		t.Fatal(err)
	}
	if err := releasefixture.ReproduceSavedRunWithProfiles(context.Background(), decoded, v, profiles, "harborcare-demo", func(ctx context.Context, _ releasefixture.SavedRun, _ testprofile.Profile, _ version.CompiledVersion) (simulate.Receipt, error) {
		return simulate.Run(ctx, differentInputs.Plan, differentInputs.Inputs, differentInputs.Options)
	}); !errors.Is(err, releasefixture.ErrRunDigestMismatch) {
		t.Fatalf("ReproduceSavedRun(changed run) = %v, want ErrRunDigestMismatch", err)
	}

	tampered := decoded
	tampered.CancelAtNode = "different-node"
	if err := tampered.Verify(v); !errors.Is(err, releasefixture.ErrSavedRunDigest) {
		t.Fatalf("Verify(tampered fixture) = %v, want ErrSavedRunDigest", err)
	}
	if _, err := releasefixture.Capture(savedRunRef, profile, releasefixture.SavedRun{}, v, simulate.Receipt{}); !errors.Is(err, releasefixture.ErrSavedRunMalformed) {
		t.Fatalf("Capture(empty claimed receipt) = %v, want ErrSavedRunMalformed", err)
	}
	if _, err := releasefixture.Capture(savedRunRef, profile, releasefixture.SavedRun{
		ClockMoves: []releasefixture.ClockMove{{To: run.EndedAt}},
	}, v, run); !errors.Is(err, releasefixture.ErrScenarioUnsupported) {
		t.Fatalf("Capture(clock move without recorded simulator support) = %v, want ErrScenarioUnsupported", err)
	}
}

func TestTodo_WF_TEST_009_Conformance(t *testing.T) {
	setup, v := publishedPromotionRun(t)
	receipt := promotionReceipt(t, setup)
	profiles, err := testprofile.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	profile, err := profiles.ResolveForTenant(testprofile.ProfileRef{ID: "harborcare-promotion", Version: 1}, "harborcare-demo")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := releasefixture.Capture(savedRunRef, profile, releasefixture.SavedRun{Outcomes: []releasefixture.OutcomeChoice{{
		NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: string(workflow.OutcomeSucceeded),
	}}}, v, receipt)
	if err != nil {
		t.Fatal(err)
	}
	runner := func(ctx context.Context, _ releasefixture.SavedRun, profile testprofile.Profile, _ version.CompiledVersion) (simulate.Receipt, error) {
		if profile.Digest() == "" {
			return simulate.Receipt{}, errors.New("saved run was not supplied its resolved profile")
		}
		return simulate.Run(ctx, setup.Plan, setup.Inputs, setup.Options)
	}
	suite, err := releasefixture.SuiteFromSavedRuns([]releasefixture.SavedRun{saved}, profiles, "harborcare-demo", runner)
	if err != nil {
		t.Fatal(err)
	}
	report := releasefixture.Run(suite, v, "runner:workflow-test-lab", ranAt)
	if err := releasefixture.Reproduce(report, v, suite); err != nil {
		t.Fatalf("saved data fixture did not gate exact-version reproduction: %v", err)
	}

	missing, err := releasefixture.Capture("fixture:undeclared", profile, releasefixture.SavedRun{}, v, receipt)
	if err == nil {
		t.Fatalf("Capture(undeclared fixture) = %+v, want refusal", missing)
	}
	if _, err := releasefixture.SuiteFromSavedRuns([]releasefixture.SavedRun{saved, saved}, profiles, "harborcare-demo", runner); !errors.Is(err, releasefixture.ErrSavedRunMalformed) {
		t.Fatalf("SuiteFromSavedRuns(duplicate ref) = %v, want ErrSavedRunMalformed", err)
	}
}

func TestTodo_WF_TEST_009_Golden(t *testing.T) {
	setup, v := publishedPromotionRun(t)
	receipt := promotionReceipt(t, setup)
	profiles, err := testprofile.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	profile, err := profiles.ResolveForTenant(testprofile.ProfileRef{ID: "harborcare-promotion", Version: 1}, "harborcare-demo")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := releasefixture.Capture(savedRunRef, profile, releasefixture.SavedRun{Outcomes: []releasefixture.OutcomeChoice{{
		NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: string(workflow.OutcomeSucceeded),
	}}}, v, receipt)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := saved.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "wf_test_009_saved_run.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(want) {
		t.Fatalf("captured run differs from the actual-run golden:\n%s", encoded)
	}
	decoded, err := releasefixture.DecodeSavedRun(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := decoded.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(reencoded) {
		t.Fatalf("saved-run encoding changed after decode/encode:\n%s\n---\n%s", encoded, reencoded)
	}
	if got := saved.ComputeDigest(); saved.FixtureDigest != got || saved.ExpectedResultDigest != "sha256:"+receipt.Digest() {
		t.Fatalf("golden digests = fixture %s/%s result %s/%s", saved.FixtureDigest, got, saved.ExpectedResultDigest, receipt.Digest())
	}
}
