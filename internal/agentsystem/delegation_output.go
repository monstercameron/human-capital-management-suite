package agentsystem

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// SpecialistOutput is a typed return to the authenticated parent. Derived
// text and results remain tainted data and cannot modify its confirmed plan.
type SpecialistOutput struct {
	ParentTaskID string                 `json:"parent_task_id"`
	TaskID       string                 `json:"task_id"`
	TaskVersion  uint64                 `json:"task_version"`
	AnswerText   string                 `json:"answer_text,omitempty"`
	Results      []agentrun.LedgerEntry `json:"results"`
	Taint        []string               `json:"taint"`
	Digest       string                 `json:"digest"`
}

// SpecialistResult reloads the completed child and authentic current grants.
// Caller-supplied child results are never accepted at this boundary.
func (r *Runner) SpecialistResult(ctx context.Context, parentTaskID, parentCredential, childTaskID string) (SpecialistOutput, error) {
	if r == nil || r.p == nil || ctx == nil {
		return SpecialistOutput{}, ErrInvalid
	}
	parent, err := r.Runtime.GetTask(ctx, parentTaskID)
	if err != nil {
		return SpecialistOutput{}, err
	}
	if parent.State != agentrun.StateRunning || parent.CurrentStep >= len(parent.Plan.Steps) {
		return SpecialistOutput{}, ErrDenied
	}
	boundary := agentdelegation.VerifyRequest{Audience: r.p.cfg.Audience, Sender: r.p.cfg.Workload}
	claims, parentAuthority, err := r.Delegation.VerifyAuthority(parentCredential, boundary)
	if err != nil {
		return SpecialistOutput{}, err
	}
	if claims.Subject != parent.UserID || claims.Actor.RunID != parent.ID || claims.Actor.StepID != parent.Plan.Steps[parent.CurrentStep].ID || claims.GrantID != GrantID(parent.ID) {
		return SpecialistOutput{}, ErrDenied
	}
	child, err := r.Runtime.GetTask(ctx, childTaskID)
	if err != nil {
		return SpecialistOutput{}, err
	}
	if child.ParentTaskID != parent.ID || child.TenantID != parent.TenantID || child.UserID != parent.UserID || child.State != agentrun.StateCompleted || len(child.Plan.Steps) == 0 {
		return SpecialistOutput{}, ErrDenied
	}
	grant, err := r.grants.Get(GrantID(child.ID))
	if err != nil {
		return SpecialistOutput{}, err
	}
	if err := r.validateDelegatedTask(ctx, child, grant); err != nil {
		return SpecialistOutput{}, err
	}
	step := child.Plan.Steps[len(child.Plan.Steps)-1]
	credential, err := r.Delegation.Exchange(agentdelegation.ExchangeRequest{SubjectToken: grant.GrantID, SubjectTokenType: agentdelegation.DelegationGrantTokenType, RunID: child.ID, StepID: step.ID, Skill: step.SkillID, Scope: grant.SkillScopes[step.SkillID], Audience: r.p.cfg.Audience, Sender: r.p.cfg.Workload})
	if err != nil {
		return SpecialistOutput{}, err
	}
	_, childAuthority, err := r.Delegation.VerifyAuthority(credential.Raw, boundary)
	if err != nil {
		return SpecialistOutput{}, err
	}
	if !utf8.ValidString(child.Ledger.AnswerText) || len(child.Ledger.AnswerText) > agentrun.MaxAnswerBytes {
		return SpecialistOutput{}, ErrDenied
	}
	result := SpecialistOutput{ParentTaskID: parent.ID, TaskID: child.ID, TaskVersion: child.Version, AnswerText: child.Ledger.AnswerText, Taint: []string{taintDerived}}
	for _, entry := range child.Ledger.Entries {
		if entry.Kind != "STEP_RESULT" {
			continue
		}
		if entry.Revoked || strings.TrimSpace(entry.Ref) == "" || strings.TrimSpace(entry.Digest) == "" || len(entry.Taint) == 0 {
			return SpecialistOutput{}, ErrDenied
		}
		sources := append(slices.Clone(entry.SourceIDs), entry.SourceID)
		for _, source := range sources {
			if source != "" && (!outputSourceAllowed(parentAuthority, source) || !outputSourceAllowed(childAuthority, source)) {
				return SpecialistOutput{}, ErrDenied
			}
		}
		entry.SourceIDs, entry.Taint = slices.Clone(entry.SourceIDs), slices.Clone(entry.Taint)
		result.Results = append(result.Results, entry)
	}
	if len(result.Results) != len(child.Plan.Steps) {
		return SpecialistOutput{}, ErrDenied
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return SpecialistOutput{}, err
	}
	result.Digest = digestOf(string(encoded))
	return result, nil
}

func outputSourceAllowed(authority trust.EffectiveAuthority, source string) bool {
	return slices.Contains(authority.Resources, source)
}
