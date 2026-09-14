package scopeceiling

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Gate names the earliest Phase 1 release a scope item is candidate for.
// NONE marks an item that is source-bound but never executes in Phase 1
// (a conformance fixture, or explicitly rejected/deferred scope).
type Gate string

const (
	GateP1A  Gate = "P1A"
	GateP1B  Gate = "P1B"
	GateNone Gate = "NONE"
)

func (g Gate) valid() bool { return g == GateP1A || g == GateP1B || g == GateNone }

// Disposition is the explicit include/defer/reject rationale RED requires
// on every scope item.
type Disposition string

const (
	Include Disposition = "INCLUDE"
	Defer   Disposition = "DEFER"
	Reject  Disposition = "REJECT"
)

func (d Disposition) valid() bool { return d == Include || d == Defer || d == Reject }

// EndpointDisposition is the closed five-value expansion-rule enum from
// planning/specs/http-grpc-endpoint-contract.md#expansion-rule. An endpoint
// item with an empty or unknown value here is exactly RED's "endpoint
// without disposition".
type EndpointDisposition string

const (
	TypedPublicMethod           EndpointDisposition = "TYPED_PUBLIC_METHOD"
	GenericIntentLifecycleOnly  EndpointDisposition = "GENERIC_INTENT_LIFECYCLE_ONLY"
	InternalCapabilityOnly      EndpointDisposition = "INTERNAL_CAPABILITY_ONLY"
	EventOrScheduleOnly         EndpointDisposition = "EVENT_OR_SCHEDULE_ONLY"
	NoEndpointWithJustification EndpointDisposition = "NO_ENDPOINT_WITH_JUSTIFICATION"
)

func (e EndpointDisposition) valid() bool {
	switch e {
	case TypedPublicMethod, GenericIntentLifecycleOnly, InternalCapabilityOnly, EventOrScheduleOnly, NoEndpointWithJustification:
		return true
	}
	return false
}

// EffectClass is the closed capability effect-class vocabulary P1A's own
// manifest enforces (READ_ONLY) extended with the two additional classes
// the Phase 1 ceiling must be able to name without granting them: the
// bounded internal mutation family and the single external provider write
// next-steps.md's P1B section describes.
type EffectClass string

const (
	ReadOnly              EffectClass = "READ_ONLY"
	InternalMutation      EffectClass = "INTERNAL_MUTATION"
	ExternalProviderWrite EffectClass = "EXTERNAL_PROVIDER_WRITE"
	IrreversibleExternal  EffectClass = "IRREVERSIBLE_EXTERNAL_MUTATION"
	OutboundMessageIntent EffectClass = "OUTBOUND_MESSAGE_INTENT"
)

func (e EffectClass) valid() bool {
	switch e {
	case ReadOnly, InternalMutation, ExternalProviderWrite, IrreversibleExternal, OutboundMessageIntent:
		return true
	}
	return false
}

// UnboundProviderSentinel is the only value ProviderDependency may carry
// when a scope item genuinely implicates an external provider. Any other
// non-empty value names a concrete vendor/product before SELECT-002 has
// selected one - exactly RED's "hidden provider dependency".
const UnboundProviderSentinel = "UNBOUND_PENDING_SELECT-002"

// deferredDomainTokens name the future native suite-depth ownership
// RED's "future native payroll/WFM/talent ownership" clause refers to
// (planning/plan.md Portfolio Discipline: "Native payroll, WFM, benefits, or
// other suite-depth investment must pass the authority-absorption gate";
// planning/next-steps.md "Explicitly deferred": "native
// payroll/benefits/time/ATS/IAM"). No Phase 1 ceiling item may be INCLUDE
// with an identifier or owner domain containing one of these tokens.
var deferredDomainTokens = []string{
	"payroll",
	"wfm",
	"workforce_management",
	"talent",
	"benefits",
	"time_and_attendance",
	"recruiting",
	"ats",
	"immigration",
	"safety_incident",
}

