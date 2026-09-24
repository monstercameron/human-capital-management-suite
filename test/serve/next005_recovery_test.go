package serve_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/clients"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestTodo_NEXT_005_Recovery proves an intent accepted by the real process
// remains readable and simulatable after that process stops and a new process
// migrates and serves the same isolated PostgreSQL schema.
func TestTodo_NEXT_005_Recovery(t *testing.T) {
	db := pgtest.NewEmpty(t)
	grpcAddr := freeLoopbackAddr(t)
	httpAddr := freeLoopbackAddr(t)
	binary := buildRecoveryHCMNext(t)
	databaseURL := schemaURL(t, db.URL, db.Schema)
	token := mintToken(t)
	httpClient := &http.Client{Timeout: 3 * time.Second}
	baseURL := "http://" + httpAddr
	client := clients.NewIntentClientConnect(httpClient, baseURL)

	first := startServe(t, binary, databaseURL, grpcAddr, httpAddr)
	defer first.forceStop(t)
	waitForPublishedEdge(t, first, httpClient, baseURL, token)
	created := createRecoveryIntent(t, client, token, "next005-recovery-key")
	firstSimulation := simulate(t, client, token, created.GetIntentId())
	if firstSimulation.GetZeroEffectReceipt() == nil || !firstSimulation.GetZeroEffectReceipt().GetZeroEffect() {
		t.Fatalf("first process did not return the P1A zero-effect receipt: %v", firstSimulation.GetZeroEffectReceipt())
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	if err := first.stop(stopCtx); err != nil {
		cancel()
		t.Fatalf("first hcmnext process did not stop cleanly: %v\n%s", err, first.output())
	}
	cancel()

	second := startServe(t, binary, databaseURL, grpcAddr, httpAddr)
	defer second.forceStop(t)
	waitForPublishedEdge(t, second, httpClient, baseURL, token)
	recovered := get(t, client, token, created.GetIntentId())
	if recovered.GetIntentId() != created.GetIntentId() ||
		recovered.GetCanonicalRequestDigest().GetDigest() != created.GetCanonicalRequestDigest().GetDigest() {
		t.Fatalf("restart changed the durable request identity: before=%s/%s after=%s/%s",
			created.GetIntentId(), created.GetCanonicalRequestDigest().GetDigest(),
			recovered.GetIntentId(), recovered.GetCanonicalRequestDigest().GetDigest())
	}
	secondSimulation := simulate(t, client, token, created.GetIntentId())
	if secondSimulation.GetMaterialProposalDigest().GetDigest() != firstSimulation.GetMaterialProposalDigest().GetDigest() {
		t.Fatalf("restart changed deterministic proposal digest: before=%s after=%s",
			firstSimulation.GetMaterialProposalDigest().GetDigest(), secondSimulation.GetMaterialProposalDigest().GetDigest())
	}
	if receipt := secondSimulation.GetZeroEffectReceipt(); receipt == nil || !receipt.GetZeroEffect() {
		t.Fatalf("recovered simulation lost its zero-effect receipt: %v", receipt)
	}
}

func buildRecoveryHCMNext(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	artifactDir := filepath.Join(root, ".artifacts", "bin")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact bin directory: %v", err)
	}
	binary := filepath.Join(artifactDir, "hcmnext-next005-recovery.exe")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/hcmnext")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build cmd/hcmnext: %v\n%s", err, output)
	}
	return binary
}

func createRecoveryIntent(t *testing.T, client clients.IntentClient, token, idempotencyKey string) *intentsv1.IntentInstance {
	t.Helper()
	request := promotionRequest(t, idempotencyKey)
	payload := &structpb.Struct{}
	if err := proto.Unmarshal(request.GetRequest().GetProtobufWireBytes(), payload); err != nil {
		t.Fatalf("decode recovery request: %v", err)
	}
	target := payload.GetFields()["target"].GetStructValue()
	if target == nil {
		t.Fatal("recovery request has no target")
	}
	target.GetFields()["position_id"] = structpb.NewStringValue(recoveryPositionRef(t))
	wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(payload)
	if err != nil {
		t.Fatalf("encode recovery request: %v", err)
	}
	request.GetRequest().ProtobufWireBytes = wire
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := client.CreateIntent(ctx, request, authenticated(token))
	if err != nil {
		t.Fatalf("CreateIntent through Connect edge: %v", err)
	}
	return response.GetIntent()
}

func recoveryPositionRef(t *testing.T) string {
	t.Helper()
	catalog, err := fixtures.NewMemoryPositionCatalog()
	if err != nil {
		t.Fatalf("NewMemoryPositionCatalog: %v", err)
	}
	ref, ok := catalog.PositionRefForCode("POS-HRBP-301")
	if !ok {
		t.Fatal("POS-HRBP-301 is missing from the governed fixture catalog")
	}
	effective, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	revision, exists, err := catalog.PositionRevisionAt(context.Background(), position.PositionQuery{
		Tenant: fixtures.Tenant, Position: ref, AsOf: position.AsOf{EffectiveOn: effective, KnownAt: knownAt},
	})
	if err != nil || !exists {
		t.Fatalf("PositionRevisionAt exists=%v err=%v", exists, err)
	}
	encoded, err := position.EncodeRevisionRef(ref, revision.Revision)
	if err != nil {
		t.Fatalf("EncodeRevisionRef: %v", err)
	}
	return encoded.String()
}
