package selectionbind

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/topology"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotblueprint"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotcommercial"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
)

// repoRoot is tools/planning/gateevidence/selectionbind -> repository root.
const repoRoot = "../../../.."

const (
	gatesDir         = "definitions/planning/gates/"
	ceilingPath      = gatesDir + "phase1-scope-ceiling.yaml"
	jurisdictionPath = gatesDir + "select-001-jurisdiction-profile.yaml"
	providerPath     = gatesDir + "select-002-provider-topology.yaml"
	blueprintPath    = gatesDir + "customer-001-pilot-blueprint.yaml"
	commercialPath   = gatesDir + "commercial-001-pilot-package.yaml"
	threatPath       = gatesDir + "threat-001-register.yaml"
	p1bTemplatePath  = gatesDir + "p1b-template.yaml"
	keyFixture       = "tools/planning/gateevidence/testdata/dev-signing-key.yaml"
	// fixtureTopologyPath is where a synthetic tree places a deployable
	// TOPOLOGY-001 decision; the live repository has none.
	fixtureTopologyPath = gatesDir + "topology-001-decision.json"
	fixtureVendorID     = "fixture-core-hcm-vendor"
)

// evaluationDate is the date every live and fixture evaluation is pinned to.
var evaluationDate = time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)

func signingKey(t testing.TB) (ed25519.PrivateKey, string) {
	t.Helper()
	priv, err := provenance.LoadSigningKeyFixture(filepath.Join(repoRoot, keyFixture))
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture: %v", err)
	}
	return priv, hex.EncodeToString(priv.Public().(ed25519.PublicKey))
}

func mustLoadLiveManifest(t testing.TB) gateevidence.P1AManifest {
	t.Helper()
	m, err := gateevidence.LoadP1AManifest(filepath.Join(repoRoot, gateevidence.P1AManifestPath))
	if err != nil {
		t.Fatalf("LoadP1AManifest: %v", err)
	}
	return *m
}

func mustLoadLiveTemplate(t testing.TB) gateevidence.P1BTemplate {
	t.Helper()
	tpl, err := gateevidence.LoadP1BTemplate(filepath.Join(repoRoot, p1bTemplatePath))
	if err != nil {
		t.Fatalf("LoadP1BTemplate: %v", err)
	}
	return *tpl
}

func liveOptions(t testing.TB) Options {
	_, pub := signingKey(t)
	return Options{Root: repoRoot, Now: evaluationDate, TrustedPublicKey: pub}
}

func signManifest(t testing.TB, m gateevidence.P1AManifest, priv ed25519.PrivateKey, pub string) gateevidence.P1AManifest {
	t.Helper()
	digest, err := m.CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := gateevidence.SignDigest(priv, digest)
	if err != nil {
		t.Fatal(err)
	}
	m.Signature = &gateevidence.Signature{Algorithm: "ed25519", PublicKey: pub, Value: sig, KeyFixture: keyFixture}
	return m
}

func cloneManifest(m gateevidence.P1AManifest) gateevidence.P1AManifest {
	m.Intents = append([]gateevidence.Intent(nil), m.Intents...)
	m.Capabilities = append([]gateevidence.Capability(nil), m.Capabilities...)
	m.EffectCeiling = append([]string(nil), m.EffectCeiling...)
	m.ForbiddenEffects = append([]string(nil), m.ForbiddenEffects...)
	m.SelectionBindings = append([]gateevidence.SelectionBinding(nil), m.SelectionBindings...)
	return m
}

func writeYAML(t testing.TB, root, rel string, v any) {
	t.Helper()
	b, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, rel, b)
}

func writeFile(t testing.TB, root, rel string, b []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyLive(t testing.TB, root, rel string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, rel, b)
}

// fix names one real-world fact that is missing from the live repository.
// A synthetic tree applies a subset of fixes; the fully consistent tree
// applies all of them.
type fix string

const (
	fixProvider     fix = "vendor-confirmed provider"
	fixJurisdiction fix = "reviewed jurisdiction"
	fixTopology     fix = "deployable topology decision"
	fixCustomer     fix = "confirmed design partner"
	fixSLO          fix = "selected SLO"
	fixThreat       fix = "mitigated critical ambiguous effect"
)

var allFixes = []fix{fixProvider, fixJurisdiction, fixTopology, fixCustomer, fixSLO, fixThreat}

