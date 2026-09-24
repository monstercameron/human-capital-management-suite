package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_REV_030_01_Integration drives publication, registry resolution and
// StageCSV through the deployed HTTP/gRPC composition over real PostgreSQL.
func TestTodo_REV_030_01_Integration(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	tenant := string(fixtures.Tenant)
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: integrationSigningKey, PageCursorKey: integrationPageCursorKey,
		Issuer: DefaultIssuer, Audience: DefaultAudience, Tenant: tenant, CellID: "cell-rev03001",
		MaxDeadline: 30 * time.Second, Migrate: false, Workspace: false, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:rev03001-execution-authority",
		ExecutionAuthorityRole: "promotion_operator", WorkflowPlan: WorkflowPlanPrototype,
		TimerTzdbVersion: DefaultTimerTzdbVersion, TimerCalendarVersion: DefaultTimerCalendarVersion,
		LegalEvidenceIssuerKeys: base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)),
	}
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "rev03001-served-integration"})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := composed.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	definition := fakeincumbent.DefaultDefinition()
	if err := composed.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	now := time.Now().UTC()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("build test verifier: %v", err)
	}
	issueToken := func(roles ...string) string {
		t.Helper()
		token, issueErr := verifier.Issue(trust.Claims{
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: "operator:rev03001", SubjectKind: "human", Tenant: tenant,
			Roles:    roles,
			Purposes: []string{"integration.read"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: "session:rev03001", IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
		})
		if issueErr != nil {
			t.Fatalf("issue test credential: %v", issueErr)
		}
		return token
	}
	definitionBody, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal connector definition: %v", err)
	}
	requestPublish := func(token string, body []byte) *http.Response {
		t.Helper()
		req, requestErr := http.NewRequest(http.MethodPost, "http://"+composed.HTTPAddr()+IntegrationDefinitionPublishPath, bytes.NewReader(body))
		if requestErr != nil {
			t.Fatalf("build publish request: %v", requestErr)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			t.Fatalf("publish request: %v", requestErr)
		}
		return response
	}
	response := requestPublish(issueToken(), definitionBody)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin publication status = %d, want 403", response.StatusCode)
	}
	// The caller cannot choose the tenant scope through the definition body.
	forged := append([]byte(nil), definitionBody[:len(definitionBody)-1]...)
	forged = append(forged, []byte(`,"tenant_id":"other-tenant"}`)...)
	response = requestPublish(issueToken("hcm_admin"), forged)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged tenant publication status = %d, want 400", response.StatusCode)
	}
	response = requestPublish(issueToken("hcm_admin"), definitionBody)
	var publication struct {
		ConnectorID string `json:"connector_id"`
		Version     string `json:"version"`
		Digest      string `json:"digest"`
		PublishedBy string `json:"published_by"`
	}
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("admin publication status = %d, body %s; want 201", response.StatusCode, body)
	}
	if err := json.NewDecoder(response.Body).Decode(&publication); err != nil {
		t.Fatalf("decode publication receipt: %v", err)
	}
	response.Body.Close()
	if publication.ConnectorID != definition.ConnectorID || publication.Version != definition.Version.String() || publication.Digest == "" || publication.PublishedBy != "operator:rev03001" {
		t.Fatalf("publication receipt = %+v, want authenticated immutable publication metadata", publication)
	}
	changedDefinition := definition
	changedDefinition.Product += " altered"
	changedBody, err := json.Marshal(changedDefinition)
	if err != nil {
		t.Fatalf("marshal changed connector definition: %v", err)
	}
	response = requestPublish(issueToken("hcm_admin"), changedBody)
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("altered same-version publication status = %d, want 409", response.StatusCode)
	}
	token := issueToken("hcm_admin")
	conn, err := grpc.NewClient(composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial composed gRPC: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := metadata.AppendToOutgoingContext(context.Background(), transport.AuthorizationMetadataKey, "Bearer "+token)

	definitions, err := integrationv1.NewIntegrationServiceClient(conn).ListConnectorDefinitions(ctx, &integrationv1.ListConnectorDefinitionsRequest{
		Scope: &commonv1.ScopeContext{TenantId: tenant, Purpose: "integration.read"},
	})
	if err != nil {
		t.Fatalf("ListConnectorDefinitions through composed server: %v", err)
	}
	if len(definitions.GetConnectorDefinitions()) != 1 || definitions.GetConnectorDefinitions()[0].GetConnectorId() != definition.ConnectorID {
		t.Fatalf("published definitions = %+v, want the one durable INTG-001 publication", definitions.GetConnectorDefinitions())
	}

	stream, err := dataopsv1.NewDataOpsServiceClient(conn).StageCSV(ctx)
	if err != nil {
		t.Fatalf("open StageCSV stream: %v", err)
	}
	if err := stream.Send(&dataopsv1.StageCSVRequest{SourceUri: "upload://rev03001", IdempotencyKey: "rev03001-stage", CsvChunk: []byte("worker_id,name\nworker-rev03001,Ada\n")}); err != nil {
		t.Fatalf("send StageCSV frame: %v", err)
	}
	receipt, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("StageCSV through composed server: %v", err)
	}
	staged := receipt.GetStagedImport()
	if staged == nil || staged.GetTenantId() != pgstore.TenantID(tenant).String() || staged.GetRowCount() != 1 || staged.GetBatchId() == "" {
		t.Fatalf("StageCSV receipt = %+v, want durable one-row batch bound to authenticated tenant", staged)
	}
}
