// Package serve_test is the cross-system integration suite for the externally
// observable P1A cell. Unlike the bootstrap suite, it starts the command
// binary in a separate process and reaches its published HTTP/Connect edge
// over TCP.
package serve_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/clients"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const (
	serveTenant   = string(fixtures.Tenant)
	serveIssuer   = "https://issuer.serve-smoke.hcm-next.invalid"
	serveAudience = "hcm-next-serve-smoke"
	serveSubject  = "user-serve-smoke"
	serveOrg      = "org-north-america"
	serveKey      = "hcmnext-serve-smoke-signing-key-32-bytes+"
)

// TestTodo_NEXT_004_Integration is the external-process proof for NEXT-004.
// It deliberately does not reuse app.NewCell: an independently built process
// must migrate its own isolated schema, register the requested tenant, bind
// the advertised listeners, authenticate a credential, and serve the complete
// Create -> Get -> Simulate sequence over the real Connect edge.
func TestTodo_NEXT_004_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	grpcAddr := freeLoopbackAddr(t)
	httpAddr := freeLoopbackAddr(t)
	binary := buildHCMNext(t)
	databaseURL := schemaURL(t, db.URL, db.Schema)

	process := startServe(t, binary, databaseURL, grpcAddr, httpAddr)
	defer process.forceStop(t)

	token := mintToken(t)
	baseURL := "http://" + httpAddr
	httpClient := &http.Client{Timeout: 2 * time.Second}
	waitForPublishedEdge(t, process, httpClient, baseURL, token)

	intentClient := clients.NewIntentClientConnect(httpClient, baseURL)
	created := create(t, intentClient, token, "serve-smoke-idempotency-1")
	if created.GetIntentId() == "" {
		t.Fatal("CreateIntent returned an empty intent id")
	}
	if created.GetCanonicalRequestDigest().GetDigest() == "" {
		t.Fatal("CreateIntent returned no canonical request digest")
	}

	// The same client key must address one durable intent even through an
	// external process.  This also catches a process that accepted requests
	// before tenant/bootstrap migration state was usable.
	replay := create(t, intentClient, token, "serve-smoke-idempotency-1")
	if replay.GetIntentId() != created.GetIntentId() {
		t.Fatalf("idempotent create returned %q, want %q", replay.GetIntentId(), created.GetIntentId())
	}
	if replay.GetCanonicalRequestDigest().GetDigest() != created.GetCanonicalRequestDigest().GetDigest() {
		t.Fatalf("idempotent create changed request digest:\n got %s\nwant %s",
			replay.GetCanonicalRequestDigest().GetDigest(), created.GetCanonicalRequestDigest().GetDigest())
	}

	got := get(t, intentClient, token, created.GetIntentId())
	if got.GetIntentId() != created.GetIntentId() {
		t.Fatalf("GetIntent returned %q, want %q", got.GetIntentId(), created.GetIntentId())
	}
	if got.GetCanonicalRequestDigest().GetDigest() != created.GetCanonicalRequestDigest().GetDigest() {
		t.Fatalf("GetIntent changed request digest:\n got %s\nwant %s",
			got.GetCanonicalRequestDigest().GetDigest(), created.GetCanonicalRequestDigest().GetDigest())
	}

	first := simulate(t, intentClient, token, created.GetIntentId())
	second := simulate(t, intentClient, token, created.GetIntentId())
	if first.GetIntentId() != created.GetIntentId() || second.GetIntentId() != created.GetIntentId() {
		t.Fatalf("simulation did not remain bound to %q: first=%q second=%q",
			created.GetIntentId(), first.GetIntentId(), second.GetIntentId())
	}
	if first.GetMaterialProposalDigest().GetDigest() == "" {
		t.Fatalf("SimulateIntent returned no material proposal digest: %v", first)
	}
	if first.GetMaterialProposalDigest().GetDigest() != second.GetMaterialProposalDigest().GetDigest() {
		t.Fatalf("simulation digest drifted across identical calls:\n first %s\nsecond %s",
			first.GetMaterialProposalDigest().GetDigest(), second.GetMaterialProposalDigest().GetDigest())
	}
	if receipt := first.GetZeroEffectReceipt(); receipt == nil || !receipt.GetZeroEffect() {
		t.Fatalf("simulation did not return a zero-effect receipt: %v", receipt)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := process.stop(shutdownCtx); err != nil {
		t.Fatalf("hcmnext did not stop cleanly: %v\nprocess output:\n%s", err, process.output())
	}
}

func buildHCMNext(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	name := "hcmnext"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/hcmnext")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build cmd/hcmnext: %v\n%s", err, output)
	}
	return binary
}

