package execution

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var releaseAt = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

func shippedByWorkflow(t *testing.T, store version.Store) map[string]version.CompiledVersion {
	t.Helper()
	published, err := PublishShippedVersions(store, releaseAt)
	if err != nil {
		t.Fatalf("PublishShippedVersions: %v", err)
	}
	out := map[string]version.CompiledVersion{}
	for _, v := range published {
		out[v.WorkflowID] = v
	}
	if len(out) != 2 {
		t.Fatalf("shipped workflows = %v", out)
	}
	return out
}

// TestTodo_WF_COMP_006_ShippedFixtures proves every fixture a shipped version
// declares passes on that version and on no other: each fixture is bound to
// the exact compiled plan, and a record changed after publication fails.
func TestTodo_WF_COMP_006_ShippedFixtures(t *testing.T) {
	shipped := shippedByWorkflow(t, version.NewRegistry())
	approval, execute := shipped[prototype.ApprovalWorkflowID], shipped[promotionexec.WorkflowID]
	suite := ShippedFixtures()
	for _, pair := range []struct{ own, other version.CompiledVersion }{{approval, execute}, {execute, approval}} {
		report := releasefixture.Run(suite, pair.own, "runner:test", releaseAt)
		if !report.Passed() || releasefixture.Verify(report, pair.own) != nil {
			t.Fatalf("%s fixtures = %+v, want every declared fixture to pass", pair.own.WorkflowID, report.Results)
		}
		for _, ref := range pair.own.FixtureRefs {
			if err := suite[ref](pair.other); err == nil {
				t.Errorf("fixture %s passed on %s's version", ref, pair.other.WorkflowID)
			}
		}
	}
	// The two shipped execute versions: each passes its own versioned
	// fixtures, and the @v1 fixtures (frozen 1.0.0) fail on 1.1.0 and the @v2
	// fixtures fail on 1.0.0.
	published, err := PublishShippedVersions(version.NewRegistry(), releaseAt)
	if err != nil || len(published) != 4 {
		t.Fatalf("PublishShippedVersions = %d versions, %v; want prototype plus execute 1.0.0, 1.1.0 and 1.2.0", len(published), err)
	}
	v1, v11, v12 := published[1], published[2], published[3]
	if v1.SemanticVersion != promotionexec.SemanticVersionV1_0 || v11.SemanticVersion != promotionexec.SemanticVersionV1_1 || v12.SemanticVersion != promotionexec.SemanticVersion || v12.CompiledPlanDigest != execute.CompiledPlanDigest {
		t.Fatalf("execute versions = %s/%s/%s, want 1.0.0, 1.1.0, then %s", v1.SemanticVersion, v11.SemanticVersion, v12.SemanticVersion, promotionexec.SemanticVersion)
	}
	frozen, _ := promotionexec.CompileV1_0()
	if v1.CompiledPlanDigest != frozen.Digest() {
		t.Fatalf("execute 1.0.0 published as %s, want the frozen %s", v1.CompiledPlanDigest, frozen.Digest())
	}
	for _, pair := range []struct {
		own, other version.CompiledVersion
		ownOnly    []string
	}{
		{v1, v11, []string{FixtureExecuteCompile, FixtureExecuteGraph}},
		{v11, v1, []string{FixtureExecuteCompileV1_1, FixtureExecuteGraphV1_1}},
		{v12, v11, []string{FixtureExecuteCompileV1_2, FixtureExecuteGraphV1_2}},
	} {
		if report := releasefixture.Run(suite, pair.own, "runner:test", releaseAt); !report.Passed() {
			t.Fatalf("execute %s fixtures = %+v, want every declared fixture to pass", pair.own.SemanticVersion, report.Results)
		}
		for _, ref := range pair.ownOnly {
			if err := suite[ref](pair.other); err == nil {
				t.Errorf("fixture %s passed on execute %s", ref, pair.other.SemanticVersion)
			}
		}
	}
	changed := approval
	changed.CanonicalPlanBytes = []byte("{}\n")
	if err := suite[FixtureApprovalCompile](changed); err == nil {
		t.Fatal("a record changed after publication passed its compile fixture")
	}
	if err := reproducesPlan(approval, func() (*workflow.CompiledWorkflow, error) { return nil, errors.New("compiler unavailable") }); err == nil {
		t.Fatal("a failing recompile passed")
	}
}