// fixture is a synthetic repository tree plus the options and signed
// manifest that evaluate it.
type fixture struct {
	root     string
	opts     Options
	manifest gateevidence.P1AManifest
	priv     ed25519.PrivateKey
	pub      string
}

func has(fixes []fix, f fix) bool {
	for _, x := range fixes {
		if x == f {
			return true
		}
	}
	return false
}

// buildFixture writes a synthetic tree derived from the live signed
// artifacts with exactly the given fixes applied, each artifact re-signed
// with the development key, and returns a P1A manifest freshly bound to that
// tree and signed. No fix is ever applied to a checked-in file.
func buildFixture(t testing.TB, fixes ...fix) fixture {
	t.Helper()
	priv, pub := signingKey(t)
	root := t.TempDir()

	copyLive(t, root, ceilingPath)
	copyLive(t, root, blueprintPath)

	profile, err := pilotjurisdiction.LoadProfile(filepath.Join(repoRoot, jurisdictionPath))
	if err != nil {
		t.Fatal(err)
	}
	if has(fixes, fixJurisdiction) {
		profile.Reviewer = pilotjurisdiction.Reviewer{Name: "Fixture Counsel", Qualification: "fixture: admitted California employment counsel", AssignedDate: "2026-09-01"}
		profile.ReviewStatus = "COUNSEL_APPROVED"
	}
	signedProfile, err := pilotjurisdiction.SignProfile(*profile, priv, pub, keyFixture)
	if err != nil {
		t.Fatal(err)
	}
	writeYAML(t, root, jurisdictionPath, signedProfile)

	provider, err := pilotprovider.LoadTopology(filepath.Join(repoRoot, providerPath))
	if err != nil {
		t.Fatal(err)
	}
	if has(fixes, fixProvider) {
		provider.SelectionStatus = pilotprovider.StatusVendorConfirmed
		provider.Provider.VendorID = fixtureVendorID
		provider.Provider.VendorDisplayName = "Fixture Core HCM Vendor (synthetic test fixture)"
		provider.VendorConfirmation = pilotprovider.VendorConfirmation{ConfirmedByContact: "fixture-contact@example.test", ContractDocumentRef: "fixture-contract-001", SignedEffectiveDate: "2026-09-01"}
	}
	signedProvider, err := pilotprovider.SignTopology(*provider, priv, pub, keyFixture)
	if err != nil {
		t.Fatal(err)
	}
	writeYAML(t, root, providerPath, signedProvider)

	freeze, err := pilotcommercial.LoadFreeze(filepath.Join(repoRoot, commercialPath))
	if err != nil {
		t.Fatal(err)
	}
	if has(fixes, fixJurisdiction) {
		freeze.Jurisdiction.Status = pilotcommercial.PromiseSelectedConfirmed
		freeze.Jurisdiction.ReviewStatus = "COUNSEL_APPROVED"
	}
	if has(fixes, fixProvider) {
		freeze.Provider.Status = pilotcommercial.PromiseSelectedConfirmed
		freeze.Provider.VendorRef = fixtureVendorID
	}
	if has(fixes, fixSLO) {
		freeze.Evidence.SLOStatus = "SELECTED"
		freeze.Evidence.SLOStatement = "fixture: interactive p95 <= 2s over the pilot window"
	}
	signedFreeze, err := pilotcommercial.SignFreeze(*freeze, priv, pub, keyFixture)
	if err != nil {
		t.Fatal(err)
	}
	writeYAML(t, root, commercialPath, signedFreeze)

	register, err := threatregister.LoadRegister(filepath.Join(repoRoot, threatPath))
	if err != nil {
		t.Fatal(err)
	}
	if has(fixes, fixThreat) {
		// The live signed register now supplies the exact-path mitigation.
	} else {
		for i := range register.Slices {
			for j := range register.Slices[i].Threats {
				if register.Slices[i].Threats[j].ID == "THR-07" {
					register.Slices[i].Threats[j].Mitigations = nil
				}
			}
			mitigations := register.Slices[i].Mitigations[:0]
			for _, mitigation := range register.Slices[i].Mitigations {
				if mitigation.ID != "MIT-ATOMIC-DOMAIN-ADVANCEMENT" {
					mitigations = append(mitigations, mitigation)
				}
			}
			register.Slices[i].Mitigations = mitigations
		}
	}
	signedRegister, err := threatregister.SignRegister(*register, priv, pub, keyFixture)
	if err != nil {
		t.Fatal(err)
	}
	writeYAML(t, root, threatPath, signedRegister)

	topologyBinding := gateevidence.SelectionBinding{TodoID: "TOPOLOGY-001", Path: "internal/platform/topology/topology.go", DigestKind: gateevidence.DigestKindFileSHA256}
	if has(fixes, fixTopology) {
		b, err := json.MarshalIndent(fixtureDecision(), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, root, fixtureTopologyPath, b)
		topologyBinding.Path = fixtureTopologyPath
	} else {
		copyLive(t, root, topologyBinding.Path)
	}

	m := cloneManifest(mustLoadLiveManifest(t))
	m.SelectionBindings = []gateevidence.SelectionBinding{
		{TodoID: "PHASE-001", Path: ceilingPath, DigestKind: gateevidence.DigestKindCanonicalJSON},
		{TodoID: "SELECT-001", Path: jurisdictionPath, DigestKind: gateevidence.DigestKindCanonicalJSON},
		{TodoID: "SELECT-002", Path: providerPath, DigestKind: gateevidence.DigestKindCanonicalJSON},
		{TodoID: "CUSTOMER-001", Path: blueprintPath, DigestKind: gateevidence.DigestKindCanonicalJSON},
		topologyBinding,
		{TodoID: "COMMERCIAL-001", Path: commercialPath, DigestKind: gateevidence.DigestKindCanonicalJSON},
		{TodoID: "THREAT-001", Path: threatPath, DigestKind: gateevidence.DigestKindCanonicalJSON},
	}
	for i := range m.SelectionBindings {
		d, err := LiveDigest(root, m.SelectionBindings[i])
		if err != nil {
			t.Fatalf("LiveDigest(%s): %v", m.SelectionBindings[i].TodoID, err)
		}
		m.SelectionBindings[i].Digest = d
	}
	m = signManifest(t, m, priv, pub)
	writeYAML(t, root, gateevidence.P1AManifestPath, m)

	opts := Options{Root: root, Now: evaluationDate, TrustedPublicKey: pub}
	if has(fixes, fixCustomer) {
		opts.Customer = pilotblueprint.CustomerFacts{DesignPartnerName: "Fixture Design Partner Inc.", WorkstreamOwners: map[pilotblueprint.WorkstreamKind]pilotblueprint.OwnerConfirmation{}}
		for _, k := range pilotblueprint.AllWorkstreamKinds() {
			opts.Customer.WorkstreamOwners[k] = pilotblueprint.OwnerConfirmation{Name: "Fixture Owner", Contact: "owner@example.test", ConfirmedAt: evaluationDate.Add(-24 * time.Hour).Format(time.RFC3339)}
		}
	}
	return fixture{root: root, opts: opts, manifest: m, priv: priv, pub: pub}
}