// freeLoopbackAddr reserves an OS-selected TCP port only long enough to learn
// it. The process is then given the explicit address, so the test has no
// fixed port and emits the exact listener target in any failure output.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate loopback port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release loopback port %s: %v", addr, err)
	}
	return addr
}

func schemaURL(t *testing.T, rawURL, schema string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse pgtest URL: %v", err)
	}
	query := parsed.Query()
	// pgx treats unrecognised connection-string settings as PostgreSQL runtime
	// parameters, so this pins every pool and Goose connection in the child to
	// this test's schema rather than to public.
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type serveProcess struct {
	cmd      *exec.Cmd
	done     chan struct{}
	mu       sync.Mutex
	exit     error
	logs     lockedBuffer
	grpcAddr string
	httpAddr string
}

func startServe(t *testing.T, binary, databaseURL, grpcAddr, httpAddr string) *serveProcess {
	t.Helper()
	p := &serveProcess{done: make(chan struct{}), grpcAddr: grpcAddr, httpAddr: httpAddr}
	p.cmd = exec.Command(binary,
		"serve",
		"-grpc-listen="+grpcAddr,
		"-http-listen="+httpAddr,
		"-database-url="+databaseURL,
		"-dev-hmac-key="+serveKey,
		"-page-cursor-key=hcmnext-serve-smoke-cursor-signing-key-32+",
		"-issuer="+serveIssuer,
		"-audience="+serveAudience,
		"-tenant="+serveTenant,
		"-cell-id=serve-smoke-cell",
		"-execution-authority=false",
		"-scheduler=false",
		"-workspace=false",
		"-otel-exporter=none",
	)
	p.cmd.Stdout = &p.logs
	p.cmd.Stderr = &p.logs
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("start hcmnext grpc=%s http=%s: %v", grpcAddr, httpAddr, err)
	}
	go func() {
		err := p.cmd.Wait()
		p.mu.Lock()
		p.exit = err
		p.mu.Unlock()
		close(p.done)
	}()
	return p
}

func (p *serveProcess) exited() (bool, error) {
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return true, p.exit
	default:
		return false, nil
	}
}

func (p *serveProcess) output() string { return p.logs.String() }

func (p *serveProcess) stop(ctx context.Context) error {
	if exited, exitErr := p.exited(); exited {
		return exitErr
	}
	// os.Process.Signal(os.Interrupt) is not a dependable child-process
	// control mechanism on Windows. The command has no authenticated shutdown
	// endpoint (correctly: P1A exposes no operator bypass), so use bounded
	// process termination there and prove the important external cleanup
	// property: both explicitly allocated listeners are released and reusable.
	if runtime.GOOS == "windows" {
		if err := p.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("terminate Windows child: %w", err)
		}
		select {
		case <-p.done:
			return p.assertListenersReleased()
		case <-ctx.Done():
			return fmt.Errorf("wait for Windows process termination: %w", ctx.Err())
		}
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("send interrupt: %w", err)
	}
	select {
	case <-p.done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.exit
	case <-ctx.Done():
		return fmt.Errorf("wait for graceful shutdown: %w", ctx.Err())
	}
}

func (p *serveProcess) assertListenersReleased() error {
	grpc, err := net.Listen("tcp", p.grpcAddr)
	if err != nil {
		return fmt.Errorf("gRPC listener %s remains reserved after process exit: %w", p.grpcAddr, err)
	}
	defer func() { _ = grpc.Close() }()
	http, err := net.Listen("tcp", p.httpAddr)
	if err != nil {
		return fmt.Errorf("HTTP listener %s remains reserved after process exit: %w", p.httpAddr, err)
	}
	return http.Close()
}

func (p *serveProcess) forceStop(t *testing.T) {
	t.Helper()
	if exited, _ := p.exited(); exited {
		return
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Logf("hcmnext process did not exit after forced cleanup")
	}
}

func mintToken(t *testing.T) string {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(serveKey),
		Issuer:   serveIssuer,
		Audience: serveAudience,
	})
	if err != nil {
		t.Fatalf("configure development token verifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer:               serveIssuer,
		Audience:             serveAudience,
		Subject:              serveSubject,
		SubjectKind:          "human",
		Tenant:               serveTenant,
		OrganizationScopeID:  serveOrg,
		Roles:                []string{"intent_author", "comp_admin"},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{"compensation_review"},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-serve-smoke",
		IssuedAtUnix:         time.Now().Add(-time.Minute).Unix(),
		ExpiresAtUnix:        time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("mint development token: %v", err)
	}
	return token
}