// TestTodo_WF_COMP_006_GovernedRelease drives the governed release path over
// the durable registry: composition leaves both shipped versions DRAFT and
// unapproved; approval is refused on a failed, missing, digest-mismatched or
// unreproduced fixture report and on self-approval; an approved version is
// still not ACTIVE until the separate activation step; and the stored approval
// carries the report.
func TestTodo_WF_COMP_006_GovernedRelease(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}
	shipped := shippedByWorkflow(t, store)
	approvalVersion, executeVersion := shipped[prototype.ApprovalWorkflowID], shipped[promotionexec.WorkflowID]
	digest := approvalVersion.CompiledPlanDigest
	suite := ShippedFixtures()
	passing := releasefixture.Run(suite, approvalVersion, "principal:release-engineer", releaseAt)
	request := func(report releasefixture.Report, by string) ReleaseApproval {
		return ReleaseApproval{CompiledPlanDigest: digest, ApprovedBy: by, Authority: "authority:workflow-release-board",
			Reason: "release", Report: report, ApprovedAt: releaseAt}
	}

	if _, err := store.ActivateApproved(ctx, digest, false); !errors.Is(err, workflowversionstore.ErrNoApproval) {
		t.Fatalf("activation before any approval = %v, want ErrNoApproval", err)
	}

	failed := releasefixture.Run(releasefixture.Suite{
		FixtureApprovalCompile:     suite[FixtureApprovalCompile],
		FixtureApprovalRequirement: func(version.CompiledVersion) error { return errors.New("approval step regressed") },
	}, approvalVersion, "principal:release-engineer", releaseAt)
	missing := passing
	missing.Results = missing.Results[:1]
	mismatched := releasefixture.Run(suite, executeVersion, "principal:release-engineer", releaseAt)
	forged := failed
	forged.Results = []releasefixture.Result{{FixtureRef: FixtureApprovalCompile, Passed: true}, {FixtureRef: FixtureApprovalRequirement, Passed: true}}
	tampered := passing
	tampered.Runner = "principal:someone-else"
	for name, tc := range map[string]struct {
		approval ReleaseApproval
		suite    releasefixture.Suite
		want     error
	}{
		"failed fixture":  {request(failed, "principal:release-manager"), suite, releasefixture.ErrFixtureFailed},
		"missing fixture": {request(missing.Seal(), "principal:release-manager"), suite, releasefixture.ErrFixtureMissing},
		"digest mismatch": {request(mismatched, "principal:release-manager"), suite, releasefixture.ErrVersionMismatch},
		"tampered report": {request(tampered, "principal:release-manager"), suite, releasefixture.ErrReportDigest},
		"not reproduced":  {request(forged.Seal(), "principal:release-manager"), releasefixture.Suite{FixtureApprovalCompile: suite[FixtureApprovalCompile]}, releasefixture.ErrNotReproduced},
		"self-approval":   {request(passing, versionPublisher), suite, workflowversionstore.ErrSelfApproval},
		"no approver":     {request(passing, " "), suite, ErrReleaseApproval},
		"unknown version": {ReleaseApproval{CompiledPlanDigest: "sha256:absent", ApprovedBy: "principal:x", Authority: "authority:x", ApprovedAt: releaseAt}, suite, ErrReleaseApproval},
	} {
		if _, err := ApproveRelease(ctx, store, tc.suite, tc.approval); !errors.Is(err, tc.want) {
			t.Errorf("%s: ApproveRelease = %v, want %v", name, err, tc.want)
		}
	}
	if _, err := ApproveRelease(ctx, nil, suite, request(passing, "principal:release-manager")); !errors.Is(err, ErrReleaseApproval) {
		t.Fatalf("ApproveRelease with no registry = %v, want ErrReleaseApproval", err)
	}
	var approvals int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM workflow_version_approval`).Scan(&approvals); err != nil || approvals != 0 {
		t.Fatalf("refused approvals left %d rows (%v), want none", approvals, err)
	}

	recorded, err := ApproveRelease(ctx, store, suite, request(passing, "principal:release-manager"))
	if err != nil {
		t.Fatalf("ApproveRelease(passing): %v", err)
	}
	if again, err := ApproveRelease(ctx, store, suite, request(passing, "principal:release-manager")); err != nil || again.ApprovalID != recorded.ApprovalID {
		t.Fatalf("replayed approval = %v, %v; want the same approval id", again.ApprovalID, err)
	}
	if still, _, _ := store.GetByDigest(digest); still.Status != version.StatusDraft {
		t.Fatalf("approval alone moved the version to %s, want it DRAFT until activation", still.Status)
	}
	active, err := store.ActivateApproved(ctx, digest, false)
	if err != nil || active.Status != version.StatusActive || active.Approvals[0].ApprovedBy != "principal:release-manager" {
		t.Fatalf("ActivateApproved = %+v, %v", active, err)
	}
	var storedDigest string
	if err := db.QueryRow(ctx, `SELECT fixture_report_digest FROM workflow_version_approval WHERE approval_id = $1`, recorded.ApprovalID).Scan(&storedDigest); err != nil || storedDigest != passing.ReportDigest {
		t.Fatalf("stored report digest = %q (%v), want %s", storedDigest, err, passing.ReportDigest)
	}
	if _, found, _ := store.GetActiveForWorkflow(promotionexec.WorkflowID); found {
		t.Fatal("approving one workflow activated another")
	}
}

// TestTodo_WF_COMP_006_BootstrapDev proves the explicit development bootstrap
// approves both shipped versions under the development release approver on
// in-process fixture reports and activates them, records nothing on a second
// run, and never undoes a governed quarantine.
func TestTodo_WF_COMP_006_BootstrapDev(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}
	if _, err := BootstrapDevVersions(ctx, nil, releaseAt); !errors.Is(err, ErrReleaseApproval) {
		t.Fatalf("BootstrapDevVersions(nil) = %v, want ErrReleaseApproval", err)
	}
	out, err := BootstrapDevVersions(ctx, store, releaseAt)
	if err != nil {
		t.Fatalf("BootstrapDevVersions: %v", err)
	}
	// The prototype and promotion execute 1.2.0 end ACTIVE; frozen promotion
	// execute 1.0.0 and 1.1.0 stand QUARANTINED by supersession. The reference
	// new-hire 1.1.0 is ACTIVE beside its frozen 1.0.0 publication.
	if len(out) != 6 {
		t.Fatalf("bootstrap returned %d versions, want prototype, execute 1.0.0/1.1.0/1.2.0, and new-hire 1.0.0/1.1.0", len(out))
	}
	wantStatus := []version.ActivationStatus{version.StatusActive, version.StatusQuarantined, version.StatusQuarantined, version.StatusActive, version.StatusQuarantined, version.StatusActive}
	wantSemver := []string{"1.0.0", promotionexec.SemanticVersionV1_0, promotionexec.SemanticVersionV1_1, promotionexec.SemanticVersion, hireexec.SemanticVersionV1_0, hireexec.SemanticVersion}
	for i, v := range out {
		if v.Status != wantStatus[i] || v.SemanticVersion != wantSemver[i] || v.Approvals[0].ApprovedBy != DevReleaseApprover {
			t.Fatalf("%s %s bootstrapped to %s by %+v, want %s", v.WorkflowID, v.SemanticVersion, v.Status, v.Approvals, wantStatus[i])
		}
	}
	if !out[1].QuarantinedBySupersession() || !out[2].QuarantinedBySupersession() {
		t.Fatalf("execute prior versions = %+v/%+v, want quarantined by supersession", out[1].Approvals, out[2].Approvals)
	}
	if active, found, err := store.GetActiveForWorkflow(promotionexec.WorkflowID); err != nil || !found || active.SemanticVersion != promotionexec.SemanticVersion {
		t.Fatalf("active promotion execute = %s (found %t, %v), want %s", active.SemanticVersion, found, err, promotionexec.SemanticVersion)
	}
	counts := func() (n int) {
		t.Helper()
		if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM workflow_version_approval WHERE fixture_report IS NOT NULL) + (SELECT count(*) FROM workflow_version_transition)`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Six approvals with reports and nine transitions, including supersession
	// between the three promotion versions and the two new-hire versions.
	if n := counts(); n != 15 {
		t.Fatalf("bootstrap wrote %d approvals with reports plus transitions, want 6 + 9", n)
	}
	if active, found, err := store.GetActiveForWorkflow(hireexec.WorkflowID); err != nil || !found || active.SemanticVersion != hireexec.SemanticVersion {
		t.Fatalf("active new hire = %s (found %t, %v), want %s", active.SemanticVersion, found, err, hireexec.SemanticVersion)
	}
	if !out[4].QuarantinedBySupersession() {
		t.Fatalf("new-hire 1.0.0 = %+v, want quarantined by supersession", out[4].Approvals)
	}
	quarantined := out[0]
	if _, err := store.Quarantine(ctx, workflowversionstore.QuarantineDeclaration{
		DeclarationID: uuid.New(), CompiledPlanDigest: quarantined.CompiledPlanDigest, Reason: "incident", EvidenceRef: "incident:1",
		DeclaredBy: "principal:ic", ApprovedBy: "principal:ops", Authority: "authority:incident", LivePolicy: workflowversionstore.LivePause,
		RecordedAt: releaseAt.Add(time.Hour),
	}); err != nil {
		t.Fatalf("Quarantine: %v", err)
	}
	before := counts()
	again, err := BootstrapDevVersions(ctx, store, releaseAt.Add(2*time.Hour))
	if err != nil || counts() != before {
		t.Fatalf("second bootstrap = %v, rows %d -> %d; want nothing written", err, before, counts())
	}
	if again[0].Status != version.StatusQuarantined {
		t.Fatalf("bootstrap returned the quarantined version as %s", again[0].Status)
	}
}

