// WF-CAP-008: preview cross-domain consequences of a personal data change.
//
// people.preview_change_impact answers "what moves if this revision
// commits" before anything commits: an address change can move tax
// withholding, benefit networks and leave eligibility, and nothing
// previews that today. The preview is generic: jurisdiction, benefits
// and leave providers each answer for their own domain over one proposed
// revision, and the aggregator orders and seals their typed consequences.
//
// The preview has no effects by construction. It takes no ports, opens no
// transactions and mutates nothing — not the revision, not any domain —
// and the result stays NOT_EXECUTED like a leave simulation. Providers
// live in their owning domains and read caller-supplied reference data,
// so this package imports no sibling domain.
package people

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/jurisdiction"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalidRevision rejects a proposed revision that names no worker,
	// no effective date, no usable addresses or jurisdictions, or no
	// benefits evaluation key.
	ErrInvalidRevision = errors.New("people: invalid proposed revision")
	// ErrDuplicateProvider rejects two providers answering for one domain.
	ErrDuplicateProvider = errors.New("people: duplicate impact provider")
	// ErrInvalidConsequence rejects a consequence outside its provider's
	// domain or without a summary and evidence.
	ErrInvalidConsequence = errors.New("people: invalid impact consequence")
)

// ImpactDomain is the closed vocabulary of previewed domains, in the
// fixed order consequences are reported: jurisdiction first, because
// withholding moves before networks and programs are re-checked.
type ImpactDomain string

const (
	DomainJurisdiction ImpactDomain = "JURISDICTION"
	DomainBenefits     ImpactDomain = "BENEFITS"
	DomainLeave        ImpactDomain = "LEAVE"
)

func (d ImpactDomain) valid() bool {
	switch d {
	case DomainJurisdiction, DomainBenefits, DomainLeave:
		return true
	default:
		return false
	}
}

func domainRank(d ImpactDomain) int {
	switch d {
	case DomainJurisdiction:
		return 0
	case DomainBenefits:
		return 1
	case DomainLeave:
		return 2
	default:
		return 3
	}
}

// PostalAddress is the jurisdiction-relevant facet of a worker address:
// enough to resolve where withholding, networks and programs apply,
// without importing the location domain (which would cycle against the
// jurisdiction provider it hosts).
type PostalAddress struct {
	CountryCode     string
	SubdivisionCode string
	PostalCode      string
	Locality        string
}

func (a PostalAddress) Validate() error {
	if strings.TrimSpace(a.CountryCode) == "" || strings.TrimSpace(a.SubdivisionCode) == "" {
		return fmt.Errorf("%w: address names no country and subdivision", ErrInvalidRevision)
	}
	return nil
}

// ProposedRevision is one worker's proposed data change: the worker, when
// it takes effect, the address and jurisdiction before and after, and the
// benefits evaluation key the revision is previewed under.
type ProposedRevision struct {
	Tenant               string
	WorkerRef            string
	EffectiveDate        time.Time
	CurrentAddress       PostalAddress
	ProposedAddress      PostalAddress
	CurrentJurisdiction  jurisdiction.Jurisdiction
	ProposedJurisdiction jurisdiction.Jurisdiction
	PlanRef              string
	PopulationRef        string
	HoursPerWeek         values.Decimal
	HoursSet             bool
	Classification       string
}

// Validate rejects a revision no provider could honestly preview.
func (r ProposedRevision) Validate() error {
	if strings.TrimSpace(r.Tenant) == "" || strings.TrimSpace(r.WorkerRef) == "" {
		return fmt.Errorf("%w: tenant and worker are required", ErrInvalidRevision)
	}
	if r.EffectiveDate.IsZero() {
		return fmt.Errorf("%w: effective date is required", ErrInvalidRevision)
	}
	if err := r.CurrentAddress.Validate(); err != nil {
		return err
	}
	if err := r.ProposedAddress.Validate(); err != nil {
		return err
	}
	if err := r.CurrentJurisdiction.Validate(); err != nil {
		return fmt.Errorf("%w: current jurisdiction: %v", ErrInvalidRevision, err)
	}
	if err := r.ProposedJurisdiction.Validate(); err != nil {
		return fmt.Errorf("%w: proposed jurisdiction: %v", ErrInvalidRevision, err)
	}
	if strings.TrimSpace(r.PlanRef) == "" || strings.TrimSpace(r.PopulationRef) == "" {
		return fmt.Errorf("%w: plan and population are required", ErrInvalidRevision)
	}
	if r.HoursSet {
		if err := r.HoursPerWeek.Validate(); err != nil {
			return fmt.Errorf("%w: hours: %v", ErrInvalidRevision, err)
		}
	}
	return nil
}

