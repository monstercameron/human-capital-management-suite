package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

// fakeAuthority answers the authority question from a fixed value and records
// the query it was asked.
type fakeAuthority struct {
	answer promotionexec.CurrentApprovalAuthority
	err    error
	asked  ApprovalAuthorityQuery
}

func (f *fakeAuthority) CurrentApprovalAuthority(_ context.Context, _ workitem.Executor, q ApprovalAuthorityQuery) (promotionexec.CurrentApprovalAuthority, error) {
	f.asked = q
	return f.answer, f.err
}

func wfstep003Revision() intent.ProposalRevision {
	return intent.ProposalRevision{ProposalRevisionID: "revision:1", MaterialDigest: digest.Reference{Digest: "sha256:proposal"}}
}

// TestTodo_WF_STEP_003_AuthorityRecheck pins the decision-time recheck: a
// current authority passes, every drift (principal, term, role/activity) is
// stale, and an unanswerable question refuses without being mistaken for
// staleness.
func TestTodo_WF_STEP_003_AuthorityRecheck(t *testing.T) {
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	const manager = "principal:manager"
	item := promotionApprovalItem(promotionexec.NodeApproveManager, manager, workitem.StatusAssigned)
	item.Assignment.Resolution.Candidates[0].TermRef = "term:current-manager-of-worker"
	candidate := item.Assignment.Resolution.Candidates[0]
	principal := journeyApproverPrincipal(t, manager, at.Add(-time.Hour), at.Add(time.Hour))
	inst := intent.Instance{Subjects: []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "worker:subject"}}}
	current := promotionexec.CurrentApprovalAuthority{
		PrincipalID: manager, Class: promotionexec.AuthorityClassCurrentManager,
		AuthorityRef: "term:current-manager-of-worker", Scope: item.OrganizationScopeID, Active: true,
	}

	t.Run("current authority passes and the query carries durable facts", func(t *testing.T) {
		source := &fakeAuthority{answer: current}
		engine := &journeyEngine{authority: source}
		stale, err := engine.recheckApprovalAuthority(context.Background(), nil, principal, inst, item, candidate, wfstep003Revision(), at)
		if stale != nil || err != nil {
			t.Fatalf("recheck = (%v, %v), want current", stale, err)
		}
		if source.asked.SubjectID != "worker:subject" || source.asked.AuthorityPrincipalID != manager ||
			source.asked.DeciderPrincipalID != manager || source.asked.TenantID != item.TenantID || !source.asked.At.Equal(at) {
			t.Fatalf("query = %+v, want the item's subject, authority principal, decider and instant", source.asked)
		}
	})

	for name, mutate := range map[string]func(*promotionexec.CurrentApprovalAuthority){
		"manager changed":   func(c *promotionexec.CurrentApprovalAuthority) { c.PrincipalID = "principal:new-manager" },
		"rule changed":      func(c *promotionexec.CurrentApprovalAuthority) { c.AuthorityRef = "term:execution-authority-approver" },
		"role revoked":      func(c *promotionexec.CurrentApprovalAuthority) { c.Active = false },
		"manager vanished":  func(c *promotionexec.CurrentApprovalAuthority) { c.PrincipalID, c.Active = "", false },
		"scope moved":       func(c *promotionexec.CurrentApprovalAuthority) { c.Scope = "acme/other" },
		"authority expired": func(c *promotionexec.CurrentApprovalAuthority) { c.ValidUntil = at },
	} {
		t.Run(name+" is stale", func(t *testing.T) {
			drifted := current
			mutate(&drifted)
			engine := &journeyEngine{authority: &fakeAuthority{answer: drifted}}
			stale, err := engine.recheckApprovalAuthority(context.Background(), nil, principal, inst, item, candidate, wfstep003Revision(), at)
			if err != nil || !errors.Is(stale, ErrPromotionAuthorityStale) {
				t.Fatalf("recheck = (%v, %v), want a stale authority", stale, err)
			}
		})
	}

	t.Run("no authority source fails closed", func(t *testing.T) {
		stale, err := (&journeyEngine{}).recheckApprovalAuthority(context.Background(), nil, principal, inst, item, candidate, wfstep003Revision(), at)
		if stale != nil || !errors.Is(err, ErrProposalDecisionUnavailable) {
			t.Fatalf("recheck without a source = (%v, %v), want ErrProposalDecisionUnavailable", stale, err)
		}
	})

	t.Run("a lookup failure refuses without invalidating", func(t *testing.T) {
		boom := errors.New("directory offline")
		engine := &journeyEngine{authority: &fakeAuthority{err: boom}}
		stale, err := engine.recheckApprovalAuthority(context.Background(), nil, principal, inst, item, candidate, wfstep003Revision(), at)
		if stale != nil || !errors.Is(err, boom) {
			t.Fatalf("recheck = (%v, %v), want the lookup error", stale, err)
		}
	})

	t.Run("a routed record with no authority term is a routing defect", func(t *testing.T) {
		bare := candidate
		bare.TermRef = ""
		engine := &journeyEngine{authority: &fakeAuthority{answer: current}}
		stale, err := engine.recheckApprovalAuthority(context.Background(), nil, principal, inst, item, bare, wfstep003Revision(), at)
		if stale != nil || !errors.Is(err, promotionexec.ErrApprovalAuthorityIncomplete) {
			t.Fatalf("recheck = (%v, %v), want ErrApprovalAuthorityIncomplete", stale, err)
		}
	})

	t.Run("non-promotion approvals are not rechecked", func(t *testing.T) {
		source := &fakeAuthority{err: errors.New("must not be asked")}
		engine := &journeyEngine{authority: source}
		for _, other := range []workitem.WorkItem{
			promotionApprovalItem(prototype.NodeApproval, manager, workitem.StatusAssigned),
			func() workitem.WorkItem { w := item; w.Kind = workitem.KindTask; return w }(),
		} {
			if stale, err := engine.recheckApprovalAuthority(context.Background(), nil, principal, inst, other, candidate, wfstep003Revision(), at); stale != nil || err != nil {
				t.Fatalf("recheck(%s/%s) = (%v, %v), want skipped", other.NodeID, other.Kind, stale, err)
			}
		}
	})

	t.Run("a delegated candidate is rechecked against its delegator", func(t *testing.T) {
		delegated := humanwork.Candidate{PrincipalID: "principal:delegate", Via: humanwork.SourceDelegated, DelegatedFrom: manager, TermRef: "term:delegation"}
		source := &fakeAuthority{answer: current}
		engine := &journeyEngine{authority: source}
		stale, err := engine.recheckApprovalAuthority(context.Background(), nil, principal, inst, item, delegated, wfstep003Revision(), at)
		if stale != nil || err != nil {
			t.Fatalf("delegated recheck = (%v, %v), want current", stale, err)
		}
		if source.asked.AuthorityPrincipalID != manager {
			t.Fatalf("delegated query authority principal = %q, want the delegator %q", source.asked.AuthorityPrincipalID, manager)
		}
		moved := current
		moved.PrincipalID = "principal:new-manager"
		engine.authority = &fakeAuthority{answer: moved}
		if stale, _ := engine.recheckApprovalAuthority(context.Background(), nil, principal, inst, item, delegated, wfstep003Revision(), at); !errors.Is(stale, ErrPromotionAuthorityStale) {
			t.Fatalf("delegated recheck after the delegator lost the authority = %v, want stale", stale)
		}
	})
}

