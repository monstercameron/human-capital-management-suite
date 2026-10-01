package workflowbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

func TestTodo_AGENT_033(t *testing.T) {
	record, run := completionFixture(t)
	for _, tc := range []struct {
		state runstate.State
		want  Outcome
	}{
		{runstate.StateCompleted, Succeeded}, {runstate.StateFailed, Failed}, {runstate.StateNeedsRepair, Failed},
		{runstate.StateCancelled, Cancelled}, {runstate.StateExpired, Expired},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			run.State = tc.state
			got, err := Derive(record, run)
			if err != nil || got.Outcome != tc.want || got.RunID != record.ID || got.Agent != record.Authority.Agent {
				t.Fatalf("Derive=%+v err=%v", got, err)
			}
			if tc.want == Succeeded && (got.OutputRef != "artifact:typed" || got.OutputDigest != completionDigest("output")) {
				t.Fatalf("typed output=%+v", got)
			}
			if tc.want != Succeeded && got.OutputRef != "" {
				t.Fatalf("non-success disclosed output=%+v", got)
			}
		})
	}
	record.Decision, record.RefusalCode, record.Authority = agentrun.DecisionRefused, "SOURCE_DENIED", agentrun.AuthoritySnapshot{}
	got, err := Derive(record, runstate.Run{})
	if err != nil || got.Outcome != Refused || got.Code != "SOURCE_DENIED" {
		t.Fatalf("refusal=%+v err=%v", got, err)
	}
}

func TestTodo_AGENT_033_Fault(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*agentrun.Record, *runstate.Run)
	}{
		{"wrong tenant", func(_ *agentrun.Record, r *runstate.Run) { r.TenantID = "other" }},
		{"wrong version", func(_ *agentrun.Record, r *runstate.Run) { r.AgentVersion = "v2" }},
		{"wrong admission", func(_ *agentrun.Record, r *runstate.Run) { r.AdmissionID = "invented" }},
		{"missing validation", func(_ *agentrun.Record, r *runstate.Run) { r.Checkpoints = r.Checkpoints[1:] }},
		{"changed output", func(_ *agentrun.Record, r *runstate.Run) { r.Checkpoints[1].Digest = completionDigest("forged") }},
		{"unknown effect", func(_ *agentrun.Record, r *runstate.Run) {
			r.Effects = []runstate.Effect{{Status: runstate.EffectUnknown}}
		}},
		{"live lease", func(_ *agentrun.Record, r *runstate.Run) { r.Lease = &runstate.Lease{Owner: "worker"} }},
		{"invalid hex", func(_ *agentrun.Record, r *runstate.Run) {
			for i := range r.Checkpoints {
				r.Checkpoints[i].Digest = "sha256:" + string(make([]byte, 64))
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record, run := completionFixture(t)
			tc.mutate(&record, &run)
			if _, err := Derive(record, run); !errors.Is(err, ErrEvidence) {
				t.Fatalf("Derive err=%v", err)
			}
		})
	}
	record, run := completionFixture(t)
	run.State = runstate.StateRunning
	if _, err := Derive(record, run); !errors.Is(err, ErrPending) {
		t.Fatalf("running completion err=%v", err)
	}
}

func completionFixture(t *testing.T) (agentrun.Record, runstate.Run) {
	t.Helper()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	request := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "tenant", Kind: agentrun.SourceWorkflow, Key: "occurrence", Ref: "workflow:instance:node"},
		LegalEntity: "legal", Agent: agentrun.VersionRef{AgentID: "agent", Version: "v1", Digest: completionDigest("agent")}, InstallationID: "installation",
		Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal", InvokerID: "user", DelegatedCredentialRef: "credential"},
		Purpose:   "workflow-analysis", Audience: agentrun.AudienceScope{ID: "audience", SnapshotID: "audience-1", Digest: completionDigest("audience")},
		Context: agentrun.ContextScope{ID: "scope", SnapshotID: "snapshot", Digest: completionDigest("context")}, Deadline: now.Add(time.Minute),
		Budget: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 100, MaxOutputTokens: 100}, CauseID: "cause"}
	id, err := agentrun.AdmissionRequestID(request.Source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentrun.AdmissionRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	record := agentrun.Record{ID: id, Request: request, RequestDigest: digest, Decision: agentrun.DecisionAccepted, AdmittedAt: now,
		Authority: agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal, Audience: request.Audience, Context: request.Context, BudgetCeiling: request.Budget, GrantRef: "grant", PolicyDigest: completionDigest("policy")}}
	if err := agentrun.ValidateAdmissionRecord(record); err != nil {
		t.Fatal(err)
	}
	run := runstate.Run{ID: id, AdmissionID: id, TenantID: "tenant", RequestDigest: digest, AgentID: request.Agent.AgentID, AgentVersion: request.Agent.Version,
		AgentDigest: request.Agent.Digest, ContextDigest: request.Context.Digest, Deadline: request.Deadline, State: runstate.StateCompleted, Version: 1,
		Checkpoints: []runstate.Checkpoint{{Sequence: 1, Phase: runstate.PhaseValidation, Ref: "artifact:typed", Digest: completionDigest("output")}, {Sequence: 2, Phase: runstate.PhaseDelivery, Ref: "artifact:typed", Digest: completionDigest("output")}}}
	return record, run
}

func completionDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}
