package selectionbind

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/topology"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotblueprint"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotcommercial"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister"
)

// Status is a selection-completeness verdict.
type Status string

// The two completeness verdicts. There is no partial or waived state:
// anything short of every binding ready and every slot filled is INCOMPLETE.
const (
	StatusComplete   Status = "COMPLETE"
	StatusIncomplete Status = "INCOMPLETE"
)

// SlotNames is PHASE-001's exact, ordered selection-slot set.
var SlotNames = []string{"provider", "jurisdiction", "topology", "slo"}

// slotSources names, per slot, every bound todo whose own gate this
// evaluator consults to decide it. A ceiling slot naming a filler outside
// this set fails closed.
var slotSources = map[string][]string{
	"provider":     {"SELECT-002"},
	"jurisdiction": {"SELECT-001"},
	"topology":     {"SELECT-002", "TOPOLOGY-001"},
	"slo":          {"SELECT-002", "COMMERCIAL-001"},
}

// Options configures Evaluate.
type Options struct {
	// Root is the directory binding paths resolve against (the repository
	// root in production, a fixture tree in tests).
	Root string
	// Now dates freshness-sensitive gates (customer confirmations, residual
	// risk expiry).
	Now time.Time
	// TrustedPublicKey is the only hex ed25519 key whose signatures count.
	// A validly signed artifact under any other key is untrusted.
	TrustedPublicKey string
	// Customer is the design partner's confirmed facts. The zero value is
	// the accurate current fact: no design partner has been selected.
	Customer pilotblueprint.CustomerFacts
}

// ErrOptions reports an Evaluate call that cannot produce a meaningful
// verdict (no root, no trusted key or no clock).
var ErrOptions = errors.New("selectionbind: incomplete options")

// BindingResult is one bound artifact's verdict.
type BindingResult struct {
	TodoID  string   `json:"todo_id"`
	Path    string   `json:"path"`
	Digest  string   `json:"digest"`
	Ready   bool     `json:"ready"`
	Reasons []string `json:"reasons"`
}

// SlotResult is one PHASE-001 selection slot's verdict.
type SlotResult struct {
	Slot           string   `json:"slot"`
	FillingTodoIDs []string `json:"filling_todo_ids"`
	Filled         bool     `json:"filled"`
	Reasons        []string `json:"reasons"`
}

// Report is Evaluate's result.
type Report struct {
	ManifestDigest  string          `json:"manifest_digest"`
	AsOf            string          `json:"as_of"`
	Status          Status          `json:"status"`
	ManifestReasons []string        `json:"manifest_reasons"`
	Bindings        []BindingResult `json:"bindings"`
	Slots           []SlotResult    `json:"slots"`
}

// Reasons flattens every named reason in r, each prefixed by its scope.
func (r Report) Reasons() []string {
	var out []string
	for _, reason := range r.ManifestReasons {
		out = append(out, "manifest: "+reason)
	}
	for _, b := range r.Bindings {
		for _, reason := range b.Reasons {
			out = append(out, b.TodoID+": "+reason)
		}
	}
	for _, s := range r.Slots {
		for _, reason := range s.Reasons {
			out = append(out, "slot "+s.Slot+": "+reason)
		}
	}
	return out
}

// RenderJSON renders r deterministically.
func RenderJSON(r Report) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// signatureReasons checks one artifact signature and the trusted key.
func signatureReasons(ok bool, err error, publicKey, trusted string) []string {
	switch {
	case err != nil:
		return []string{fmt.Sprintf("signature unverifiable: %v", err)}
	case !ok:
		return []string{"signature does not verify against the artifact's canonical digest - tampered"}
	case publicKey != trusted:
		return []string{fmt.Sprintf("signed by untrusted key %s", publicKey)}
	}
	return nil
}

func violationStrings[V fmt.Stringer](prefix string, vs []V) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, prefix+v.String())
	}
	return out
}

// loaded carries every bound artifact Evaluate could read.
type loaded struct {
	paths    map[string]string // todo -> repository-relative path
	ceiling  *scopeceiling.ScopeCeilingManifest
	profile  *pilotjurisdiction.JurisdictionProfile
	provider *pilotprovider.ProviderTopology
	customer *pilotblueprint.Blueprint
	decision *topology.Decision
	freeze   *pilotcommercial.PilotCommercialFreeze
	register *threatregister.Register
}

