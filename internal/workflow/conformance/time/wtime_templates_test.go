package time

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timeclock"
	"gopkg.in/yaml.v3"
)

const timeTemplateCatalogSHA256 = "4caec58e8ee35a2e8f66dd74cab8d67034474742f38ea1139d89bedce57eb82e"

type timeTemplateCatalog struct {
	CatalogVersion       int                    `yaml:"catalog_version"`
	CatalogFamily        string                 `yaml:"catalog_family"`
	After                string                 `yaml:"after"`
	SimulationProjection string                 `yaml:"simulation_projection"`
	EdgeCases            []timeTemplateEdgeCase `yaml:"edge_cases"`
	Entries              []timeTemplateEntry    `yaml:"entries"`
}

type timeTemplateEdgeCase struct {
	ID         string `yaml:"id"`
	TemplateID string `yaml:"template_id"`
	Route      string `yaml:"route"`
	Evidence   string `yaml:"evidence"`
}

type timeTemplateEntry struct {
	ID           string             `yaml:"id"`
	Name         string             `yaml:"name"`
	TemplateID   string             `yaml:"template_id"`
	WorkflowID   string             `yaml:"workflow_id"`
	Intent       timeTemplateIntent `yaml:"intent"`
	Steps        []timeTemplateStep `yaml:"steps"`
	Evidence     []string           `yaml:"evidence"`
	Routes       []string           `yaml:"routes"`
	Jurisdiction map[string]string  `yaml:"jurisdiction"`
}

type timeTemplateIntent struct {
	Type     string `yaml:"type"`
	Relation string `yaml:"relation"`
}

type timeTemplateStep struct {
	Number    int    `yaml:"n"`
	Primitive string `yaml:"primitive"`
	Action    string `yaml:"action"`
}

func readTimeTemplateCatalog(t *testing.T) (timeTemplateCatalog, []byte) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "definitions", "workflows", "time", "catalog.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read time template catalog: %v", err)
	}
	var catalog timeTemplateCatalog
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&catalog); err != nil {
		t.Fatalf("decode time template catalog: %v", err)
	}
	return catalog, raw
}

func catalogEntriesByTemplate(t *testing.T, catalog timeTemplateCatalog) map[string]timeTemplateEntry {
	t.Helper()
	entries := make(map[string]timeTemplateEntry, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		if _, exists := entries[entry.TemplateID]; exists {
			t.Fatalf("duplicate catalog template %q", entry.TemplateID)
		}
		entries[entry.TemplateID] = entry
	}
	return entries
}

// TestTodo_WTIME_008 is the PRIMARY catalog proof. It checks that the five
// profile-facing templates are named, versioned after WF-TIM-006, and carry
// enough workflow material to be reviewed without consulting prose elsewhere.
func TestTodo_WTIME_008(t *testing.T) {
	catalog, _ := readTimeTemplateCatalog(t)
	if catalog.CatalogVersion != 1 || catalog.CatalogFamily != "time-reference-workflows" {
		t.Fatalf("catalog identity = %d/%q", catalog.CatalogVersion, catalog.CatalogFamily)
	}
	if catalog.After != "WF-TIM-006" || catalog.SimulationProjection != "internal/workflow/conformance/time" {
		t.Fatalf("catalog boundary = after %q, projection %q", catalog.After, catalog.SimulationProjection)
	}
	if len(catalog.Entries) != 5 {
		t.Fatalf("catalog entries = %d, want five profile templates", len(catalog.Entries))
	}
	wantTemplates := []string{"time.punch_session", "time.duration_timesheet", "time.exception_period", "time.contractor_invoice", "time.agency_vms"}
	entries := catalogEntriesByTemplate(t, catalog)
	for i, templateID := range wantTemplates {
		entry, ok := entries[templateID]
		if !ok {
			t.Fatalf("catalog missing template %q", templateID)
		}
		wantID := fmt.Sprintf("WF-TIM-%03d", 7+i)
		if entry.ID != wantID {
			t.Errorf("template %q id = %q, want %q", templateID, entry.ID, wantID)
		}
		if entry.Name == "" || entry.WorkflowID == "" || entry.Intent.Type == "" || entry.Intent.Relation != "creates" {
			t.Errorf("template %q has incomplete identity or intent: %+v", templateID, entry)
		}
		if len(entry.Steps) < 5 || len(entry.Evidence) < 3 || len(entry.Routes) == 0 {
			t.Errorf("template %q has steps=%d evidence=%d routes=%d", templateID, len(entry.Steps), len(entry.Evidence), len(entry.Routes))
		}
		for _, key := range []string{"us_federal", "us_state_variation", "international"} {
			if strings.TrimSpace(entry.Jurisdiction[key]) == "" {
				t.Errorf("template %q has no %s jurisdiction note", templateID, key)
			}
		}
	}
	if len(catalog.EdgeCases) != 4 {
		t.Fatalf("edge-case fixtures = %d, want missing-out, reopen, overlay-forbidden and rest-breach", len(catalog.EdgeCases))
	}
}

