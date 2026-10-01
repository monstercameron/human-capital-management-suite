package application

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func TestAgentSpecialistInvocationRejectsPersonaRecruitmentAndWidening(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	parent := agentrun.Request{
		Source:      agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourceAPI, Key: "parent", Ref: "parent"},
		LegalEntity: "entity-a", Agent: agentrun.VersionRef{AgentID: "parent", Version: "1", Digest: "sha256:" + string(make([]byte, 64))},
		InstallationID: "install-parent", Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "principal-parent", InvokerID: "user-a", DelegatedCredentialRef: "parent-grant"},
		Purpose: "agent", Audience: agentrun.AudienceScope{ID: "private", SnapshotID: "aud-1", Digest: "aud-digest"}, Context: agentrun.ContextScope{ID: "thread", SnapshotID: "ctx-1", Digest: "ctx-digest"}, Deadline: now.Add(time.Hour), Budget: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 100, MaxOutputTokens: 100},
	}
	child := parent
	child.Agent = agentrun.VersionRef{AgentID: "specialist", Version: "2", Digest: "sha256:" + string(make([]byte, 64))}
	child.InstallationID = "install-specialist"
	child.Principal = agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "principal-specialist", InvokerID: "user-a", DelegatedCredentialRef: "child-grant"}
	if err := validateSpecialistNarrowing(parent, child, now.Add(30*time.Minute)); err != nil {
		t.Fatalf("same context and bounded deadline refused: %v", err)
	}
	child.Persona = &agentrun.PersonaRef{ID: "persona", Version: "1", Digest: "sha256:persona"}
	if err := validateSpecialistShape(SpecialistInvocation{ParentAdmissionID: "admission", ParentTaskID: "task", ParentCredential: "credential", TaskID: "child", Target: child, Goal: "goal", Steps: []agentrun.PlanStep{{ID: "step", Type: agentrun.StepRead, SkillID: "skill", SkillVersion: 1, ExpectedOutput: "ref", Tier: agentrun.TierRead}}, Deadline: now.Add(time.Minute)}); !errors.Is(err, ErrSpecialistDenied) {
		t.Fatalf("persona recruitment error = %v", err)
	}
	child.Persona = nil
	child.Context.Digest = "widened"
	if err := validateSpecialistNarrowing(parent, child, now.Add(time.Minute)); !errors.Is(err, ErrSpecialistDenied) {
		t.Fatalf("context widening error = %v", err)
	}
}

func TestTodo_AGENT_050_SpecialistPurposeSecurity(t *testing.T) {
	deadline := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	parent := agentrun.Request{Purpose: "parent-purpose", Deadline: deadline, Principal: agentrun.PrincipalChain{InvokerID: "user", DelegatedCredentialRef: "parent-grant"}}
	child := parent
	child.Purpose = "other-purpose"
	child.Principal.DelegatedCredentialRef = "child-grant"
	if err := validateSpecialistNarrowing(parent, child, deadline); !errors.Is(err, ErrSpecialistDenied) {
		t.Fatalf("purpose expansion = %v", err)
	}
}
