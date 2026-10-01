package clockservice

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type evidenceReaderFake struct {
	row   PunchNodeEvidence
	found bool
	err   error
}

func (f evidenceReaderFake) LoadPunchNodeEvidence(context.Context, uuid.UUID, uuid.UUID, string) (PunchNodeEvidence, bool, error) {
	return f.row, f.found, f.err
}

func TestTodo_WTIME004_PunchRecoveryRequiresExactCompletedEngineEvidence(t *testing.T) {
	telemetry, _, _ := newClockWorkflowTelemetryFixture(t)
	b := TimeClockRunBinding{TenantID: uuid.New(), InstanceID: uuid.New(), TenantKey: "tenant", SessionID: "session", WorkflowID: "published", PlanDigest: "pinned-plan", StartKey: "start", CorrelationID: "start"}
	w := PunchWork{Session: SessionRecord{ID: "session", TenantID: "tenant"}, Observation: ObservationRecord{ID: "observation", TenantID: "tenant", Digest: "original-input"}}
	r := PunchResult{Session: w.Session, Observation: w.Observation}
	e := PunchNodeEvidence{TenantID: b.TenantID, InstanceID: b.InstanceID, NodeID: "commit_punch", ObservationID: "observation", SessionID: "session", PlanDigest: b.PlanDigest, TraceID: "durable-trace", OutputDigest: PunchCommitEvidenceDigest("observation", "session"), CompletedState: "SUCCEEDED", Attempt: 1, InstanceVersion: 2}
	for _, tc := range []struct {
		name    string
		mutate  func(*PunchNodeEvidence)
		missing bool
		err     error
	}{
		{name: "valid"}, {name: "missing", missing: true}, {name: "storage failure", err: errors.New("unavailable")},
		{name: "foreign tenant", mutate: func(e *PunchNodeEvidence) { e.TenantID = uuid.New() }},
		{name: "foreign instance", mutate: func(e *PunchNodeEvidence) { e.InstanceID = uuid.New() }},
		{name: "wrong observation", mutate: func(e *PunchNodeEvidence) { e.ObservationID = "other" }},
		{name: "wrong session", mutate: func(e *PunchNodeEvidence) { e.SessionID = "other" }},
		{name: "wrong plan", mutate: func(e *PunchNodeEvidence) { e.PlanDigest = "other" }},
		{name: "uncompleted", mutate: func(e *PunchNodeEvidence) { e.CompletedState = "RUNNING" }},
		{name: "wrong node", mutate: func(e *PunchNodeEvidence) { e.NodeID = "classify_punch" }},
		{name: "missing trace", mutate: func(e *PunchNodeEvidence) { e.TraceID = "" }},
		{name: "wrong digest", mutate: func(e *PunchNodeEvidence) { e.OutputDigest = "other" }},
		{name: "missing attempt", mutate: func(e *PunchNodeEvidence) { e.Attempt = 0 }},
		{name: "missing version", mutate: func(e *PunchNodeEvidence) { e.InstanceVersion = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := e
			if tc.mutate != nil {
				tc.mutate(&candidate)
			}
			d := WorkflowPunchDriver{Evidence: evidenceReaderFake{row: candidate, found: !tc.missing, err: tc.err}, Telemetry: telemetry}
			got, err := d.provenResult(context.Background(), b, w, r)
			if tc.name == "valid" {
				if err != nil || !got.Committed || got.TraceID != e.TraceID || got.Attempt != 1 {
					t.Fatalf("result=%+v err=%v", got, err)
				}
			} else if err == nil || got.Committed {
				t.Fatalf("unproven effect accepted: %+v", got)
			}
		})
	}
}

type evidenceCommitter struct{ calls int }

func (c *evidenceCommitter) CommitPunch(_ context.Context, _ string, w PunchWork) (PunchResult, error) {
	c.calls++
	return PunchResult{Session: w.Session, Observation: w.Observation}, nil
}

func TestTodo_WTIME004_CommitNodePersistsEffectReferences(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	commit := &evidenceCommitter{}
	work := PunchWork{Session: SessionRecord{TenantID: "tenant", ID: "session"}, Observation: ObservationRecord{TenantID: "tenant", ID: "observation"}}
	runner := PunchCommitStepRunner{Tenant: "tenant", TenantID: tenant, Work: work, Commit: commit, NodeID: "commit_punch"}
	req := execute.StepRequest{TenantID: tenant, InstanceID: instance, Attempt: 1, Node: workflow.CompiledNode{ID: "commit_punch", EffectRole: workflow.RoleAuthoritativeCore}}
	outcome, refs, err := runner.Run(context.Background(), req)
	if err != nil || commit.calls != 1 || outcome.OutputDigest != PunchCommitEvidenceDigest("observation", "session") || len(refs.EffectRefs) != 2 || refs.EffectRefs[0] != "time_observation:observation" || refs.EffectRefs[1] != "time_session:session" || refs.CapabilityExecutionID != runtime.NodeExecutionID(tenant, instance, "commit_punch", 1).String() {
		t.Fatalf("outcome=%+v refs=%+v err=%v", outcome, refs, err)
	}
	req.TenantID = uuid.New()
	if _, _, err := runner.Run(context.Background(), req); err == nil || commit.calls != 1 {
		t.Fatal("foreign runtime tenant wrote time effect")
	}
}