// Evaluate computes the selection completeness of m under opts. It never
// trusts m's claims about its selections: it recomputes every binding's
// digest, verifies every bound artifact's signature against the trusted key,
// and runs each artifact's own real-selection or readiness gate. The only
// error it returns is ErrOptions; every defect in the manifest or its
// selections is a named reason in an INCOMPLETE report.
func Evaluate(m gateevidence.P1AManifest, opts Options) (Report, error) {
	if opts.Root == "" || opts.TrustedPublicKey == "" || opts.Now.IsZero() {
		return Report{}, ErrOptions
	}
	report := Report{AsOf: opts.Now.UTC().Format("2006-01-02"), Status: StatusIncomplete}
	if digest, err := m.CanonicalDigest(); err != nil {
		report.ManifestReasons = append(report.ManifestReasons, err.Error())
	} else {
		report.ManifestDigest = digest
	}
	report.ManifestReasons = append(report.ManifestReasons, violationStrings("", m.Validate())...)
	ok, err := gateevidence.VerifyManifestSignature(m)
	publicKey := ""
	if m.Signature != nil {
		publicKey = m.Signature.PublicKey
	}
	report.ManifestReasons = append(report.ManifestReasons, signatureReasons(ok, err, publicKey, opts.TrustedPublicKey)...)

	state := loaded{paths: map[string]string{}}
	results := map[string]*BindingResult{}
	for _, todo := range gateevidence.RequiredSelectionBindingTodoIDs {
		report.Bindings = append(report.Bindings, BindingResult{TodoID: todo})
	}
	for i := range report.Bindings {
		results[report.Bindings[i].TodoID] = &report.Bindings[i]
	}
	for _, b := range m.SelectionBindings {
		res, required := results[b.TodoID]
		if !required || res.Path != "" {
			continue // Validate already named the unexpected or duplicate binding
		}
		res.Path, res.Digest = b.Path, b.Digest
		state.paths[b.TodoID] = b.Path
		for _, v := range VerifyBindings(opts.Root, []gateevidence.SelectionBinding{b}) {
			res.Reasons = append(res.Reasons, v.Issue)
		}
		state.load(opts.Root, b, res)
	}
	for _, res := range report.Bindings {
		if res.Path == "" {
			results[res.TodoID].Reasons = append(results[res.TodoID].Reasons, "not bound by the manifest - an omitted selection")
		}
	}

	state.gates(m, opts, results)
	for i := range report.Bindings {
		report.Bindings[i].Ready = len(report.Bindings[i].Reasons) == 0
	}
	report.Slots = state.slots(results)

	complete := len(report.ManifestReasons) == 0
	for _, b := range report.Bindings {
		complete = complete && b.Ready
	}
	for _, s := range report.Slots {
		complete = complete && s.Filled
	}
	if complete {
		report.Status = StatusComplete
	}
	return report, nil
}

// load parses the artifact binding b names into s, recording any read or
// parse failure against res.
func (s *loaded) load(root string, b gateevidence.SelectionBinding, res *BindingResult) {
	path, err := resolve(root, b.Path)
	if err != nil {
		res.Reasons = append(res.Reasons, err.Error())
		return
	}
	fail := func(err error) { res.Reasons = append(res.Reasons, fmt.Sprintf("cannot load: %v", err)) }
	switch b.TodoID {
	case "PHASE-001":
		if s.ceiling, err = scopeceiling.LoadManifest(path); err != nil {
			fail(err)
		}
	case "SELECT-001":
		if s.profile, err = pilotjurisdiction.LoadProfile(path); err != nil {
			fail(err)
		}
	case "SELECT-002":
		if s.provider, err = pilotprovider.LoadTopology(path); err != nil {
			fail(err)
		}
	case "CUSTOMER-001":
		if s.customer, err = pilotblueprint.LoadBlueprint(path); err != nil {
			fail(err)
		}
	case "TOPOLOGY-001":
		if !strings.HasSuffix(b.Path, ".json") {
			return // a contract source, not a decision; the gate names it
		}
		content, err := os.ReadFile(path)
		if err != nil {
			fail(err)
			return
		}
		var d topology.Decision
		dec := json.NewDecoder(bytes.NewReader(content))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&d); err != nil {
			fail(err)
			return
		}
		s.decision = &d
	case "COMMERCIAL-001":
		if s.freeze, err = pilotcommercial.LoadFreeze(path); err != nil {
			fail(err)
		}
	case "THREAT-001":
		if s.register, err = threatregister.LoadRegister(path); err != nil {
			fail(err)
		}
	}
}

