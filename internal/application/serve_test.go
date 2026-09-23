package application

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// stubServeConfig is the configuration every composition test in this package
// starts from: real defaults, loopback listeners on an OS-chosen port, no
// migration (the seam that would need a database) and telemetry off.
func stubServeConfig() ServeConfig {
	return ServeConfig{
		GRPCListen:    "127.0.0.1:0",
		HTTPListen:    "127.0.0.1:0",
		DatabaseURL:   "postgres://stub.invalid/hcmnext",
		DevHMACKey:    testDevKey,
		PageCursorKey: testPageCursorKey,
		Issuer:        DefaultIssuer,
		Audience:      DefaultAudience,
		CellID:        "cell-composition-test",
		MaxDeadline:   30 * time.Second,
		Migrate:       false,
		Workspace:     true,
		OTelExporter:  OTelExporterNone,
	}
}

func TestTodo_PROMOUX_014_LocalDevClockIsBoundedToTheDevelopmentProfile(t *testing.T) {
	t.Parallel()

	const instant = "2026-12-01T12:00:00-05:00"
	cfg := stubServeConfig()
	cfg.Profile = ServeProfileLocalDev
	cfg.LocalDevNow = instant

	options, err := optionsForServeConfig(cfg, Options{})
	if err != nil {
		t.Fatalf("optionsForServeConfig(local-dev): %v", err)
	}
	want := time.Date(2026, time.December, 1, 17, 0, 0, 0, time.UTC)
	if options.Now == nil {
		t.Fatal("optionsForServeConfig returned no local development clock")
	}
	if got := options.Now(); !got.Equal(want) {
		t.Fatalf("local development clock = %v, want %v", got, want)
	}

	explicit := func() time.Time { return time.Date(2031, time.January, 2, 3, 4, 5, 0, time.UTC) }
	options, err = optionsForServeConfig(cfg, Options{Now: explicit})
	if err != nil {
		t.Fatalf("optionsForServeConfig(explicit test seam): %v", err)
	}
	if got := options.Now(); !got.Equal(explicit()) {
		t.Fatalf("explicit clock = %v, want %v", got, explicit())
	}

	production := cfg
	production.Profile = ServeProfileStandard
	if _, err := optionsForServeConfig(production, Options{}); err == nil {
		t.Fatal("optionsForServeConfig accepted a production clock override")
	}
	malformed := cfg
	malformed.LocalDevNow = "tomorrow"
	if _, err := optionsForServeConfig(malformed, Options{}); err == nil {
		t.Fatal("optionsForServeConfig accepted a malformed clock override")
	}
}

// composeStub composes the serve role with persistence and admission
// replaced. It is the whole point of Options: this is the deployed wiring,
// with two adapters swapped and no branch anywhere that knows it is a test.
func composeStub(t *testing.T, cfg ServeConfig, opts ...Option) (*App, *recordingLogger, *stubStore) {
	t.Helper()
	logger := &recordingLogger{}
	store := &stubStore{}
	base := []Option{WithStore(store), WithVerifier(stubVerifier{})}
	options := Options{}.Apply(append(base, opts...)...)
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config:   cfg,
		Logger:   logger,
		Identity: "instance-composition-test",
		Options:  options,
	})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := composed.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return composed, logger, store
}