func waitForPublishedEdge(t *testing.T, process *serveProcess, httpClient *http.Client, baseURL, token string) {
	t.Helper()
	registry := clients.NewRegistryClientConnect(httpClient, baseURL)
	deadline := time.Now().Add(45 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if exited, exitErr := process.exited(); exited {
			t.Fatalf("hcmnext exited before its edge became ready: %v\nprocess output:\n%s", exitErr, process.output())
		}
		callCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		response, err := registry.ListIntentDefinitions(callCtx, &registryv1.ListIntentDefinitionsRequest{}, authenticated(token))
		cancel()
		if err == nil && len(response.GetIntentDefinitions()) == 14 {
			return
		}
		if err == nil {
			lastErr = fmt.Errorf("registry published %d definitions, want 14", len(response.GetIntentDefinitions()))
		} else {
			lastErr = err
		}
		select {
		case <-time.After(25 * time.Millisecond):
		case <-process.done:
		}
	}
	t.Fatalf("timed out waiting for published Connect edge at %s: %v\nprocess output:\n%s", baseURL, lastErr, process.output())
}

func authenticated(token string) clients.CallOption {
	return clients.WithHeader(transport.AuthorizationMetadataKey, "Bearer "+token)
}

func create(t *testing.T, client clients.IntentClient, token, idempotencyKey string) *intentsv1.IntentInstance {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := client.CreateIntent(ctx, promotionRequest(t, idempotencyKey), authenticated(token))
	if err != nil {
		t.Fatalf("CreateIntent through Connect edge: %v", err)
	}
	return response.GetIntent()
}

func get(t *testing.T, client clients.IntentClient, token, id string) *intentsv1.IntentInstance {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := client.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: id}, authenticated(token))
	if err != nil {
		t.Fatalf("GetIntent through Connect edge: %v", err)
	}
	return response.GetIntent()
}

func simulate(t *testing.T, client clients.IntentClient, token, id string) *intentsv1.SimulationArtifact {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := client.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: id}, authenticated(token))
	if err != nil {
		t.Fatalf("SimulateIntent through Connect edge: %v", err)
	}
	return response.GetSimulation()
}

func promotionRequest(t *testing.T, idempotencyKey string) *intentsv1.CreateIntentRequest {
	t.Helper()
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("resolve fixture worker: %v", err)
	}
	payload := mustStruct(t, map[string]any{
		"worker_ref": "omar-reyes", "known_at": "2026-05-15",
		// This authority-free API smoke uses the compiled corpus rather than
		// the development workspace's PostgreSQL workforce. A position id
		// would therefore claim a durable Position-domain fact the process did
		// not compose; leaving it absent exercises the certified corpus vector.
		"target":         map[string]any{"job_code": "OPS-HRBP3", "grade": "P3", "org_unit": "people-ops", "pay_zone": "US-EAST"},
		"effective_date": "2026-06-01", "evaluation_date": "2026-05-15", "business_reason": "promotion_into_senior_hrbp",
		"current":  map[string]any{"base": "93000.00", "currency": "USD", "pay_basis": "ANNUAL_SALARY", "bonus_target": "0.0500", "effective_date": "2026-06-01", "revision_stream": "rewards.package.omar", "revision_sequence": "11"},
		"proposed": map[string]any{"base": "98000.00", "currency": "USD", "pay_basis": "ANNUAL_SALARY", "bonus_target": "0.0500", "effective_date": "2026-06-01", "revision_stream": "rewards.package.omar", "revision_sequence": "11"},
		"budget":   map[string]any{"available_amount": "50000.00", "currency": "USD", "owner_system": "adaptive.planning", "policy_ref": "finance.authority/2026.1", "scope": "people-ops:FY26-merit", "period": "FY2026", "baseline_version": "fy26-merit-r7", "observation_id": "obs_budget_fy26_merit_r7"},
	})
	return &intentsv1.CreateIntentRequest{
		IdempotencyKey: idempotencyKey,
		Definition:     &intentsv1.DefinitionReference{IntentTypeId: promotion.IntentType, Version: 1},
		Subjects: []*intentsv1.SubjectReference{
			{SubjectKind: "EMPLOYMENT", SubjectId: worker.Id, AuthorityDomain: "PEOPLE"},
		},
		Request: &intentsv1.TypedPayload{Schema: &intentsv1.SchemaReference{
			SchemaId: "hcmnext.people.v1.PromoteWorkerRequest", Version: 1, ProtobufFullName: "hcmnext.people.v1.PromoteWorkerRequest",
		}, ProtobufWireBytes: payload},
		ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
	}
}

func mustStruct(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	value, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatalf("encode request payload: %v", err)
	}
	wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(value)
	if err != nil {
		t.Fatalf("marshal request payload: %v", err)
	}
	return wire
}
