package workordertemplate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/extensionregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const digestProfile = "hcmnext.workorder.Template/v1"

var (
	ErrInvalid       = errors.New("work order template: invalid")
	ErrUnsafeOverlay = errors.New("work order template: unsafe overlay")
	ErrMutated       = errors.New("work order template: published content was mutated")
	idPattern        = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	fieldPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

type FieldType string

const (
	FieldText     FieldType = "TEXT"
	FieldDecimal  FieldType = "DECIMAL"
	FieldBoolean  FieldType = "BOOLEAN"
	FieldDate     FieldType = "DATE"
	FieldEnum     FieldType = "ENUM"
	FieldEvidence FieldType = "EVIDENCE_REF"
)

type FormField struct {
	ID       string    `json:"id"`
	Type     FieldType `json:"type"`
	Required bool      `json:"required"`
	Options  []string  `json:"options,omitempty"`
}
type RequestForm struct {
	ID      string      `json:"id"`
	Version string      `json:"version"`
	Fields  []FormField `json:"fields"`
}
type RequestKind string

const (
	RequestBudget        RequestKind = "BUDGET"
	RequestBudgetChange  RequestKind = "BUDGET_CHANGE"
	RequestCrew          RequestKind = "CREW"
	RequestMaterial      RequestKind = "MATERIAL"
	RequestEquipment     RequestKind = "EQUIPMENT"
	RequestApproval      RequestKind = "APPROVAL"
	RequestInspection    RequestKind = "INSPECTION"
	RequestDocument      RequestKind = "DOCUMENT"
	RequestChangeOrder   RequestKind = "CHANGE_ORDER"
	RequestBillingReview RequestKind = "BILLING_REVIEW"
)

type RequestDefinition struct {
	ID                string      `json:"id"`
	Kind              RequestKind `json:"kind"`
	FormRef           string      `json:"form_ref"`
	AllowedPhases     []string    `json:"allowed_phases"`
	ApprovalPolicyRef string      `json:"approval_policy_ref,omitempty"`
	Optional          bool        `json:"optional"`
}
type EvidenceRequirement struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	AtPhase     string `json:"at_phase"`
	Required    bool   `json:"required"`
}
type Gate struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	PolicyRef string `json:"policy_ref,omitempty"`
	Required  bool   `json:"required"`
}
type Phase struct {
	ID                string   `json:"id"`
	AllowedExits      []string `json:"allowed_exits"`
	RequiredRequests  []string `json:"required_requests,omitempty"`
	RequiredDecisions []string `json:"required_decisions,omitempty"`
	RequiredEvidence  []string `json:"required_evidence,omitempty"`
	Gates             []Gate   `json:"gates,omitempty"`
	ActorRoles        []string `json:"actor_roles"`
	Optional          bool     `json:"optional"`
}
type RoleGrant struct {
	Role    string   `json:"role"`
	Actions []string `json:"actions"`
}
type OptionalBranch struct {
	ID            string `json:"id"`
	EntryPhase    string `json:"entry_phase"`
	ReturnPhase   string `json:"return_phase"`
	MaxIterations uint32 `json:"max_iterations"`
	Optional      bool   `json:"optional"`
}
type PolicyRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type ReportPolicy struct {
	ID       string     `json:"id"`
	Version  string     `json:"version"`
	Kind     ReportKind `json:"kind"`
	Required bool       `json:"required"`
}

type ReportKind string

const (
	ReportDailyField   ReportKind = "daily-field"
	ReportProgress     ReportKind = "progress"
	ReportCost         ReportKind = "cost"
	ReportOpenRequests ReportKind = "open-requests"
	ReportCloseout     ReportKind = "closeout"
)

type BillingPolicy struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Mode     string `json:"mode"`
	Required bool   `json:"required"`
}
type ExtensionRef struct {
	Kind    extensionregistry.Kind `json:"kind"`
	ID      string                 `json:"id"`
	Version string                 `json:"version"`
}

