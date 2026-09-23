package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func TestTodo_NAAS_002_ManagerScope(t *testing.T) {
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	principal := journeyApproverPrincipal(t, "manager", at.Add(-time.Hour), at.Add(time.Hour))
	for _, name := range []string{"current manager", "changed manager", "inactive", "lookup failure", "missing source", "other tenant", "other revision tenant", "finance", "delegated", "configured fallback", "not routed", "expired credential"} {
		t.Run(name, func(t *testing.T) {
			item := promotionApprovalItem(promotionexec.NodeApproveManager, principal.Subject(), workitem.StatusAssigned)
			item.OrganizationScopeID = "acme/people-ops"
			item.Assignment.Resolution.Candidates[0].TermRef = "term:current-manager-of-worker"
			candidate := item.Assignment.Resolution.Candidates[0]
			revision := wfstep003Revision()
			revision.Tenant = principal.Tenant()
			inst := intent.Instance{Tenant: principal.Tenant(), Subjects: []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "worker:subject"}}}
			source := &fakeAuthority{answer: promotionexec.CurrentApprovalAuthority{
				PrincipalID: principal.Subject(), Class: promotionexec.AuthorityClassCurrentManager,
				AuthorityRef: candidate.TermRef, Scope: item.OrganizationScopeID, Active: true,
			}}
			engine := &journeyEngine{authority: source}
			decidedAt := at
			switch name {
			case "changed manager":
				source.answer.PrincipalID = "other-manager"
			case "inactive":
				source.answer.Active = false
			case "lookup failure":
				source.err = errors.New("directory unavailable")
			case "missing source":
				engine.authority = nil
			case "other tenant":
				inst.Tenant = "other-corp"
			case "other revision tenant":
				revision.Tenant = "other-corp"
			case "finance":
				item.NodeID = promotionexec.NodeApproveFinance
			case "delegated":
				candidate.Via = humanwork.SourceDelegated
			case "configured fallback":
				candidate.TermRef = "term:execution-authority-approver"
			case "not routed":
				item.Assignment.Resolution.Candidates = nil
			case "expired credential":
				decidedAt = principal.ExpiresAt()
			}
			err := engine.validateRoutedJourneyApprover(context.Background(), nil, principal, inst, item, candidate, revision, decidedAt)
			if name == "current manager" {
				if err != nil {
					t.Fatalf("current assigned manager refused: %v", err)
				}
				if source.asked.SubjectID != "worker:subject" || source.asked.DeciderPrincipalID != principal.Subject() {
					t.Fatalf("wrong authority query: %+v", source.asked)
				}
			} else if err == nil {
				t.Fatal("invalid cross-scope approval admitted")
			} else if name == "lookup failure" {
				if !errors.Is(err, source.err) {
					t.Fatalf("lost lookup error: %v", err)
				}
			} else if name == "missing source" {
				if !errors.Is(err, ErrProposalDecisionUnavailable) {
					t.Fatalf("missing source error: %v", err)
				}
			} else if !errors.Is(err, ErrPromotionAuthorityStale) {
				t.Fatalf("wrong refusal: %v", err)
			}
		})
	}
}

func TestPromotionAuthorityDiagnostics(t *testing.T) {
	err := promotionAuthorityError("APPROVER_ORGANIZATION_SCOPE_MISMATCH")
	if !errors.Is(err, ErrPromotionAuthorityStale) || observe.ErrorCode(err) != "APPROVER_ORGANIZATION_SCOPE_MISMATCH" || err.Error() == "" {
		t.Fatalf("authority diagnostic lost classification: %v", err)
	}
	if mapped := journeyDecisionError(err); !errors.Is(mapped, workspace.ErrDenied) || !errors.Is(mapped, ErrPromotionAuthorityStale) {
		t.Fatalf("authority refusal is not a public denial: %v", mapped)
	}
}