// TestComposeServeBuildsTheWholeRoleFromOneConfigValue is the GREEN of
// ARCH-GO-020 for the serve role: registries, governance, engines, ports,
// adapters, transports and the workloads all come out of one validated
// configuration and one Options value.
func TestComposeServeBuildsTheWholeRoleFromOneConfigValue(t *testing.T) {
	composed, logger, store := composeStub(t, stubServeConfig())

	cell := composed.Cell()
	if cell == nil {
		t.Fatal("ComposeServe returned no cell")
	}
	if cell.Definitions == nil || cell.Definitions.Len() == 0 {
		t.Error("the composed cell has no intent definitions registry")
	}
	if cell.Capabilities == nil || len(cell.Capabilities.List()) == 0 {
		t.Error("the composed cell has no capability registry")
	}
	if cell.Gateway == nil {
		t.Error("the composed cell has no governed capability gateway")
	}
	if cell.Service == nil {
		t.Error("the composed cell has no application service")
	}
	if cell.Evidence == nil {
		t.Error("the composed cell has no evidence sink")
	}
	if !cell.WorkspaceEnabled() {
		t.Error("the composed cell does not serve the workspace, though -workspace defaulted on")
	}
	// Store returns a struct value, so a nil comparison could never fail;
	// keep the registration call for its effect.
	_ = app.Store(store)
	if len(store.tenants()) != 0 {
		t.Errorf("the composition registered %v with no -tenant configured", store.tenants())
	}
	if composed.GRPCAddr() == "" || composed.HTTPAddr() == "" {
		t.Errorf("addresses = %q/%q, want two bound listeners", composed.GRPCAddr(), composed.HTTPAddr())
	}
	if strings.HasSuffix(composed.GRPCAddr(), ":0") {
		t.Errorf("gRPC listener reported %q, want the port it actually bound", composed.GRPCAddr())
	}
	if !logger.saw("hcmnext.serving") {
		t.Error("the composition never announced what it published")
	}

	runtime := composed.Runtime()
	if len(runtime.Workloads) != 2 {
		t.Fatalf("composed %d workloads, want the gRPC surface and the HTTP edge", len(runtime.Workloads))
	}
	if runtime.Workloads[0].Name != workloadNameGRPC || runtime.Workloads[1].Name != workloadNameHTTP {
		t.Errorf("workloads = %q/%q, want %q/%q",
			runtime.Workloads[0].Name, runtime.Workloads[1].Name, workloadNameGRPC, workloadNameHTTP)
	}
	wantShutdown := []string{shutdownNameHTTP, shutdownNameGRPC, shutdownNameChat, shutdownNameTelemetry}
	if len(runtime.Shutdown) != len(wantShutdown) {
		t.Fatalf("composed %d shutdown steps, want %d", len(runtime.Shutdown), len(wantShutdown))
	}
	for i, want := range wantShutdown {
		if runtime.Shutdown[i].Name != want {
			t.Errorf("shutdown step %d = %q, want %q", i, runtime.Shutdown[i].Name, want)
		}
	}
}

// TestComposeServeRegistersTheConfiguredTenant proves the one start-time
// write the role performs is driven by configuration, not by a package that
// registered itself.
func TestComposeServeRegistersTheConfiguredTenant(t *testing.T) {
	cfg := stubServeConfig()
	cfg.Tenant = "harborcare-demo"
	_, logger, store := composeStub(t, cfg)
	if got := store.tenants(); len(got) != 1 || got[0] != "harborcare-demo" {
		t.Errorf("registered tenants = %v, want [harborcare-demo]", got)
	}
	if !logger.saw("hcmnext.tenant_registered") {
		t.Error("the tenant registration was not announced")
	}
}

// TestComposeServeRefusesWhenAConfiguredDependencyIsAbsent proves the
// composition fails loudly rather than degrading. A -migrate=true role with
// no migrator adapter is the case that matters: silently skipping the schema
// would start a listener against a database it does not match.
func TestComposeServeRefusesWhenAConfiguredDependencyIsAbsent(t *testing.T) {
	cfg := stubServeConfig()
	cfg.Migrate = true
	_, err := ComposeServe(context.Background(), ServeInput{
		Config:  cfg,
		Options: Options{}.Apply(WithStore(&stubStore{}), WithVerifier(stubVerifier{})),
	})
	if err == nil {
		t.Fatal("ComposeServe started a role with migrations on and no migrator")
	}
	if !strings.Contains(err.Error(), FieldMigrate) {
		t.Errorf("error = %q, want it to name -%s", err, FieldMigrate)
	}

	sentinel := errors.New("schema apply failed")
	_, err = ComposeServe(context.Background(), ServeInput{
		Config: cfg,
		Options: Options{}.Apply(WithStore(&stubStore{}), WithVerifier(stubVerifier{}),
			WithMigrator(func(context.Context, string, bootstrap.Logger) error { return sentinel })),
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("a failing migration produced %v, want %v", err, sentinel)
	}
}

// TestComposeServeRefusesAFactoryThatSuppliesNothing guards the seam itself:
// a supplied factory that returns no adapter must not compose a role with a
// nil port in it.
func TestComposeServeRefusesAFactoryThatSuppliesNothing(t *testing.T) {
	cfg := stubServeConfig()
	_, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg,
		Options: Options{}.Apply(
			WithStoreFactory(func(*pgxadapter.Pool, ServeConfig) (app.Store, error) { return nil, nil }),
			WithVerifier(stubVerifier{})),
	})
	if err == nil || !strings.Contains(err.Error(), "store factory") {
		t.Errorf("nil store = %v, want a refusal naming the store factory", err)
	}

	_, err = ComposeServe(context.Background(), ServeInput{
		Config: cfg,
		Options: Options{}.Apply(WithStore(&stubStore{}),
			WithVerifier(nil)),
	})
	if err == nil || !strings.Contains(err.Error(), "verifier factory") {
		t.Errorf("nil verifier = %v, want a refusal naming the verifier factory", err)
	}
}