// sloSelected reports whether f promises an SLO rather than NONE.
func sloSelected(f *pilotcommercial.PilotCommercialFreeze) bool {
	return f != nil && f.Evidence.SLOStatus != pilotcommercial.SLOStatusNone && strings.TrimSpace(f.Evidence.SLOStatement) != ""
}

// sloPremiseRule reports whether a COMMERCIAL-001 finding is the one rule
// whose only premise is "PHASE-001's slo slot is unfilled": slo_status must
// be NONE. NEXT-002 is the documented filler of that slot, so when a freeze
// genuinely promises an SLO this evaluator is the check that decides the
// slot, and that single premise-conditional rule is not applied to it.
// Every other COMMERCIAL-001 rule - including every other slo_status rule -
// still blocks.
func sloPremiseRule(f *pilotcommercial.PilotCommercialFreeze, field, issue string) bool {
	return sloSelected(f) && field == "evidence.slo_status" && strings.HasPrefix(issue, "must be "+pilotcommercial.SLOStatusNone+" while")
}

// gates runs every loaded artifact's own gate, appending reasons to results.
func (s *loaded) gates(m gateevidence.P1AManifest, opts Options, results map[string]*BindingResult) {
	add := func(todo string, reasons ...string) {
		results[todo].Reasons = append(results[todo].Reasons, reasons...)
	}
	trusted := opts.TrustedPublicKey

	if c := s.ceiling; c != nil {
		ok, err := scopeceiling.VerifyManifestSignature(*c)
		add("PHASE-001", signatureReasons(ok, err, sigKey(c.Signature != nil, func() string { return c.Signature.PublicKey }), trusted)...)
		add("PHASE-001", violationStrings("", c.Validate())...)
		add("PHASE-001", ceilingCoversManifest(*c, m)...)
	}
	if p := s.profile; p != nil {
		ok, err := pilotjurisdiction.VerifyProfileSignature(*p)
		add("SELECT-001", signatureReasons(ok, err, sigKey(p.Signature != nil, func() string { return p.Signature.PublicKey }), trusted)...)
		add("SELECT-001", violationStrings("", p.Validate())...)
		if status, err := legal.ParseReviewStatus(p.ReviewStatus); err != nil || !status.Releasable() {
			add("SELECT-001", fmt.Sprintf("review_status %q is not releasable - the jurisdiction profile is unreviewed", p.ReviewStatus))
		}
	}
	if t := s.provider; t != nil {
		ok, err := pilotprovider.VerifyTopologySignature(*t)
		add("SELECT-002", signatureReasons(ok, err, sigKey(t.Signature != nil, func() string { return t.Signature.PublicKey }), trusted)...)
		add("SELECT-002", violationStrings("", t.Validate())...)
		if pass, violations := t.SatisfiesRealProviderSelectionGate(); !pass {
			add("SELECT-002", violationStrings("real provider-selection gate: ", violations)...)
		}
	}
	s.topologyGate(add)
	s.customerGate(opts, add)
	s.commercialGate(opts, add)
	if r := s.register; r != nil {
		ok, err := threatregister.VerifyRegisterSignature(*r)
		add("THREAT-001", signatureReasons(ok, err, sigKey(r.Signature != nil, func() string { return r.Signature.PublicKey }), trusted)...)
		add("THREAT-001", violationStrings("", r.Validate())...)
		if blocked, blockers := r.ReleaseDecision(opts.Now); blocked {
			add("THREAT-001", violationStrings("release blocked: ", blockers)...)
		}
	}
}

func sigKey(present bool, key func() string) string {
	if !present {
		return ""
	}
	return key()
}

func (s *loaded) topologyGate(add func(string, ...string)) {
	path, bound := s.paths["TOPOLOGY-001"]
	if !bound {
		return
	}
	if s.decision == nil {
		if !strings.HasSuffix(path, ".json") {
			add("TOPOLOGY-001", fmt.Sprintf("no deployable topology decision artifact is checked in; %s pins only the TOPOLOGY-001 contract source, so topology.Compile has no decision to evaluate", path))
		}
		return
	}
	evidence, err := topology.Compile(*s.decision)
	if err != nil {
		add("TOPOLOGY-001", fmt.Sprintf("topology.Compile rejects the decision: %v", err))
		return
	}
	if !evidence.Ready {
		add("TOPOLOGY-001", fmt.Sprintf("topology decision status %s: blockers %v, human inputs %v", evidence.Status, evidence.Blockers, evidence.HumanInputs))
	}
}