// fixtureDecision is a deployable, non-placeholder TOPOLOGY-001 decision
// whose provider_ref agrees with the fixture provider's vendor_id.
func fixtureDecision() topology.Decision {
	m := topology.Manifest{
		SchemaVersion: 1, DecisionID: "decision:pilot-cell:fixture", CellID: "cell:pilot-fixture", Environment: "sandbox",
		Selection:    topology.Selection{ProviderRef: fixtureVendorID, Region: "fixture-region-1", OwnerRef: "fixture-cell-owner"},
		Zones:        []string{"zone-a", "zone-b"},
		Processes:    []topology.Process{{ID: "api", Role: "request-handler", DataDependencies: []string{"config", "ledger"}}, {ID: "worker", Role: "queue-consumer", DataDependencies: []string{"outbox"}}},
		Paths:        []topology.Path{{ID: "api-ledger", From: "api", To: "ledger", Plane: "data", Trust: "cell-identity", DataPath: "append-only-ledger"}},
		FailureModes: []topology.FailureMode{{ID: "zone-loss", Component: "zone", Degradation: "serve-from-surviving-zone", Recovery: "replace-and-rebalance"}},
		Budget:       topology.ResourceBudget{MaxTenants: 1, MaxConcurrentJobs: 8, CPUUnits: 4, MemoryMiB: 2048},
		Recovery:     topology.RecoveryPlan{BackupRef: "backup-policy:v1", RestoreProcedure: "restore-cell-checkpoint", DrainProcedure: "drain-with-fence", ReplacementPath: "replace-failed-zone"},
		Probes:       []topology.Probe{{Kind: "HEALTH", Expectation: "ready", BudgetSecs: 30}, {Kind: "LOAD", Expectation: "within envelope", BudgetSecs: 120}, {Kind: "DRAIN", Expectation: "fenced", BudgetSecs: 60}, {Kind: "RESTORE", Expectation: "digest matches", BudgetSecs: 180}},
	}
	return topology.Decision{Topology: m, Deploy: topology.DeploymentManifest{SchemaVersion: 1, Inventory: []string{"api", "config", "ledger", "worker", "outbox"}, Paths: m.Paths, Zones: m.Zones, FailureModes: m.FailureModes, Budget: m.Budget, Recovery: m.Recovery}}
}

