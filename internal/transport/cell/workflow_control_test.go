package cell

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func designerPrincipal(t *testing.T, roles ...string) *trust.Principal {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "author-a", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-designer", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0),
		CredentialDigest: "credential-digest", Roles: roles,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return principal
}

// hookStore is the minimal fake roleaccess.Store for the authorizer tests:
// only Load matters; the write surface is never exercised here.
type hookStore struct {
	snapshot roleaccess.Snapshot
	err      error
}

func (s hookStore) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return s.snapshot, s.err
}

func (s hookStore) Bootstrap(context.Context, values.TenantId, string) error { return nil }

func (s hookStore) SaveRole(_ context.Context, _ values.TenantId, _ string, role roleaccess.Role) (roleaccess.Role, error) {
	return role, nil
}

func (s hookStore) SaveAssignment(_ context.Context, _ values.TenantId, _ string, assignment roleaccess.Assignment) (roleaccess.Assignment, error) {
	return assignment, nil
}

func (s hookStore) SaveVisibility(_ context.Context, _ values.TenantId, _, _ string, policy roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	return policy, nil
}

func (s hookStore) SavePagePermission(_ context.Context, _ values.TenantId, _ string, permission roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	return permission, nil
}

func (s hookStore) SaveFeaturePermission(_ context.Context, _ values.TenantId, _ string, permission roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	return permission, nil
}

func assignmentSnapshot(worker string, roles ...string) roleaccess.Snapshot {
	return roleaccess.Snapshot{Assignments: []roleaccess.Assignment{{WorkerRef: worker, RoleIDs: roles}}}
}

// TestWorkflowDraftReaderMapsAbsence pins the adapter's non-disclosing
// contract: a malformed draft ID and a cell without a draft store both
// read as not found, never as an internal error.
func TestWorkflowDraftReaderMapsAbsence(t *testing.T) {
	ctx := context.Background()
	tenant := values.TenantId("tenant-a")
	r := workflowDraftReader{cell: &app.Cell{}}
	if _, err := r.ReadWorkflowDraft(ctx, tenant, "not-a-draft-id"); !errors.Is(err, transportworkflow.ErrNotFound) {
		t.Fatalf("malformed draft id error = %v, want not found", err)
	}
	if _, err := r.ReadWorkflowDraft(ctx, tenant, uuid.NewString()); !errors.Is(err, transportworkflow.ErrNotFound) {
		t.Fatalf("storeless draft read error = %v, want not found", err)
	}
}

// The destructive draft commands must be gated by the same designer roles as
// every other authoring command. A new action that falls through to a
// permissive default would let any authenticated caller delete another
// author's work. The grant is resolved from durable assignments, never from
// the credential: a token naming designer roles without a durable row
// behind them still loses.
func TestWorkflowDependenciesGateEveryDraftAuthoringAction(t *testing.T) {
	ctx := context.Background()
	deps := workflowDependencies(nil, nil, nil, nil)
	if deps.Authorize == nil {
		t.Fatal("workflow dependencies carry no authorizer")
	}
	for _, action := range []string{
		transportworkflow.ActionMoveWorkflowDraftNode,
		transportworkflow.ActionRemoveWorkflowDraftNode,
		transportworkflow.ActionClearWorkflowDraftOutcome,
		transportworkflow.ActionRenameWorkflowDraft,
	} {
		// No role store, no admission: the unwired cell denies everything.
		if deps.Authorize(ctx, designerPrincipal(t, "intent_author"), action) {
			t.Fatalf("%q admitted without a role store", action)
		}
	}

	assigned := workflowAuthorizer(hookStore{snapshot: assignmentSnapshot("author-a", "intent_author")})
	bystander, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "bystander-b", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-bystander", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0),
		CredentialDigest: "credential-digest", Roles: []string{"worker"},
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	// A token naming the designer role without a durable row behind it is
	// exactly the revoked-user gap RBAC-RT-002 closed: it must lose.
	claimsOnly, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "claims-only", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-claims", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0),
		CredentialDigest: "credential-digest", Roles: []string{"intent_author", "hcm_admin"},
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	for _, action := range []string{
		transportworkflow.ActionMoveWorkflowDraftNode,
		transportworkflow.ActionRemoveWorkflowDraftNode,
		transportworkflow.ActionClearWorkflowDraftOutcome,
		transportworkflow.ActionRenameWorkflowDraft,
	} {
		if !assigned(ctx, designerPrincipal(t), action) {
			t.Fatalf("designer was refused %q", action)
		}
		if assigned(ctx, bystander, action) {
			t.Fatalf("%q admitted a principal without a durable grant", action)
		}
		if assigned(ctx, nil, action) {
			t.Fatalf("%q admitted an unauthenticated caller", action)
		}
		if assigned(ctx, claimsOnly, action) {
			t.Fatalf("%q admitted credential claims without a durable assignment", action)
		}
	}
}
