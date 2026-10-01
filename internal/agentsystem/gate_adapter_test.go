package agentsystem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type recordingCallAuthorizer struct {
	requests []agentgate.CallRequest
	err      error
}

func (a *recordingCallAuthorizer) Authorize(_ context.Context, request agentgate.CallRequest) (agentgate.CallDecision, error) {
	a.requests = append(a.requests, request)
	return agentgate.CallDecision{GrantID: "current-grant"}, a.err
}

type mutableCallScopeResolver struct {
	scope   ResolvedCallScope
	err     error
	calls   int
	request CallScopeRequest
}

func (r *mutableCallScopeResolver) ResolveCallScope(_ context.Context, request CallScopeRequest) (ResolvedCallScope, error) {
	r.calls++
	r.request = request
	return r.scope, r.err
}

func newGateAdapterFixture(t *testing.T) (*GateAdapter, *recordingCallAuthorizer, *mutableCallScopeResolver, agentrun.AgentTask, agentrun.PlanStep, agentskills.SkillRecord, agentdelegation.Claims) {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	tenant := values.TenantId("tenant-a")
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: "user-1", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-a", Roles: []string{"employee"}, Purposes: []string{"workforce:read"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-1", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "cred-1",
	})
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	ref := values.EntityRef{Tenant: tenant, Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000001"}
	scope := ResolvedCallScope{
		User:     agentgate.UserContext{Principal: principal, Population: "employees", Roles: []string{"employee"}, OrganizationScopes: []string{"org-a"}},
		Subjects: []agentgate.Subject{{Ref: ref, Organization: authz.OrgUnitRef{Tenant: tenant, ID: "org-a"}}},
		Fields:   []authz.FieldID{authz.FieldWorkerNumber},
	}
	task := agentrun.AgentTask{ID: "task-1", UserID: "user-1", TenantID: "tenant-a"}
	step := agentrun.PlanStep{ID: "step-1", SkillID: "hcmnext.skill.worker_state", SkillVersion: 2}
	skill := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: step.SkillID, Version: step.SkillVersion}, Digest: "sha256:skill-v2"}
	claims := agentdelegation.Claims{
		Subject: task.UserID, Tenant: task.TenantID, GrantID: GrantID(task.ID), Purpose: "workforce:read", Skill: step.SkillID,
		Actor: agentdelegation.ActorClaim{AgentVersion: "agent-v3", InstallationID: "install-4", RunID: task.ID, StepID: step.ID},
	}
	authorizer := &recordingCallAuthorizer{}
	resolver := &mutableCallScopeResolver{scope: scope}
	adapter, err := NewGateAdapter(authorizer, resolver)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	return adapter, authorizer, resolver, task, step, skill, claims
}

func TestTodo_AGENT2_005_AdapterBuildsBoundFreshRequest(t *testing.T) {
	adapter, authorizer, resolver, task, step, skill, claims := newGateAdapterFixture(t)
	prepared := Prepared{Purpose: "workforce:read"}
	first, err := adapter.Authorize(context.Background(), task, step, skill, claims, prepared)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if first.GrantID != "current-grant" || resolver.calls != 1 || len(authorizer.requests) != 1 {
		t.Fatalf("decision=%+v resolver calls=%d authorizer calls=%d", first, resolver.calls, len(authorizer.requests))
	}
	if resolver.request.Prepared.Purpose != prepared.Purpose {
		t.Fatalf("resolver did not receive the prepared owner declaration: %+v", resolver.request.Prepared)
	}
	request := authorizer.requests[0]
	if request.User.Principal.Subject() != task.UserID || request.User.Roles[0] != "employee" || request.Actor.RunID != task.ID || request.Actor.StepID != step.ID ||
		request.Skill.ID != skill.Definition.ID || request.Skill.Version != skill.Definition.Version || request.Skill.Digest != skill.Digest || request.Purpose != claims.Purpose ||
		len(request.Subjects) != 1 || request.Subjects[0].Ref.Id != "00000000-0000-4000-8000-000000000001" || len(request.Fields) != 1 || request.Fields[0] != authz.FieldWorkerNumber {
		t.Fatalf("adapter built an incorrectly bound gate request: %+v", request)
	}
	resolver.scope.User.Roles = []string{"former-employee"}
	if _, err := adapter.Authorize(context.Background(), task, step, skill, claims, prepared); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if resolver.calls != 2 || len(authorizer.requests) != 2 || authorizer.requests[1].User.Roles[0] != "former-employee" {
		t.Fatalf("call did not resolve current context again: calls=%d requests=%+v", resolver.calls, authorizer.requests)
	}
}

func TestTodo_AGENT2_005_AdapterFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		edit func(*agentrun.AgentTask, *agentrun.PlanStep, *agentskills.SkillRecord, *agentdelegation.Claims, *mutableCallScopeResolver)
	}{
		{name: "delegation purpose mismatch", edit: func(_ *agentrun.AgentTask, _ *agentrun.PlanStep, _ *agentskills.SkillRecord, claims *agentdelegation.Claims, _ *mutableCallScopeResolver) {
			claims.Purpose = "payroll:write"
		}},
		{name: "missing roles", edit: func(_ *agentrun.AgentTask, _ *agentrun.PlanStep, _ *agentskills.SkillRecord, _ *agentdelegation.Claims, resolver *mutableCallScopeResolver) {
			resolver.scope.User.Roles = nil
		}},
		{name: "missing subjects", edit: func(_ *agentrun.AgentTask, _ *agentrun.PlanStep, _ *agentskills.SkillRecord, _ *agentdelegation.Claims, resolver *mutableCallScopeResolver) {
			resolver.scope.Subjects = nil
		}},
		{name: "missing fields", edit: func(_ *agentrun.AgentTask, _ *agentrun.PlanStep, _ *agentskills.SkillRecord, _ *agentdelegation.Claims, resolver *mutableCallScopeResolver) {
			resolver.scope.Fields = nil
		}},
		{name: "cross tenant subject", edit: func(_ *agentrun.AgentTask, _ *agentrun.PlanStep, _ *agentskills.SkillRecord, _ *agentdelegation.Claims, resolver *mutableCallScopeResolver) {
			resolver.scope.Subjects[0].Ref.Tenant = values.TenantId("tenant-b")
		}},
		{name: "resolver outage", edit: func(_ *agentrun.AgentTask, _ *agentrun.PlanStep, _ *agentskills.SkillRecord, _ *agentdelegation.Claims, resolver *mutableCallScopeResolver) {
			resolver.err = errors.New("directory unavailable")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, authorizer, resolver, task, step, skill, claims := newGateAdapterFixture(t)
			tc.edit(&task, &step, &skill, &claims, resolver)
			if _, err := adapter.Authorize(context.Background(), task, step, skill, claims, Prepared{Purpose: "workforce:read"}); !errors.Is(err, agentgate.ErrDenied) {
				t.Fatalf("authorize error = %v, want fail-closed gate denial", err)
			}
			if len(authorizer.requests) != 0 {
				t.Fatalf("invalid call reached policy authorizer: %+v", authorizer.requests)
			}
		})
	}
}

func TestTodo_AGENT2_005_AdapterRequiresPorts(t *testing.T) {
	if _, err := NewGateAdapter(nil, &mutableCallScopeResolver{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil authorizer error = %v", err)
	}
	if _, err := NewGateAdapter(&recordingCallAuthorizer{}, nil); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil resolver error = %v", err)
	}
}