// TestTodo_WTIME_008_Conformance compiles each real execute-mode template and
// proves that the catalogue's route fixture set is exactly the route set on
// its authored graph. This prevents a happy-path fixture from hiding a route
// added to a template later.
func TestTodo_WTIME_008_Conformance(t *testing.T) {
	catalog, _ := readTimeTemplateCatalog(t)
	entries := catalogEntriesByTemplate(t, catalog)
	for _, plan := range timeclock.Plans() {
		entry, ok := entries[plan.TemplateID]
		if !ok {
			continue
		}
		t.Run(entry.ID, func(t *testing.T) {
			compiled, err := timeclock.Compile(plan.TemplateID, timeclock.DefaultParams())
			if err != nil {
				t.Fatalf("compile %s: %v", plan.TemplateID, err)
			}
			if compiled.WorkflowID != entry.WorkflowID || compiled.Digest() == "" {
				t.Fatalf("compiled identity = %q/%q", compiled.WorkflowID, compiled.Digest())
			}
			definition := plan.Definition(timeclock.DefaultParams())
			if got, want := routeKeys(definition.Edges), entry.Routes; !sameStrings(got, want) {
				t.Fatalf("catalog routes differ for %s: graph=%v catalog=%v", plan.TemplateID, got, want)
			}
			for _, route := range entry.Routes {
				receipt := runTemplateRoute(t, entry, route)
				if receipt.Terminal.NodeID == "" || receipt.Terminal.TerminalCode == "" {
					t.Fatalf("route %q produced incomplete terminal: %+v", route, receipt.Terminal)
				}
			}
		})
	}

	for _, edgeCase := range catalog.EdgeCases {
		t.Run("edge_case_"+edgeCase.ID, func(t *testing.T) {
			switch edgeCase.ID {
			case "missing_out", "reopen", "rest_breach":
				entry, ok := entries[edgeCase.TemplateID]
				if !ok || !containsString(entry.Routes, edgeCase.Route) {
					t.Fatalf("edge case %q route %q is not declared by %q", edgeCase.ID, edgeCase.Route, edgeCase.TemplateID)
				}
				receipt := runTemplateRoute(t, entry, edgeCase.Route)
				if receipt.Terminal.NodeID == "" {
					t.Fatalf("edge case %q did not reach a terminal", edgeCase.ID)
				}
			case "overlay_forbidden":
				unknown := &workflow.CompiledWorkflow{WorkflowID: "time.overlay.fixture", Nodes: []workflow.CompiledNode{{
					ID: "overlay-added-form", Type: workflow.StepTask,
					OutputSchema: workflow.SchemaRef{SchemaID: "hcmnext.forms.time.overlay_unknown/v1", Version: 1, ProtobufFullName: "hcmnext.forms.v1.FormSubmission"},
				}}}
				if _, err := timeclock.PlanNodes(unknown); !errors.Is(err, timeclock.ErrUnmanifestedNode) {
					t.Fatalf("unknown overlay was not refused: %v", err)
				}
			default:
				t.Fatalf("unhandled edge-case fixture %q", edgeCase.ID)
			}
		})
	}
}