// IntentItem is one candidate BusinessIntent definition
// (planning/specs/business-intent-catalog.md's fourteen DRAFT_CONTRACT
// definitions, cross-checked live against
// definitions/planning/capability-coverage.yaml's `kind: intent` entries).
type IntentItem struct {
	ID           string      `yaml:"id" json:"id"`
	OwnerDomain  string      `yaml:"owner_domain" json:"owner_domain"`
	KernelFamily string      `yaml:"kernel_family" json:"kernel_family"`
	SideEffect   string      `yaml:"side_effect" json:"side_effect"`
	Gate         Gate        `yaml:"gate" json:"gate"`
	Disposition  Disposition `yaml:"disposition" json:"disposition"`
	Rationale    string      `yaml:"rationale" json:"rationale"`
}

// CapabilityItem is one candidate capability-registry entry
// (internal/capability/bootstrap.go bootstrapDefinitions, cross-checked
// live against definitions/planning/capability-coverage.yaml's
// `kind: capability` entries).
type CapabilityItem struct {
	ID          string      `yaml:"id" json:"id"`
	OwnerDomain string      `yaml:"owner_domain" json:"owner_domain"`
	EffectClass EffectClass `yaml:"effect_class" json:"effect_class"`
	Gate        Gate        `yaml:"gate" json:"gate"`
	Disposition Disposition `yaml:"disposition" json:"disposition"`
	Rationale   string      `yaml:"rationale" json:"rationale"`
}

// WorkflowItem is one candidate compiled workflow definition. VerticalSlice
// must name a slice_id that actually exists in
// definitions/planning/product-slices.yaml - RED's "workflow without
// vertical slice".
type WorkflowItem struct {
	ID            string      `yaml:"id" json:"id"`
	VerticalSlice string      `yaml:"vertical_slice" json:"vertical_slice"`
	Gate          Gate        `yaml:"gate" json:"gate"`
	Disposition   Disposition `yaml:"disposition" json:"disposition"`
	Rationale     string      `yaml:"rationale" json:"rationale"`
}

// UserFlowItem is one candidate entry from planning/user-flows/catalog.md.
type UserFlowItem struct {
	ID          string      `yaml:"id" json:"id"`
	Archetype   string      `yaml:"archetype" json:"archetype"`
	Gate        Gate        `yaml:"gate" json:"gate"`
	Disposition Disposition `yaml:"disposition" json:"disposition"`
	Rationale   string      `yaml:"rationale" json:"rationale"`
}

// EndpointItem is one candidate typed endpoint from
// planning/specs/http-grpc-endpoint-contract.md's initial inventory, cross-
// checked live against definitions/api/endpoint-manifest.json for the
// endpoints that already exist there.
type EndpointItem struct {
	EndpointID          string              `yaml:"endpoint_id" json:"endpoint_id"`
	ServiceFullName     string              `yaml:"service_full_name" json:"service_full_name"`
	MethodName          string              `yaml:"method_name" json:"method_name"`
	Generated           bool                `yaml:"generated" json:"generated"`
	Gate                Gate                `yaml:"gate" json:"gate"`
	EndpointDisposition EndpointDisposition `yaml:"endpoint_disposition" json:"endpoint_disposition"`
	Disposition         Disposition         `yaml:"disposition" json:"disposition"`
	Rationale           string              `yaml:"rationale" json:"rationale"`
}

// ModelItem is one candidate canonical domain or kernel model contract
// named directly by planning/execution-plan.md's Plane Dependency Rule
// section (not the 530-name, source-unbound vocabulary in
// planning/data/models/intent-coverage-matrix.md).
type ModelItem struct {
	ID          string      `yaml:"id" json:"id"`
	SpecRef     string      `yaml:"spec_ref" json:"spec_ref"`
	Gate        Gate        `yaml:"gate" json:"gate"`
	Disposition Disposition `yaml:"disposition" json:"disposition"`
	Rationale   string      `yaml:"rationale" json:"rationale"`
}

