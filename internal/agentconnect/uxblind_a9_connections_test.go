package agentconnect

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/secrets"
)

type fakeIssuer struct {
	next        int
	last        lease.Request
	used        []lease.CredentialLease
	revoked     []string
	useFailure  error
	mintFailure error
}

func (f *fakeIssuer) Mint(req lease.Request) (lease.CredentialLease, lease.Evidence, error) {
	if f.mintFailure != nil {
		return lease.CredentialLease{}, lease.Evidence{}, f.mintFailure
	}
	f.next++
	f.last = req
	got := lease.CredentialLease{ID: fmt.Sprintf("lease-%d", f.next), CustodyLeaseID: fmt.Sprintf("custody-%d", f.next), Handle: req.Handle, Workload: req.Workload, Tenant: req.Tenant, Purpose: req.Purpose, Destination: req.Destination, Operation: req.Operation, Nonce: fmt.Sprintf("nonce-%d", f.next), IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(100, 0).Add(req.TTL)}
	return got, lease.Evidence{LeaseID: got.ID, Outcome: "granted"}, nil
}

func (f *fakeIssuer) Use(got lease.CredentialLease, destination string, operation custody.Operation) (lease.Evidence, error) {
	if f.useFailure != nil {
		return lease.Evidence{}, f.useFailure
	}
	f.used = append(f.used, got)
	return lease.Evidence{LeaseID: got.ID, Destination: destination, Operation: operation, Outcome: "granted"}, nil
}

func (f *fakeIssuer) Revoke(id, _ string) (lease.Evidence, error) {
	f.revoked = append(f.revoked, id)
	return lease.Evidence{LeaseID: id, Outcome: "granted"}, nil
}