// TestTodo_WTIME_008_Golden pins the catalog bytes. A changed route, intent,
// evidence or jurisdiction note therefore requires an intentional fixture
// update rather than silently changing the conformance contract.
func TestTodo_WTIME_008_Golden(t *testing.T) {
	_, raw := readTimeTemplateCatalog(t)
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != timeTemplateCatalogSHA256 {
		t.Fatalf("time template catalog digest = %s, want %s", got, timeTemplateCatalogSHA256)
	}
}

// TestTodo_WTIME_008_ModelBased walks every route through the actual
// simulator. The projection is intentionally zero-effect: it is the
// reference-workflow proof of route reachability, while the execute template
// remains separately compiled and is never misrepresented as a simulation.
func TestTodo_WTIME_008_ModelBased(t *testing.T) {
	catalog, _ := readTimeTemplateCatalog(t)
	for _, entry := range catalog.Entries {
		t.Run(entry.TemplateID, func(t *testing.T) {
			for _, route := range entry.Routes {
				t.Run(route, func(t *testing.T) {
					receipt := runTemplateRoute(t, entry, route)
					if receipt.Mode != workflow.ModeSimulate {
						t.Fatalf("mode = %s, want SIMULATE", receipt.Mode)
					}
					if receipt.Terminal.TerminalCode != "TIME_REFERENCE_"+routeSlug(route) {
						t.Fatalf("terminal = %q, want route-bound terminal", receipt.Terminal.TerminalCode)
					}
					trace, ok := receipt.Node("choose_route")
					if !ok || trace.RouteKey != route || !strings.Contains(trace.Detail, route) {
						t.Fatalf("route trace = %+v, want %s evidence", trace, route)
					}
				})
			}
		})
	}
}

func routeKeys(edges []workflow.Edge) []string {
	seen := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		seen[edge.RouteKey] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for route := range seen {
		out = append(out, route)
	}
	sort.Strings(out)
	return out
}

func sameStrings(a, b []string) bool {
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func runTemplateRoute(t *testing.T, entry timeTemplateEntry, route string) simulate.Receipt {
	t.Helper()
	plan, err := workflow.Compile(templateProjectionDefinition(entry), workflow.Options{Phase: workflow.PhaseP1A})
	if err != nil {
		t.Fatalf("compile simulation projection for %s: %v", entry.TemplateID, err)
	}
	if err := simulate.Admit(plan); err != nil {
		t.Fatalf("simulation projection for %s is not admissible: %v", entry.TemplateID, err)
	}
	receipt, err := simulate.Run(context.Background(), plan, simulate.Inputs{Values: simulate.Bag{
		"subject_key": simulate.NewString("worker:reference:" + entry.TemplateID),
	}}, simulate.Options{
		Decisions:  routeDecision{route: route},
		SubjectRef: "principal:time-reference-conformance",
	})
	if err != nil {
		t.Fatalf("simulate %s route %q: %v", entry.TemplateID, route, err)
	}
	return receipt
}

type routeDecision struct{ route string }

func (d routeDecision) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	for _, route := range req.Decision.Routes {
		if route.Key == d.route {
			return simulate.DecisionResult{RouteKey: d.route, TraceRef: "fixture:" + d.route, Detail: "fixture route " + d.route}, nil
		}
	}
	return simulate.DecisionResult{}, fmt.Errorf("route %q is not declared", d.route)
}

