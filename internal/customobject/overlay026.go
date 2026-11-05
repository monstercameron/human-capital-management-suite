// WF-EXT-026: let tenants publish signed overlays on product templates.
//
// Customers need their own approval steps, notices and thresholds
// without a product release, and without weakening mandatory controls.
// A tenant overlay lives in the tenant-owned definition namespace and
// moves DRAFT, VALIDATED, REVIEWED, PUBLISHED, ACTIVE with separation
// of duties and signing; inheritance resolves enterprise, company and
// legal-entity levels with lineage; publishing requires passing
// simulation fixtures. Signing reuses the shared cryptoagility
// primitives: author and reviewer sign the canonical overlay bytes, and
// activation verifies both.
package customobject

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/cryptoagility"
)

// Overlay errors. All are matchable with errors.Is.
var (
	// ErrOverlayInvalid is returned for a malformed overlay or request.
	ErrOverlayInvalid = errors.New("customobject: invalid overlay")
	// ErrOverlayState is returned for a lifecycle step taken out of order.
	ErrOverlayState = errors.New("customobject: overlay step out of order")
	// ErrOverlayFixtures is returned when simulation fixtures fail.
	ErrOverlayFixtures = errors.New("customobject: overlay simulation fixtures failed")
	// ErrOverlaySoD is returned when separation of duties is violated.
	ErrOverlaySoD = errors.New("customobject: overlay separation of duties violated")
	// ErrOverlayControl is returned when an overlay weakens a mandatory
	// control: omitted phase, authored compensation, unbounded loop or
	// unlisted capability.
	ErrOverlayControl = errors.New("customobject: overlay weakens a mandatory control")
	// ErrOverlaySignature is returned when overlay signatures fail to
	// verify.
	ErrOverlaySignature = errors.New("customobject: overlay signature invalid")
)

// OverlayState is one overlay lifecycle state.
type OverlayState string

// Overlay lifecycle states.
const (
	OverlayDraft     OverlayState = "DRAFT"
	OverlayValidated OverlayState = "VALIDATED"
	OverlayReviewed  OverlayState = "REVIEWED"
	OverlayPublished OverlayState = "PUBLISHED"
	OverlayActive    OverlayState = "ACTIVE"
)

// OverlayLevel is one inheritance level, enterprise first.
type OverlayLevel string

// Inheritance levels.
const (
	OverlayEnterprise  OverlayLevel = "enterprise"
	OverlayCompany     OverlayLevel = "company"
	OverlayLegalEntity OverlayLevel = "legal-entity"
)

// Overlay step kinds.
const (
	StepPhase        = "phase"
	StepApproval     = "approval"
	StepNotice       = "notice"
	StepLoop         = "loop"
	StepCapability   = "capability-call"
	StepCompensation = "compensation"
)

// OverlayID is the tenant-owned definition namespace: tenant plus name.
// The product namespace is reserved and cannot be squatted.
type OverlayID struct {
	Tenant string
	Name   string
}

// OverlayStep is one tenant-authored workflow step.
type OverlayStep struct {
	Name       string
	Kind       string
	Bound      int
	Capability string
}

// Overlay is one tenant overlay on a product template.
type Overlay struct {
	ID         OverlayID
	TemplateID string
	Level      OverlayLevel
	State      OverlayState
	Author     string
	Reviewer   string
	Steps      []OverlayStep
	Thresholds map[string]string
	Fixtures   FixtureReport
	Signatures []cryptoagility.Signature
	Digest     string
}

// OverlayTemplate is the product template an overlay extends.
type OverlayTemplate struct {
	ID                string
	MandatoryPhases   []string
	MaxLoopBound      int
	AllowCompensation bool
}

// FixtureReport is the simulation fixture outcome publish requires.
type FixtureReport struct {
	Passed int
	Total  int
}

// AllPassed reports whether every fixture passed.
func (r FixtureReport) AllPassed() bool { return r.Total > 0 && r.Passed == r.Total }

// FixtureRunner runs the overlay's simulation fixtures.
type FixtureRunner interface {
	RunFixtures(o Overlay) (FixtureReport, error)
}