func testConnection(t *testing.T, id, tenant string) *connectivity.ConnectorConnection {
	t.Helper()
	version, err := connectivity.ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := connectivity.ParseCredentialRef("vault://tenant/connector")
	if err != nil {
		t.Fatal(err)
	}
	capability := connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead}
	definition := connectivity.ConnectorDefinition{ConnectorID: "hris", Version: version, AuthModes: []connectivity.AuthMode{connectivity.AuthOAuth2ClientCredentials}, Capabilities: []connectivity.Capability{capability}, Bounds: connectivity.Bounds{MaxPageSize: 100, MaxPagesPerRun: 10, MaxRecordsPerRun: 1000, MaxRecordBytes: 4096}}
	connection, err := connectivity.NewConnection(connectivity.Publication{Definition: definition}, connectivity.ConnectionSpec{ConnectionID: id, TenantID: tenant, OrgID: "org-a", SystemID: "system-a", Environment: connectivity.EnvironmentProduction, Residency: "us", ConnectorID: "hris", ConnectorVersion: version, AuthMode: connectivity.AuthOAuth2ClientCredentials, CredentialRef: credential, Scopes: []string{"worker.read"}, EndpointPolicy: connectivity.EndpointPolicy{AllowedHosts: []string{"hris.example"}, RequireTLS: true, EgressProfile: "egress/us"}, Capabilities: []connectivity.Capability{capability}, Bounds: definition.Bounds, CreatedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []connectivity.LifecycleState{connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive} {
		if err := connection.Transition(state, connectivity.TransitionEvidence{Reason: "test", ActorRef: "admin:test", EvidenceRef: "evidence:test", OccurredAt: time.Unix(2, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	return connection
}

func testBinding(tenant, id string) CredentialBinding {
	return CredentialBinding{Reference: secrets.SecretReference{ID: id, Kind: secrets.OAuthGrant, Version: "v1", Provider: "vault", ProviderPath: "opaque/path", Tenant: tenant, Region: "us", State: secrets.Active}, Handle: custody.Handle{ID: id, Kind: custody.Secret, Version: "v1", Tenant: tenant, Region: "us"}}
}

func testUser() UserContext {
	return UserContext{TenantID: "tenant-a", UserID: "user-a", Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}}
}

func testRevision(t *testing.T, mode CredentialMode) ConnectionRevision {
	connection := testConnection(t, "conn-a", "tenant-a")
	revision := ConnectionRevision{ID: "conn-a", TenantID: "tenant-a", Revision: 1, Endpoint: "hris.example", Connection: connection, CredentialMode: mode, Skills: []SkillExposure{{ID: "workers.read", Version: "1", Tool: agentsecurity.ToolDescriptor{Name: "workers.read", Capability: "workers.read", Version: 1, Class: agentsecurity.ToolRead, DataScope: []string{"workers.basic"}, Cost: 1, Schema: "workers.v1"}, Tier: TierT0, CredentialOperation: custody.LeaseOperation, SharedRead: mode == Brokered}}, Grants: []GrantScope{{ID: "grant-a", Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}, Skills: []string{"workers.read"}}}}
	if mode == Brokered {
		revision.Skills[0].RecordFilter = "user.organization_scope"
		revision.Skills[0].SharedRead = false
		revision.BrokeredCredential = testBinding("tenant-a", "brokered")
		digest, err := revision.ApprovalDigest()
		if err != nil {
			t.Fatal(err)
		}
		revision.Approval = &SecondAdminApproval{RequestedBy: "admin-one", ApprovedBy: "admin-two", RequestHash: digest, ApprovedAt: time.Unix(10, 0), StepUp: true}
	}
	return revision
}

func newTestRegistry(t *testing.T, issuer *fakeIssuer) *Registry {
	t.Helper()
	registry, err := NewRegistry(issuer, func() time.Time { return time.Unix(20, 0) })
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestTodo_AGENT2_007(t *testing.T) {
	issuer := &fakeIssuer{}
	registry := newTestRegistry(t, issuer)
	if err := registry.Register(testRevision(t, UserDelegated)); err != nil {
		t.Fatal(err)
	}
	user := testUser()
	if _, err := registry.IssueLease(user, "conn-a", "workers.read", "agent-a", "run-a", "case-review", time.Minute); !errors.Is(err, ErrAccountNotLinked) {
		t.Fatalf("unlinked IssueLease = %v, want ErrAccountNotLinked", err)
	}
	if err := registry.LinkAccount(user, "conn-a", testBinding("tenant-a", "user-account"), "external-user-a"); err != nil {
		t.Fatal(err)
	}
	account, err := registry.LinkedAccount(user, "conn-a")
	if err != nil || account.ExternalAccountID != "external-user-a" || account.Binding.Reference.ID != "user-account" {
		t.Fatalf("linked account metadata = %+v, err=%v", account, err)
	}
	call, err := registry.IssueLease(user, "conn-a", "workers.read", "agent-a", "run-a", "case-review", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if issuer.last.Handle.ID != "user-account" || issuer.last.Tenant != user.TenantID || issuer.last.Destination != "hris.example" || issuer.last.Workload != "agent/agent-a/run/run-a" {
		t.Fatalf("lease request widened or lost acting context: %+v", issuer.last)
	}
	if call.UserID != user.UserID || call.RecordFilter != "" {
		t.Fatalf("user delegated call = %+v", call)
	}
	if _, err := registry.UseLease(call, "hris.example", custody.LeaseOperation); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_AGENT2_007_Golden(t *testing.T) {
	first := testRevision(t, Brokered)
	second := testRevision(t, Brokered)
	one, err := first.ApprovalDigest()
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.ApprovalDigest()
	if err != nil {
		t.Fatal(err)
	}
	if one != two || !strings.HasPrefix(one, "sha256:") {
		t.Fatalf("approval digest is not stable: %q vs %q", one, two)
	}
	first.Approval.RequestHash = "sha256:changed"
	if err := first.validate(); !errors.Is(err, ErrSecondAdminRequired) {
		t.Fatalf("changed revision approval = %v, want ErrSecondAdminRequired", err)
	}
	second = testRevision(t, Brokered)
	digest, err := second.ApprovalDigest()
	if err != nil {
		t.Fatal(err)
	}
	second.Approval.RequestHash = digest
	second.Skills[0].Tool.Capability = "different-capability"
	if err := second.validate(); !errors.Is(err, ErrSecondAdminRequired) {
		t.Fatalf("descriptor-mutated approval = %v, want ErrSecondAdminRequired", err)
	}
	brokeredService := testRevision(t, Brokered)
	brokeredService.BrokeredCredential.Reference.Kind = secrets.APICredential
	digest, err = brokeredService.ApprovalDigest()
	if err != nil {
		t.Fatal(err)
	}
	brokeredService.Approval.RequestHash = digest
	if err := brokeredService.validate(); err != nil {
		t.Fatalf("API-credential brokered revision = %v, want valid", err)
	}
}

func TestTodo_AGENT2_007_Security(t *testing.T) {
	issuer := &fakeIssuer{}
	registry := newTestRegistry(t, issuer)
	revision := testRevision(t, Brokered)
	if err := registry.Register(revision); err != nil {
		t.Fatal(err)
	}
	user := testUser()
	call, err := registry.IssueLease(user, "conn-a", "workers.read", "agent-a", "run-a", "security-review", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	foreignRole := user
	foreignRole.Roles = []string{"employee"}
	if _, err := registry.IssueLease(foreignRole, "conn-a", "workers.read", "agent-a", "run-a", "security-review", time.Minute); !errors.Is(err, ErrDenied) {
		t.Fatalf("ungranted role lease = %v, want ErrDenied", err)
	}
	foreignOrg := user
	foreignOrg.OrganizationScopes = []string{"org-b"}
	if _, err := registry.IssueLease(foreignOrg, "conn-a", "workers.read", "agent-a", "run-a", "security-review", time.Minute); !errors.Is(err, ErrDenied) {
		t.Fatalf("ungranted organization lease = %v, want ErrDenied", err)
	}
	if call.RecordFilter != "user.organization_scope" || issuer.last.Handle.ID != "brokered" {
		t.Fatalf("brokered lease did not carry the declared per-user filter: %+v", call)
	}
	if strings.Contains(fmt.Sprintf("%+v", call), "raw-token") || strings.Contains(fmt.Sprintf("%+v", revision), "raw-token") {
		t.Fatal("credential material appeared in an agent connection value")
	}
	for name, mutate := range map[string]func(*CallLease){
		"audience": func(value *CallLease) { value.Audience = "other.example" },
		"filter":   func(value *CallLease) { value.RecordFilter = "all_records" },
		"workload": func(value *CallLease) { value.Credential.Workload = "agent/other/run/other" },
	} {
		mutated := call
		mutate(&mutated)
		if _, err := registry.UseLease(mutated, "hris.example", custody.LeaseOperation); !errors.Is(err, ErrLeaseFenced) {
			t.Fatalf("mutated %s call = %v, want ErrLeaseFenced", name, err)
		}
	}
	if _, err := registry.UseLease(call, "hris.example", custody.Sign); !errors.Is(err, ErrDenied) {
		t.Fatalf("wrong operation = %v, want ErrDenied", err)
	}
	if err := registry.UnlinkAccount(user, "conn-a", "not linked"); err != nil {
		t.Fatalf("unlink on brokered connection = %v, want no user account requirement", err)
	}
}

func TestTodo_AGENT2_007_Integration(t *testing.T) {
	issuer := &fakeIssuer{}
	registry := newTestRegistry(t, issuer)
	if err := registry.Register(testRevision(t, Brokered)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.Revision("tenant-a", "conn-a")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Skills[0].Tool.DataScope[0] = "workers.salary"
	stored, err := registry.Revision("tenant-a", "conn-a")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Skills[0].Tool.DataScope[0] != "workers.basic" {
		t.Fatalf("revision copy exposed mutable tool scope: %+v", stored.Skills[0].Tool.DataScope)
	}
	user := testUser()
	call, err := registry.IssueLease(user, "conn-a", "workers.read", "agent-a", "run-a", "integration", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.UseLease(call, "hris.example", custody.LeaseOperation); err != nil {
		t.Fatal(err)
	}
	if len(issuer.used) != 1 || issuer.used[0].Tenant != user.TenantID || issuer.used[0].Purpose != "integration" {
		t.Fatalf("issuer did not receive acting-user-bound lease: %+v", issuer.used)
	}
	if _, err := registry.UseLease(call, "hris.example", custody.LeaseOperation); !errors.Is(err, ErrLeaseFenced) {
		t.Fatalf("replayed call = %v, want ErrLeaseFenced", err)
	}
	issuer = &fakeIssuer{}
	registry = newTestRegistry(t, issuer)
	if err := registry.Register(testRevision(t, Brokered)); err != nil {
		t.Fatal(err)
	}
	call, err = registry.IssueLease(testUser(), "conn-a", "workers.read", "agent-a", "run-a", "disconnect", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Disconnect("tenant-a", "conn-a", "admin-a", "connection disabled", "evidence:disconnect"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.UseLease(call, "hris.example", custody.LeaseOperation); !errors.Is(err, ErrLeaseFenced) {
		t.Fatalf("cached lease after disconnect = %v, want ErrLeaseFenced", err)
	}
	if len(issuer.revoked) != 1 {
		t.Fatalf("disconnect revoked %d leases, want one", len(issuer.revoked))
	}
}

func TestTodo_AGENT2_007_Fault(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code string
	}{
		{name: "expired", err: lease.ErrExpired, code: "EXTERNAL_CREDENTIAL_EXPIRED"},
		{name: "revoked", err: lease.ErrRevoked, code: "EXTERNAL_CREDENTIAL_REVOKED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			issuer := &fakeIssuer{useFailure: test.err}
			registry := newTestRegistry(t, issuer)
			if err := registry.Register(testRevision(t, Brokered)); err != nil {
				t.Fatal(err)
			}
			call, err := registry.IssueLease(testUser(), "conn-a", "workers.read", "agent-a", "run-a", "fault", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			var fault *CredentialFault
			if _, err := registry.UseLease(call, "hris.example", custody.LeaseOperation); !errors.As(err, &fault) || fault.Code != test.code || !strings.Contains(fault.Prompt, "Reconnect") {
				t.Fatalf("fault = %v, want typed reconnect prompt %s", err, test.code)
			}
		})
	}
	issuer := &fakeIssuer{}
	registry := newTestRegistry(t, issuer)
	if err := registry.Register(testRevision(t, UserDelegated)); err != nil {
		t.Fatal(err)
	}
	user := testUser()
	invalidBinding := testBinding("tenant-a", "account")
	invalidBinding.Reference.Version = "different"
	if err := registry.LinkAccount(user, "conn-a", invalidBinding, "external"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched account binding = %v, want ErrInvalid", err)
	}
	if err := registry.LinkAccount(user, "conn-a", testBinding("tenant-a", "account"), "external"); err != nil {
		t.Fatal(err)
	}
	call, err := registry.IssueLease(user, "conn-a", "workers.read", "agent-a", "run-a", "revoke", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.UnlinkAccount(user, "conn-a", "user unlinked"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.UseLease(call, "hris.example", custody.LeaseOperation); !errors.Is(err, ErrLeaseFenced) {
		t.Fatalf("cached lease after unlink = %v, want ErrLeaseFenced", err)
	}
	if len(issuer.revoked) != 1 {
		t.Fatalf("unlink revoked %d leases, want one", len(issuer.revoked))
	}
	if _, err := registry.LinkedAccount(user, "conn-a"); !errors.Is(err, ErrAccountNotLinked) {
		t.Fatalf("linked account after unlink = %v, want ErrAccountNotLinked", err)
	}
}