// Digest seals the revision inputs every consequence below is computed from.
func (r ProposedRevision) Digest() string {
	parts := []string{
		"people-change-impact-revision",
		r.Tenant, r.WorkerRef, r.EffectiveDate.UTC().Format(time.RFC3339),
		r.CurrentAddress.CountryCode, r.CurrentAddress.SubdivisionCode, r.CurrentAddress.PostalCode, r.CurrentAddress.Locality,
		r.ProposedAddress.CountryCode, r.ProposedAddress.SubdivisionCode, r.ProposedAddress.PostalCode, r.ProposedAddress.Locality,
		r.CurrentJurisdiction.String(), r.ProposedJurisdiction.String(),
		r.PlanRef, r.PopulationRef, r.HoursPerWeek.String(), r.Classification,
	}
	if r.HoursSet {
		parts = append(parts, "hours-set")
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Consequence is one typed cross-domain effect of the revision. Kind is
// the owning domain's closed vocabulary; Field names the changed worker
// facet; Before and After carry the domain-typed values; EvidenceRefs cite
// the releases, rules and programs behind the call.
type Consequence struct {
	Domain       ImpactDomain
	Kind         string
	Field        string
	Before       string
	After        string
	Summary      string
	EvidenceRefs []string
}

// ImpactProvider answers for exactly one domain. Implementations are pure:
// they read the revision and their own reference data and return
// consequences, with no effects and no ports.
type ImpactProvider interface {
	Domain() ImpactDomain
	Preview(rev ProposedRevision) ([]Consequence, error)
}

// ChangeImpact is one sealed, unexecuted preview.
type ChangeImpact struct {
	RevisionDigest string
	EffectiveDate  time.Time
	Consequences   []Consequence
	NotExecuted    bool
	Digest         string
}

// Verify recomputes the seal. A preview whose consequences were edited
// after the fact never verifies.
func (c ChangeImpact) Verify() error {
	if !c.NotExecuted {
		return fmt.Errorf("%w: preview must stay NOT_EXECUTED", ErrInvalidConsequence)
	}
	if c.Digest == "" || c.Digest != sealedDigest(c.RevisionDigest, c.EffectiveDate, c.Consequences) {
		return fmt.Errorf("%w: preview digest does not match its content", ErrInvalidConsequence)
	}
	return nil
}

func sealedDigest(revision string, at time.Time, consequences []Consequence) string {
	parts := []string{"people-change-impact", revision, at.UTC().Format(time.RFC3339)}
	for _, c := range consequences {
		refs := append([]string(nil), c.EvidenceRefs...)
		sort.Strings(refs)
		parts = append(parts, strings.Join([]string{string(c.Domain), c.Kind, c.Field, c.Before, c.After, c.Summary}, "\x01")+"\x02"+strings.Join(refs, "\x03"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PreviewChangeImpact fans one proposed revision out to its domain
// providers and seals their consequences in domain rank order. Each domain
// answers at most once; a provider failure, or a consequence outside its
// domain, fails the whole preview rather than returning a partial one.
func PreviewChangeImpact(rev ProposedRevision, providers []ImpactProvider) (ChangeImpact, error) {
	if err := rev.Validate(); err != nil {
		return ChangeImpact{}, err
	}
	seen := make(map[ImpactDomain]struct{}, len(providers))
	var out []Consequence
	for _, p := range providers {
		if p == nil {
			return ChangeImpact{}, fmt.Errorf("%w: provider is nil", ErrDuplicateProvider)
		}
		domain := p.Domain()
		if !domain.valid() {
			return ChangeImpact{}, fmt.Errorf("%w: provider answers for no declared domain", ErrDuplicateProvider)
		}
		if _, dup := seen[domain]; dup {
			return ChangeImpact{}, fmt.Errorf("%w: %s answers twice", ErrDuplicateProvider, domain)
		}
		seen[domain] = struct{}{}
		list, err := p.Preview(rev)
		if err != nil {
			return ChangeImpact{}, fmt.Errorf("%w: %s: %v", ErrInvalidConsequence, domain, err)
		}
		for _, c := range list {
			if c.Domain != domain {
				return ChangeImpact{}, fmt.Errorf("%w: %s answers for %s", ErrInvalidConsequence, domain, c.Domain)
			}
			if strings.TrimSpace(c.Kind) == "" || strings.TrimSpace(c.Summary) == "" || len(c.EvidenceRefs) == 0 {
				return ChangeImpact{}, fmt.Errorf("%w: %s consequence lacks kind, summary or evidence", ErrInvalidConsequence, domain)
			}
		}
		out = append(out, list...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return domainRank(out[i].Domain) < domainRank(out[j].Domain)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Field < out[j].Field
	})
	impact := ChangeImpact{
		RevisionDigest: rev.Digest(),
		EffectiveDate:  rev.EffectiveDate.UTC(),
		Consequences:   out,
		NotExecuted:    true,
	}
	impact.Digest = sealedDigest(impact.RevisionDigest, impact.EffectiveDate, out)
	return impact, nil
}
