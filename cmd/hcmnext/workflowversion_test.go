package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var versionCommandNow = func() time.Time { return time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC) }

type failingRegistry struct{ err error }

func (f failingRegistry) List(string) ([]version.CompiledVersion, error) { return nil, f.err }
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
	registry := openFixed(failingRegistry{err: workflowversionstore.ErrInvalid})
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
		"store fails": {[]string{"lift", "-database-url", "postgres://x"}, openFixed(failingRegistry{err: errors.New("boom")}), 1},
		"list fails":  {[]string{"list", "-database-url", "postgres://x", "-workflow", "wf"}, openFixed(failingRegistry{err: errors.New("boom")}), 1},
	} {
		var stdout, stderr bytes.Buffer
		if got := runWorkflowVersion(tc.args, &stdout, &stderr, versionCommandNow, tc.open); got != tc.want {
			t.Errorf("%s: exit %d, want %d (stderr %q)", name, got, tc.want, stderr.String())
		}
	}
}

// TestWorkflowVersionCommandQuarantinesAndLiftsAgainstPostgreSQL drives the
// operator command end to end: list shows the ACTIVE version, quarantine with
// a distinct approver and a PAUSE disposition takes it out of service, and a
// lift by a different reviewer returns it.
func TestWorkflowVersionCommandQuarantinesAndLiftsAgainstPostgreSQL(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatal(err)
	}
	published, err := version.Publish(store, prototype.ApprovalDefinition(), plan, workflow.Options{Phase: workflow.PhaseP1B},
		version.PublishMeta{SemanticVersion: "1.0.0", PublishedAt: versionCommandNow().Add(-time.Hour), PublishedBy: "cmd/hcmnext:execution-authority"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordApproval(ctx, workflowversionstore.Approval{
		ApprovalID: [16]byte{1}, CompiledPlanDigest: published.CompiledPlanDigest, ReviewedPlanDigest: published.CompiledPlanDigest,
		ApprovedBy: "principal:release-manager", Authority: "authority:release", TestsPassed: true, ApprovedAt: versionCommandNow().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActivateApproved(ctx, published.CompiledPlanDigest, false); err != nil {
		t.Fatal(err)
	}
	open := openFixed(store)
	run := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := runWorkflowVersion(append(args, "-database-url", "postgres://test"), &stdout, &stderr, versionCommandNow, open); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, stderr.String())
		}
		return stdout.String()
	}

	if out := run("list", "-workflow", plan.WorkflowID); !strings.Contains(out, "ACTIVE\t"+published.CompiledPlanDigest) {
		t.Fatalf("list = %q", out)
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