// Draft is declarative data. It contains no expressions, scripts, or executable callbacks.
type Draft struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Phases         []Phase               `json:"phases"`
	Forms          []RequestForm         `json:"forms"`
	Requests       []RequestDefinition   `json:"requests"`
	Evidence       []EvidenceRequirement `json:"evidence"`
	Roles          []RoleGrant           `json:"roles"`
	Branches       []OptionalBranch      `json:"branches,omitempty"`
	ReportPolicies []ReportPolicy        `json:"report_policies,omitempty"`
	Billing        *BillingPolicy        `json:"billing,omitempty"`
	PolicyRefs     []PolicyRef           `json:"policy_refs,omitempty"`
	ExtensionRefs  []ExtensionRef        `json:"extension_refs,omitempty"`
}

type OverlayOperation string

const (
	OverlayAdd     OverlayOperation = "ADD"
	OverlayOmit    OverlayOperation = "OMIT"
	OverlayReplace OverlayOperation = "REPLACE"
)

// PhaseOverlay mirrors workflow expansion's Add/Omit(reason)/Replace(reason) contract.
type PhaseOverlay struct {
	Operation OverlayOperation `json:"operation"`
	Phase     Phase            `json:"phase"`
	TargetID  string           `json:"target_id,omitempty"`
	Reason    string           `json:"reason"`
}
type Overlay struct {
	Operations []PhaseOverlay `json:"operations"`
}

type PublishMeta struct {
	Version     string `json:"version"`
	PublishedBy string `json:"published_by"`
	ReviewRef   string `json:"review_ref"`
}
type Pin struct {
	TemplateID string `json:"template_id"`
	Version    string `json:"version"`
	Digest     string `json:"digest"`
}
type Published struct {
	draft         Draft
	version       string
	publishedBy   string
	reviewRef     string
	digest        string
	overlayDigest string
}

func (p Published) Version() string       { return p.version }
func (p Published) PublishedBy() string   { return p.publishedBy }
func (p Published) ReviewRef() string     { return p.reviewRef }
func (p Published) Digest() string        { return p.digest }
func (p Published) OverlayDigest() string { return p.overlayDigest }
func (p Published) Pin() Pin {
	return Pin{TemplateID: p.draft.ID, Version: p.version, Digest: p.digest}
}
func (p Published) Snapshot() Draft { return cloneDraft(p.draft) }
func (p Published) AllowsTransition(from, to string) bool {
	for _, phase := range p.draft.Phases {
		if phase.ID == from {
			return hasExit(phase, to)
		}
	}
	return false
}
func (p Published) Request(id string) (RequestDefinition, bool) {
	for _, request := range p.draft.Requests {
		if request.ID == id {
			request.AllowedPhases = append([]string(nil), request.AllowedPhases...)
			return request, true
		}
	}
	return RequestDefinition{}, false
}
func (p Published) AllowsRequest(id, phaseID string) bool {
	request, ok := p.Request(id)
	if !ok {
		return false
	}
	for _, allowed := range request.AllowedPhases {
		if allowed == phaseID {
			return true
		}
	}
	return false
}
func (p Published) Form(id string) (RequestForm, bool) {
	for _, form := range p.draft.Forms {
		if form.ID == id {
			copy := cloneDraft(Draft{Forms: []RequestForm{form}}).Forms[0]
			return copy, true
		}
	}
	return RequestForm{}, false
}
func (p Published) Verify() error {
	if p.digest == "" || p.digest != digest(publishedIdentity{Draft: p.draft, Version: p.version, PublishedBy: p.publishedBy, ReviewRef: p.reviewRef, OverlayDigest: p.overlayDigest}) {
		return ErrMutated
	}
	return nil
}

type persistedPublished struct {
	Draft         Draft  `json:"draft"`
	Version       string `json:"version"`
	PublishedBy   string `json:"published_by"`
	ReviewRef     string `json:"review_ref"`
	OverlayDigest string `json:"overlay_digest,omitempty"`
	Digest        string `json:"digest"`
}

// MarshalJSON emits the immutable, digest-bearing persistence envelope. A
// modified Published value cannot be persisted because verification runs first.
func (p Published) MarshalJSON() ([]byte, error) {
	if err := p.Verify(); err != nil {
		return nil, err
	}
	return json.Marshal(persistedPublished{
		Draft: p.draft, Version: p.version, PublishedBy: p.publishedBy,
		ReviewRef: p.reviewRef, OverlayDigest: p.overlayDigest, Digest: p.digest,
	})
}

