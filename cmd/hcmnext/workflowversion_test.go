package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var versionCommandNow = func() time.Time { return time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC) }

type failingRegistry struct {
	*version.Registry
	err error
}

func (f failingRegistry) List(string) ([]version.CompiledVersion, error) { return nil, f.err }
func (f failingRegistry) GetByDigest(string) (version.CompiledVersion, bool, error) {
	return version.CompiledVersion{}, false, f.err
}
func (f failingRegistry) RecordApproval(context.Context, workflowversionstore.Approval) error {
	return f.err
}
func (f failingRegistry) ActivateApproved(context.Context, string, bool) (version.CompiledVersion, error) {
	return version.CompiledVersion{}, f.err
}
func (f failingRegistry) Quarantine(context.Context, workflowversionstore.QuarantineDeclaration) (version.CompiledVersion, error) {
	return version.CompiledVersion{}, f.err
}
func (f failingRegistry) LiftQuarantine(context.Context, workflowversionstore.QuarantineLift) (version.CompiledVersion, error) {
	return version.CompiledVersion{}, f.err
}

func openFixed(r versionRegistry) openVersionRegistry {
	return func(context.Context, string) (versionRegistry, func(), error) { return r, func() {}, nil }
}

func TestWorkflowVersionCommandRefusesBadInvocations(t *testing.T) {
	registry := openFixed(failingRegistry{Registry: version.NewRegistry(), err: workflowversionstore.ErrInvalid})
	boom := openFixed(failingRegistry{Registry: version.NewRegistry(), err: errors.New("boom")})
	missingDigest := openFixed(failingRegistry{Registry: version.NewRegistry()})
	for name, tc := range map[string]struct {
		args []string
		open openVersionRegistry
		want int
	}{
		"no action":      {nil, registry, 2},
		"no database":    {[]string{"list", "-database-url", " "}, registry, 2},
		"unknown action": {[]string{"purge", "-database-url", "postgres://x"}, registry, 2},
		"list needs id":  {[]string{"list", "-database-url", "postgres://x"}, registry, 2},
		"bad flag":       {[]string{"list", "-nope"}, registry, 2},
		"invalid input":  {[]string{"quarantine", "-database-url", "postgres://x"}, registry, 2},
		"open fails": {[]string{"list", "-database-url", "postgres://x", "-workflow", "wf"}, func(context.Context, string) (versionRegistry, func(), error) {
			return nil, nil, errors.New("unreachable")
		}, 1},
		"store fails":          {[]string{"lift", "-database-url", "postgres://x"}, boom, 1},
		"list fails":           {[]string{"list", "-database-url", "postgres://x", "-workflow", "wf"}, boom, 1},
		"activate fails":       {[]string{"activate", "-database-url", "postgres://x", "-digest", "sha256:x"}, boom, 1},
		"fixtures fails":       {[]string{"fixtures", "-database-url", "postgres://x", "-digest", "sha256:x"}, boom, 1},
		"fixtures unknown":     {[]string{"fixtures", "-database-url", "postgres://x", "-digest", "sha256:x"}, missingDigest, 1},
		"approve needs report": {[]string{"approve", "-database-url", "postgres://x", "-digest", "sha256:x"}, boom, 2},
		"approve bad report":   {[]string{"approve", "-database-url", "postgres://x", "-fixture-report", filepath.Join(t.TempDir(), "absent.json")}, boom, 1},
		"bootstrap fails":      {[]string{"bootstrap-dev", "-database-url", "postgres://x"}, boom, 1},
	} {
		var stdout, stderr bytes.Buffer
		if got := runWorkflowVersion(tc.args, &stdout, &stderr, versionCommandNow, tc.open); got != tc.want {
			t.Errorf("%s: exit %d, want %d (stderr %q)", name, got, tc.want, stderr.String())
		}
	}
}