// Canonical renders the overlay deterministically for signing and
// golden pinning. It covers identity, template, level, authorship,
// steps and thresholds — never state, fixtures, signatures or digest,
// which all postdate the signature.
func (o Overlay) Canonical() string {
	var b strings.Builder
	b.WriteString("overlay/v1\n")
	b.WriteString("tenant: " + o.ID.Tenant + "\n")
	b.WriteString("name: " + o.ID.Name + "\n")
	b.WriteString("template: " + o.TemplateID + "\n")
	b.WriteString("level: " + string(o.Level) + "\n")
	b.WriteString("author: " + o.Author + "\n")
	b.WriteString("reviewer: " + o.Reviewer + "\n")
	steps := append([]OverlayStep(nil), o.Steps...)
	sort.Slice(steps, func(i, j int) bool { return steps[i].Name < steps[j].Name })
	for _, s := range steps {
		fmt.Fprintf(&b, "step: %s|%s|%d|%s\n", s.Name, s.Kind, s.Bound, s.Capability)
	}
	var keys []string
	for k := range o.Thresholds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("threshold: " + k + "=" + o.Thresholds[k] + "\n")
	}
	return b.String()
}

// DigestFor binds the overlay to its template: the sha256 of the
// template id over the canonical bytes.
func (o Overlay) DigestFor(template OverlayTemplate) string {
	sum := sha256.Sum256([]byte(template.ID + "\n" + o.Canonical()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func checkContent(o Overlay, template OverlayTemplate, allow []string) error {
	if o.TemplateID != template.ID {
		return fmt.Errorf("%w: template %q is not %q", ErrOverlayInvalid, o.TemplateID, template.ID)
	}
	allowed := make(map[string]bool, len(allow))
	for _, c := range allow {
		allowed[c] = true
	}
	seen := make(map[string]bool, len(o.Steps))
	phases := make(map[string]bool, len(o.Steps))
	for _, s := range o.Steps {
		if strings.TrimSpace(s.Name) == "" || seen[s.Name] {
			return fmt.Errorf("%w: blank or duplicate step %q", ErrOverlayInvalid, s.Name)
		}
		seen[s.Name] = true
		switch s.Kind {
		case StepPhase:
			phases[s.Name] = true
		case StepApproval, StepNotice:
		case StepLoop:
			if s.Bound < 1 || s.Bound > template.MaxLoopBound {
				return fmt.Errorf("%w: loop %q bound %d outside 1..%d", ErrOverlayControl, s.Name, s.Bound, template.MaxLoopBound)
			}
		case StepCapability:
			if s.Capability == "" || !allowed[s.Capability] {
				return fmt.Errorf("%w: capability %q outside the tenant allow list", ErrOverlayControl, s.Capability)
			}
		case StepCompensation:
			if !template.AllowCompensation {
				return fmt.Errorf("%w: compensation %q cannot be authored on %q", ErrOverlayControl, s.Name, template.ID)
			}
		default:
			return fmt.Errorf("%w: unknown step kind %q", ErrOverlayInvalid, s.Kind)
		}
	}
	for _, phase := range template.MandatoryPhases {
		if !phases[phase] {
			return fmt.Errorf("%w: mandatory phase %q omitted", ErrOverlayControl, phase)
		}
	}
	return nil
}

func checkNamespace(o Overlay) error {
	if strings.TrimSpace(o.ID.Tenant) == "" || strings.TrimSpace(o.ID.Name) == "" {
		return fmt.Errorf("%w: tenant and name are required", ErrOverlayInvalid)
	}
	if strings.EqualFold(strings.TrimSpace(o.ID.Tenant), "product") {
		return fmt.Errorf("%w: the product namespace is reserved", ErrOverlayInvalid)
	}
	switch o.Level {
	case OverlayEnterprise, OverlayCompany, OverlayLegalEntity:
	default:
		return fmt.Errorf("%w: unknown level %q", ErrOverlayInvalid, o.Level)
	}
	if strings.TrimSpace(o.Author) == "" {
		return fmt.Errorf("%w: author is required", ErrOverlayInvalid)
	}
	return nil
}

// ValidateOverlay moves a DRAFT overlay to VALIDATED: namespace,
// template content rules, and passing simulation fixtures.
func ValidateOverlay(o Overlay, template OverlayTemplate, allow []string, runner FixtureRunner) (Overlay, error) {
	if o.State != "" && o.State != OverlayDraft {
		return o, fmt.Errorf("%w: validate from %s", ErrOverlayState, o.State)
	}
	if err := checkNamespace(o); err != nil {
		return o, err
	}
	if err := checkContent(o, template, allow); err != nil {
		return o, err
	}
	if runner == nil {
		return o, fmt.Errorf("%w: no fixture runner", ErrOverlayInvalid)
	}
	report, err := runner.RunFixtures(o)
	if err != nil {
		return o, fmt.Errorf("%w: %v", ErrOverlayFixtures, err)
	}
	if !report.AllPassed() {
		return o, fmt.Errorf("%w: %d of %d passed", ErrOverlayFixtures, report.Passed, report.Total)
	}
	o.Fixtures = report
	o.State = OverlayValidated
	return o, nil
}

// ReviewOverlay moves a VALIDATED overlay to REVIEWED under separation
// of duties: the reviewer must differ from the author.
func ReviewOverlay(o Overlay, reviewer string) (Overlay, error) {
	if o.State != OverlayValidated {
		return o, fmt.Errorf("%w: review from %s", ErrOverlayState, o.State)
	}
	if strings.TrimSpace(reviewer) == "" {
		return o, fmt.Errorf("%w: reviewer is required", ErrOverlayInvalid)
	}
	if reviewer == o.Author {
		return o, fmt.Errorf("%w: author cannot review", ErrOverlaySoD)
	}
	o.Reviewer = reviewer
	o.State = OverlayReviewed
	return o, nil
}

// PublishOverlay moves a REVIEWED overlay to PUBLISHED: it re-checks
// content and re-runs fixtures, then author and reviewer sign the
// canonical bytes under two different keys.
func PublishOverlay(o Overlay, template OverlayTemplate, allow []string, runner FixtureRunner, authorKey, reviewerKey cryptoagility.Key, policy cryptoagility.AlgorithmPolicy, at time.Time) (Overlay, error) {
	if o.State != OverlayReviewed {
		return o, fmt.Errorf("%w: publish from %s", ErrOverlayState, o.State)
	}
	if o.Reviewer == "" || o.Reviewer == o.Author {
		return o, fmt.Errorf("%w: no independent review", ErrOverlaySoD)
	}
	if authorKey.ID == reviewerKey.ID {
		return o, fmt.Errorf("%w: author and reviewer share key %q", ErrOverlaySoD, authorKey.ID)
	}
	if err := checkContent(o, template, allow); err != nil {
		return o, err
	}
	if runner == nil {
		return o, fmt.Errorf("%w: no fixture runner", ErrOverlayInvalid)
	}
	report, err := runner.RunFixtures(o)
	if err != nil || !report.AllPassed() {
		return o, fmt.Errorf("%w: publish requires passing fixtures", ErrOverlayFixtures)
	}
	o.Fixtures = report
	canonical := []byte(o.Canonical())
	sigAuthor, err := cryptoagility.Sign(canonical, authorKey, policy)
	if err != nil {
		return o, fmt.Errorf("%w: author sign: %v", ErrOverlaySignature, err)
	}
	sigReviewer, err := cryptoagility.Sign(canonical, reviewerKey, policy)
	if err != nil {
		return o, fmt.Errorf("%w: reviewer sign: %v", ErrOverlaySignature, err)
	}
	o.Signatures = []cryptoagility.Signature{sigAuthor, sigReviewer}
	o.Digest = o.DigestFor(template)
	o.State = OverlayPublished
	_ = at
	return o, nil
}

// ActivateOverlay moves a PUBLISHED overlay to ACTIVE after verifying
// both signatures and the digest over the current bytes: any tampering
// since publish refuses.
func ActivateOverlay(o Overlay, keys map[string]cryptoagility.Key, policy cryptoagility.AlgorithmPolicy, at time.Time) (Overlay, error) {
	if o.State != OverlayPublished {
		return o, fmt.Errorf("%w: activate from %s", ErrOverlayState, o.State)
	}
	if len(o.Signatures) != 2 {
		return o, fmt.Errorf("%w: want 2 signatures, have %d", ErrOverlaySignature, len(o.Signatures))
	}
	if o.Signatures[0].KeyID == o.Signatures[1].KeyID {
		return o, fmt.Errorf("%w: signatures share key %q; author and reviewer must sign under different keys", ErrOverlaySignature, o.Signatures[0].KeyID)
	}
	canonical := []byte(o.Canonical())
	for _, sig := range o.Signatures {
		if err := cryptoagility.Verify(canonical, sig, keys, policy, at); err != nil {
			return o, fmt.Errorf("%w: %v", ErrOverlaySignature, err)
		}
	}
	sum := sha256.Sum256([]byte(o.TemplateID + "\n" + o.Canonical()))
	if want := "sha256:" + hex.EncodeToString(sum[:]); o.Digest != want {
		return o, fmt.Errorf("%w: digest mismatch", ErrOverlaySignature)
	}
	o.State = OverlayActive
	return o, nil
}

// LineageEntry records which level won one merged key.
type LineageEntry struct {
	Key   string
	Level OverlayLevel
}

// ResolvedOverlay is the merged enterprise/company/legal-entity view.
type ResolvedOverlay struct {
	Steps      []OverlayStep
	Thresholds map[string]string
	Lineage    []LineageEntry
}

// ResolveInheritance merges enterprise, company and legal-entity
// overlays for one overlay id: deeper levels override by step name and
// threshold key, and every winning value carries its level as lineage.
//
// Every input must be PUBLISHED or ACTIVE: the merged view is the
// artifact that runs, so unsigned draft content can never flow into it.
// The merged view itself passes the template content rules, so an
// override can neither omit a mandatory phase nor smuggle in an
// unlisted capability, an unbounded loop or authored compensation.
func ResolveInheritance(template OverlayTemplate, allow []string, enterprise, company, legal Overlay) (ResolvedOverlay, error) {
	for _, in := range []struct {
		level OverlayLevel
		state OverlayState
	}{
		{OverlayEnterprise, enterprise.State},
		{OverlayCompany, company.State},
		{OverlayLegalEntity, legal.State},
	} {
		if in.state != OverlayPublished && in.state != OverlayActive {
			return ResolvedOverlay{}, fmt.Errorf("%w: %s overlay is %s, want PUBLISHED or ACTIVE", ErrOverlayState, in.level, in.state)
		}
	}
	if enterprise.Level != OverlayEnterprise || company.Level != OverlayCompany || legal.Level != OverlayLegalEntity {
		return ResolvedOverlay{}, fmt.Errorf("%w: levels must be enterprise, company, legal-entity", ErrOverlayInvalid)
	}
	if enterprise.ID != company.ID || enterprise.ID != legal.ID || enterprise.TemplateID != company.TemplateID || enterprise.TemplateID != legal.TemplateID {
		return ResolvedOverlay{}, fmt.Errorf("%w: inheritance mixes overlays", ErrOverlayInvalid)
	}
	var out ResolvedOverlay
	out.Thresholds = make(map[string]string)
	lineage := make(map[string]OverlayLevel)
	index := make(map[string]int)
	apply := func(level OverlayLevel, steps []OverlayStep, thresholds map[string]string) {
		for _, s := range steps {
			if i, ok := index[s.Name]; ok {
				out.Steps[i] = s
			} else {
				index[s.Name] = len(out.Steps)
				out.Steps = append(out.Steps, s)
			}
			lineage["step:"+s.Name] = level
		}
		var tkeys []string
		for k := range thresholds {
			tkeys = append(tkeys, k)
		}
		sort.Strings(tkeys)
		for _, k := range tkeys {
			out.Thresholds[k] = thresholds[k]
			lineage["threshold:"+k] = level
		}
	}
	apply(OverlayEnterprise, enterprise.Steps, enterprise.Thresholds)
	apply(OverlayCompany, company.Steps, company.Thresholds)
	apply(OverlayLegalEntity, legal.Steps, legal.Thresholds)
	var lkeys []string
	for k := range lineage {
		lkeys = append(lkeys, k)
	}
	sort.Strings(lkeys)
	for _, k := range lkeys {
		out.Lineage = append(out.Lineage, LineageEntry{Key: k, Level: lineage[k]})
	}
	merged := Overlay{ID: enterprise.ID, TemplateID: enterprise.TemplateID, Steps: out.Steps}
	if err := checkContent(merged, template, allow); err != nil {
		return ResolvedOverlay{}, err
	}
	return out, nil
}
