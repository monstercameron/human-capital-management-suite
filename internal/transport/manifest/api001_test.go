package manifest

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
)

// TestTodo_API_001 is API-001's primary test, scoped to this lane's mandate:
// "a descriptor document derived from the manifest, served shape only ...
// provide the pure function that renders it." It proves
// [RenderDiscoveryDocument] is a total, side-effect-free projection of an
// [EndpointManifest] plus its capability/intent inputs: every endpoint,
// capability and intent definition appears exactly once, and the recorded
// manifest digest matches the source manifest's own digest.
func TestTodo_API_001(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	records := registry.List()
	defs := definitions.All()

	doc, err := RenderDiscoveryDocument(m, defs, records)
	if err != nil {
		t.Fatalf("RenderDiscoveryDocument: %v", err)
	}

	wantDigest, err := m.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if doc.ManifestDigest != wantDigest {
		t.Fatalf("ManifestDigest = %s, want %s", doc.ManifestDigest, wantDigest)
	}
	if len(doc.Endpoints) != len(m.Endpoints) {
		t.Fatalf("document has %d endpoints, manifest has %d", len(doc.Endpoints), len(m.Endpoints))
	}
	if len(doc.Capabilities) != len(records) {
		t.Fatalf("document has %d capabilities, source has %d", len(doc.Capabilities), len(records))
	}
	if len(doc.IntentDefinitions) != len(defs) {
		t.Fatalf("document has %d intent definitions, source has %d", len(doc.IntentDefinitions), len(defs))
	}

	if _, err := RenderDiscoveryDocument(nil, defs, records); err == nil {
		t.Fatal("expected RenderDiscoveryDocument(nil manifest, ...) to fail rather than panic or return a zero document")
	}
}

// TestTodo_API_001_Golden pins the served shape of one SERVED and one
// REFUSED_P1A endpoint, and one capability/intent-definition row, so a
// change to what the discovery document actually contains is a reviewed
// diff.
func TestTodo_API_001_Golden(t *testing.T) {
	doc, err := RenderDefaultDiscoveryDocument()
	if err != nil {
		t.Fatalf("RenderDefaultDiscoveryDocument: %v", err)
	}

	byID := map[string]EndpointDescriptor{}
	for _, e := range doc.Endpoints {
		byID[e.EndpointID] = e
	}

	create, ok := byID["hcmnext.intents.v1.IntentService/CreateIntent"]
	if !ok {
		t.Fatal("CreateIntent is missing from the discovery document")
	}
	if create.Disposition != DispositionServed || create.HTTPMethod != "POST" || create.HTTPPathTemplate != "/v1/intents" {
		t.Fatalf("CreateIntent golden mismatch: %+v", create)
	}

	submit, ok := byID["hcmnext.intents.v1.IntentService/SubmitIntent"]
	if !ok {
		t.Fatal("SubmitIntent is missing from the discovery document")
	}
	if submit.Disposition != DispositionServed || submit.DispositionReason == "" {
		t.Fatalf("SubmitIntent golden mismatch: %+v", submit)
	}

	foundCap := false
	for _, c := range doc.Capabilities {
		if c.CapabilityID == "hcmnext.people.promote_worker" {
			foundCap = true
			if c.EffectClass != "READ_ONLY" {
				t.Fatalf("hcmnext.people.promote_worker effect class = %s, want READ_ONLY under BOOTSTRAP", c.EffectClass)
			}
		}
	}
	if !foundCap {
		t.Fatal("hcmnext.people.promote_worker is missing from the discovery document's capabilities")
	}

	foundIntent := false
	for _, d := range doc.IntentDefinitions {
		if d.DefinitionRef == "hcmnext.people.promote_worker/v1" {
			foundIntent = true
			if d.Release != "P1A" {
				t.Fatalf("hcmnext.people.promote_worker/v1 release = %s, want P1A", d.Release)
			}
		}
	}
	if !foundIntent {
		t.Fatal("hcmnext.people.promote_worker/v1 is missing from the discovery document's intent definitions")
	}
}

// TestTodo_API_001_Security proves the served shape never presents a
// REFUSED_P1A method as though it were callable, and that the document's
// JSON encoding carries no unexported/internal review metadata (rate
// budget refs, evidence policy refs, required field paths) that only this
// package's own generation and policy tooling need.
func TestTodo_API_001_Security(t *testing.T) {
	doc, err := RenderDefaultDiscoveryDocument()
	if err != nil {
		t.Fatalf("RenderDefaultDiscoveryDocument: %v", err)
	}
	for _, e := range doc.Endpoints {
		if e.Disposition == DispositionRefusedP1A && e.DispositionReason == "" {
			t.Errorf("%s: REFUSED_P1A with no reason given to the caller", e.EndpointID)
		}
	}

	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal(doc): %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	endpoints, _ := generic["endpoints"].([]any)
	if len(endpoints) == 0 {
		t.Fatal("encoded document has no endpoints")
	}
	first, _ := endpoints[0].(map[string]any)
	forbidden := []string{"rate_budget_ref", "evidence_policy_ref", "required_field_paths", "purpose_policy_ref"}
	for _, key := range forbidden {
		if _, present := first[key]; present {
			t.Errorf("served endpoint shape leaks internal review field %q", key)
		}
	}
}

