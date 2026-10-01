package invocationpath

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

const testModule = "github.com/monstercameron/human-capital-management-suite"

// TestMaterialFeatureCannotBypassIntentGateway is INTENT-013's primary
// denial test. Each direct effect edge is typed and the exact existing
// BIND-001 admin exception is retained as accepted evidence.
func TestMaterialFeatureCannotBypassIntentGateway(t *testing.T) {
	sources := []Source{
		{Package: "internal/transport/fixture", Filename: "transport.go", Content: `package fixture
import (
  "github.com/monstercameron/human-capital-management-suite/internal/domains/people/store"
  "github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe/adapters/postgres"
  "github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
  "github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
  "github.com/monstercameron/human-capital-management-suite/internal/messaging"
)
var _ = store.ErrNotFound
var _ = postgres.New
var _ = outbox.Commit
var _ runtime.WorkflowResolver
var _ messaging.Sink
`},
		{Package: "internal/transport/admin", Filename: "server.go", Content: `package admin
import "github.com/monstercameron/human-capital-management-suite/internal/domains/people"
var _ people.WorkerFacts
`},
	}
	findings, accepted, err := ScanSources(testModule, sources)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(findings), 5; got != want {
		t.Fatalf("actionable findings = %d, want %d: %v", got, want, findings)
	}
	for _, want := range []FindingKind{FindingDomainStore, FindingProviderAdapter, FindingOutbox, FindingWorkflowRuntime, FindingMessagingSink} {
		if !hasKind(findings, want) {
			t.Errorf("missing typed finding %q", want)
		}
	}
	if len(accepted) != 1 || accepted[0].AllowlistID != "BIND-001" {
		t.Fatalf("accepted = %#v, want one BIND-001 admin bypass", accepted)
	}
	for _, finding := range findings {
		if finding.File == "" || finding.Imported == "" {
			t.Errorf("finding is not file- and import-specific: %#v", finding)
		}
	}
}

// TestTodo_INTENT_013_Golden pins the exact route-facing projection of the
// real endpoint manifest, while also proving the real import graph is read.
func TestTodo_INTENT_013_Golden(t *testing.T) {
	root := repopath.RootDir()
	m, err := manifest.Build()
	if err != nil {
		t.Fatal(err)
	}
	wantBytes, err := os.ReadFile(filepath.Join(root, "tools", "policy", "invocationpath", "testdata", "intent_013_manifest.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ManifestGolden(m), string(wantBytes); got != want {
		t.Fatalf("manifest golden mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}
	report, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Packages == 0 || report.ImportEdges == 0 {
		t.Fatalf("real import graph is empty: %#v", report)
	}
	t.Logf("real tree report digest: %s; findings=%d accepted=%d", report.Digest(), len(report.Findings), len(report.Accepted))
}

// TestTodo_INTENT_013_Race checks that repeated pure scans do not share
// mutable route or finding state.
func TestTodo_INTENT_013_Race(t *testing.T) {
	source := Source{Package: "internal/transport/fixture", Filename: "fixture.go", Content: `package fixture
import "github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
var _ = outbox.Commit
`}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			findings, _, err := ScanSources(testModule, []Source{source})
			if err != nil || len(findings) != 1 || findings[0].Kind != FindingOutbox {
				t.Errorf("concurrent scan = %v, %v", findings, err)
			}
		}()
	}
	wg.Wait()
}

// TestTodo_INTENT_013_Integration runs the checker against the live tree and
// requires every live actionable edge to retain a typed file finding.
func TestTodo_INTENT_013_Integration(t *testing.T) {
	report, err := Analyze(repopath.RootDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("live tree unexpectedly has no INTENT-013 findings")
	}
	for _, finding := range report.Findings {
		if finding.File == "" || finding.Kind == "" {
			t.Errorf("untyped live finding: %#v", finding)
		}
		if finding.Kind == FindingUnscopedHandlerPath {
			t.Errorf("live IntentService lifecycle contract is incomplete: %#v", finding)
		}
	}
}

// TestTodo_INTENT_013_Conformance checks exact set equality for enabled
// channels and confirms disabled channels do not become accidental routes.
func TestTodo_INTENT_013_Conformance(t *testing.T) {
	m := &manifest.EndpointManifest{Endpoints: []manifest.EndpointDefinition{{
		EndpointID: "service/Create", CapabilityRefs: []string{"cap/a", "cap/b"}, AcceptedIntentDefinitionRefs: []string{"intent/a"},
	}, {
		EndpointID: "service/Get", CapabilityRefs: []string{"cap/read"},
	}}}
	expected := ExpectedRoutes(m)
	if findings := CheckManifestConformance(m, []Channel{{Name: "grpc", Enabled: true, Routes: expected}, {Name: "browser", Enabled: false}}); len(findings) != 0 {
		t.Fatalf("valid channel has findings: %v", findings)
	}
	bad := append([]Route(nil), expected...)
	bad[0].Capabilities = []string{"cap/forged"}
	if findings := CheckManifestConformance(m, []Channel{{Name: "grpc", Enabled: true, Routes: bad}}); len(findings) == 0 || findings[0].Kind != FindingRouteManifest {
		t.Fatalf("capability mismatch was not typed: %v", findings)
	}
}

// TestTodo_INTENT_013_Mutation injects a direct outbox write into a fixture
// handler and proves the source is denied before any runtime effect exists.
func TestTodo_INTENT_013_Mutation(t *testing.T) {
	findings, err := ScanSource(testModule, Source{
		Package:  "internal/transport/mutant",
		Filename: "handler.go",
		Content: `package mutant
import "github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
func Handle() { _ = outbox.Commit }
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Kind != FindingOutbox || findings[0].File != "handler.go" {
		t.Fatalf("direct effect mutation was not denied: %v", findings)
	}

	contract := validIntentServiceContractSources()
	findings, err = CheckIntentServiceContract(testModule, contract)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("valid IntentService lifecycle contract was rejected: %v", findings)
	}
	contract[0].Content = strings.Replace(contract[0].Content, "s.gateway.Invoke()", "handler()", 1)
	findings, err = CheckIntentServiceContract(testModule, contract)
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(findings, FindingUnscopedHandlerPath) {
		t.Fatalf("gateway invocation mutation was not denied: %v", findings)
	}
}

func validIntentServiceContractSources() []Source {
	return []Source{
		{Package: "internal/intent/app", Filename: "service.go", Content: `package app
import capability "github.com/monstercameron/human-capital-management-suite/internal/capability"
type Options struct { Gateway *capability.Gateway }
type IntentService struct { gateway *capability.Gateway }
func NewIntentService(opts Options) (*IntentService, error) {
 if opts.Gateway == nil { return nil, nil }
 return &IntentService{gateway: opts.Gateway}, nil
}
func (s *IntentService) invoke() { s.gateway.Invoke() }
`},
		{Package: "internal/intent/app", Filename: "cell.go", Content: `package app
import capability "github.com/monstercameron/human-capital-management-suite/internal/capability"
func NewCell(gateway *capability.Gateway) { _ = Options{Gateway: gateway} }
`},
	}
}

func TestRunReturnsNonZeroErrorForRealFindings(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(repopath.RootDir(), &stdout, &stderr); err == nil {
		t.Fatal("Run returned nil for live actionable findings")
	}
	if !strings.Contains(stdout.String(), "invocation path:") {
		t.Fatalf("Run did not explain its report: %q", stdout.String())
	}
}

func hasKind(findings []Finding, want FindingKind) bool {
	for _, finding := range findings {
		if finding.Kind == want {
			return true
		}
	}
	return false
}
