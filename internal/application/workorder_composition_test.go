package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/application/workorderservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ironridgeseed"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectmemberstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	workorderflow "github.com/monstercameron/human-capital-management-suite/internal/workflow/workorder"
)

type workOrderTemplatesFixture struct{ published workordertemplate.Published }

func (f workOrderTemplatesFixture) ResolvePublished(context.Context, string, string, string) (workordertemplate.Published, error) {
	return f.published, nil
}

type workOrderProjectsFixture struct{ snapshot projectmemberstore.Snapshot }

func (f workOrderProjectsFixture) GetSnapshot(context.Context, string, string) (projectmemberstore.Snapshot, error) {
	return f.snapshot, nil
}

type workOrderOrdersFixture struct{ snapshot workorder.Snapshot }

func (f workOrderOrdersFixture) Get(context.Context, string, string) (workorder.Snapshot, error) {
	return f.snapshot, nil
}

type workOrderStandingFixture struct{ err error }

func (f workOrderStandingFixture) CheckInvitee(context.Context, *trust.Principal, string) (uint8, error) {
	return 1, f.err
}

type workOrderWorkersFixture struct {
	refs     map[string]string
	eligible bool
}

func (f workOrderWorkersFixture) ResolveWorker(_ context.Context, _, ref string) (string, bool, error) {
	value, ok := f.refs[ref]
	return value, ok, nil
}

func (f workOrderWorkersFixture) ResolveEligible(_ context.Context, _, ref string) (bool, error) {
	_, ok := f.refs[ref]
	return ok && f.eligible, nil
}

func workOrderTestPrincipal(t *testing.T, subject string) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "ironridge-demo", Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "workorder-test-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "test-digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func workOrderAuthFixture(role projectaccess.Role, actor, assigned string, workers workOrderWorkersFixture) workOrderAuthorizer {
	return workOrderAuthorizer{
		Projects: workOrderProjectsFixture{snapshot: projectmemberstore.Snapshot{
			TenantID: "ironridge-demo", ProjectID: "project-1",
			Members: []projectmemberstore.Membership{{TenantID: "ironridge-demo", ProjectID: "project-1", UserID: actor, Role: role, State: projectaccess.MembershipActive}},
		}},
		Orders: workOrderOrdersFixture{snapshot: workorder.Snapshot{
			ID: "order-1", TenantID: "ironridge-demo", ProjectID: "project-1", InitiatorID: actor,
			Assignments: []workorder.Assignment{{ID: "assignment-1", WorkerID: assigned}},
		}},
		Standing: workOrderStandingFixture{}, Workers: workers,
	}
}

func TestWorkOrderAuthorizerAllowsAdvanceOnlyForInitiator(t *testing.T) {
	workers := workOrderWorkersFixture{refs: map[string]string{"initiator": "00000000-0000-4000-8000-000000000001", "peer": "00000000-0000-4000-8000-000000000002"}, eligible: true}
	initiatorAuth := workOrderAuthFixture(projectaccess.RoleContributor, "initiator", "", workers)
	if err := initiatorAuth.Authorize(context.Background(), workOrderTestPrincipal(t, "initiator"), "project-1", "order-1", workorderaccess.Advance); err != nil {
		t.Fatalf("initiator transition authorization = %v, want allowed", err)
	}
	peerAuth := workOrderAuthFixture(projectaccess.RoleContributor, "peer", "", workers)
	peerAuth.Orders = workOrderOrdersFixture{snapshot: workorder.Snapshot{
		ID: "order-1", TenantID: "ironridge-demo", ProjectID: "project-1", InitiatorID: "initiator",
	}}
	if err := peerAuth.Authorize(context.Background(), workOrderTestPrincipal(t, "peer"), "project-1", "order-1", workorderaccess.Advance); !errors.Is(err, workorderaccess.ErrUnauthorized) {
		t.Fatalf("peer transition authorization = %v, want unauthorized", err)
	}
}

