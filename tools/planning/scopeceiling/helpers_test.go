package scopeceiling

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

// versionSuffixPattern matches the trailing "/v<N>" a definition_ref carries
// (e.g. "hcmnext.people.promote_worker/v1") that
// definitions/planning/capability-coverage.yaml's `kind: intent` entries
// key without.
var versionSuffixPattern = regexp.MustCompile(`/v\d+$`)

func baseIntentID(id string) string {
	return versionSuffixPattern.ReplaceAllString(id, "")
}

// repoRoot is the path from this package's directory to the repository
// root: tools/planning/scopeceiling is three levels below root, exactly
// like tools/planning/gateevidence.
const repoRoot = "../../.."

const ceilingPath = repoRoot + "/definitions/planning/gates/phase1-scope-ceiling.yaml"

func mustLoadCeiling(t *testing.T) ScopeCeilingManifest {
	t.Helper()
	m, err := LoadManifest(ceilingPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if violations := m.Validate(); len(violations) != 0 {
		t.Fatalf("phase1-scope-ceiling.yaml fails Validate: %v", violations)
	}
	return *m
}

// coverageItem is the subset of definitions/planning/capability-coverage.yaml
// (CLOSE-001/product-slices' own live registry) this package cross-checks
// its intents and capabilities sections against, so a change to that
// registry that the ceiling has not followed fails loudly here instead of
// silently drifting.
type coverageItem struct {
	ID          string `yaml:"id"`
	Kind        string `yaml:"kind"`
	OwnerDomain string `yaml:"owner_domain"`
	State       string `yaml:"state"`
}

type coverageFile struct {
	Version int            `yaml:"version"`
	Items   []coverageItem `yaml:"items"`
}

func mustLoadCapabilityCoverage(t *testing.T) coverageFile {
	t.Helper()
	b, err := os.ReadFile(repoRoot + "/definitions/planning/capability-coverage.yaml")
	if err != nil {
		t.Fatalf("read capability-coverage.yaml: %v", err)
	}
	var f coverageFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse capability-coverage.yaml: %v", err)
	}
	return f
}

// liveIntentIDs returns the set of `kind: intent` ids in
// capability-coverage.yaml: the fourteen source-bound business intents.
func liveIntentIDs(t *testing.T) map[string]coverageItem {
	t.Helper()
	f := mustLoadCapabilityCoverage(t)
	out := make(map[string]coverageItem)
	for _, it := range f.Items {
		if it.Kind == "intent" {
			out[it.ID] = it
		}
	}
	return out
}

// liveCapabilityIDs returns the set of `kind: capability` ids in
// capability-coverage.yaml: the ten bootstrap capability-registry entries.
func liveCapabilityIDs(t *testing.T) map[string]coverageItem {
	t.Helper()
	f := mustLoadCapabilityCoverage(t)
	out := make(map[string]coverageItem)
	for _, it := range f.Items {
		if it.Kind == "capability" {
			out[it.ID] = it
		}
	}
	return out
}

type productSlice struct {
	SliceID string `yaml:"slice_id"`
}

type productSliceFile struct {
	Slices []productSlice `yaml:"slices"`
}

// liveSliceIDs returns the vertical-slice ids in
// definitions/planning/product-slices.yaml.
func liveSliceIDs(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(repoRoot + "/definitions/planning/product-slices.yaml")
	if err != nil {
		t.Fatalf("read product-slices.yaml: %v", err)
	}
	var f productSliceFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse product-slices.yaml: %v", err)
	}
	out := make(map[string]bool, len(f.Slices))
	for _, s := range f.Slices {
		out[s.SliceID] = true
	}
	return out
}

type endpointManifestEntry struct {
	EndpointID  string `json:"endpoint_id"`
	Phase       string `json:"phase"`
	Disposition string `json:"disposition"`
}

type endpointManifestFile struct {
	Endpoints []endpointManifestEntry `json:"endpoints"`
}

// liveEndpoints returns the generated endpoint definitions in
// definitions/api/endpoint-manifest.json, keyed by endpoint_id.
func liveEndpoints(t *testing.T) map[string]endpointManifestEntry {
	t.Helper()
	b, err := os.ReadFile(repoRoot + "/definitions/api/endpoint-manifest.json")
	if err != nil {
		t.Fatalf("read endpoint-manifest.json: %v", err)
	}
	var f endpointManifestFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse endpoint-manifest.json: %v", err)
	}
	out := make(map[string]endpointManifestEntry, len(f.Endpoints))
	for _, e := range f.Endpoints {
		out[e.EndpointID] = e
	}
	return out
}

// findIntent, findCapability, findWorkflow, findEndpoint, findModel and
// findEffect locate one ceiling item by identity, for tests that need to
// assert on a specific entry.
func findIntent(m ScopeCeilingManifest, id string) (IntentItem, bool) {
	for _, it := range m.Intents {
		if it.ID == id {
			return it, true
		}
	}
	return IntentItem{}, false
}

func findCapability(m ScopeCeilingManifest, id string) (CapabilityItem, bool) {
	for _, c := range m.Capabilities {
		if c.ID == id {
			return c, true
		}
	}
	return CapabilityItem{}, false
}

func findEndpoint(m ScopeCeilingManifest, id string) (EndpointItem, bool) {
	for _, e := range m.Endpoints {
		if e.EndpointID == id {
			return e, true
		}
	}
	return EndpointItem{}, false
}
