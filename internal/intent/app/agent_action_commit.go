package app

import (
	"context"
	"strings"
)

// AgentIntentCommitPin is derived from the owning intent and the workflow's
// exact approved proposal at the governed HCM write boundary.
type AgentIntentCommitPin struct {
	TenantID, IntentID, ForUser, OriginEventRef string
	ProposalRevisionID, ProposalDigest          string
}

type AgentIntentCommitAuthority interface {
	VerifyAgentIntentCommit(context.Context, AgentIntentCommitPin) error
}

// BindAgentIntentCommitAuthority is composition-only, before serving requests.
func (s *IntentService) BindAgentIntentCommitAuthority(authority AgentIntentCommitAuthority) error {
	if s == nil || authority == nil || s.agentCommitAuthority != nil {
		return ErrAgentActionDraft
	}
	s.agentCommitAuthority = authority
	return nil
}

func (s *IntentService) verifyAgentIntentCommit(ctx context.Context, call PromotionStepCall) error {
	instance, _, ownedErr := s.loadInstance(ctx, call.Delegation.TenantKey, call.IntentID)
	if ownedErr != nil {
		return ownedErr
	}
	if instance.OriginEventRef == nil || !strings.HasPrefix(*instance.OriginEventRef, "agent-run:") {
		return nil
	}
	if s.agentCommitAuthority == nil {
		return ErrAgentActionDraft
	}
	return s.agentCommitAuthority.VerifyAgentIntentCommit(ctx, AgentIntentCommitPin{TenantID: call.Delegation.TenantKey, IntentID: call.IntentID, ForUser: call.Delegation.Subject, OriginEventRef: *instance.OriginEventRef, ProposalRevisionID: call.ProposalRevision.ProposalRevisionID, ProposalDigest: call.ProposalRevision.MaterialDigest.Digest})
}