// RestorePublished reconstructs a published value from its persistence
// envelope and verifies both the stored and caller-pinned digest. The registry
// is not needed here: exact extension references were resolved at publication
// time, and their typed identities remain covered by the digest.
func RestorePublished(raw json.RawMessage, expectedDigest string) (Published, error) {
	if len(raw) == 0 || strings.TrimSpace(expectedDigest) == "" {
		return Published{}, fmt.Errorf("%w: persisted template and expected digest are required", ErrInvalid)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var stored persistedPublished
	if err := decoder.Decode(&stored); err != nil {
		return Published{}, fmt.Errorf("%w: decode published template: %v", ErrInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Published{}, fmt.Errorf("%w: trailing data in published template", ErrInvalid)
	}
	if stored.Digest == "" || stored.Digest != expectedDigest {
		return Published{}, ErrMutated
	}
	if version.ValidateSemanticVersion(stored.Version) != nil || strings.TrimSpace(stored.PublishedBy) == "" || strings.TrimSpace(stored.ReviewRef) == "" {
		return Published{}, fmt.Errorf("%w: persisted publication metadata is incomplete", ErrInvalid)
	}
	draft, err := normalize(stored.Draft)
	if err != nil {
		return Published{}, err
	}
	if err = validate(draft, nil); err != nil {
		return Published{}, err
	}
	identity := publishedIdentity{Draft: draft, Version: stored.Version, PublishedBy: stored.PublishedBy, ReviewRef: stored.ReviewRef, OverlayDigest: stored.OverlayDigest}
	p := Published{draft: draft, version: stored.Version, publishedBy: stored.PublishedBy, reviewRef: stored.ReviewRef, overlayDigest: stored.OverlayDigest, digest: stored.Digest}
	if digest(identity) != stored.Digest || p.Verify() != nil {
		return Published{}, ErrMutated
	}
	return p, nil
}

type publishedIdentity struct {
	Draft         Draft  `json:"draft"`
	Version       string `json:"version"`
	PublishedBy   string `json:"published_by"`
	ReviewRef     string `json:"review_ref"`
	OverlayDigest string `json:"overlay_digest,omitempty"`
}

func Publish(d Draft, meta PublishMeta, registry *extensionregistry.Registry) (Published, error) {
	return PublishWithOverlay(d, Overlay{}, meta, registry)
}
func PublishWithOverlay(base Draft, overlay Overlay, meta PublishMeta, registry *extensionregistry.Registry) (Published, error) {
	if version.ValidateSemanticVersion(meta.Version) != nil || strings.TrimSpace(meta.PublishedBy) == "" || strings.TrimSpace(meta.ReviewRef) == "" {
		return Published{}, fmt.Errorf("%w: publication version, publisher, and review reference are required", ErrInvalid)
	}
	d, err := applyOverlay(base, overlay)
	if err != nil {
		return Published{}, err
	}
	if len(d.ExtensionRefs) > 0 && registry == nil {
		return Published{}, fmt.Errorf("%w: extension registry is required to resolve declared references", ErrInvalid)
	}
	d, err = normalize(d)
	if err != nil {
		return Published{}, err
	}
	if err = validate(d, registry); err != nil {
		return Published{}, err
	}
	overlayDigest := ""
	if len(overlay.Operations) > 0 {
		overlayDigest = digest(overlay)
	}
	identity := publishedIdentity{Draft: d, Version: meta.Version, PublishedBy: meta.PublishedBy, ReviewRef: meta.ReviewRef, OverlayDigest: overlayDigest}
	return Published{draft: d, version: meta.Version, publishedBy: meta.PublishedBy, reviewRef: meta.ReviewRef, digest: digest(identity), overlayDigest: overlayDigest}, nil
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(append(append([]byte(digestProfile), 0), b...))
	return "sha256:" + hex.EncodeToString(h[:])
}

func normalize(d Draft) (Draft, error) {
	d = cloneDraft(d)
	if !idPattern.MatchString(d.ID) || strings.TrimSpace(d.Name) == "" {
		return Draft{}, fmt.Errorf("%w: template ID and name are required", ErrInvalid)
	}
	sort.Slice(d.Phases, func(i, j int) bool { return d.Phases[i].ID < d.Phases[j].ID })
	sort.Slice(d.Forms, func(i, j int) bool { return d.Forms[i].ID < d.Forms[j].ID })
	sort.Slice(d.Requests, func(i, j int) bool { return d.Requests[i].ID < d.Requests[j].ID })
	sort.Slice(d.Evidence, func(i, j int) bool { return d.Evidence[i].ID < d.Evidence[j].ID })
	sort.Slice(d.Roles, func(i, j int) bool { return d.Roles[i].Role < d.Roles[j].Role })
	sort.Slice(d.Branches, func(i, j int) bool { return d.Branches[i].ID < d.Branches[j].ID })
	sort.Slice(d.ReportPolicies, func(i, j int) bool { return d.ReportPolicies[i].ID < d.ReportPolicies[j].ID })
	sort.Slice(d.PolicyRefs, func(i, j int) bool {
		if d.PolicyRefs[i].ID != d.PolicyRefs[j].ID {
			return d.PolicyRefs[i].ID < d.PolicyRefs[j].ID
		}
		return d.PolicyRefs[i].Version < d.PolicyRefs[j].Version
	})
	sort.Slice(d.ExtensionRefs, func(i, j int) bool {
		a, b := d.ExtensionRefs[i], d.ExtensionRefs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Version < b.Version
	})
	for i := range d.Phases {
		sort.Strings(d.Phases[i].AllowedExits)
		sort.Strings(d.Phases[i].RequiredRequests)
		sort.Strings(d.Phases[i].RequiredDecisions)
		sort.Strings(d.Phases[i].RequiredEvidence)
		sort.Strings(d.Phases[i].ActorRoles)
		sort.Slice(d.Phases[i].Gates, func(a, b int) bool { return d.Phases[i].Gates[a].ID < d.Phases[i].Gates[b].ID })
	}
	for i := range d.Forms {
		sort.Slice(d.Forms[i].Fields, func(a, b int) bool { return d.Forms[i].Fields[a].ID < d.Forms[i].Fields[b].ID })
		for j := range d.Forms[i].Fields {
			sort.Strings(d.Forms[i].Fields[j].Options)
		}
	}
	for i := range d.Requests {
		sort.Strings(d.Requests[i].AllowedPhases)
	}
	for i := range d.Roles {
		sort.Strings(d.Roles[i].Actions)
	}
	return d, nil
}

func validate(d Draft, registry *extensionregistry.Registry) error {
	fail := func(s string) error { return fmt.Errorf("%w: %s", ErrInvalid, s) }
	phases := map[string]Phase{}
	for _, p := range d.Phases {
		if !idPattern.MatchString(p.ID) || len(p.ActorRoles) == 0 {
			return fail("phases need stable IDs and actor roles")
		}
		if _, ok := phases[p.ID]; ok {
			return fail("duplicate phase " + p.ID)
		}
		phases[p.ID] = p
	}
	for _, id := range []string{"DRAFT", "AUTHORIZATION", "READY", "EXECUTION", "INSPECTION", "ACCEPTED", "CLOSED"} {
		if _, ok := phases[id]; !ok {
			return fail("required phase " + id + " is missing")
		}
	}
	if !hasExit(phases["DRAFT"], "AUTHORIZATION") || !hasExit(phases["AUTHORIZATION"], "READY") || !hasExit(phases["READY"], "EXECUTION") || !hasExit(phases["EXECUTION"], "INSPECTION") || !hasExit(phases["INSPECTION"], "ACCEPTED") || !hasExit(phases["ACCEPTED"], "CLOSED") {
		return fail("mandatory lifecycle phase path is incomplete")
	}
	for _, p := range d.Phases {
		for _, to := range p.AllowedExits {
			if _, ok := phases[to]; !ok {
				return fail("phase " + p.ID + " exits to missing phase " + to)
			}
		}
	}
	forms := map[string]bool{}
	for _, f := range d.Forms {
		if !idPattern.MatchString(f.ID) || f.Version == "" || len(f.Fields) == 0 {
			return fail("form identity and fields are required")
		}
		if forms[f.ID] {
			return fail("duplicate form " + f.ID)
		}
		forms[f.ID] = true
		seen := map[string]bool{}
		for _, x := range f.Fields {
			if !fieldPattern.MatchString(x.ID) || seen[x.ID] {
				return fail("invalid or duplicate form field")
			}
			seen[x.ID] = true
			switch x.Type {
			case FieldText, FieldDecimal, FieldBoolean, FieldDate, FieldEvidence:
				if len(x.Options) > 0 {
					return fail("options only apply to enum fields")
				}
			case FieldEnum:
				if len(x.Options) == 0 {
					return fail("enum field needs options")
				}
			default:
				return fail("unknown field type")
			}
		}
	}
	requests := map[string]bool{}
	for _, r := range d.Requests {
		if !idPattern.MatchString(r.ID) || requests[r.ID] || !forms[r.FormRef] || !validRequest(r.Kind) {
			return fail("request needs unique ID, known kind, and form")
		}
		requests[r.ID] = true
		for _, ph := range r.AllowedPhases {
			if _, ok := phases[ph]; !ok {
				return fail("request references missing phase")
			}
		}
	}
	evidence := map[string]bool{}
	for _, e := range d.Evidence {
		if !idPattern.MatchString(e.ID) || e.Description == "" || evidence[e.ID] {
			return fail("evidence requirements need unique IDs and description")
		}
		if _, ok := phases[e.AtPhase]; !ok {
			return fail("evidence references missing phase")
		}
		evidence[e.ID] = true
	}
	for _, p := range d.Phases {
		for _, id := range p.RequiredRequests {
			if !requests[id] {
				return fail("phase references missing request " + id)
			}
		}
		for _, id := range p.RequiredEvidence {
			if !evidence[id] {
				return fail("phase references missing evidence " + id)
			}
		}
		for _, g := range p.Gates {
			if !idPattern.MatchString(g.ID) || g.Kind == "" {
				return fail("gate identity and kind are required")
			}
		}
	}
	mandatoryGates := map[string]bool{"AUTHORIZATION": false, "SAFETY": false, "RECONCILIATION": false, "CLOSURE": false}
	for _, p := range d.Phases {
		for _, g := range p.Gates {
			kind := strings.ToUpper(g.Kind)
			if _, required := mandatoryGates[kind]; required && g.Required {
				mandatoryGates[kind] = true
			}
		}
	}
	for kind, found := range mandatoryGates {
		if !found {
			return fail("mandatory " + kind + " gate is required")
		}
	}
	roles := map[string]bool{}
	for _, r := range d.Roles {
		if r.Role == "" || roles[r.Role] || len(r.Actions) == 0 {
			return fail("role grants need unique roles and actions")
		}
		roles[r.Role] = true
	}
	for _, p := range d.Phases {
		for _, r := range p.ActorRoles {
			if !roles[r] {
				return fail("phase role has no grant: " + r)
			}
		}
	}
	for _, b := range d.Branches {
		if !idPattern.MatchString(b.ID) || !b.Optional || b.MaxIterations == 0 || b.MaxIterations > 100 {
			return fail("optional branches require stable ID and bounded iterations")
		}
		if _, ok := phases[b.EntryPhase]; !ok {
			return fail("branch entry phase missing")
		}
		if _, ok := phases[b.ReturnPhase]; !ok {
			return fail("branch return phase missing")
		}
	}
	for _, r := range d.ReportPolicies {
		if r.ID == "" || r.Version == "" || !validReportKind(r.Kind) {
			return fail("report policy references must be versioned and use a supported report kind")
		}
	}
	if d.Billing != nil {
		if d.Billing.ID == "" || d.Billing.Version == "" || !oneOf(d.Billing.Mode, "UNIT_PRICE", "TIME_AND_MATERIAL", "FIXED_MILESTONE") {
			return fail("billing policy needs version and supported pricing mode")
		}
	}
	for _, r := range d.PolicyRefs {
		if r.ID == "" || r.Version == "" {
			return fail("policy references must be versioned")
		}
	}
	for _, r := range d.ExtensionRefs {
		if !r.Kind.Valid() || r.ID == "" || r.Version == "" {
			return fail("extension references must be exact and typed")
		}
		if registry != nil {
			record, ok := registry.Resolve(extensionregistry.Ref{ID: r.ID, Version: r.Version})
			if !ok || record.Manifest.Kind != r.Kind {
				return fail("unresolved or kind-mismatched extension " + r.ID + "@" + r.Version)
			}
		}
	}
	return nil
}

func validReportKind(kind ReportKind) bool {
	return kind == ReportDailyField || kind == ReportProgress || kind == ReportCost || kind == ReportOpenRequests || kind == ReportCloseout
}

func hasExit(p Phase, id string) bool {
	for _, x := range p.AllowedExits {
		if x == id {
			return true
		}
	}
	return false
}
func validRequest(k RequestKind) bool {
	return oneOf(string(k), string(RequestBudget), string(RequestBudgetChange), string(RequestCrew), string(RequestMaterial), string(RequestEquipment), string(RequestApproval), string(RequestInspection), string(RequestDocument), string(RequestChangeOrder), string(RequestBillingReview))
}
func oneOf(v string, options ...string) bool {
	for _, o := range options {
		if v == o {
			return true
		}
	}
	return false
}

func applyOverlay(base Draft, overlay Overlay) (Draft, error) {
	d := cloneDraft(base)
	for _, op := range overlay.Operations {
		if strings.TrimSpace(op.Reason) == "" {
			return Draft{}, fmt.Errorf("%w: every operation needs a reason", ErrUnsafeOverlay)
		}
		idx := -1
		for i, p := range d.Phases {
			if p.ID == op.TargetID || p.ID == op.Phase.ID {
				idx = i
				break
			}
		}
		switch op.Operation {
		case OverlayAdd:
			if idx >= 0 {
				return Draft{}, fmt.Errorf("%w: phase already exists", ErrUnsafeOverlay)
			}
			if op.Phase.ID == "" {
				return Draft{}, fmt.Errorf("%w: added phase needs ID", ErrUnsafeOverlay)
			}
			op.Phase.Optional = true
			d.Phases = append(d.Phases, op.Phase)
		case OverlayOmit:
			if idx < 0 {
				return Draft{}, fmt.Errorf("%w: omitted phase not found", ErrUnsafeOverlay)
			}
			p := d.Phases[idx]
			if !p.Optional || isProtectedPhase(p.ID) || hasRequiredGate(p) {
				return Draft{}, fmt.Errorf("%w: phase %s is required", ErrUnsafeOverlay, p.ID)
			}
			for _, other := range d.Phases {
				if other.ID != p.ID && hasExit(other, p.ID) {
					return Draft{}, fmt.Errorf("%w: phase %s is still reachable", ErrUnsafeOverlay, p.ID)
				}
			}
			d.Phases = append(d.Phases[:idx], d.Phases[idx+1:]...)
		case OverlayReplace:
			if idx < 0 || op.Phase.ID != d.Phases[idx].ID || isProtectedPhase(op.Phase.ID) || hasRequiredGate(d.Phases[idx]) {
				return Draft{}, fmt.Errorf("%w: replacement must preserve an optional phase identity", ErrUnsafeOverlay)
			}
			if d.Phases[idx].Optional != op.Phase.Optional {
				return Draft{}, fmt.Errorf("%w: replacement cannot change phase optionality", ErrUnsafeOverlay)
			}
			d.Phases[idx] = op.Phase
		default:
			return Draft{}, fmt.Errorf("%w: unknown operation", ErrUnsafeOverlay)
		}
	}
	return d, nil
}
func isProtectedPhase(id string) bool {
	return oneOf(id, "DRAFT", "AUTHORIZATION", "READY", "EXECUTION", "INSPECTION", "ACCEPTED", "CLOSED")
}
func hasRequiredGate(p Phase) bool {
	for _, g := range p.Gates {
		if g.Required {
			return true
		}
	}
	return false
}

func cloneDraft(d Draft) Draft {
	c := d
	c.Phases = append([]Phase(nil), d.Phases...)
	for i := range c.Phases {
		p := &c.Phases[i]
		p.AllowedExits = append([]string(nil), p.AllowedExits...)
		p.RequiredRequests = append([]string(nil), p.RequiredRequests...)
		p.RequiredDecisions = append([]string(nil), p.RequiredDecisions...)
		p.RequiredEvidence = append([]string(nil), p.RequiredEvidence...)
		p.ActorRoles = append([]string(nil), p.ActorRoles...)
		p.Gates = append([]Gate(nil), p.Gates...)
	}
	c.Forms = append([]RequestForm(nil), d.Forms...)
	for i := range c.Forms {
		c.Forms[i].Fields = append([]FormField(nil), c.Forms[i].Fields...)
		for j := range c.Forms[i].Fields {
			c.Forms[i].Fields[j].Options = append([]string(nil), c.Forms[i].Fields[j].Options...)
		}
	}
	c.Requests = append([]RequestDefinition(nil), d.Requests...)
	for i := range c.Requests {
		c.Requests[i].AllowedPhases = append([]string(nil), c.Requests[i].AllowedPhases...)
	}
	c.Evidence = append([]EvidenceRequirement(nil), d.Evidence...)
	c.Roles = append([]RoleGrant(nil), d.Roles...)
	for i := range c.Roles {
		c.Roles[i].Actions = append([]string(nil), c.Roles[i].Actions...)
	}
	c.Branches = append([]OptionalBranch(nil), d.Branches...)
	c.ReportPolicies = append([]ReportPolicy(nil), d.ReportPolicies...)
	c.PolicyRefs = append([]PolicyRef(nil), d.PolicyRefs...)
	c.ExtensionRefs = append([]ExtensionRef(nil), d.ExtensionRefs...)
	if d.Billing != nil {
		b := *d.Billing
		c.Billing = &b
	}
	return c
}

type MigrationClass string

const (
	MigrationSafe           MigrationClass = "SAFE"
	MigrationTransformable  MigrationClass = "TRANSFORMABLE"
	MigrationRequiresRepair MigrationClass = "REQUIRES_REPAIR"
	MigrationImpossible     MigrationClass = "IMPOSSIBLE"
)

type MigrationPreview struct {
	Source  Pin            `json:"source"`
	Target  Pin            `json:"target"`
	Class   MigrationClass `json:"class"`
	Changes []string       `json:"changes"`
	Reason  string         `json:"reason"`
}

func PreviewMigration(source, target Published, activePhase string, completedPhases []string) (MigrationPreview, error) {
	if err := source.Verify(); err != nil {
		return MigrationPreview{}, err
	}
	if err := target.Verify(); err != nil {
		return MigrationPreview{}, err
	}
	out := MigrationPreview{Source: source.Pin(), Target: target.Pin(), Class: MigrationSafe}
	oldP := map[string]Phase{}
	newP := map[string]Phase{}
	for _, p := range source.draft.Phases {
		oldP[p.ID] = p
	}
	for _, p := range target.draft.Phases {
		newP[p.ID] = p
	}
	if _, ok := newP[activePhase]; !ok {
		out.Class = MigrationImpossible
		out.Reason = "target template has no equivalent active phase"
		return out, nil
	}
	completed := map[string]bool{}
	for _, id := range completedPhases {
		completed[id] = true
	}
	for id, old := range oldP {
		next, ok := newP[id]
		if !ok {
			out.Changes = append(out.Changes, "removed phase "+id)
			if completed[id] {
				out.Class = MigrationImpossible
				out.Reason = "target removes a completed phase"
			} else if out.Class != MigrationImpossible {
				out.Class = MigrationRequiresRepair
				out.Reason = "target removes an uncompleted phase"
			}
			continue
		}
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(next)
		if string(a) != string(b) {
			out.Changes = append(out.Changes, "changed phase "+id)
			if completed[id] {
				out.Class = MigrationImpossible
				out.Reason = "target changes a completed phase"
			} else if id == activePhase && out.Class != MigrationImpossible {
				out.Class = MigrationRequiresRepair
				out.Reason = "active phase semantics changed"
			} else if out.Class == MigrationSafe {
				out.Class = MigrationTransformable
				out.Reason = "future phase semantics changed"
			}
		}
	}
	for id := range newP {
		if _, ok := oldP[id]; !ok {
			out.Changes = append(out.Changes, "added phase "+id)
		}
	}
	sort.Strings(out.Changes)
	return out, nil
}
