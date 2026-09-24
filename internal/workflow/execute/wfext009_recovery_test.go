package execute_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type planHistoryStore struct {
	activeDigest string
	versions     map[string]version.CompiledVersion
}

func (s *planHistoryStore) Put(v version.CompiledVersion) error {
	s.versions[v.CompiledPlanDigest] = clonePlanVersion(v)
	if v.Status == version.StatusActive {
		s.activeDigest = v.CompiledPlanDigest
	}
	return nil
}

func (s *planHistoryStore) GetByDigest(digest string) (version.CompiledVersion, bool, error) {
	v, ok := s.versions[digest]
	return clonePlanVersion(v), ok, nil
}

func (s *planHistoryStore) GetActiveForWorkflow(workflowID string) (version.CompiledVersion, bool, error) {
	v, ok := s.versions[s.activeDigest]
	if !ok || v.WorkflowID != workflowID || v.Status != version.StatusActive {
		return version.CompiledVersion{}, false, nil
	}
	return clonePlanVersion(v), true, nil
}

func (s *planHistoryStore) List(workflowID string) ([]version.CompiledVersion, error) {
	out := make([]version.CompiledVersion, 0, len(s.versions))
	for _, v := range s.versions {
		if v.WorkflowID == workflowID {
			out = append(out, clonePlanVersion(v))
		}
	}
	return out, nil
}

func clonePlanVersion(v version.CompiledVersion) version.CompiledVersion {
	v.CanonicalPlanBytes = append([]byte(nil), v.CanonicalPlanBytes...)
	v.Approvals = append([]version.ApprovalRecord(nil), v.Approvals...)
	return v
}

func recordPlanVersion(t *testing.T, plan *workflow.CompiledWorkflow, semantic string, status version.ActivationStatus) version.CompiledVersion {
	t.Helper()
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return version.CompiledVersion{
		WorkflowID: plan.WorkflowID, DefinitionVersion: plan.Version,
		SemanticVersion: semantic, CompiledPlanDigest: plan.Digest(),
		CompilerVersion: plan.CompilerVersion, CanonicalPlanBytes: append(raw, '\n'), Status: status,
	}
}

type planHistoryResolver struct {
	store        *planHistoryStore
	activeDigest string
}

func (r planHistoryResolver) ResolveWorkflow(_ context.Context, req runtime.StartRequest) (runtime.WorkflowSelection, error) {
	digest := r.activeDigest
	if req.PinnedCompiledPlanDigest != "" {
		digest = req.PinnedCompiledPlanDigest
	}
	v, found, err := r.store.GetByDigest(digest)
	if err != nil {
		return runtime.WorkflowSelection{}, err
	}
	if !found {
		return runtime.WorkflowSelection{}, errors.New("workflow version is unavailable")
	}
	plan, err := workflow.DecodeCanonicalPlan(v.CanonicalPlanBytes)
	if err != nil {
		return runtime.WorkflowSelection{}, err
	}
	return runtime.WorkflowSelection{
		WorkflowID: v.WorkflowID,
		Pin:        version.Pin{CompiledPlanDigest: v.CompiledPlanDigest},
		Plan:       plan,
	}, nil
}

// TestTodo_WF_EXT_009_Recovery recovers a database-persisted v1 instance after
// a v2 publication becomes active. The recovery process rehydrates the exact
// v1 bytes and executes the durable READY node on the old schema.
func TestTodo_WF_EXT_009_Recovery(t *testing.T) {
	h := newRedeliveryHarness(t, "wfext009-prior-schema-recovery")
	interrupted := h.die(t, boundaryBeforeEffect)
	h.setNow(h.f.at.Add(driverTTL + time.Second))
	oldPlan := h.f.plan
	if oldPlan.SchemaVersion() != 1 {
		t.Fatalf("interrupted plan schema = %d, want v1", oldPlan.SchemaVersion())
	}

	upgraded, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("compile current plan after upgrade: %v", err)
	}
	if upgraded.SchemaVersion() != workflow.CurrentIRSchemaVersion {
		t.Fatalf("current plan schema = %d, want %d", upgraded.SchemaVersion(), workflow.CurrentIRSchemaVersion)
	}

	oldVersion := recordPlanVersion(t, oldPlan, "1.0.0", version.StatusQuarantined)
	oldVersion.Approvals = []version.ApprovalRecord{{
		ApprovedBy: "release-manager", Authority: "authority:release-management",
		Reason:     version.SupersededReasonPrefix + upgraded.Digest(),
		ApprovedAt: h.f.at, Result: version.StatusQuarantined,
	}}
	newVersion := recordPlanVersion(t, upgraded, "1.1.0", version.StatusActive)
	history := &planHistoryStore{activeDigest: newVersion.CompiledPlanDigest, versions: map[string]version.CompiledVersion{
		oldVersion.CompiledPlanDigest: oldVersion,
		newVersion.CompiledPlanDigest: newVersion,
	}}
	// Simulate a fresh recovery process: it resolves canonical plan bytes from
	// the version records instead of retaining either in-process plan pointer.
	h.f.start.Versions = history
	h.f.start.Resolver = planHistoryResolver{store: history, activeDigest: newVersion.CompiledPlanDigest}
	h.f.start.PinnedCompiledPlanDigest = oldVersion.CompiledPlanDigest

	result, err := h.driver(t, h.f.conn, sweepHolderA, drainBoundary("")).RedeliverReady(context.Background(), execute.RedeliverRequest{
		Start: h.f.start, InstanceID: interrupted.InstanceID, ExpectedInstanceVersion: interrupted.InstanceVersion,
	})
	if err != nil {
		t.Fatalf("recover v1 instance after v2 activation: %v", err)
	}
	if result.Status != execute.StatusParked && result.Status != execute.StatusComplete {
		t.Fatalf("recovery status = %s, want PARKED or COMPLETE", result.Status)
	}
	if got := h.performs.Load(); got != 1 {
		t.Fatalf("recovered v1 core effect count = %d, want exactly one", got)
	}
	final := instanceByTenant(t, h.f)
	if final.CompiledPlanHash != oldVersion.CompiledPlanDigest {
		t.Fatalf("recovered instance plan digest = %s, want its persisted v1 pin %s", final.CompiledPlanHash, oldVersion.CompiledPlanDigest)
	}
}
