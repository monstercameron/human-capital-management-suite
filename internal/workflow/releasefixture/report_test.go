package releasefixture_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var ranAt = time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)

func publish(t *testing.T, fixtures ...string) version.CompiledVersion {
	t.Helper()
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatal(err)
	}
	v, err := version.Publish(version.NewRegistry(), prototype.ApprovalDefinition(), plan, workflow.Options{Phase: workflow.PhaseP1B},
		version.PublishMeta{SemanticVersion: "1.0.0", PublishedAt: ranAt, PublishedBy: "publisher", FixtureRefs: fixtures})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func passing(version.CompiledVersion) error { return nil }

// TestTodo_WF_COMP_006_FixtureReport proves a run report is sealed, bound to
// the exact version and verifiable, and that every way it can be wrong is a
// typed refusal: tampering, another version, missing, extra or failed
// fixtures, a version with no declared fixtures, and a recorded pass the
// suite does not reproduce.
func TestTodo_WF_COMP_006_FixtureReport(t *testing.T) {
	v := publish(t, "fixture:a", "fixture:b")
	suite := releasefixture.Suite{"fixture:a": passing, "fixture:b": passing}

	report := releasefixture.Run(suite, v, "runner:test", ranAt)
	if !report.Passed() || report.ReportDigest == "" || !strings.HasPrefix(report.ReportDigest, "sha256:") {
		t.Fatalf("report = %+v, want a sealed passing report", report)
	}
	if err := releasefixture.Reproduce(report, v, suite); err != nil {
		t.Fatalf("Reproduce(passing) = %v", err)
	}
	encoded, err := report.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := releasefixture.Decode(encoded)
	if err != nil || decoded.ReportDigest != report.ReportDigest || releasefixture.Verify(decoded, v) != nil {
		t.Fatalf("round trip = %+v, %v", decoded, err)
	}
	if got := decoded.FixtureRefs(); len(got) != 2 || got[0] != "fixture:a" {
		t.Fatalf("FixtureRefs = %v", got)
	}

	if _, err := releasefixture.Decode([]byte(`{"format":"x","extra":1}`)); !errors.Is(err, releasefixture.ErrMalformed) {
		t.Fatalf("Decode(unknown field) = %v, want ErrMalformed", err)
	}
	if err := releasefixture.Verify(releasefixture.Report{}, v); !errors.Is(err, releasefixture.ErrMalformed) {
		t.Fatalf("Verify(empty) = %v, want ErrMalformed", err)
	}

	tampered := report
	tampered.Results = []releasefixture.Result{{FixtureRef: "fixture:a", Passed: true}, {FixtureRef: "fixture:b", Passed: true, Detail: "edited"}}
	if err := releasefixture.Verify(tampered, v); !errors.Is(err, releasefixture.ErrReportDigest) {
		t.Fatalf("Verify(tampered) = %v, want ErrReportDigest", err)
	}

	other := publish(t, "fixture:a")
	if err := releasefixture.Verify(report, other); !errors.Is(err, releasefixture.ErrVersionMismatch) {
		t.Fatalf("Verify(other record) = %v, want ErrVersionMismatch", err)
	}
	rebound := report
	rebound.CompiledPlanDigest = "sha256:another-plan"
	if err := releasefixture.Verify(rebound.Seal(), v); !errors.Is(err, releasefixture.ErrVersionMismatch) {
		t.Fatalf("Verify(resealed for another plan) = %v, want ErrVersionMismatch", err)
	}

	undeclared := publish(t)
	if err := releasefixture.Verify(releasefixture.Run(suite, undeclared, "runner:test", ranAt), undeclared); !errors.Is(err, releasefixture.ErrNoDeclaredFixtures) {
		t.Fatalf("Verify(no declared fixtures) = %v, want ErrNoDeclaredFixtures", err)
	}

	missing := report
	missing.Results = missing.Results[:1]
	if err := releasefixture.Verify(missing.Seal(), v); !errors.Is(err, releasefixture.ErrFixtureMissing) {
		t.Fatalf("Verify(missing fixture) = %v, want ErrFixtureMissing", err)
	}
	extra := report
	extra.Results = append(append([]releasefixture.Result(nil), report.Results...), releasefixture.Result{FixtureRef: "fixture:c", Passed: true})
	if err := releasefixture.Verify(extra.Seal(), v); !errors.Is(err, releasefixture.ErrFixtureMissing) {
		t.Fatalf("Verify(extra fixture) = %v, want ErrFixtureMissing", err)
	}

	failingSuite := releasefixture.Suite{"fixture:a": passing, "fixture:b": func(version.CompiledVersion) error { return errors.New("graph drifted") }}
	failed := releasefixture.Run(failingSuite, v, "runner:test", ranAt)
	if failed.Passed() {
		t.Fatal("a report with a failing fixture reports Passed")
	}
	if err := releasefixture.Verify(failed, v); !errors.Is(err, releasefixture.ErrFixtureFailed) || !strings.Contains(err.Error(), "graph drifted") {
		t.Fatalf("Verify(failed) = %v, want ErrFixtureFailed naming the detail", err)
	}
	unregistered := releasefixture.Run(releasefixture.Suite{"fixture:a": passing}, v, "runner:test", ranAt)
	if err := releasefixture.Verify(unregistered, v); !errors.Is(err, releasefixture.ErrFixtureFailed) {
		t.Fatalf("Verify(unregistered fixture) = %v, want ErrFixtureFailed", err)
	}

	// A forged report: every result claims a pass and the digest is resealed,
	// but the in-process suite fails fixture:b.
	forged := failed
	forged.Results = []releasefixture.Result{{FixtureRef: "fixture:a", Passed: true}, {FixtureRef: "fixture:b", Passed: true}}
	forged = forged.Seal()
	if err := releasefixture.Verify(forged, v); err != nil {
		t.Fatalf("Verify(forged) = %v; the structural check alone cannot see a forgery", err)
	}
	if err := releasefixture.Reproduce(forged, v, failingSuite); !errors.Is(err, releasefixture.ErrNotReproduced) {
		t.Fatalf("Reproduce(forged) = %v, want ErrNotReproduced", err)
	}
	if err := releasefixture.Reproduce(failed, v, suite); !errors.Is(err, releasefixture.ErrFixtureFailed) {
		t.Fatalf("Reproduce(failed report) = %v, want ErrFixtureFailed", err)
	}
	if (releasefixture.Report{}).Passed() {
		t.Fatal("an empty report reports Passed")
	}
}
