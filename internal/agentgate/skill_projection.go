package agentgate

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

// SkillAuthorizationProjection is the current, per-skill authority evidence
// an application adapter may use to construct a user authority. Capabilities
// contain one PDP decision per resolved operation; subjects and fields are the
// exact values supplied by the projection request.
type SkillAuthorizationProjection struct {
	Skill        agentskills.SkillRecord
	Grant        SkillGrant
	Capabilities []CapabilityDecision
	EvaluatedAt  time.Time
	Purpose      string
}

// ProjectSkillAuthorization resolves one active exact skill version, its
// current matching administrator grant, and every underlying capability PDP
// decision at call time. It is read-only and performs the same grant, consent,
// subject, field and purpose checks as Authorize. Empty subjects or fields are
// rejected because a projection must never imply a wildcard authority.
func (g *Gate) ProjectSkillAuthorization(ctx context.Context, req DiscoveryRequest, key agentskills.SkillKey) (SkillAuthorizationProjection, error) {
	if g == nil {
		return SkillAuthorizationProjection{}, fmt.Errorf("%w: nil gate", ErrInvalid)
	}
	if key.ID == "" || key.Version == 0 || len(req.Subjects) == 0 || len(req.Fields) == 0 {
		return SkillAuthorizationProjection{}, &DeniedError{Code: DenyInvalid, Skill: key, Detail: "exact skill, subject and field bindings are required"}
	}
	at, err := g.resolveTime(req.At)
	if err != nil {
		return SkillAuthorizationProjection{}, err
	}
	if err := validateUser(req.User, req.Purpose); err != nil {
		return SkillAuthorizationProjection{}, err
	}
	if err := validateSubjects(req.User, req.Subjects); err != nil {
		return SkillAuthorizationProjection{}, err
	}
	record, ok := g.activeSkill(key)
	if !ok {
		return SkillAuthorizationProjection{}, &DeniedError{Code: DenySkillNotFound, Skill: key, Detail: "skill version is not available"}
	}
	if !skillAllowsPurpose(record.Definition.RequiredPurposes, req.Purpose) {
		return SkillAuthorizationProjection{}, &DeniedError{Code: DenyPurpose, Skill: key, Detail: "skill is not available for the requested purpose"}
	}
	grant, err := g.matchGrant(ctx, req.User, key, req.Purpose, at, req.Subjects)
	if err != nil {
		return SkillAuthorizationProjection{}, err
	}
	decision, err := g.authorizeResolved(ctx, req.User, discoveryActor(), key, record, grant, req.Purpose, req.Subjects, req.Fields, at)
	if err != nil {
		return SkillAuthorizationProjection{}, err
	}
	return SkillAuthorizationProjection{
		Skill:        record,
		Grant:        cloneGrant(grant),
		Capabilities: cloneCapabilityDecisions(decision.Capabilities),
		EvaluatedAt:  decision.EvaluatedAt,
		Purpose:      decision.Purpose,
	}, nil
}

func (g *Gate) activeSkill(key agentskills.SkillKey) (agentskills.SkillRecord, bool) {
	for _, record := range g.skills.List() {
		if record.Definition.Key() == key && record.Status == agentskills.StatusActive {
			return record, true
		}
	}
	return agentskills.SkillRecord{}, false
}

func discoveryActor() AgentActor {
	return AgentActor{AgentVersion: "discovery", InstallationID: "discovery", RunID: "discovery", StepID: "discovery"}
}

func cloneCapabilityDecisions(in []CapabilityDecision) []CapabilityDecision {
	out := make([]CapabilityDecision, len(in))
	for i, decision := range in {
		out[i] = cloneCapabilityDecision(decision)
	}
	return out
}
