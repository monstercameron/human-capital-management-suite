package toolbridge

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type fakeAdmission struct {
	pins         []agentskills.SkillPin
	badRun       bool
	nonce        string
	purpose      string
	missingNonce bool
	baseGate     agentgate.CallRequest
}

func (a *fakeAdmission) Resolve(_ context.Context, ref RunReference, callID string) (AdmittedRun, error) {
	runID := ref.RunID
	if a.badRun {
		runID = "another-run"
	}
	nonce := a.nonce
	if nonce == "" && !a.missingNonce {
		nonce = "nonce:" + callID
	}
	purpose := a.purpose
	if purpose == "" {
		purpose = testPurpose
	}
	gate := a.baseGate
	gate.Purpose = purpose
	gate.Actor = agentgate.AgentActor{AgentVersion: "agent/v1", InstallationID: "install-1", RunID: runID, StepID: callID}
	return AdmittedRun{RunID: runID, SourceKey: ref.SourceKey, Nonce: nonce, Pins: append([]agentskills.SkillPin(nil), a.pins...), RemainingBudgetMicros: 5000, Gate: gate}, nil
}

type mutableSkillGrants struct{ rows []agentgate.SkillGrant }

func (g *mutableSkillGrants) Grants(_ context.Context, tenant values.TenantId, key agentskills.SkillKey) ([]agentgate.SkillGrant, error) {
	var out []agentgate.SkillGrant
	for _, grant := range g.rows {
		if grant.Tenant == tenant && grant.Skill == key {
			out = append(out, grant)
		}
	}
	return out, nil
}

type allowRequestedFields struct{}

func (allowRequestedFields) Authorize(_ context.Context, req agentgate.CapabilityRequest) (agentgate.CapabilityDecision, error) {
	decision := agentgate.CapabilityDecision{Capability: req.Capability.Definition.Key(), Allowed: true}
	for _, subject := range req.Subjects {
		fields := make(map[authz.FieldID]authz.Effect, len(req.Fields))
		for _, field := range req.Fields {
			fields[field] = authz.EffectAllow
		}
		decision.Subjects = append(decision.Subjects, agentgate.SubjectDecision{Subject: subject.Ref, Fields: fields})
	}
	return decision, nil
}

type fakeGate struct {
	requests []agentgate.CallRequest
	err      error
	skills   SkillCatalog
}

func (g *fakeGate) Authorize(_ context.Context, req agentgate.CallRequest) (agentgate.CallDecision, error) {
	g.requests = append(g.requests, req)
	decision := agentgate.CallDecision{Skill: req.Skill.Key(), GrantID: "grant-1", Purpose: req.Purpose}
	if skill, err := g.skills.ResolvePin(req.Skill); err == nil {
		for _, operation := range skill.ResolvedOperations {
			decision.Capabilities = append(decision.Capabilities, agentgate.CapabilityDecision{Capability: operation.Capability.Definition.Key(), Allowed: true})
		}
	}
	return decision, g.err
}

type fakeCost struct {
	skills []agentskills.SkillKey
	err    error
}

func (c *fakeCost) Check(_ context.Context, skill agentskills.SkillRecord, budget int64) error {
	c.skills = append(c.skills, skill.Definition.Key())
	if budget <= 0 && c.err == nil {
		return ErrCost
	}
	return c.err
}

type fakeReplay struct {
	keys []ReplayKey
	err  error
	used map[replayIdentity]string
}

func (r *fakeReplay) Claim(_ context.Context, key ReplayKey) error {
	r.keys = append(r.keys, key)
	if r.err != nil {
		return r.err
	}
	if r.used == nil {
		r.used = make(map[replayIdentity]string)
	}
	identity := replayIdentity{RunID: key.RunID, SourceKey: key.SourceKey, CallID: key.CallID, Nonce: key.Nonce, ToolName: key.ToolName}
	if prior, ok := r.used[identity]; ok {
		if prior != key.ArgumentsSHA {
			return errors.New("same idempotency key was rebound")
		}
		return ErrReplay
	}
	r.used[identity] = key.ArgumentsSHA
	return nil
}

type replayIdentity struct {
	RunID     string
	SourceKey string
	CallID    string
	Nonce     string
	ToolName  string
}

type fakeOwner struct {
	calls    int
	call     CapabilityCall
	response any
	err      error
}

func (o *fakeOwner) Invoke(_ context.Context, call CapabilityCall) (any, error) {
	o.calls++
	o.call = call
	return o.response, o.err
}

type fakeReview struct {
	calls    int
	requests []ReviewRequest
}

func (r *fakeReview) Request(_ context.Context, req ReviewRequest) (string, error) {
	r.calls++
	r.requests = append(r.requests, req)
	return fmt.Sprintf("review-%d", r.calls), nil
}
