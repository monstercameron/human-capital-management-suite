// PROGRAM-CONF-001: the shared Program abstraction across Benefit, Bonus,
// Learning and Leave. Every domain binds the same seven facets; shared
// status vocabularies stay shared while rule sets stay domain-distinct.
package program

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// CodeRejected is the typed denial code for every PROGRAM-CONF-001 refusal.
const CodeRejected = "PROGRAM_CONF_001_REJECTED"

// The conformance domain set: exactly these four, no more and no fewer.
const (
	DomainBenefit  = "benefit"
	DomainBonus    = "bonus"
	DomainLearning = "learning"
	DomainLeave    = "leave"
)

// Shared facet keys every domain must bind.
const (
	FacetDefinition    = "definition"
	FacetRevision      = "revision"
	FacetPopulation    = "population"
	FacetEligibility   = "eligibility"
	FacetCycle         = "cycle"
	FacetParticipation = "participation"
	FacetOutcome       = "outcome"
)

// SharedFacets is the closed facet vocabulary, in canonical order.
var SharedFacets = []string{
	FacetDefinition,
	FacetRevision,
	FacetPopulation,
	FacetEligibility,
	FacetCycle,
	FacetParticipation,
	FacetOutcome,
}

// Shared participation statuses.
const (
	ParticipationEnrolled  = "ENROLLED"
	ParticipationPending   = "PENDING"
	ParticipationWithdrawn = "WITHDRAWN"
)

// Shared outcome statuses.
const (
	OutcomeAchieved = "ACHIEVED"
	OutcomePartial  = "PARTIAL"
	OutcomePending  = "PENDING"
)

// meaningless marks forced placeholder values that reject the abstraction:
// a facet carrying one of these was filled to satisfy a shape, not bound
// to a real revision.
var meaningless = map[string]bool{
	"": true, "N/A": true, "n/a": true, "TBD": true,
	"tbd": true, "-": true, "NONE": true, "UNKNOWN": true,
}

// Rejection is the typed PROGRAM-CONF-001 denial. It names the offending
// field, state and version without naming which domains exist.
type Rejection struct {
	Code    string
	Field   string
	State   string
	Version string
}

func (e *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s", e.Code, e.Field, e.State, e.Version)
}

// AsRejection reports whether err is a *Rejection.
func AsRejection(err error) (*Rejection, bool) {
	var rej *Rejection
	if errors.As(err, &rej) {
		return rej, true
	}
	return nil, false
}

func reject(field, state, version string) *Rejection {
	return &Rejection{Code: CodeRejected, Field: field, State: state, Version: version}
}

// Fixture is one domain's conformance claim: refs to the shared engines
// plus per-facet digests and shared-vocabulary statuses.
type Fixture struct {
	Domain              string
	Tenant              string
	DefinitionRef       string
	RevisionRef         string
	PopulationRef       string
	EligibilityRef      string
	CycleRef            string
	RuleRefs            []string
	ParticipationStatus string
	OutcomeStatus       string
	FacetDigests        map[string]string
}

// Report is the sealed cross-domain verdict.
type Report struct {
	Domains []string
	Facets  []string
	Digest  string
}