func TestTodo_LEGAL_014_CompositionPreservesDisabledStateAndRequiresStorage(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())
	component, ok := composed.Graph().Component(ComponentLegalEvidenceVerifier)
	if !ok || component.Impl != "<nil>" {
		t.Fatalf("legal verifier without trusted keys = %+v, want an explicit nil component", component)
	}

	cfg := stubServeConfig()
	cfg.LegalEvidenceIssuerKeys = base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	_, err := ComposeServe(context.Background(), ServeInput{
		Config:  cfg,
		Options: Options{}.Apply(WithStore(&stubStore{}), WithVerifier(stubVerifier{})),
	})
	if err == nil || !strings.Contains(err.Error(), "database pool") {
		t.Fatalf("legal verifier without storage = %v, want database-pool refusal", err)
	}
}

// TestComposeServeBuildsTheRealVerifierFromConfiguration exercises the
// production adapter path: with no verifier seam supplied, the signing key
// and issuer in the configuration are what the listener authenticates with,
// and a key the trust package rejects is a composition failure.
func TestComposeServeBuildsTheRealVerifierFromConfiguration(t *testing.T) {
	cfg := stubServeConfig()
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config:  cfg,
		Options: Options{}.Apply(WithStore(&stubStore{})),
	})
	if err != nil {
		t.Fatalf("ComposeServe with the production verifier: %v", err)
	}
	t.Cleanup(func() {
		if err := composed.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	cfg.DevHMACKey = ""
	_, err = ComposeServe(context.Background(), ServeInput{
		Config:  cfg,
		Options: Options{}.Apply(WithStore(&stubStore{})),
	})
	if err == nil || !strings.Contains(err.Error(), "credential verifier") {
		t.Errorf("empty signing key = %v, want a refusal naming the credential verifier", err)
	}
}

// TestComposeServeStartsAndDrainsBothSurfaces drives the composed lifecycle
// end to end over real loopback sockets: the HTTP edge answers while running
// and refuses connections once the ordered shutdown has drained it.
func TestComposeServeStartsAndDrainsBothSurfaces(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())
	if err := composed.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := "http://" + composed.HTTPAddr() + app.DiscoveryPath
	var resp *http.Response
	var err error
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = client.Get(url)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if resp.StatusCode >= 500 {
		t.Errorf("GET %s = %d, want the edge to answer", url, resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := composed.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := net.DialTimeout("tcp", composed.HTTPAddr(), 2*time.Second); err == nil {
		t.Error("the HTTP edge still accepts connections after shutdown")
	}
}

// TestComposeServeSurfacesAListenerFailure proves a bound-port conflict is a
// startup failure with the address in it, and that the first listener is
// released rather than leaked when the second one fails.
func TestComposeServeSurfacesAListenerFailure(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = held.Close() }()

	cfg := stubServeConfig()
	cfg.HTTPListen = held.Addr().String()
	opened := 0
	_, err = ComposeServe(context.Background(), ServeInput{
		Config: cfg,
		Options: Options{}.Apply(WithStore(&stubStore{}), WithVerifier(stubVerifier{}),
			WithListener(func(network, address string) (net.Listener, error) {
				opened++
				return net.Listen(network, address)
			})),
	})
	if err == nil {
		t.Fatal("ComposeServe bound a port that was already taken")
	}
	if !strings.Contains(err.Error(), cfg.HTTPListen) {
		t.Errorf("error = %q, want it to name %s", err, cfg.HTTPListen)
	}
	if opened != 2 {
		t.Errorf("the composition opened %d listeners, want it to try both", opened)
	}
}

// TestRequestLoggerRecordsTheContractFields pins the per-request record the
// composition installs on the cell.
func TestRequestLoggerRecordsTheContractFields(t *testing.T) {
	logger := &recordingLogger{}
	RequestLogger(logger)(transport.LogRecord{Method: "CreateIntent", Transport: "grpc"})
	if !logger.saw("hcmnext.request") {
		t.Error("a completed request produced no record")
	}
	// A composition with no logger must not panic on the request path: the
	// request log is observational, and losing it never changes an answer.
	RequestLogger(nil)(transport.LogRecord{})
}

// TestDiscardLoggerSatisfiesTheLoggerPort covers the fallback the composition
// uses when a caller supplies none, which is what keeps every log call site
// in ComposeServe unconditional.
func TestDiscardLoggerSatisfiesTheLoggerPort(t *testing.T) {
	var logger bootstrap.Logger = discardLogger{}
	logger.Info("ignored", "k", "v")
	logger.Error("ignored", "k", "v")
}
