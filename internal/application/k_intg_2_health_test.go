package application

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestTodo_INTG_017_Integration proves the shipped integration RPC publishes
// normalized connector health and dependency impact.
func TestTodo_INTG_017_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	tenant := string(fixtures.Tenant)
	cfg := ServeConfig{
		Profile:       ServeProfileLocalDev,
		GRPCListen:    "127.0.0.1:0",
		HTTPListen:    "127.0.0.1:0",
		DatabaseURL:   db.URL,
		DevHMACKey:    integrationSigningKey,
		PageCursorKey: integrationPageCursorKey,
		Issuer:        DefaultIssuer,
		Audience:      DefaultAudience,
		Tenant:        tenant,
		CellID:        "cell-intg017",
		MaxDeadline:   30 * time.Second,
		Migrate:       false,
		Workspace:     false,
		OTelExporter:  OTelExporterNone,
		LegalEvidenceIssuerKeys: base64.StdEncoding.EncodeToString(
			ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)),
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	composed, err := ComposeServe(ctx, ServeInput{Config: cfg, Pool: pool, Identity: "intg017-served"})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := composed.Stop(stopCtx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	if err := composed.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience,
	})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	now := time.Now().UTC()
	token, err := verifier.Issue(trust.Claims{
		Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: "operator:intg017", SubjectKind: "human",
		Tenant: tenant, OrganizationScopeID: "org-default", Purposes: []string{"integration.read"},
		AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session:intg017",
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue test credential: %v", err)
	}
	conn, err := grpc.NewClient(composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial composed gRPC: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	callCtx := metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+token)
	client := integrationv1.NewIntegrationServiceClient(conn)

	connections, err := client.ListConnectorConnections(callCtx, &integrationv1.ListConnectorConnectionsRequest{
		Scope: &commonv1.ScopeContext{TenantId: tenant, OrganizationScopeId: "org-default", Purpose: "integration.read"},
	})
	if err != nil {
		t.Fatalf("ListConnectorConnections: %v", err)
	}
	if len(connections.GetConnections()) != 1 {
		t.Fatalf("organization-scoped connections = %d, want 1", len(connections.GetConnections()))
	}
	connection := connections.GetConnections()[0]
	if connection.GetTenantId() != tenant || connection.GetOrgId() != "org-default" || connection.GetSystemId() != "sys-incumbent" {
		t.Fatalf("published connection = %+v, want the composed tenant/org/system", connection)
	}

	result, err := client.TestConnectorConnection(callCtx, &integrationv1.TestConnectorConnectionRequest{
		Scope:        &commonv1.ScopeContext{TenantId: tenant, OrganizationScopeId: "org-default", Purpose: "integration.read"},
		ConnectionId: connection.GetConnectionId(),
		Objects:      []integrationv1.ObjectKind{integrationv1.ObjectKind_OBJECT_KIND_WORKER},
	})
	if err != nil {
		t.Fatalf("TestConnectorConnection: %v", err)
	}
	diagnostic := result.GetDiagnostic()
	if diagnostic == nil {
		t.Fatal("missing connector diagnostic")
	}
	if !hasINTG017String(diagnostic.GetImpact(), "health_status:HEALTHY") || !hasINTG017String(diagnostic.GetImpact(), "health_cause:ALL_CHECKS_PASSED") {
		t.Fatalf("health publication=%v, want healthy status and cause", diagnostic.GetImpact())
	}
	if !hasINTG017String(diagnostic.GetImpact(), "capability:WORKER:READ") {
		t.Fatalf("dependency impact=%v, want worker read capability", diagnostic.GetImpact())
	}

	_, err = client.ListConnectorConnections(callCtx, &integrationv1.ListConnectorConnectionsRequest{
		Scope: &commonv1.ScopeContext{TenantId: "wrong-tenant", OrganizationScopeId: "org-default", Purpose: "integration.read"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("caller-selected tenant error = %v, want trusted-context InvalidArgument", err)
	}
	_, err = client.ListConnectorConnections(callCtx, &integrationv1.ListConnectorConnectionsRequest{
		Scope: &commonv1.ScopeContext{TenantId: tenant, OrganizationScopeId: "wrong-organization", Purpose: "integration.read"},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("caller-selected organization error = %v, want trusted-context InvalidArgument", err)
	}
	// A caller cannot forge the scope, and a genuinely authenticated foreign
	// scope must not see the composed tenant/organization's connection either.
	for _, scope := range []*commonv1.ScopeContext{
		{TenantId: "ironridge-demo", OrganizationScopeId: "org-default", Purpose: "integration.read"},
		{TenantId: tenant, OrganizationScopeId: "other-organization", Purpose: "integration.read"},
	} {
		t.Run(scope.TenantId+"/"+scope.OrganizationScopeId, func(t *testing.T) {
			foreignToken, issueErr := verifier.Issue(trust.Claims{
				Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: "operator:intg017-foreign", SubjectKind: "human",
				Tenant: scope.TenantId, OrganizationScopeID: scope.OrganizationScopeId, Purposes: []string{"integration.read"},
				AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session:intg017-foreign",
				IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
			})
			if issueErr != nil {
				t.Fatalf("issue foreign credential: %v", issueErr)
			}
			foreignCtx := metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+foreignToken)
			foreign, listErr := client.ListConnectorConnections(foreignCtx, &integrationv1.ListConnectorConnectionsRequest{Scope: scope})
			if listErr != nil || len(foreign.GetConnections()) != 0 {
				t.Fatalf("foreign scope connections=%v error=%v, want no visible connections", foreign.GetConnections(), listErr)
			}
		})
	}
}

func hasINTG017String(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
