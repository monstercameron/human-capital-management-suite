package projectservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type membershipRepoStub struct {
	accepted               bool
	invited                bool
	tenant, project, actor string
	revision               uint64
	key                    string
	snapshot               MembershipSnapshot
}
type inviteeEligibilityStub struct {
	class uint8
	err   error
	users []string
}

func (e *inviteeEligibilityStub) CheckInvitee(_ context.Context, _ *trust.Principal, user string) (uint8, error) {
	e.users = append(e.users, user)
	return e.class, e.err
}

func (r *membershipRepoStub) GetMemberships(_ context.Context, t, p string) (MembershipSnapshot, error) {
	r.tenant = t
	r.project = p
	return r.snapshot, nil
}
func (r *membershipRepoStub) ValidateInviteeClassification(context.Context, string, string, uint8) error {
	return nil
}
func (r *membershipRepoStub) InviteMember(context.Context, string, string, string, string, projectaccess.Role, uint8, uint64, string) error {
	r.invited = true
	return nil
}

func TestMembershipInviteFailsClosedWithoutTrustedEligibility(t *testing.T) {
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "d"})
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeAuth{}
	repo := &membershipRepoStub{}
	svc := Service{Auth: auth, Members: repo}
	err = svc.InviteMember(context.Background(), p, "project-a", "arbitrary-user", projectaccess.RoleViewer, 2, "invite-key")
	if err != ErrUnavailable {
		t.Fatalf("InviteMember error=%v, want fail closed", err)
	}
	if auth.calls != 1 || repo.accepted || repo.invited {
		t.Fatalf("eligibility failure did not stop before membership write: auth calls=%d repo=%+v", auth.calls, repo)
	}
}
func (r *membershipRepoStub) AcceptMemberInvitation(_ context.Context, t, p, u string, v uint64, k string) error {
	r.accepted = true
	r.tenant = t
	r.project = p
	r.actor = u
	r.revision = v
	r.key = k
	return nil
}
func (r *membershipRepoStub) ChangeMemberRole(context.Context, string, string, string, string, projectaccess.Role, uint64, string) error {
	return nil
}
func (r *membershipRepoStub) RevokeMember(context.Context, string, string, string, string, uint64, string) error {
	return nil
}
func (r *membershipRepoStub) TransferMemberOwnership(context.Context, string, string, string, string, uint64, string) error {
	return nil
}

func TestMembershipAcceptanceUsesTrustedSubjectWithoutManagerGrant(t *testing.T) {
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "d"})
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeAuth{denied: true}
	repo := &membershipRepoStub{}
	svc := Service{Auth: auth, Members: repo, Invitees: &inviteeEligibilityStub{class: 0}}
	if err := svc.AcceptMemberInvitation(context.Background(), p, "project-a", 4, "accept-key"); err != nil {
		t.Fatalf("AcceptMemberInvitation: %v", err)
	}
	if !repo.accepted || repo.tenant != "tenant-a" || repo.project != "project-a" || repo.actor != "alice" || repo.revision != 4 || repo.key != "accept-key" {
		t.Fatalf("acceptance did not bind trusted identity and revision: %+v", repo)
	}
	if auth.calls != 0 {
		t.Fatalf("acceptance must rely on store invitation check, authorization calls=%d", auth.calls)
	}
}

func TestWorkforceInviteeEligibilityRequiresCurrentSameTenantEmployee(t *testing.T) {
	db := pgtest.New(t)
	tenantAKey, tenantBKey := "project-invite-tenant-a", "project-invite-tenant-b"
	tenantA, tenantB := pgstore.TenantID(tenantAKey), pgstore.TenantID(tenantBKey)
	for _, entry := range []struct {
		key string
		id  uuid.UUID
	}{{tenantAKey, tenantA}, {tenantBKey, tenantB}} {
		if _, err := db.SQL.Exec(`INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,$2,'cell-local',$3,'ACTIVE',now())`, entry.id, entry.key, entry.key); err != nil {
			t.Fatal(err)
		}
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	for _, row := range []workforce.WorkerRow{
		inviteeWorker(tenantA, "active-employee", "employee", "active"),
		inviteeWorker(tenantA, "inactive-employee", "employee", "TERMINATED"),
		inviteeWorker(tenantA, "active-contractor", "CONTRACTOR", "active"),
		inviteeWorker(tenantB, "cross-tenant-only", "employee", "active"),
	} {
		tx, err := conn.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := tenancy.WithTenant(context.Background(), tx, row.TenantID); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatal(err)
		}
		if _, err := (workforce.Store{}).Create(context.Background(), tx, row); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatal(err)
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	adapter := WorkforceInviteeEligibility{DB: conn, TenantID: func(id kernelvalues.TenantId) uuid.UUID { return pgstore.TenantID(string(id)) }}
	// Use the corresponding tenant namespace and a server-verified subject.
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: kernelvalues.TenantId(tenantAKey), Subject: "manager", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "invite-test", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "invite-test"})
	if err != nil {
		t.Fatal(err)
	}
	class, err := adapter.CheckInvitee(context.Background(), principal, "active-employee")
	if err != nil || class != workforceInviteeBaselineClass {
		t.Fatalf("active employee eligibility=(%d,%v), want baseline/allowed", class, err)
	}
	for _, key := range []string{"inactive-employee", "active-contractor", "cross-tenant-only", "unknown-worker"} {
		if _, err := adapter.CheckInvitee(context.Background(), principal, key); !errors.Is(err, projectaccess.ErrMemberNotFound) {
			t.Errorf("CheckInvitee(%q) error=%v, want nonrevealing ineligible result", key, err)
		}
	}
}

func inviteeWorker(tenantID uuid.UUID, key, kind, lifecycle string) workforce.WorkerRow {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	return workforce.WorkerRow{TenantID: tenantID, WorkerID: uuid.New(), WorkerKey: key, LegalName: "Test Employee", PreferredName: "Test", WorkerNumber: "W-" + key, WorkerType: kind, LifecycleStatus: lifecycle, EmploymentID: "emp-" + key, AssignmentID: "asg-" + key, JobCode: "OPS-HRBP2", Grade: "P2", OrgUnit: "people", PositionID: "position", Location: "Boston", PayZone: "US-EAST", FTE: "1.0000", ManagerRelationshipRef: "manager", BasePay: "100000.00", Currency: "USD", PayBasis: "ANNUAL", BonusTarget: "0.00", RevisionStream: "worker:" + key, RevisionSequence: 1, KnownAt: now, RecordedAt: now, CreatedBy: "seed", Source: workforce.SourceCreated, HireDate: "2026-01-01", EffectiveFrom: "2026-01-01"}
}
