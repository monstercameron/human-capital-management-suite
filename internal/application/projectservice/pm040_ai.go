package projectservice

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AIProposalSourcePort is the application boundary for current source grants.
// Implementations must resolve both identities against current tenant policy;
// the service never trusts grants embedded in model output.
type AIProposalSourcePort interface {
	RecheckProposalSources(context.Context, string, string, string, string, []projectworkflow.SourceCitation) ([]projectworkflow.SourceGrant, []projectworkflow.SourceGrant, error)
}

type AdmitAIRequest struct {
	Policy  projectworkflow.AIPolicy
	Request projectworkflow.AIRequest
}

func (s Service) AdmitAIRequest(_ context.Context, p *trust.Principal, request AdmitAIRequest) (projectworkflow.AIAdmission, error) {
	if err := validPrincipal(p); err != nil {
		return projectworkflow.AIAdmission{}, err
	}
	if request.Policy.TenantID != tenant(p) || request.Request.TenantID != tenant(p) {
		return projectworkflow.AIAdmission{}, &projectworkflow.AIPolicyDenial{Code: projectworkflow.AIDenialTenantMismatch, Message: "request tenant does not match the authenticated tenant"}
	}
	return projectworkflow.AdmitAIRequest(request.Policy, request.Request)
}

type PublishAIProposalRequest struct {
	Proposal               projectworkflow.BoardProposal
	ExpectedConfigVersion  uint64
	ExpectedDraftRevision  uint64
	ReviewedProposalDigest string
	ReviewedConfigDigest   string
	ReviewedSourceDigest   string
	ReviewedPlanDigest     string
	IdempotencyKey         string
	Mappings               projectworkflow.MigrationMappings
	ReviewEvidence         WorkflowReviewEvidence
}

// PublishAIProposal rechecks all source and revision fences before delegating
// to the existing workflow publisher. The publisher is the only side-effecting
// operation and is unreachable when the proposal has stale or revoked inputs.
func (s Service) PublishAIProposal(ctx context.Context, p *trust.Principal, req PublishAIProposalRequest) (WorkflowConfiguration, error) {
	if err := validPrincipal(p); err != nil {
		return WorkflowConfiguration{}, err
	}
	if s.Auth == nil || s.Workflows == nil || s.AIProposalSources == nil {
		return WorkflowConfiguration{}, ErrUnavailable
	}
	if req.Proposal.TenantID != tenant(p) || req.Proposal.ProjectID == "" || req.ExpectedConfigVersion == 0 || req.ExpectedDraftRevision == 0 || req.ReviewedProposalDigest == "" || req.ReviewedConfigDigest == "" || req.ReviewedSourceDigest == "" || req.ReviewedPlanDigest == "" || req.IdempotencyKey == "" {
		return WorkflowConfiguration{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, p, req.Proposal.ProjectID, projectaccess.PublishConfiguration); err != nil {
		return WorkflowConfiguration{}, err
	}
	current, err := s.Workflows.GetPublished(ctx, tenant(p), req.Proposal.ProjectID)
	if err != nil {
		return WorkflowConfiguration{}, err
	}
	requesterGrants, publisherGrants, err := s.AIProposalSources.RecheckProposalSources(ctx, tenant(p), req.Proposal.ProjectID, req.Proposal.RequesterID, p.Subject(), req.Proposal.CitedSources)
	if err != nil {
		return WorkflowConfiguration{}, err
	}
	if _, err := projectworkflow.RevalidateProposalPublication(projectworkflow.ProposalPublicationRequest{
		Proposal: req.Proposal, CurrentConfig: current.Config, CurrentConfigVersion: current.Version, ExpectedConfigVersion: req.ExpectedConfigVersion,
		ReviewedProposalDigest: req.ReviewedProposalDigest, ReviewedConfigDigest: req.ReviewedConfigDigest, ReviewedSourceDigest: req.ReviewedSourceDigest,
		RequesterID: req.Proposal.RequesterID, PublisherID: p.Subject(), RequesterGrants: requesterGrants, PublisherGrants: publisherGrants,
	}); err != nil {
		return WorkflowConfiguration{}, err
	}
	return s.PublishWorkflowDraft(ctx, p, PublishWorkflowDraftRequest{
		ProjectID: req.Proposal.ProjectID, DraftID: CurrentWorkflowDraftID, ExpectedDraftRevision: req.ExpectedDraftRevision, ExpectedConfigVersion: req.ExpectedConfigVersion,
		ReviewedDigest: req.ReviewedConfigDigest, ReviewedPlanDigest: req.ReviewedPlanDigest, IdempotencyKey: req.IdempotencyKey, Mappings: req.Mappings, ReviewEvidence: req.ReviewEvidence,
	})
}

// AIProposalSources is kept on Service rather than in a package registry so
// tenant-specific source authority is supplied by the composition root.
