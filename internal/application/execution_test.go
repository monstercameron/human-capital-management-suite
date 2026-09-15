package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// executionServeConfig is a serve configuration with the P1B gate on and
// every field the gate requires supplied.
func executionServeConfig() ServeConfig {
	cfg := stubServeConfig()
	cfg.ExecutionAuthority = true
	cfg.ExecutionAuthorityDigest = "sha256:signed-p1b-amendment"
	cfg.ExecutionAuthorityRole = "promotion_operator"
	cfg.ExecutionApprover = "principal:promotion-approver"
	return cfg
}

// TestComposeExecutionAuthorityFillsOnlyTheExecutionShapedFields is the
// composition contract of the P1B gate: it is additive wiring over a
// CellConfig the root already built, never a second cell configuration.
func TestComposeExecutionAuthorityFillsOnlyTheExecutionShapedFields(t *testing.T) {
	evidence := app.NewMemoryEvidenceSink()
	cellConfig := app.CellConfig{Audience: "aud", ExecutionCellID: "", Evidence: evidence}
	cfg := executionServeConfig()

	if err := ComposeExecutionAuthority(&cellConfig, versionTestPool(t), evidence, cfg); err != nil {
		t.Fatalf("ComposeExecutionAuthority: %v", err)
	}
	if cellConfig.Audience != "aud" {
		t.Error("the execution composer rewrote a field it does not own")
	}
	if cellConfig.ExecutionAuthority == nil {
		t.Error("no execution authority gate was composed")
	}
	if cellConfig.Executor == nil || cellConfig.ExecutionResolver == nil || cellConfig.ExecutionVersions == nil {
		t.Error("the executor was composed without its resolver and version store, which leaves EXECUTE unavailable past the gate")
	}
	if cellConfig.TenantUUID == nil {
		t.Error("no tenant derivation was composed; the driver and the store would disagree about which row a tenant names")
	}
	if cellConfig.ExecutionCellID != cfg.CellID {
		t.Errorf("ExecutionCellID = %q, want the configured %q", cellConfig.ExecutionCellID, cfg.CellID)
	}
	if cellConfig.ExecutionApprover != cfg.ExecutionApprover {
		t.Errorf("ExecutionApprover = %q, want %q; a disagreement here is refused by the approval step",
			cellConfig.ExecutionApprover, cfg.ExecutionApprover)
	}
}

// TestComposeExecutionAuthorityRefusesWithNoCellConfiguration guards the one
// call shape that would silently compose nothing.
func TestComposeExecutionAuthorityRefusesWithNoCellConfiguration(t *testing.T) {
	err := ComposeExecutionAuthority(nil, nil, app.NewMemoryEvidenceSink(), executionServeConfig())
	if err == nil {
		t.Fatal("ComposeExecutionAuthority accepted no cell configuration")
	}
	if !strings.Contains(err.Error(), "cell configuration") {
		t.Errorf("error = %q, want it to name the missing cell configuration", err)
	}
}

// TestTenantKeyMapperAgreesWithTheComposedStore covers the derivation both
// the execution driver and the operator surface's workflow-instance reader
// are built with. If it ever stopped agreeing with the store's own tenant
// table, a governed execution would run against a row nobody else can see.
//
// The mapper is generic so this root never writes the store's row-key type
// down; the test is what proves that indirection still lands on the store's
// own answer.
func TestTenantKeyMapperAgreesWithTheComposedStore(t *testing.T) {
	mapper := tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)
	tenant := kernelvalues.TenantId("harborcare-demo")
	if got, want := mapper(tenant), pgstore.TenantID(string(tenant)); got != want {
		t.Errorf("the composed mapper derives %s for %q, want the store's %s", got, tenant, want)
	}
	if mapper("a") == mapper("b") {
		t.Error("two tenants derive the same row")
	}
	firstDerivation, secondDerivation := mapper(tenant), mapper(tenant)
	if firstDerivation != secondDerivation {
		t.Error("the derivation is not stable")
	}

	// The two call sites in this package build the mapper the same way; a
	// second construction has to agree with the first, or the driver and the
	// operator read would disagree about which row a tenant names.
	second := tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)
	if second(tenant) != mapper(tenant) {
		t.Error("two constructions of the tenant mapper disagree")
	}
}

// TestComposeServeOffByDefaultAndGatedOnRequest is the P1A/P1B boundary as
// the composition sees it: with no -execution-authority the composed cell
// carries no gate at all, and with it the gate, the executor and the journey
// are all present. A cell that could execute by default would be a P1B cell
// shipped under a P1A gate.
func TestComposeServeOffByDefaultAndGatedOnRequest(t *testing.T) {
	p1a, _, _ := composeStub(t, stubServeConfig())
	for _, name := range []string{
		ComponentExecutionAuthority, ComponentProposalExecutor,
		ComponentWorkflowResolver, ComponentWorkflowVersions,
	} {
		component, ok := p1a.Graph().Component(name)
		if !ok {
			t.Fatalf("the graph does not record %q at all", name)
		}
		if component.Impl != "<nil>" {
			t.Errorf("%s = %q on a cell composed with no -%s, want absent",
				name, component.Impl, FieldExecutionAuthority)
		}
	}

	composedCalls := 0
	pool := versionTestPool(t)
	gated, logger, _ := composeStub(t, executionServeConfig(),
		WithExecutionComposer(func(cellConfig *app.CellConfig, _ *pgxadapter.Pool, evidence *app.MemoryEvidenceSink, cfg ServeConfig) error {
			composedCalls++
			return ComposeExecutionAuthority(cellConfig, pool, evidence, cfg)
		}))
	if composedCalls != 1 {
		t.Fatalf("the execution composer ran %d times, want exactly once", composedCalls)
	}
	if !logger.saw("hcmnext.execution_authority_enabled") {
		t.Error("the composition did not announce that the P1B gate was composed")
	}
	for _, name := range []string{
		ComponentExecutionAuthority, ComponentProposalExecutor,
		ComponentWorkflowResolver, ComponentWorkflowVersions,
	} {
		component, ok := gated.Graph().Component(name)
		if !ok || component.Impl == "<nil>" {
			t.Errorf("%s = %+v on a gated cell, want a composed component", name, component)
		}
	}
	if gated.Graph().Digest() == p1a.Graph().Digest() {
		t.Error("the gated and ungated compositions digest the same")
	}
}

// TestComposeServeReportsAFailingExecutionComposer proves the gate is a
// startup decision: a cell that was asked to execute and could not compose
// the authority must not start as though it had.
func TestComposeServeReportsAFailingExecutionComposer(t *testing.T) {
	_, err := ComposeServe(context.Background(), ServeInput{
		Config: executionServeConfig(),
		Options: Options{}.Apply(WithStore(&stubStore{}), WithVerifier(stubVerifier{}),
			WithExecutionComposer(func(*app.CellConfig, *pgxadapter.Pool, *app.MemoryEvidenceSink, ServeConfig) error {
				return errNoAuthority
			})),
	})
	if err == nil {
		t.Fatal("ComposeServe started a cell whose execution authority failed to compose")
	}
	if !strings.Contains(err.Error(), "P1B execution authority") {
		t.Errorf("error = %q, want it to name the P1B execution authority", err)
	}
}

// errNoAuthority is the composer failure the last case injects.
var errNoAuthority = errors.New("no signed authority amendment")
