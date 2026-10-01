package application

import (
	"context"
	"encoding/json"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"strings"
)

// AgentActionAuthorityPin identifies the validated output of an admitted run.
// Proposal is present at compilation for comparison to that durable output;
// later boundaries resolve the same immutable pin from the owning run store.
type AgentActionAuthorityPin struct {
	TenantID           string
	ForUser            string
	RunID              string
	AgentVersionRef    string
	ModelDigest        string
	DraftDigest        string
	IntentID           string
	ProposalRevisionID string
	ProposalDigest     string
	Proposal           *AgentActionCompileRequest
}

// AgentActionAuthority resolves admitted run/output pins and rechecks current
// installation, grants, scope and source authority. Caller metadata is never
// authority evidence. Missing composition refuses drafting and submission.
type AgentActionAuthority interface {
	VerifyAgentAction(context.Context, AgentActionAuthorityPin) error
}

type AgentActionRunProposalSource interface {
	ReadAgentActionProposal(context.Context, string, string, string) (AgentActionCompileRequest, error)
}

// CompileRun obtains the action material from its actual validated owner. The
// foreground request carries only the run ID; it cannot supply model pins.
func (s *AgentActionService) CompileRun(ctx context.Context, runID string) (AgentActionState, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return AgentActionState{}, err
	}
	source, ok := s.authority.(AgentActionRunProposalSource)
	if !ok || strings.TrimSpace(runID) == "" {
		return AgentActionState{}, ErrAgentActionApproval
	}
	proposal, err := source.ReadAgentActionProposal(ctx, p.Tenant().String(), p.Subject(), runID)
	if err != nil {
		return AgentActionState{}, err
	}
	return s.Compile(ctx, proposal)
}

// BindAuthority is a composition-time operation, before the handler is served.
func (s *AgentActionService) BindAuthority(authority AgentActionAuthority) error {
	if s == nil || s.cell == nil || s.cell.Service == nil || authority == nil || s.authority != nil {
		return ErrAgentActionApproval
	}
	if err := s.cell.Service.BindAgentIntentCommitAuthority(s); err != nil {
		return err
	}
	s.authority = authority
	return nil
}

// VerifyAgentIntentCommit bridges the exact owning intent pin to the current
// admitted run authority immediately before the normal HCM transactional write.
func (s *AgentActionService) VerifyAgentIntentCommit(ctx context.Context, pin app.AgentIntentCommitPin) error {
	var origin agentActionOrigin
	raw, ok := strings.CutPrefix(pin.OriginEventRef, "agent-run:")
	if !ok || json.Unmarshal([]byte(raw), &origin) != nil || origin.ForUser != pin.ForUser || pin.IntentID == "" || pin.ProposalRevisionID == "" || pin.ProposalDigest == "" {
		return ErrAgentActionApproval
	}
	return s.verifyAuthority(ctx, AgentActionAuthorityPin{TenantID: pin.TenantID, ForUser: pin.ForUser, RunID: origin.RunID, AgentVersionRef: origin.AgentVersion, ModelDigest: origin.ModelDigest, DraftDigest: origin.ProposalDigest, IntentID: pin.IntentID, ProposalRevisionID: pin.ProposalRevisionID, ProposalDigest: pin.ProposalDigest})
}

func (s *AgentActionService) verifyAuthority(ctx context.Context, pin AgentActionAuthorityPin) error {
	if s == nil || s.authority == nil || pin.TenantID == "" || pin.ForUser == "" || pin.RunID == "" || pin.AgentVersionRef == "" || pin.ModelDigest == "" || pin.DraftDigest == "" {
		return ErrAgentActionApproval
	}
	if err := s.authority.VerifyAgentAction(ctx, pin); err != nil {
		return err
	}
	return nil
}