func templateProjectionDefinition(entry timeTemplateEntry) workflow.Definition {
	schema := workflow.SchemaRef{
		SchemaID: "hcmnext.workflows.time.conformance." + routeSlug(entry.TemplateID), Version: 1,
		ProtobufFullName: "hcmnext.workflows.time.ConformanceReference",
	}
	stringType := workflow.ValueType{Kind: workflow.KindString}
	input := workflow.Field{Path: "subject_key", Type: stringType}
	nodes := []workflow.Node{{
		ID: "choose_route", Type: workflow.StepDecision, InputSchema: schema, OutputSchema: schema,
		Inputs: []workflow.Field{input}, InputMappings: []workflow.Mapping{{Target: "subject_key", Source: builders.FromInput("subject_key")}},
		Decision: &workflow.DecisionSpec{
			EvaluatorRef: "engines.conformance.time_route", EvaluatorVersion: 1,
			RuleRef: "rules.time.reference_route/v1", InputDigestProfile: "hcmnext.workflow.InputMappingSet/v1",
			Routes: projectionRoutes(entry.Routes),
		},
		DeclaredEffect: capability.EffectPure,
		Governance:     workflow.NodeGovernance{Purpose: "SIMULATE_TIME_TEMPLATE", Classification: "CONFIDENTIAL_TIME_AND_ATTENDANCE", RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: "data-access.time.reference-conformance/v1"},
	}}
	edges := make([]workflow.Edge, 0, len(entry.Routes))
	for _, route := range entry.Routes {
		terminalID := "end_" + routeSlug(route)
		nodes = append(nodes, workflow.Node{
			ID: terminalID, Type: workflow.StepEnd, InputSchema: schema, OutputSchema: schema,
			Inputs: []workflow.Field{input, {Path: "terminal_code", Type: stringType}},
			InputMappings: []workflow.Mapping{
				{Target: "subject_key", Source: builders.FromInput("subject_key")},
				{Target: "terminal_code", Source: builders.Constant("TIME_REFERENCE_"+routeSlug(route), stringType)},
			},
			DeclaredEffect: capability.EffectPure,
			End:            &workflow.EndSpec{TerminalCode: "TIME_REFERENCE_" + routeSlug(route), RuntimeStatus: workflow.RuntimeCompleted, CompletionMapping: builders.Completion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "PENDING_OBSERVATION", "PENDING")},
			Governance:     workflow.NodeGovernance{Purpose: "SIMULATE_TIME_TEMPLATE", Classification: "CONFIDENTIAL_TIME_AND_ATTENDANCE", RevalidationBoundary: workflow.RevalidatePreClosure, DataAccessManifestRef: "data-access.time.reference-conformance/v1"},
		})
		edges = append(edges, workflow.Edge{From: "choose_route", To: terminalID, RouteKey: route})
	}
	return workflow.Definition{
		WorkflowID: "hcmnext.conformance.time." + routeSlug(entry.TemplateID), Version: 1,
		Name: "SIMULATE reference for " + entry.Name, IntentType: entry.Intent.Type,
		InputSchema: schema, OutputSchema: schema, VariablesSchema: schema,
		Inputs: []workflow.Field{input}, Outputs: []workflow.Field{{Path: "terminal_code", Type: stringType}},
		TenantScope: "tenant-template", OrganizationScope: "tenant-template/time", RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeSimulate}, TerminalProfile: workflow.TerminalProfileSimulateOnly,
		StartNodeID: "choose_route", Nodes: nodes, Edges: edges,
		Limits:           workflow.Limits{MaxFanOut: uint32(len(entry.Routes)), MaxDepth: 2, MaxNodes: uint32(len(nodes))},
		FailurePolicyRef: "policy.workflow.failure.simulation/v1", CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef: "policy.workflow.migration.pinned/v1", RetentionPolicyRef: "policy.workflow.retention.time-records/v1",
	}
}

func projectionRoutes(routes []string) []workflow.DecisionRoute {
	out := make([]workflow.DecisionRoute, 0, len(routes))
	for i, route := range routes {
		out = append(out, workflow.DecisionRoute{Key: route, Predicate: "route." + route, Precedence: (i + 1) * 10})
	}
	return out
}

func routeSlug(value string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(value) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