// EffectItem is one candidate effect class Phase 1 build work may ever
// produce. ProviderDependency is either "" or exactly
// [UnboundProviderSentinel].
type EffectItem struct {
	Class              EffectClass `yaml:"class" json:"class"`
	Gate               Gate        `yaml:"gate" json:"gate"`
	Disposition        Disposition `yaml:"disposition" json:"disposition"`
	ProviderDependency string      `yaml:"provider_dependency,omitempty" json:"provider_dependency,omitempty"`
	Rationale          string      `yaml:"rationale" json:"rationale"`
}

// SelectionSlot is a named selection this manifest can only ever leave
// empty. Filled and Value must stay false/"" in every checked-in ceiling;
// NEXT-002's dependency chain (SELECT-001, SELECT-002, CUSTOMER-001,
// TOPOLOGY-001, COMMERCIAL-001) fills the real selection elsewhere.
type SelectionSlot struct {
	Name          string `yaml:"name" json:"name"`
	FillingTodoID string `yaml:"filling_todo_id" json:"filling_todo_id"`
	Filled        bool   `yaml:"filled" json:"filled"`
	Value         string `yaml:"value,omitempty" json:"value,omitempty"`
}

// DeferredDomain names one bucket of source-bound-but-out-of-phase catalog
// scope (planning/next-steps.md "Explicitly deferred";
// planning/plan.md#3.12 Portfolio Discipline). This is what makes "all
// other catalog items remain visibly deferred" a checkable list rather than
// a sentence in a comment.
type DeferredDomain struct {
	Name      string `yaml:"name" json:"name"`
	SourceRef string `yaml:"source_ref" json:"source_ref"`
	Rationale string `yaml:"rationale" json:"rationale"`
}

// Signature is an Ed25519 signature over a ScopeCeilingManifest's
// CanonicalDigest, structurally identical to
// tools/planning/gateevidence.Signature.
type Signature struct {
	Algorithm  string `yaml:"algorithm" json:"algorithm"`
	PublicKey  string `yaml:"public_key" json:"public_key"`
	Value      string `yaml:"value" json:"value"`
	KeyFixture string `yaml:"key_fixture" json:"key_fixture"`
}

// ScopeCeilingManifest is PHASE-001's signed scope-ceiling manifest schema
// (definitions/planning/gates/phase1-scope-ceiling.yaml).
type ScopeCeilingManifest struct {
	SchemaVersion       int              `yaml:"schema_version" json:"schema_version"`
	TodoID              string           `yaml:"todo_id" json:"todo_id"`
	SignedDate          string           `yaml:"signed_date" json:"signed_date"`
	FreshnessWindowDays int              `yaml:"freshness_window_days" json:"freshness_window_days"`
	Intents             []IntentItem     `yaml:"intents" json:"intents"`
	Capabilities        []CapabilityItem `yaml:"capabilities" json:"capabilities"`
	Workflows           []WorkflowItem   `yaml:"workflows" json:"workflows"`
	UserFlows           []UserFlowItem   `yaml:"user_flows" json:"user_flows"`
	Endpoints           []EndpointItem   `yaml:"endpoints" json:"endpoints"`
	Models              []ModelItem      `yaml:"models" json:"models"`
	Effects             []EffectItem     `yaml:"effects" json:"effects"`
	SelectionSlots      []SelectionSlot  `yaml:"selection_slots" json:"selection_slots"`
	DeferredDomains     []DeferredDomain `yaml:"deferred_domains" json:"deferred_domains"`
	Signature           *Signature       `yaml:"signature,omitempty" json:"signature,omitempty"`
}