// TestTodo_WF_COMP_006_OperatorRelease drives the governed release commands
// end to end against PostgreSQL: a version composition published is DRAFT;
// fixtures writes a sealed passing report; approve refuses the publisher, a
// report edited to hide a failure and another version's report, then records
// the approval without activating; activate is the separate step that puts the
// version into service.
func TestTodo_WF_COMP_006_OperatorRelease(t *testing.T) {
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}
	published, err := platformexecution.PublishShippedVersions(store, versionCommandNow().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	open := openFixed(store)
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := runWorkflowVersion(append(args, "-database-url", "postgres://test"), &stdout, &stderr, versionCommandNow, open)
		return code, stdout.String(), stderr.String()
	}
	var approval, execute version.CompiledVersion
	for _, v := range published {
		if v.WorkflowID == prototype.ApprovalWorkflowID {
			approval = v
		} else if v.WorkflowID == promotionexec.WorkflowID {
			execute = v
		}
	}
	digest := approval.CompiledPlanDigest
	dir := t.TempDir()
	reportPath, otherPath := filepath.Join(dir, "report.json"), filepath.Join(dir, "other.json")

	if code, out, stderr := run("fixtures", "-digest", digest, "-out", reportPath, "-runner", "principal:release-engineer"); code != 0 || out != "" {
		t.Fatalf("fixtures: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	if code, out, _ := run("fixtures", "-digest", execute.CompiledPlanDigest); code != 0 || !strings.Contains(out, releasefixture.Format) {
		t.Fatalf("fixtures to stdout: exit %d, %q", code, out)
	} else if err := os.WriteFile(otherPath, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := releasefixture.Decode(stored)
	if err != nil || !report.Passed() || report.CompiledPlanDigest != digest {
		t.Fatalf("written report = %+v, %v", report, err)
	}
	failedPath := filepath.Join(dir, "failed.json")
	failed := report
	failed.Results = append([]releasefixture.Result(nil), report.Results...)
	failed.Results[0].Passed, failed.Results[0].Detail = false, "recompiled digest drifted"
	encoded, err := failed.Seal().Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failedPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	approve := func(by, path string) (int, string, string) {
		return run("approve", "-digest", digest, "-approved-by", by, "-authority", "authority:workflow-release-board", "-reason", "release", "-fixture-report", path)
	}
	for name, tc := range map[string]struct {
		by, path, want string
	}{
		"self-approval":   {"cmd/hcmnext:execution-authority", reportPath, "publisher cannot approve"},
		"failed fixture":  {"principal:release-manager", failedPath, "declared fixture failed"},
		"another version": {"principal:release-manager", otherPath, "not bound to this version"},
	} {
		if code, _, stderr := approve(tc.by, tc.path); code == 0 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%s: approve exit %d, stderr %q; want a refusal naming %q", name, code, stderr, tc.want)
		}
	}
	if code, out, stderr := run("activate", "-digest", digest); code == 0 || out != "" {
		t.Fatalf("activate before approval: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	if code, out, stderr := approve("principal:release-manager", reportPath); code != 0 || !strings.Contains(out, "DRAFT\t"+digest) {
		t.Fatalf("approve: exit %d, stdout %q, stderr %q; want the version still DRAFT", code, out, stderr)
	}
	if code, out, stderr := run("activate", "-digest", digest); code != 0 || !strings.Contains(out, "ACTIVE\t"+digest) {
		t.Fatalf("activate: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	if code, out, _ := run("list", "-workflow", prototype.ApprovalWorkflowID); code != 0 || !strings.Contains(out, "ACTIVE\t"+digest) {
		t.Fatalf("list = %d %q", code, out)
	}

	if code, out, stderr := run("bootstrap-dev"); code != 0 || strings.Count(out, "ACTIVE\t") != 2 {
		t.Fatalf("bootstrap-dev: exit %d, stdout %q, stderr %q; want both shipped versions ACTIVE", code, out, stderr)
	}
	if active, found, err := store.GetActiveForWorkflow(promotionexec.WorkflowID); err != nil || !found || active.Approvals[0].ApprovedBy != platformexecution.DevReleaseApprover {
		t.Fatalf("bootstrapped execute version = %+v (%v, %v)", active, found, err)
	}
}

// TestWorkflowVersionCommandQuarantinesAndLiftsAgainstPostgreSQL drives the
// operator command end to end: quarantine with a distinct approver and a PAUSE
// disposition takes a governed-active version out of service, and a lift by a
// different reviewer returns it.
func TestWorkflowVersionCommandQuarantinesAndLiftsAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}
	active, err := platformexecution.BootstrapDevVersions(ctx, store, versionCommandNow().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	published := active[0]
	open := openFixed(store)
	run := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := runWorkflowVersion(append(args, "-database-url", "postgres://test"), &stdout, &stderr, versionCommandNow, open); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, stderr.String())
		}
		return stdout.String()
	}

	if out := run("quarantine", "-digest", published.CompiledPlanDigest, "-reason", "bad routing", "-evidence", "incident:INC-1",
		"-declared-by", "principal:incident-commander", "-approved-by", "principal:ops-lead", "-authority", "authority:incident", "-policy", "pause"); !strings.Contains(out, "QUARANTINED") {
		t.Fatalf("quarantine = %q", out)
	}
	if policy, quarantined, err := store.LiveInstancePolicy(ctx, db.Conn, published.CompiledPlanDigest); err != nil || !quarantined || policy != "PAUSE" {
		t.Fatalf("policy after quarantine = %q, %v, %v", policy, quarantined, err)
	}
	later := func() time.Time { return versionCommandNow().Add(time.Hour) }
	var stdout, stderr bytes.Buffer
	if code := runWorkflowVersion([]string{"lift", "-database-url", "postgres://test", "-digest", published.CompiledPlanDigest,
		"-reviewed-by", "principal:incident-reviewer", "-validation-evidence", "validation:INC-1", "-reason", "fixed",
		"-authority", "authority:incident", "-tests-passed"}, &stdout, &stderr, later, open); code != 0 || !strings.Contains(stdout.String(), "ACTIVE") {
		t.Fatalf("lift: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}