func (s *loaded) customerGate(opts Options, add func(string, ...string)) {
	bp := s.customer
	if bp == nil {
		return
	}
	ok, err := pilotblueprint.VerifyBlueprintSignature(*bp)
	add("CUSTOMER-001", signatureReasons(ok, err, sigKey(bp.Signature != nil, func() string { return bp.Signature.PublicKey }), opts.TrustedPublicKey)...)
	add("CUSTOMER-001", violationStrings("", bp.Validate())...)
	for _, ref := range []struct{ field, got, todo string }{
		{"provider_topology_ref", bp.ProviderTopologyRef, "SELECT-002"},
		{"jurisdiction_profile_ref", bp.JurisdictionProfileRef, "SELECT-001"},
		{"scope_ceiling_ref", bp.ScopeCeilingRef, "PHASE-001"},
	} {
		if want := s.paths[ref.todo]; ref.got != want {
			add("CUSTOMER-001", fmt.Sprintf("%s %q is not the %s artifact the manifest binds (%q)", ref.field, ref.got, ref.todo, want))
		}
	}
	if s.provider == nil || s.profile == nil {
		add("CUSTOMER-001", "readiness cannot be instantiated without the bound SELECT-001 and SELECT-002 artifacts")
		return
	}
	readiness := pilotblueprint.Instantiate(*bp, opts.Customer, *s.provider, *s.profile, opts.Now)
	if readiness.Overall == pilotblueprint.ReadinessReady {
		return
	}
	notReady := 0
	seen := map[string]bool{}
	var reasons []string
	for _, w := range readiness.Workstreams {
		if w.Status == pilotblueprint.ReadinessReady {
			continue
		}
		notReady++
		for _, reason := range w.Reasons {
			line := fmt.Sprintf("%s: %s", w.Status, reason)
			if !seen[line] {
				seen[line] = true
				reasons = append(reasons, line)
			}
		}
	}
	add("CUSTOMER-001", fmt.Sprintf("Instantiate reports %s (%d of %d workstreams not READY)", readiness.Overall, notReady, len(readiness.Workstreams)))
	add("CUSTOMER-001", reasons...)
}

func (s *loaded) commercialGate(opts Options, add func(string, ...string)) {
	f := s.freeze
	if f == nil {
		return
	}
	ok, err := pilotcommercial.VerifyFreezeSignature(*f)
	add("COMMERCIAL-001", signatureReasons(ok, err, sigKey(f.Signature != nil, func() string { return f.Signature.PublicKey }), opts.TrustedPublicKey)...)
	for _, v := range f.Validate() {
		if !sloPremiseRule(f, v.Field, v.Issue) {
			add("COMMERCIAL-001", v.String())
		}
	}
	ceilingPath, okC := s.paths["PHASE-001"]
	profilePath, okJ := s.paths["SELECT-001"]
	providerPath, okP := s.paths["SELECT-002"]
	if !okC || !okJ || !okP {
		add("COMMERCIAL-001", "live-registry conformance needs the bound PHASE-001, SELECT-001 and SELECT-002 artifacts")
		return
	}
	abs := func(p string) string { r, _ := resolve(opts.Root, p); return r }
	mismatches, err := pilotcommercial.ConformsToLiveRegistries(*f, abs(ceilingPath), abs(profilePath), abs(providerPath))
	if err != nil {
		add("COMMERCIAL-001", fmt.Sprintf("live-registry conformance could not run: %v", err))
		return
	}
	for _, mm := range mismatches {
		if !sloPremiseRule(f, mm.Field, mm.Issue) {
			add("COMMERCIAL-001", "live registry: "+mm.String())
		}
	}
}

