package app

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func journeyApproverPrincipal(t *testing.T, subject string, from, until time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "acme-corp", Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "acme/engineering", Roles: []string{"promotion_operator"},
		AuthorityRefs: []string{"authority:promotion"}, Purposes: []string{"hcm_operations"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session:promotion", IssuedAt: from, ExpiresAt: until, CredentialDigest: "credential:promotion",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func promotionApprovalItem(node, principal string, status workitem.Status) workitem.WorkItem {
	return workitem.WorkItem{
		TenantID:   uuid.MustParse("11111111-2222-4333-8444-555555555555"),
		WorkItemID: uuid.New(), Kind: workitem.KindApproval, NodeID: node, Status: status,
		CompletedBy: principal, ApprovalRequirementRef: "approval", ProposalRef: "sha256:proposal",
		OrganizationScopeID: "acme/engineering",
		Assignment:          workitem.Assignment{Resolution: humanwork.Resolution{Candidates: []humanwork.Candidate{{PrincipalID: principal, Via: humanwork.SourceDirect}}}},
	}
}

func TestPromotionJourneyCurrentAuthorityIsCheckedBeforeEffects(t *testing.T) {
	at := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	item := promotionApprovalItem(promotionexec.NodeApproveManager, "principal:manager", workitem.StatusInProgress)
	if err := ValidatePromotionJourneyApprover(journeyApproverPrincipal(t, item.CompletedBy, at.Add(-time.Hour), at.Add(time.Hour)), item, item.CompletedBy, at); err != nil {
		t.Fatalf("current credential refused: %v", err)
	}
	expired := journeyApproverPrincipal(t, item.CompletedBy, at.Add(-2*time.Hour), at.Add(-time.Second))
	if err := ValidatePromotionJourneyApprover(expired, item, item.CompletedBy, at); !errors.Is(err, ErrPromotionAuthorityStale) {
		t.Fatalf("expired credential error = %v, want ErrPromotionAuthorityStale", err)
	}
	wrongScope := item
	wrongScope.OrganizationScopeID = "acme/other"
	if err := ValidatePromotionJourneyApprover(journeyApproverPrincipal(t, item.CompletedBy, at.Add(-time.Hour), at.Add(time.Hour)), wrongScope, item.CompletedBy, at); !errors.Is(err, ErrPromotionAuthorityStale) {
		t.Fatalf("scope-drift error = %v, want ErrPromotionAuthorityStale", err)
	}
	noLongerRouted := item
	noLongerRouted.Assignment = workitem.Assignment{}
	if err := ValidatePromotionJourneyApprover(journeyApproverPrincipal(t, item.CompletedBy, at.Add(-time.Hour), at.Add(time.Hour)), noLongerRouted, item.CompletedBy, at); !errors.Is(err, ErrPromotionAuthorityStale) {
		t.Fatalf("candidate-drift error = %v, want ErrPromotionAuthorityStale", err)
	}
}

func TestPromotionJourneyRejectsOneApproverAcrossAuthorityClasses(t *testing.T) {
	principal := "principal:dual-role"
	finance := promotionApprovalItem(promotionexec.NodeApproveFinance, principal, workitem.StatusCompleted)
	manager := promotionApprovalItem(promotionexec.NodeApproveManager, principal, workitem.StatusInProgress)
	if err := ValidatePromotionApprovalHistory([]workitem.WorkItem{finance, manager}, manager, principal); !errors.Is(err, ErrProposalDecisionSeparation) {
		t.Fatalf("cross-class decision error = %v, want ErrProposalDecisionSeparation", err)
	}
	other := promotionApprovalItem(promotionexec.NodeApproveManager, "principal:other", workitem.StatusInProgress)
	if err := ValidatePromotionApprovalHistory([]workitem.WorkItem{finance, other}, other, other.CompletedBy); err != nil {
		t.Fatalf("distinct manager approver refused: %v", err)
	}
}