// LoadManifest reads and parses a ScopeCeilingManifest YAML file.
func LoadManifest(path string) (*ScopeCeilingManifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m ScopeCeilingManifest
	if err := yaml.Unmarshal(content, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &m, nil
}

// Violation names one manifest defect. Field is dotted for list entries
// (e.g. "intents[3].rationale") so a test can name exactly what a mutation
// broke.
type Violation struct {
	Field string
	Issue string
}

func (v Violation) String() string { return fmt.Sprintf("%s: %s", v.Field, v.Issue) }

var requiredSelectionSlots = []string{"provider", "jurisdiction", "topology", "slo"}

// Validate returns every structural violation on m, one per RED clause
// wherever RED names a distinct condition. It does not verify the signature
// (see VerifyManifestSignature).
func (m ScopeCeilingManifest) Validate() []Violation {
	var violations []Violation
	add := func(field, issue string) { violations = append(violations, Violation{Field: field, Issue: issue}) }

	if m.SchemaVersion == 0 {
		add("schema_version", "missing")
	}
	if m.TodoID != "PHASE-001" {
		add("todo_id", "must be PHASE-001")
	}
	if strings.TrimSpace(m.SignedDate) == "" {
		add("signed_date", "missing")
	}
	if m.FreshnessWindowDays <= 0 {
		add("freshness_window_days", "must be positive")
	}

	if len(m.Intents) == 0 {
		add("intents", "missing - the ceiling must name its maximum candidate intents")
	}
	seenIntent := map[string]bool{}
	for i, it := range m.Intents {
		field := fmt.Sprintf("intents[%d]", i)
		if strings.TrimSpace(it.ID) == "" {
			add(field+".id", "missing - a source-unbound intent has no stable identifier")
		} else if seenIntent[it.ID] {
			add(field+".id", "duplicate intent in ceiling")
		}
		seenIntent[it.ID] = true
		if strings.TrimSpace(it.OwnerDomain) == "" {
			add(field+".owner_domain", "missing owner")
		}
		if !it.Gate.valid() {
			add(field+".gate", fmt.Sprintf("unknown gate %q", it.Gate))
		}
		if !it.Disposition.valid() {
			add(field+".disposition", "missing explicit include/defer/reject rationale disposition")
		}
		if strings.TrimSpace(it.Rationale) == "" {
			add(field+".rationale", "missing include/defer/reject rationale")
		}
		if it.Disposition == Include && domainTokenHit(it.ID, it.OwnerDomain) {
			add(field, "future native payroll/WFM/talent ownership must not be INCLUDE")
		}
	}

	if len(m.Capabilities) == 0 {
		add("capabilities", "missing - the ceiling must name its maximum candidate capabilities")
	}
	seenCap := map[string]bool{}
	for i, c := range m.Capabilities {
		field := fmt.Sprintf("capabilities[%d]", i)
		if strings.TrimSpace(c.ID) == "" {
			add(field+".id", "missing")
		} else if seenCap[c.ID] {
			add(field+".id", "duplicate capability in ceiling")
		}
		seenCap[c.ID] = true
		if strings.TrimSpace(c.OwnerDomain) == "" {
			add(field+".owner_domain", "capability without owner")
		}
		if !c.EffectClass.valid() {
			add(field+".effect_class", fmt.Sprintf("unknown effect class %q", c.EffectClass))
		}
		if !c.Gate.valid() {
			add(field+".gate", fmt.Sprintf("unknown gate %q", c.Gate))
		}
		if !c.Disposition.valid() {
			add(field+".disposition", "missing explicit include/defer/reject rationale disposition")
		}
		if strings.TrimSpace(c.Rationale) == "" {
			add(field+".rationale", "missing include/defer/reject rationale")
		}
		if c.Disposition == Include && domainTokenHit(c.ID, c.OwnerDomain) {
			add(field, "future native payroll/WFM/talent ownership must not be INCLUDE")
		}
	}

	if len(m.Workflows) == 0 {
		add("workflows", "missing - the ceiling must name its maximum candidate workflows")
	}
	for i, w := range m.Workflows {
		field := fmt.Sprintf("workflows[%d]", i)
		if strings.TrimSpace(w.ID) == "" {
			add(field+".id", "missing")
		}
		if strings.TrimSpace(w.VerticalSlice) == "" {
			add(field+".vertical_slice", "workflow without vertical slice")
		}
		if !w.Gate.valid() {
			add(field+".gate", fmt.Sprintf("unknown gate %q", w.Gate))
		}
		if !w.Disposition.valid() {
			add(field+".disposition", "missing explicit include/defer/reject rationale disposition")
		}
		if strings.TrimSpace(w.Rationale) == "" {
			add(field+".rationale", "missing include/defer/reject rationale")
		}
	}

	if len(m.UserFlows) == 0 {
		add("user_flows", "missing - the ceiling must name its maximum candidate user flows")
	}
	for i, uf := range m.UserFlows {
		field := fmt.Sprintf("user_flows[%d]", i)
		if strings.TrimSpace(uf.ID) == "" {
			add(field+".id", "missing")
		}
		if !uf.Gate.valid() {
			add(field+".gate", fmt.Sprintf("unknown gate %q", uf.Gate))
		}
		if !uf.Disposition.valid() {
			add(field+".disposition", "missing explicit include/defer/reject rationale disposition")
		}
		if strings.TrimSpace(uf.Rationale) == "" {
			add(field+".rationale", "missing include/defer/reject rationale")
		}
	}

	if len(m.Endpoints) == 0 {
		add("endpoints", "missing - the ceiling must name its maximum candidate endpoints")
	}
	seenEndpoint := map[string]bool{}
	for i, e := range m.Endpoints {
		field := fmt.Sprintf("endpoints[%d]", i)
		if strings.TrimSpace(e.EndpointID) == "" {
			add(field+".endpoint_id", "missing")
		} else if seenEndpoint[e.EndpointID] {
			add(field+".endpoint_id", "duplicate endpoint in ceiling")
		}
		seenEndpoint[e.EndpointID] = true
		if !e.EndpointDisposition.valid() {
			add(field+".endpoint_disposition", "endpoint without disposition")
		}
		if !e.Gate.valid() {
			add(field+".gate", fmt.Sprintf("unknown gate %q", e.Gate))
		}
		if !e.Disposition.valid() {
			add(field+".disposition", "missing explicit include/defer/reject rationale disposition")
		}
		if strings.TrimSpace(e.Rationale) == "" {
			add(field+".rationale", "missing include/defer/reject rationale")
		}
	}

	if len(m.Models) == 0 {
		add("models", "missing - the ceiling must name its maximum candidate models")
	}
	for i, mo := range m.Models {
		field := fmt.Sprintf("models[%d]", i)
		if strings.TrimSpace(mo.ID) == "" {
			add(field+".id", "missing")
		}
		if !mo.Gate.valid() {
			add(field+".gate", fmt.Sprintf("unknown gate %q", mo.Gate))
		}
		if !mo.Disposition.valid() {
			add(field+".disposition", "missing explicit include/defer/reject rationale disposition")
		}
		if strings.TrimSpace(mo.Rationale) == "" {
			add(field+".rationale", "missing include/defer/reject rationale")
		}
	}

	if len(m.Effects) == 0 {
		add("effects", "missing - the ceiling must name its maximum candidate effects")
	}
	for i, e := range m.Effects {
		field := fmt.Sprintf("effects[%d]", i)
		if !e.Class.valid() {
			add(field+".class", fmt.Sprintf("unknown effect class %q", e.Class))
		}
		if !e.Gate.valid() {
			add(field+".gate", fmt.Sprintf("unknown gate %q", e.Gate))
		}
		if !e.Disposition.valid() {
			add(field+".disposition", "missing explicit include/defer/reject rationale disposition")
		}
		if strings.TrimSpace(e.Rationale) == "" {
			add(field+".rationale", "missing include/defer/reject rationale")
		}
		if e.ProviderDependency != "" && e.ProviderDependency != UnboundProviderSentinel {
			add(field+".provider_dependency", fmt.Sprintf("hidden provider dependency: names %q instead of the unbound sentinel", e.ProviderDependency))
		}
	}

	if len(m.SelectionSlots) != len(requiredSelectionSlots) {
		add("selection_slots", fmt.Sprintf("must name exactly %v", requiredSelectionSlots))
	}
	seenSlot := map[string]bool{}
	for i, s := range m.SelectionSlots {
		field := fmt.Sprintf("selection_slots[%d]", i)
		if !containsString(requiredSelectionSlots, s.Name) {
			add(field+".name", fmt.Sprintf("unknown selection slot %q", s.Name))
		}
		if seenSlot[s.Name] {
			add(field+".name", "duplicate selection slot")
		}
		seenSlot[s.Name] = true
		if strings.TrimSpace(s.FillingTodoID) == "" {
			add(field+".filling_todo_id", "missing - a slot must name what fills it")
		}
		if s.Filled {
			add(field+".filled", "a Phase 1 scope ceiling must never carry a filled selection slot - selection happens through NEXT-002, not here")
		}
		if s.Value != "" {
			add(field+".value", fmt.Sprintf("a Phase 1 scope ceiling must never carry a selected value (%q) - selection happens through NEXT-002, not here", s.Value))
		}
	}
	for _, name := range requiredSelectionSlots {
		found := false
		for _, s := range m.SelectionSlots {
			if s.Name == name {
				found = true
			}
		}
		if !found {
			add("selection_slots", fmt.Sprintf("missing required slot %q", name))
		}
	}

	if len(m.DeferredDomains) == 0 {
		add("deferred_domains", "missing - all other catalog items must remain visibly deferred somewhere")
	}
	for i, d := range m.DeferredDomains {
		field := fmt.Sprintf("deferred_domains[%d]", i)
		if strings.TrimSpace(d.Name) == "" {
			add(field+".name", "missing")
		}
		if strings.TrimSpace(d.SourceRef) == "" {
			add(field+".source_ref", "missing")
		}
		if strings.TrimSpace(d.Rationale) == "" {
			add(field+".rationale", "missing")
		}
	}

	if m.Signature == nil {
		add("signature", "missing - a Phase 1 scope ceiling must be signed")
	} else {
		if m.Signature.Algorithm != "ed25519" {
			add("signature.algorithm", fmt.Sprintf("unsupported algorithm %q", m.Signature.Algorithm))
		}
		if m.Signature.PublicKey == "" {
			add("signature.public_key", "missing")
		}
		if m.Signature.Value == "" {
			add("signature.value", "missing")
		}
	}

	return violations
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func domainTokenHit(values ...string) bool {
	joined := strings.ToLower(strings.Join(values, " "))
	for _, tok := range deferredDomainTokens {
		if strings.Contains(joined, tok) {
			return true
		}
	}
	return false
}

// digestPayload is the canonical projection hashed by CanonicalDigest and
// signed by Sign/Verify: every manifest field except the signature itself.
type digestPayload struct {
	SchemaVersion       int              `json:"schema_version"`
	TodoID              string           `json:"todo_id"`
	SignedDate          string           `json:"signed_date"`
	FreshnessWindowDays int              `json:"freshness_window_days"`
	Intents             []IntentItem     `json:"intents"`
	Capabilities        []CapabilityItem `json:"capabilities"`
	Workflows           []WorkflowItem   `json:"workflows"`
	UserFlows           []UserFlowItem   `json:"user_flows"`
	Endpoints           []EndpointItem   `json:"endpoints"`
	Models              []ModelItem      `json:"models"`
	Effects             []EffectItem     `json:"effects"`
	SelectionSlots      []SelectionSlot  `json:"selection_slots"`
	DeferredDomains     []DeferredDomain `json:"deferred_domains"`
}

func (m ScopeCeilingManifest) payload() digestPayload {
	return digestPayload{
		SchemaVersion:       m.SchemaVersion,
		TodoID:              m.TodoID,
		SignedDate:          m.SignedDate,
		FreshnessWindowDays: m.FreshnessWindowDays,
		Intents:             m.Intents,
		Capabilities:        m.Capabilities,
		Workflows:           m.Workflows,
		UserFlows:           m.UserFlows,
		Endpoints:           m.Endpoints,
		Models:              m.Models,
		Effects:             m.Effects,
		SelectionSlots:      m.SelectionSlots,
		DeferredDomains:     m.DeferredDomains,
	}
}

// CanonicalDigest returns the hex-encoded sha256 digest of m's canonical
// JSON projection (every field except Signature).
func (m ScopeCeilingManifest) CanonicalDigest() (string, error) {
	b, err := json.Marshal(m.payload())
	if err != nil {
		return "", fmt.Errorf("marshal canonical manifest: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