// ceilingCoversManifest proves every P1A intent, capability and the workflow
// sit inside the ceiling as INCLUDE at gate P1A.
func ceilingCoversManifest(c scopeceiling.ScopeCeilingManifest, m gateevidence.P1AManifest) []string {
	var reasons []string
	intents := map[string]scopeceiling.IntentItem{}
	for _, it := range c.Intents {
		intents[it.ID] = it
	}
	for _, it := range m.Intents {
		if ci, ok := intents[it.ID]; !ok || ci.Disposition != scopeceiling.Include || ci.Gate != scopeceiling.GateP1A {
			reasons = append(reasons, fmt.Sprintf("P1A intent %s is not INCLUDE at gate P1A in the scope ceiling - unauthorized scope", it.ID))
		}
	}
	capabilities := map[string]scopeceiling.CapabilityItem{}
	for _, ci := range c.Capabilities {
		capabilities[ci.ID] = ci
	}
	for _, mc := range m.Capabilities {
		if ci, ok := capabilities[mc.ID]; !ok || ci.Disposition != scopeceiling.Include || ci.Gate != scopeceiling.GateP1A {
			reasons = append(reasons, fmt.Sprintf("P1A capability %s is not INCLUDE at gate P1A in the scope ceiling - unauthorized scope", mc.ID))
		}
	}
	workflowOK := false
	for _, w := range c.Workflows {
		workflowOK = workflowOK || (w.ID == m.Workflow && w.Disposition == scopeceiling.Include && w.Gate == scopeceiling.GateP1A)
	}
	if !workflowOK {
		reasons = append(reasons, fmt.Sprintf("P1A workflow %s is not INCLUDE at gate P1A in the scope ceiling", m.Workflow))
	}
	return reasons
}

// slots resolves PHASE-001's four selection slots from the bound
// artifacts' gate verdicts.
func (s *loaded) slots(results map[string]*BindingResult) []SlotResult {
	ready := func(todo string) bool { return len(results[todo].Reasons) == 0 }
	ceilingSlots := map[string]scopeceiling.SelectionSlot{}
	if s.ceiling != nil {
		for _, slot := range s.ceiling.SelectionSlots {
			ceilingSlots[slot.Name] = slot
		}
	}
	var out []SlotResult
	for _, name := range SlotNames {
		res := SlotResult{Slot: name}
		slot, declared := ceilingSlots[name]
		if !declared {
			res.Reasons = append(res.Reasons, "the bound scope ceiling declares no such selection slot")
		}
		for _, filler := range strings.Split(slot.FillingTodoID, ",") {
			if filler = strings.TrimSpace(filler); filler == "" {
				continue
			}
			res.FillingTodoIDs = append(res.FillingTodoIDs, filler)
			if !contains(slotSources[name], filler) {
				res.Reasons = append(res.Reasons, fmt.Sprintf("ceiling names filler %s, which this evaluator does not consult for %s", filler, name))
			}
		}
		for _, source := range slotSources[name] {
			if !ready(source) {
				res.Reasons = append(res.Reasons, fmt.Sprintf("%s has not passed its own gate (%d reason(s))", source, len(results[source].Reasons)))
			}
		}
		switch name {
		case "topology":
			if s.decision != nil && s.provider != nil && s.decision.Topology.Selection.ProviderRef != s.provider.Provider.VendorID {
				res.Reasons = append(res.Reasons, fmt.Sprintf("topology decision provider_ref %q disagrees with SELECT-002 vendor_id %q", s.decision.Topology.Selection.ProviderRef, s.provider.Provider.VendorID))
			}
		case "slo":
			if !sloSelected(s.freeze) {
				res.Reasons = append(res.Reasons, "COMMERCIAL-001 promises no SLO (evidence.slo_status NONE) - no SLO has been selected")
			}
		}
		res.Filled = len(res.Reasons) == 0
		out = append(out, res)
	}
	if len(ceilingSlots) != len(SlotNames) && s.ceiling != nil {
		out[0].Reasons = append(out[0].Reasons, fmt.Sprintf("the scope ceiling declares %d selection slots, want exactly %v", len(ceilingSlots), SlotNames))
		out[0].Filled = false
	}
	return out
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// EvaluateP1B returns every reason tpl is not a valid, trusted, current P1B
// template for the P1A manifest under root: structural violations, an
// untrusted or broken signature, a stale P1A binding, or overlap with P1A.
func EvaluateP1B(tpl gateevidence.P1BTemplate, root, trustedPublicKey string) []string {
	reasons := violationStrings("", tpl.Validate())
	ok, err := gateevidence.VerifyP1BTemplateSignature(tpl)
	reasons = append(reasons, signatureReasons(ok, err, sigKey(tpl.Signature != nil, func() string { return tpl.Signature.PublicKey }), trustedPublicKey)...)
	path, err := resolve(root, tpl.P1AManifest.Path)
	if err != nil {
		return append(reasons, err.Error())
	}
	p1a, err := gateevidence.LoadP1AManifest(path)
	if err != nil {
		return append(reasons, fmt.Sprintf("cannot load bound P1A manifest: %v", err))
	}
	return append(reasons, violationStrings("", gateevidence.CheckDisjoint(*p1a, tpl))...)
}
