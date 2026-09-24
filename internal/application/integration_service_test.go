package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type integrationDefinitionLoader struct {
	registry  *connectivity.Registry
	tenant    uuid.UUID
	gotTenant uuid.UUID
	gotOrg    string
}

func (l *integrationDefinitionLoader) LoadRegistry(_ context.Context, tenant uuid.UUID, org string) (*connectivity.Registry, error) {
	l.gotTenant, l.gotOrg = tenant, org
	return l.registry, nil
}

func integrationServiceFixture(t *testing.T) (*IntegrationService, *fakeincumbent.Incumbent, context.Context, *connectivity.ConnectorConnection) {
	t.Helper()
	incumbent := fakeincumbent.MustNew(fakeincumbent.Options{})
	registry := connectivity.NewRegistry()
	published, err := registry.Publish(fakeincumbent.DefaultDefinition(), connectivity.PublicationMeta{PublishedBy: "test", PublishedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := connectivity.ParseCredentialRef("secretref://hcmnext/test/client")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := connectivity.NewConnection(published, connectivity.ConnectionSpec{
		ConnectionID: incumbent.Descriptor().ConnectionID, TenantID: "tenant-a", OrgID: "org-a", SystemID: "system-a",
		Environment: connectivity.EnvironmentSandbox, Residency: "us-east", ConnectorID: published.Definition.ConnectorID,
		ConnectorVersion: published.Definition.Version, AuthMode: connectivity.AuthOAuth2ClientCredentials, CredentialRef: credential,
		Scopes: []string{"worker.read", "position.read", "compensation.read"}, EndpointPolicy: connectivity.EndpointPolicy{AllowedHosts: []string{"fakeincumbent.invalid"}, RequireTLS: true, EgressProfile: "egress/test"},
		Capabilities: connectivity.ReadCapabilities(connectivity.ObjectKinds()...), Bounds: published.Definition.Bounds, CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, state := range []connectivity.LifecycleState{connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive} {
		if err := connection.Transition(state, connectivity.TransitionEvidence{Reason: "test", ActorRef: "test", EvidenceRef: "test:transition", OccurredAt: time.Date(2026, 9, 1, 0, i+1, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	definitionLoader := &integrationDefinitionLoader{registry: registry, tenant: uuid.New()}
	svc, err := NewIntegrationService(IntegrationOptions{Definitions: definitionLoader, TenantID: func(tenant string) uuid.UUID {
		if tenant == "tenant-a" {
			return definitionLoader.tenant
		}
		return uuid.Nil
	}, Connections: []IntegrationConnection{{Connection: connection, Connector: incumbent, OrgID: "org-a", SystemID: "system-a", Environment: connectivity.EnvironmentSandbox, Residency: "us-east", AuthMode: connectivity.AuthOAuth2ClientCredentials, CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}}, Observations: observe.NewMemoryStore()})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "operator-a", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-a", Purposes: []string{"integration.read"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), CredentialDigest: "digest-a"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, incumbent, trust.WithPrincipal(context.Background(), principal), connection
}

func TestIntegrationServiceProjectsRealConnectivityAndScopesTenant(t *testing.T) {
	svc, incumbent, ctx, connection := integrationServiceFixture(t)
	defs, err := svc.ListConnectorDefinitions(ctx, &integrationv1.ListConnectorDefinitionsRequest{Scope: &commonv1.ScopeContext{TenantId: "tenant-a"}})
	if err != nil || len(defs.GetConnectorDefinitions()) != 1 || defs.GetConnectorDefinitions()[0].GetConnectorId() != connection.ConnectorID() {
		t.Fatalf("definitions=%v err=%v", defs, err)
	}
	definitionLoader := svc.definitions.(*integrationDefinitionLoader)
	if definitionLoader.gotTenant != definitionLoader.tenant || definitionLoader.gotOrg != "org-a" {
		t.Fatalf("definition scope=(%s,%q), want principal scope=(%s,%q)", definitionLoader.gotTenant, definitionLoader.gotOrg, definitionLoader.tenant, "org-a")
	}
	conns, err := svc.ListConnectorConnections(ctx, &integrationv1.ListConnectorConnectionsRequest{Scope: &commonv1.ScopeContext{TenantId: "tenant-a", OrganizationScopeId: "org-a"}})
	if err != nil || len(conns.GetConnections()) != 1 || conns.GetConnections()[0].GetTenantId() != "tenant-a" {
		t.Fatalf("connections=%v err=%v", conns, err)
	}
	incumbent.ResetCalls()
	result, err := svc.TestConnectorConnection(ctx, &integrationv1.TestConnectorConnectionRequest{Scope: &commonv1.ScopeContext{TenantId: "tenant-a"}, ConnectionId: connection.ID(), Objects: []integrationv1.ObjectKind{integrationv1.ObjectKind_OBJECT_KIND_WORKER}})
	if err != nil || result.GetDiagnostic() == nil || incumbent.MutatingCalls() != 0 {
		t.Fatalf("diagnostic=%v err=%v mutating=%d", result, err, incumbent.MutatingCalls())
	}
	if _, err := svc.GetConnectorConnection(ctx, &integrationv1.GetConnectorConnectionRequest{Scope: &commonv1.ScopeContext{TenantId: "tenant-b"}, ConnectionId: connection.ID()}); err == nil {
		t.Fatal("cross-tenant scope accepted")
	}
	// Organization scope is a second visibility boundary. A tenant match
	// alone must not expose a connection bound to another organization.
	svc.connections[0].OrgID = "org-b"
	orgFiltered, err := svc.ListConnectorConnections(ctx, &integrationv1.ListConnectorConnectionsRequest{Scope: &commonv1.ScopeContext{TenantId: "tenant-a", OrganizationScopeId: "org-a"}})
	if err != nil {
		t.Fatalf("organization-filtered list: %v", err)
	}
	if len(orgFiltered.GetConnections()) != 0 {
		t.Fatalf("organization scope leaked %d connections", len(orgFiltered.GetConnections()))
	}
}

func TestIntegrationServiceObservationHistoryUsesTenantBoundStore(t *testing.T) {
	svc, incumbent, ctx, connection := integrationServiceFixture(t)
	snapshot, err := incumbent.Snapshot(ctx, connectivity.ObjectWorker)
	if err != nil {
		t.Fatal(err)
	}
	page, err := incumbent.Read(ctx, connectivity.ReadRequest{Object: connectivity.ObjectWorker, Mode: connectivity.ReadFull, Cursor: connectivity.StartCursor(snapshot), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := observe.Record(page, observe.RecordOptions{TenantID: "tenant-a", Descriptor: incumbent.Descriptor(), PageSequence: 1, StartCursor: connectivity.StartCursor(snapshot), FreshnessBudget: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.observations.(*observe.MemoryStore).Append(ctx, observation); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.ListExternalObservations(ctx, &integrationv1.ListExternalObservationsRequest{Scope: &commonv1.ScopeContext{TenantId: "tenant-a"}, ConnectionId: connection.ID()})
	if err != nil || len(listed.GetObservations()) != 1 || listed.GetObservations()[0].GetTenantId() != "tenant-a" {
		t.Fatalf("observations=%v err=%v", listed, err)
	}
}