func validDigest(s string) bool {
	if !strings.HasPrefix(s, "sha256:") || len(s) != len("sha256:")+64 {
		return false
	}
	for _, c := range s[len("sha256:"):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// ValidateFixture checks one fixture without comparing it to others.
func ValidateFixture(fx Fixture) error {
	switch fx.Domain {
	case DomainBenefit, DomainBonus, DomainLearning, DomainLeave:
	default:
		return reject("domain", fx.Domain, "v1")
	}
	for _, ref := range []struct {
		name, value string
	}{
		{"definition_ref", fx.DefinitionRef},
		{"revision_ref", fx.RevisionRef},
		{"population_ref", fx.PopulationRef},
		{"eligibility_ref", fx.EligibilityRef},
		{"cycle_ref", fx.CycleRef},
	} {
		if strings.TrimSpace(ref.value) == "" || meaningless[ref.value] {
			return reject(ref.name, "missing-or-forced", "v1")
		}
	}
	if len(fx.RuleRefs) == 0 {
		return reject("rule_refs", "empty", "v1")
	}
	for _, r := range fx.RuleRefs {
		if strings.TrimSpace(r) == "" || meaningless[r] {
			return reject("rule_refs", "forced-placeholder", "v1")
		}
	}
	switch fx.ParticipationStatus {
	case ParticipationEnrolled, ParticipationPending, ParticipationWithdrawn:
	default:
		return reject("participation_status", fx.ParticipationStatus, "v1")
	}
	switch fx.OutcomeStatus {
	case OutcomeAchieved, OutcomePartial, OutcomePending:
	default:
		return reject("outcome_status", fx.OutcomeStatus, "v1")
	}
	if len(fx.FacetDigests) != len(SharedFacets) {
		return reject("facet_digests", "incomplete", "v1")
	}
	for _, facet := range SharedFacets {
		d, ok := fx.FacetDigests[facet]
		if !ok {
			return reject("facet_digests", "missing-"+facet, "v1")
		}
		if meaningless[d] || !validDigest(d) {
			return reject("facet_digests", "forced-"+facet, "v1")
		}
	}
	for k := range fx.FacetDigests {
		known := false
		for _, facet := range SharedFacets {
			if k == facet {
				known = true
			}
		}
		if !known {
			return reject("facet_digests", "unknown-"+k, "v1")
		}
	}
	return nil
}

// CheckSharedAbstraction validates four same-tenant fixtures against the
// shared abstraction and seals the cross-domain verdict. Any divergence —
// a missing or extra domain, a cross-tenant fixture, an unbound facet, a
// forced placeholder, a status outside the shared vocabularies, or one
// mono-rule across all four — refuses with a typed rejection that names
// no domain.
func CheckSharedAbstraction(fixtures []Fixture, tenant string) (Report, error) {
	if strings.TrimSpace(tenant) == "" {
		return Report{}, reject("tenant", "missing", "v1")
	}
	want := []string{DomainBenefit, DomainBonus, DomainLearning, DomainLeave}
	if len(fixtures) != len(want) {
		return Report{}, reject("fixtures", "count-mismatch", "v1")
	}
	seen := map[string]bool{}
	for i := range fixtures {
		fx := &fixtures[i]
		if seen[fx.Domain] {
			return Report{}, reject("fixtures", "duplicate", "v1")
		}
		seen[fx.Domain] = true
		if fx.Tenant != tenant {
			return Report{}, reject("tenant", "scope-mismatch", "v1")
		}
		if err := ValidateFixture(*fx); err != nil {
			return Report{}, err
		}
	}
	for _, d := range want {
		if !seen[d] {
			return Report{}, reject("fixtures", "domain-missing", "v1")
		}
	}
	// Rule sets must stay domain-distinct: one identical rule list across
	// all four means the abstraction forced a mono-rule.
	first := strings.Join(sortedCopy(fixtures[0].RuleRefs), "\x00")
	mono := true
	for _, fx := range fixtures[1:] {
		if strings.Join(sortedCopy(fx.RuleRefs), "\x00") != first {
			mono = false
		}
	}
	if mono {
		return Report{}, reject("rule_refs", "mono-rule", "v1")
	}
	ordered := append([]string(nil), want...)
	w := canonicalbytes.New("program-conf-001", 1)
	w.String("tenant", tenant)
	for _, d := range ordered {
		var fx Fixture
		for _, c := range fixtures {
			if c.Domain == d {
				fx = c
			}
		}
		n := canonicalbytes.New("program-conf-domain", 1)
		n.String("domain", fx.Domain)
		n.String("definition_ref", fx.DefinitionRef)
		n.String("revision_ref", fx.RevisionRef)
		n.String("population_ref", fx.PopulationRef)
		n.String("eligibility_ref", fx.EligibilityRef)
		n.String("cycle_ref", fx.CycleRef)
		n.SortedStrings("rule_refs", fx.RuleRefs)
		n.String("participation_status", fx.ParticipationStatus)
		n.String("outcome_status", fx.OutcomeStatus)
		for _, facet := range SharedFacets {
			n.String("facet:"+facet, fx.FacetDigests[facet])
		}
		w.Nested("domain", n)
	}
	raw, err := w.Bytes()
	if err != nil {
		return Report{}, reject("digest", "unsealed", "v1")
	}
	return Report{
		Domains: ordered,
		Facets:  append([]string(nil), SharedFacets...),
		Digest:  canonicalbytes.Digest(raw),
	}, nil
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