func TestWorkOrderAuthorizerBindsWorkEntryToResolvedActorWorker(t *testing.T) {
	directory := workOrderWorkersFixture{
		refs:     map[string]string{"employee-key": "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000001": "00000000-0000-4000-8000-000000000001", "other-key": "00000000-0000-4000-8000-000000000002"},
		eligible: true,
	}
	authorizer := workOrderAuthFixture(projectaccess.RoleContributor, "employee-key", "00000000-0000-4000-8000-000000000001", directory)
	err := authorizer.AuthorizeWorkEntry(context.Background(), workOrderTestPrincipal(t, "employee-key"), "project-1", "order-1", "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("self-recorded assigned work entry was denied: %v", err)
	}
}

func TestWorkOrderAuthorizerRejectsContributorLoggingAnotherWorkersTime(t *testing.T) {
	directory := workOrderWorkersFixture{
		refs:     map[string]string{"employee-key": "00000000-0000-4000-8000-000000000001", "other-key": "00000000-0000-4000-8000-000000000002"},
		eligible: true,
	}
	authorizer := workOrderAuthFixture(projectaccess.RoleContributor, "employee-key", "other-key", directory)
	err := authorizer.AuthorizeWorkEntry(context.Background(), workOrderTestPrincipal(t, "employee-key"), "project-1", "order-1", "other-key")
	if !errors.Is(err, workorderaccess.ErrUnauthorized) {
		t.Fatalf("contributor logging another worker's time error = %v, want unauthorized", err)
	}
}

func TestWorkOrderAuthorizerAllowsManagerOverrideOnlyForEligibleAssignedWorker(t *testing.T) {
	directory := workOrderWorkersFixture{
		refs:     map[string]string{"foreman-key": "00000000-0000-4000-8000-000000000003", "employee-key": "00000000-0000-4000-8000-000000000001"},
		eligible: true,
	}
	authorizer := workOrderAuthFixture(projectaccess.RoleManager, "foreman-key", "employee-key", directory)
	err := authorizer.AuthorizeWorkEntry(context.Background(), workOrderTestPrincipal(t, "foreman-key"), "project-1", "order-1", "employee-key")
	if err != nil {
		t.Fatalf("manager override for assigned eligible worker was denied: %v", err)
	}
	directory.eligible = false
	authorizer.Workers = directory
	err = authorizer.AuthorizeWorkEntry(context.Background(), workOrderTestPrincipal(t, "foreman-key"), "project-1", "order-1", "employee-key")
	if !errors.Is(err, workorderaccess.ErrWorkerIneligible) {
		t.Fatalf("inactive assigned worker error = %v, want worker ineligible", err)
	}
}

func TestWorkOrderPhaseGateFailsClosedForUnboundDraftTransition(t *testing.T) {
	published, err := ironridgeseed.PublishedTemplate()
	if err != nil {
		t.Fatalf("publish template fixture: %v", err)
	}
	pin := published.Pin()
	order := workorder.Snapshot{
		ID: "order-1", TenantID: "ironridge-demo", ProjectID: "project-1",
		TemplateID: pin.TemplateID, TemplateVersion: pin.Version, TemplateDigest: pin.Digest,
		Phase: workorder.PhaseDraft, Revision: 1,
		Transitions: []workorder.PhaseTransition{{From: workorder.PhaseDraft, To: workorder.PhaseAuthorization}},
	}
	gate := workOrderTemplatePhaseGate{Templates: workOrderTemplatesFixture{published: published}}
	err = gate.EvaluateTransition(context.Background(), workOrderTestPrincipal(t, "initiator"), order, workorder.TransitionInput{
		Target:       workorder.PhaseAuthorization,
		EvidenceRefs: []string{"client-asserted:evidence"},
	})
	if !errors.Is(err, workorderflow.ErrStaleGateFacts) {
		t.Fatalf("unbound DRAFT to AUTHORIZATION transition = %v, want fail-closed ErrStaleGateFacts", err)
	}
}

var _ projectservice.InviteeEligibility = workOrderStandingFixture{}
var _ workorderservice.WorkerDirectory = workOrderWorkersFixture{}