func mustLoad[T any](t testing.TB, path string, load func(string) (*T, error)) *T {
	t.Helper()
	v, err := load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return v
}

var loadFreeze = pilotcommercial.LoadFreeze

func signFreezeWith(t testing.TB, freeze *pilotcommercial.PilotCommercialFreeze, f fixture) pilotcommercial.PilotCommercialFreeze {
	t.Helper()
	signed, err := pilotcommercial.SignFreeze(*freeze, f.priv, f.pub, keyFixture)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// resignAndRebind writes v at rel, re-binds todo's digest in the fixture
// manifest and re-signs the manifest, so the only thing left for Evaluate
// to judge is the artifact's own gate.
func resignAndRebind(t testing.TB, f *fixture, todo, rel string, v any) {
	t.Helper()
	if raw, ok := v.([]byte); ok {
		writeFile(t, f.root, rel, raw)
	} else {
		writeYAML(t, f.root, rel, v)
	}
	f.manifest = cloneManifest(f.manifest)
	for i, b := range f.manifest.SelectionBindings {
		if b.TodoID != todo {
			continue
		}
		b.Path = rel
		d, err := LiveDigest(f.root, b)
		if err != nil {
			t.Fatalf("LiveDigest(%s): %v", todo, err)
		}
		b.Digest = d
		f.manifest.SelectionBindings[i] = b
	}
	f.manifest = signManifest(t, f.manifest, f.priv, f.pub)
}

func rebindTopology(t testing.TB, f *fixture, d topology.Decision) {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	resignAndRebind(t, f, "TOPOLOGY-001", fixtureTopologyPath, b)
}

// editSignedDate moves one signed artifact's signed_date and re-signs it
// under the trusted key without re-binding the manifest.
func editSignedDate(t testing.TB, f fixture, rel string) {
	t.Helper()
	path := filepath.Join(f.root, filepath.FromSlash(rel))
	var signed any
	var err error
	switch rel {
	case jurisdictionPath:
		p := mustLoad(t, path, pilotjurisdiction.LoadProfile)
		p.SignedDate = "2026-09-14"
		signed, err = pilotjurisdiction.SignProfile(*p, f.priv, f.pub, keyFixture)
	case commercialPath:
		c := mustLoad(t, path, pilotcommercial.LoadFreeze)
		c.SignedDate = "2026-09-14"
		signed, err = pilotcommercial.SignFreeze(*c, f.priv, f.pub, keyFixture)
	case threatPath:
		r := mustLoad(t, path, threatregister.LoadRegister)
		r.SignedDate = "2026-09-14"
		signed, err = threatregister.SignRegister(*r, f.priv, f.pub, keyFixture)
	default:
		t.Fatalf("editSignedDate: unsupported %s", rel)
	}
	if err != nil {
		t.Fatal(err)
	}
	writeYAML(t, f.root, rel, signed)
}

func evaluate(t testing.TB, f fixture) Report {
	t.Helper()
	r, err := Evaluate(f.manifest, f.opts)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return r
}

func joinedReasons(r Report) string { return strings.Join(r.Reasons(), "\n") }

func bindingByTodo(r Report, todo string) BindingResult {
	for _, b := range r.Bindings {
		if b.TodoID == todo {
			return b
		}
	}
	return BindingResult{}
}

func slotByName(r Report, name string) SlotResult {
	for _, s := range r.Slots {
		if s.Slot == name {
			return s
		}
	}
	return SlotResult{}
}