func TestApprovalAuthorityHelpers(t *testing.T) {
	tenant := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	if got := ApprovalAuthorityScope(workitem.WorkItem{TenantID: tenant, OrganizationScopeID: " acme/people "}); got != "acme/people" {
		t.Errorf("scope = %q, want the organization scope", got)
	}
	if got := ApprovalAuthorityScope(workitem.WorkItem{TenantID: tenant}); got != "tenant:"+tenant.String() {
		t.Errorf("scope without an organization = %q, want the tenant", got)
	}
	if got := authorityPrincipal(humanwork.Candidate{PrincipalID: "a", Via: humanwork.SourceDelegated}); got != "a" {
		t.Errorf("delegated candidate with no delegator = %q, want itself", got)
	}
	item := workitem.WorkItem{SubjectRefs: []string{"worker:ref"}}
	if got := employmentSubjectOf(intent.Instance{}, item); got != "worker:ref" {
		t.Errorf("subject fallback = %q, want the item's first subject", got)
	}
	if got := employmentSubjectOf(intent.Instance{}, workitem.WorkItem{}); got != "" {
		t.Errorf("subject with nothing = %q, want empty", got)
	}
	binding := routedApprovalAuthority(promotionApprovalItem(promotionexec.NodeApproveFinance, "p", workitem.StatusAssigned),
		humanwork.Candidate{PrincipalID: "p", Via: humanwork.SourceDirect, TermRef: "term:finance"}, wfstep003Revision(),
		promotionexec.CurrentApprovalAuthority{AuthorityRef: "term:other"})
	if binding.Authority.Class != promotionexec.AuthorityClassFinancePartner || binding.Authority.AuthorityRef != "term:finance" ||
		binding.ProposalRevisionID != "revision:1" || binding.MaterialDigest != "sha256:proposal" || !binding.Authority.Active {
		t.Errorf("binding = %+v, want the routed finance authority", binding)
	}
}