// TestTodo_WF_COMP_006_ServedStartNeedsGovernedActivation composes the real
// driver over the durable registry: a start before any governed activation is
// refused with the typed error naming the release commands and writes no
// instance; composition wrote no approval; after the fixtures -> approve ->
// activate path the same composition starts and parks.
func TestTodo_WF_COMP_006_ServedStartNeedsGovernedActivation(t *testing.T) {
	database := pgtest.New(t)
	tenant := uuid.New()
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	database.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'retry-tenant','cell-local','Release tenant','ACTIVE',$2)`, tenant, at.Add(-time.Hour))
	database.Exec(t, `CREATE TABLE execution_retry_approval (tenant_id uuid PRIMARY KEY, approved boolean NOT NULL)`)
	database.Exec(t, `INSERT INTO execution_retry_approval (tenant_id,approved) VALUES ($1,true)`, tenant)
	ctx := context.Background()
	store := workflowversionstore.Store{DB: database.Conn}
	execution, err := NewPromotionExecution(PromotionExecutionConfig{DB: database.Conn, Terminal: stubTerminal{}, Clock: func() time.Time { return at }, Versions: store, Currency: retryCurrency()})
	if err != nil {
		t.Fatalf("NewPromotionExecution: %v", err)
	}
	rows := func(query string) (n int) {
		t.Helper()
		if err := database.QueryRow(ctx, query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := rows(`SELECT count(*) FROM workflow_version_approval`); n != 0 {
		t.Fatalf("composition recorded %d approvals, want none", n)
	}
	_, err = execution.Executor.Execute(ctx, retryStart(execution, tenant, "before-activation", at))
	if !errors.Is(err, ErrNoActiveWorkflowVersion) || runtime.CodeOf(err) != runtime.CodeVersionNotActive {
		t.Fatalf("start before activation = %v, want ErrNoActiveWorkflowVersion", err)
	}
	if n := rows(`SELECT count(*) FROM workflow_instance`); n != 0 {
		t.Fatalf("a refused start wrote %d instances", n)
	}

	if _, found, err := store.GetActiveForWorkflow(prototype.ApprovalWorkflowID); err != nil || found {
		t.Fatalf("an ACTIVE version exists before governance (%v)", err)
	}
	versions, err := store.List(prototype.ApprovalWorkflowID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("List = %d, %v", len(versions), err)
	}
	draft := versions[0]
	report := releasefixture.Run(ShippedFixtures(), draft, "principal:release-engineer", at)
	if _, err := ApproveRelease(ctx, store, ShippedFixtures(), ReleaseApproval{
		CompiledPlanDigest: draft.CompiledPlanDigest, ApprovedBy: "principal:release-manager", Authority: "authority:workflow-release-board",
		Reason: "release", Report: report, ApprovedAt: at,
	}); err != nil {
		t.Fatalf("ApproveRelease: %v", err)
	}
	if _, err := store.ActivateApproved(ctx, draft.CompiledPlanDigest, false); err != nil {
		t.Fatalf("ActivateApproved: %v", err)
	}
	result, err := execution.Executor.Execute(ctx, retryStart(execution, tenant, "after-activation", at))
	if err != nil || !result.Parked || len(result.ParkedWorkItems) != 1 {
		t.Fatalf("start after governed activation = %+v, %v", result, err)
	}
}

// TestNoActiveVersionNamesTheGovernedCommands proves a start refused because
// the shipped version is not ACTIVE is the typed refusal that names the
// release commands, while keeping the runtime code, and that every other
// error passes through unchanged.
func TestNoActiveVersionNamesTheGovernedCommands(t *testing.T) {
	refusal := &runtime.Error{Code: runtime.CodeVersionNotActive, Detail: "resolved version is DRAFT"}
	err := noActiveVersion(refusal)
	if !errors.Is(err, ErrNoActiveWorkflowVersion) || runtime.CodeOf(err) != runtime.CodeVersionNotActive ||
		!strings.Contains(err.Error(), "hcmnext workflow-version bootstrap-dev") {
		t.Fatalf("noActiveVersion = %v", err)
	}
	other := errors.New("boom")
	if got := noActiveVersion(other); got != other {
		t.Fatalf("an unrelated error was rewrapped: %v", got)
	}
}
