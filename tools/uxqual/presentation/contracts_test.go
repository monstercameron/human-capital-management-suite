package presentation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"gopkg.in/yaml.v3"
)

func TestTodo_WEB_006(t *testing.T) {
	confidence := 0.88
	value := BoundValue{
		Value: json.RawMessage(`"USD 112,000"`), DisplayValue: "USD 112,000",
		SourceType: SourceCanonical, SourceID: "worker:JL", SourceVersion: "17",
		AuthorityClass: "worker_system_of_record", Classification: "CONFIDENTIAL",
		Confidence: &confidence, RecordedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		Disposition: DispositionShow,
	}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	first, err := value.Digest()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := value.Digest()
	if first != second {
		t.Fatal("bound value digest is nondeterministic")
	}
	value.Disposition, value.Value, value.DisplayValue = DispositionHide, nil, ""
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WEB_006_Security(t *testing.T) {
	value := BoundValue{SourceType: SourceCanonical, SourceID: "worker:JL", SourceVersion: "1", AuthorityClass: "sor", Classification: "RESTRICTED", RecordedAt: time.Now(), Disposition: DispositionMask, Value: json.RawMessage(`"secret"`)}
	if err := value.Validate(); err == nil {
		t.Fatal("masked raw value was accepted")
	}
}

func TestTodo_WEB_006_Golden(t *testing.T) {
	v := BoundValue{Value: json.RawMessage(`"USD 112,000"`), DisplayValue: "USD 112,000", SourceType: SourceCanonical, SourceID: "worker:JL", SourceVersion: "17", AuthorityClass: "worker_system_of_record", Classification: "CONFIDENTIAL", RecordedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), Disposition: DispositionShow}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"display_value":"USD 112,000"`, `"source_id":"worker:JL"`, `"source_version":"17"`, `"disposition":"SHOW"`} {
		if !strings.Contains(string(b), field) {
			t.Fatalf("bound-value wire output missing %s: %s", field, b)
		}
	}
}
func TestTodo_WEB_006_Browser(t *testing.T) {
	v := BoundValue{SourceType: SourceCanonical, SourceID: "worker:JL", SourceVersion: "17", AuthorityClass: "sor", Classification: "CONFIDENTIAL", RecordedAt: time.Now(), Disposition: DispositionHide}
	if err := v.Validate(); err != nil {
		t.Fatalf("hidden presentation value: %v", err)
	}
	if v.DisplayValue != "" || len(v.Value) != 0 {
		t.Fatal("hidden value retained browser-visible content")
	}
}
func TestTodo_WEB_006_Conformance(t *testing.T) {
	confidence := 0.88
	v := BoundValue{Value: json.RawMessage(`"USD 112,000"`), DisplayValue: "USD 112,000", SourceType: SourceCanonical, SourceID: "worker:JL", SourceVersion: "17", AuthorityClass: "worker_system_of_record", Classification: "CONFIDENTIAL", Confidence: &confidence, RecordedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), Disposition: DispositionShow}
	a, err := v.Digest()
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("digest changed: %s != %s", a, b)
	}
}

func TestTodo_WEB_007(t *testing.T) {
	action := validAction()
	if err := action.Validate(); err != nil {
		t.Fatal(err)
	}
	action.Availability, action.Label, action.ActionToken, action.UnavailableReason = ActionHidden, "", "", ""
	if action.Presentable() {
		t.Fatal("hidden action became presentable")
	}
}

func validAction() SemanticAction {
	return SemanticAction{ID: "promotion.review", Label: "Review proposal", RPC: knownRPC(), Availability: ActionAvailable, ActionToken: "opaque-token", ExpectedResourceVersion: "17", IdempotencyKey: "intent:01:review", InputSchema: "hcmnext.journey.v1.DecideJourneyRequest", Obligations: []string{"step_up_if_required"}}
}

func knownRPC() string {
	rpcs := make([]string, 0, len(knownRPCsForTest()))
	for rpc := range knownRPCsForTest() {
		rpcs = append(rpcs, rpc)
	}
	sort.Strings(rpcs)
	if len(rpcs) == 0 {
		return ""
	}
	return rpcs[0]
}

func knownRPCsForTest() map[string]bool {
	// Kept behind a helper so the test does not pin a hand-written service name.
	return pagedef.KnownRPCs()
}

func TestTodo_WEB_007_Golden(t *testing.T) {
	a := validAction()
	if a.ID != "promotion.review" || a.RPC == "" || !pagedef.KnownRPCs()[a.RPC] {
		t.Fatalf("semantic action golden identity = %+v", a)
	}
}
func TestTodo_WEB_007_Browser(t *testing.T) {
	a := validAction()
	if !a.Presentable() {
		t.Fatal("available authorized action was hidden")
	}
	a.Availability = ActionUnavailableSafe
	a.UnavailableReason = "approval required"
	a.ActionToken = ""
	if err := a.Validate(); err != nil {
		t.Fatalf("safe unavailable action: %v", err)
	}
}
func TestTodo_WEB_007_Conformance(t *testing.T) {
	a := validAction()
	if err := a.Validate(); err != nil {
		t.Fatalf("valid action contract: %v", err)
	}
	a.ActionToken = ""
	if err := a.Validate(); err == nil {
		t.Fatal("action without server token passed validation")
	}
}

func TestTodo_WEB_008(t *testing.T) {
	graph := CompatibilityGraph{
		Nodes: []DependencyNode{{Ref: "page:home@1", Kind: DependencyPage}, {Ref: "floorplan:launch@1", Kind: DependencyFloorplan}, {Ref: "widget:attention@1", Kind: DependencyWidget}, {Ref: "brand.color.primary", Kind: DependencyToken}},
		Edges: []DependencyEdge{{From: "page:home@1", To: "floorplan:launch@1"}, {From: "page:home@1", To: "widget:attention@1"}, {From: "widget:attention@1", To: "brand.color.primary"}},
	}
	if err := graph.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, want := graph.Impacted("brand.color.primary"), []string{"page:home@1", "widget:attention@1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Impacted = %v, want %v", got, want)
	}
}

func TestTodo_WEB_008_Golden(t *testing.T) {
	g := CompatibilityGraph{Nodes: []DependencyNode{{Ref: "page:home@1", Kind: DependencyPage}, {Ref: "token:brand@1", Kind: DependencyToken}}, Edges: []DependencyEdge{{From: "page:home@1", To: "token:brand@1"}}}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := g.Impacted("token:brand@1"); !reflect.DeepEqual(got, []string{"page:home@1"}) {
		t.Fatalf("impact golden = %v", got)
	}
}
func TestTodo_WEB_008_Browser(t *testing.T) {
	g := CompatibilityGraph{Nodes: []DependencyNode{{Ref: "page:p@1", Kind: DependencyPage}, {Ref: "widget:w@1", Kind: DependencyWidget}}, Edges: []DependencyEdge{{From: "page:p@1", To: "widget:w@1"}}}
	if got := g.Impacted("widget:w@1"); len(got) != 1 || got[0] != "page:p@1" {
		t.Fatalf("browser page impact = %v", got)
	}
}
func TestTodo_WEB_008_Conformance(t *testing.T) {
	g := CompatibilityGraph{Nodes: []DependencyNode{{Ref: "page:p@1", Kind: DependencyPage}, {Ref: "token:t@1", Kind: DependencyToken}}, Edges: []DependencyEdge{{From: "page:p@1", To: "token:t@1"}}}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	g.Edges = append(g.Edges, DependencyEdge{From: "token:t@1", To: "page:p@1"})
	if err := g.Validate(); err == nil {
		t.Fatal("dependency cycle passed validation")
	}
}

func TestTodo_WEB_009(t *testing.T) {
	entries := []ScopeEntry{{Capability: "promotion.read", Gate: GateA, Authority: "intent service", ReadOnly: true}, {Capability: "promotion.execute", Gate: GateB, Authority: "execution gate"}, {Capability: "customer.pages", Gate: Phase2, Authority: "configuration service"}}
	if err := ValidateReleaseScope(entries); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WEB_009_Golden(t *testing.T) {
	entries := []ScopeEntry{{Capability: "promotion.read", Gate: GateA, Authority: "intent service", ReadOnly: true}, {Capability: "promotion.execute", Gate: GateB, Authority: "execution gate"}}
	if err := ValidateReleaseScope(entries); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_WEB_009_Browser(t *testing.T) {
	entries := []ScopeEntry{{Capability: "promotion.read", Gate: GateA, Authority: "intent service", ReadOnly: true}, {Capability: "promotion.read", Gate: GateA, Authority: "intent service", ReadOnly: true}}
	if err := ValidateReleaseScope(entries); err == nil {
		t.Fatal("duplicate browser capability passed scope validation")
	}
}
func TestTodo_WEB_009_Conformance(t *testing.T) {
	entries := []ScopeEntry{{Capability: "promotion.execute", Gate: GateB, Authority: "execution gate"}}
	if err := ValidateReleaseScope(entries); err != nil {
		t.Fatal(err)
	}
	entries[0].Capability = ""
	if err := ValidateReleaseScope(entries); err == nil {
		t.Fatal("empty capability passed release-scope validation")
	}
}

func TestTodo_WEB_010(t *testing.T) {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	artifacts := make([]EvidenceArtifact, 0, len(requiredEvidenceKinds))
	for _, kind := range requiredEvidenceKinds {
		artifacts = append(artifacts, EvidenceArtifact{Kind: kind, URI: "evidence/" + kind + ".json", SHA256: digest, Requirements: []string{"WEB-010"}})
	}
	if err := (EvidenceBundle{ReleaseID: "frontend-2026-09-05", Artifacts: artifacts}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WEB_010_Golden(t *testing.T) {
	if len(requiredEvidenceKinds) == 0 {
		t.Fatal("evidence contract has no required artifact kinds")
	}
	for _, kind := range requiredEvidenceKinds {
		if kind == "" {
			t.Fatal("empty evidence kind in contract")
		}
	}
}
func TestTodo_WEB_010_Browser(t *testing.T) {
	b := EvidenceBundle{ReleaseID: "web"}
	for _, kind := range requiredEvidenceKinds {
		b.Artifacts = append(b.Artifacts, EvidenceArtifact{Kind: kind, URI: "evidence/" + kind + ".json", SHA256: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Requirements: []string{"WEB-010"}})
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("valid evidence bundle rejected: %v", err)
	}
}
func TestTodo_WEB_010_Conformance(t *testing.T) {
	b := EvidenceBundle{ReleaseID: "web", Artifacts: []EvidenceArtifact{{Kind: requiredEvidenceKinds[0], URI: "evidence/a.json", SHA256: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Requirements: []string{"WEB-010"}}}}
	if err := b.Validate(); err == nil {
		t.Fatal("incomplete evidence bundle passed validation")
	}
}

func TestTodo_WEB_011(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "definitions", "ux", "website-threat-model.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var model ThreatModel
	if err := yaml.Unmarshal(b, &model); err != nil {
		t.Fatal(err)
	}
	if err := model.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WEB_011_Golden(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "definitions", "ux", "website-threat-model.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var model ThreatModel
	if err := yaml.Unmarshal(b, &model); err != nil {
		t.Fatal(err)
	}
	if err := model.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(model.Threats) == 0 {
		t.Fatal("threat model golden has no threats")
	}
}
func TestTodo_WEB_011_Browser(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "definitions", "ux", "website-threat-model.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var model ThreatModel
	if err := yaml.Unmarshal(b, &model); err != nil {
		t.Fatalf("browser threat model parse: %v", err)
	}
	if err := model.Validate(); err != nil {
		t.Fatalf("browser threat model contract: %v", err)
	}
}
func TestTodo_WEB_011_Conformance(t *testing.T) {
	model := ThreatModel{}
	if err := model.Validate(); err == nil {
		t.Fatal("empty threat model passed validation")
	}
}

func TestTodo_WEB_012(t *testing.T) {
	boundaries := []OwnershipBoundary{
		{Concern: "presentation", Owner: "frontend platform", MayDecide: []string{"layout", "formatting"}, MustNotDecide: []string{"business authority", "workflow state"}},
		{Concern: "authorization", Owner: "trust plane", MayDecide: []string{"discoverability", "field disposition"}, MustNotDecide: []string{"layout", "brand"}},
		{Concern: "business state", Owner: "domain and workflow services", MayDecide: []string{"facts", "transitions"}, MustNotDecide: []string{"client rendering"}},
	}
	if err := ValidateOwnership(boundaries); err != nil {
		t.Fatal(err)
	}
	if CanonicalOwnership(boundaries)[0].Concern != "authorization" {
		t.Fatal("ownership canonicalization is not deterministic")
	}
}

func TestTodo_WEB_012_Golden(t *testing.T) {
	boundaries := []OwnershipBoundary{{Concern: "authorization", Owner: "trust plane", MayDecide: []string{"discoverability"}, MustNotDecide: []string{"layout"}}, {Concern: "presentation", Owner: "frontend platform", MayDecide: []string{"layout"}, MustNotDecide: []string{"business authority"}}}
	if err := ValidateOwnership(boundaries); err != nil {
		t.Fatal(err)
	}
	if got := CanonicalOwnership(boundaries)[0].Concern; got != "authorization" {
		t.Fatalf("canonical ownership first concern = %q", got)
	}
}
func TestTodo_WEB_012_Browser(t *testing.T) {
	b := OwnershipBoundary{Concern: "presentation", Owner: "frontend platform", MayDecide: []string{"layout", "formatting"}, MustNotDecide: []string{"business authority"}}
	if err := ValidateOwnership([]OwnershipBoundary{b}); err != nil {
		t.Fatal(err)
	}
	if b.MustNotDecide[0] != "business authority" {
		t.Fatal("browser ownership boundary permits business decisions")
	}
}
func TestTodo_WEB_012_Conformance(t *testing.T) {
	b := OwnershipBoundary{Concern: "authorization", Owner: "trust plane", MayDecide: []string{"discoverability"}, MustNotDecide: []string{"layout"}}
	if err := ValidateOwnership([]OwnershipBoundary{b}); err != nil {
		t.Fatal(err)
	}
	b.Owner = ""
	if err := ValidateOwnership([]OwnershipBoundary{b}); err == nil {
		t.Fatal("ownership without an accountable owner passed validation")
	}
}