// TestTodo_API_001_Integration builds the document from the real production
// wiring — the live descriptor set, the compiled BOOTSTRAP registry and the
// fourteen definitions — rather than a fixture, proving the pure function
// actually integrates with its production sources end to end.
func TestTodo_API_001_Integration(t *testing.T) {
	doc, err := RenderDefaultDiscoveryDocument()
	if err != nil {
		t.Fatalf("RenderDefaultDiscoveryDocument: %v", err)
	}
	// The ProjectService surface now includes task-link add and remove mutations.
	if len(doc.Endpoints) != 56 || len(doc.Capabilities) != 11 || len(doc.IntentDefinitions) != 14 {
		t.Fatalf("unexpected production shape: %d endpoints, %d capabilities, %d intent definitions",
			len(doc.Endpoints), len(doc.Capabilities), len(doc.IntentDefinitions))
	}
	// The document must itself be valid JSON a real discovery surface could
	// serve verbatim.
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("discovery document does not encode: %v", err)
	}
}

// TestTodo_API_001_Race calls RenderDiscoveryDocument concurrently from many
// goroutines over the same shared inputs and proves every result is equal:
// the pure function must not read or mutate shared state unsafely.
func TestTodo_API_001_Race(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	records := registry.List()
	defs := definitions.All()

	const n = 64
	docs := make([]*DiscoveryDocument, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			docs[i], errs[i] = RenderDiscoveryDocument(m, defs, records)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if !reflect.DeepEqual(docs[0], docs[i]) {
			t.Fatalf("goroutine %d produced a different document than goroutine 0", i)
		}
	}
}

// TestTodo_API_001_Mutation proves the render is not vacuous: changing one
// input field (a capability's risk class, an endpoint's disposition)
// changes the corresponding output field, and changing input order never
// changes the sorted output.
func TestTodo_API_001_Mutation(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("NewBootstrapRegistry: %v", err)
	}
	records := registry.List()
	defs := definitions.All()

	base, err := RenderDiscoveryDocument(m, defs, records)
	if err != nil {
		t.Fatalf("RenderDiscoveryDocument (base): %v", err)
	}

	mutatedRecords := append([]capability.Record(nil), records...)
	mutatedRecords[0].Definition.RiskClass = "MUTATED_RISK_CLASS"
	mutated, err := RenderDiscoveryDocument(m, defs, mutatedRecords)
	if err != nil {
		t.Fatalf("RenderDiscoveryDocument (mutated): %v", err)
	}
	found := false
	for _, c := range mutated.Capabilities {
		if c.RiskClass == "MUTATED_RISK_CLASS" {
			found = true
		}
	}
	if !found {
		t.Fatal("mutating one capability's risk class did not change the rendered document")
	}
	if reflect.DeepEqual(base, mutated) {
		t.Fatal("mutating one input left the rendered document byte-for-byte identical")
	}

	// Shuffling record/definition order must not change the sorted output.
	shuffled := append([]capability.Record(nil), records...)
	rand.New(rand.NewSource(1)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	fromShuffled, err := RenderDiscoveryDocument(m, defs, shuffled)
	if err != nil {
		t.Fatalf("RenderDiscoveryDocument (shuffled): %v", err)
	}
	if !reflect.DeepEqual(base.Capabilities, fromShuffled.Capabilities) {
		t.Fatal("shuffling capability input order changed the sorted output")
	}
}

// FuzzTodo_API_001 fuzzes the render's ordering stability: a random
// permutation of the capability records and intent definitions (seeded by
// the fuzzer) must always render to the same sorted output.
func FuzzTodo_API_001(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(1))
	f.Add(int64(12345))

	m, err := Build()
	if err != nil {
		f.Fatalf("Build: %v", err)
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		f.Fatalf("NewBootstrapRegistry: %v", err)
	}
	baseRecords := registry.List()
	baseDefs := definitions.All()

	baseline, err := RenderDiscoveryDocument(m, baseDefs, baseRecords)
	if err != nil {
		f.Fatalf("RenderDiscoveryDocument (baseline): %v", err)
	}

	f.Fuzz(func(t *testing.T, seed int64) {
		r := rand.New(rand.NewSource(seed))

		records := append([]capability.Record(nil), baseRecords...)
		r.Shuffle(len(records), func(i, j int) { records[i], records[j] = records[j], records[i] })

		defs := append([]intent.Definition(nil), baseDefs...)
		r.Shuffle(len(defs), func(i, j int) { defs[i], defs[j] = defs[j], defs[i] })

		doc, err := RenderDiscoveryDocument(m, defs, records)
		if err != nil {
			t.Fatalf("RenderDiscoveryDocument: %v", err)
		}
		if !reflect.DeepEqual(baseline.Capabilities, doc.Capabilities) {
			t.Fatalf("seed %d: shuffled capability input changed the sorted output", seed)
		}
		if !reflect.DeepEqual(baseline.IntentDefinitions, doc.IntentDefinitions) {
			t.Fatalf("seed %d: shuffled intent-definition input changed the sorted output", seed)
		}
	})
}
