package timeprofile

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AssignmentFacts is the subset of an assignment Resolve matches rules
// against. It carries no time data; Resolve is given "at" separately so the
// same facts can be resolved at different instants without mutation.
type AssignmentFacts struct {
	TenantRef      values.TenantId
	JobRef         string
	PositionRef    string
	Category       WorkerCategory
	LocationRef    string
	LegalEntityRef string
}

func (f AssignmentFacts) validate() error {
	if err := f.TenantRef.Validate(); err != nil {
		return invalidProfile("tenant_ref", err.Error())
	}
	if !f.Category.Valid() {
		return invalidProfile("category", "assignment category is not declared")
	}
	return nil
}

// EligibilityRule binds an assignment shape to the TimeProfile it resolves
// to. An empty matcher field is a wildcard on every axis except tenant,
// which is never a wildcard: a rule only ever matches assignments in its own
// tenant, which is what keeps Resolve from ever returning one tenant's
// profile for another tenant's assignment.
type EligibilityRule struct {
	ID             string
	TenantRef      values.TenantId
	JobRef         string
	PositionRef    string
	Category       WorkerCategory
	LocationRef    string
	LegalEntityRef string
	// Priority ranks rules when more than one matches; the highest wins.
	// Two matching rules that tie on the highest priority are an
	// ErrAmbiguousProfile, never a silent pick.
	Priority int
	Profile  TimeProfile
}

func (r EligibilityRule) validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return invalidProfile("id", "eligibility rule id is required")
	}
	if err := r.TenantRef.Validate(); err != nil {
		return invalidProfile("tenant_ref", err.Error())
	}
	if r.Category != "" && !r.Category.Valid() {
		return invalidProfile("category", "eligibility rule category is not declared")
	}
	if err := r.Profile.Validate(); err != nil {
		return err
	}
	if r.Profile.TenantRef != r.TenantRef {
		return invalidProfile("tenant_ref", "eligibility rule tenant and its profile's tenant must match")
	}
	return nil
}

// matches reports whether the rule's declared axes all agree with facts. An
// empty rule field matches any value on that axis; the tenant field is
// compared exactly and is never a wildcard.
func (r EligibilityRule) matches(f AssignmentFacts) bool {
	if r.TenantRef != f.TenantRef {
		return false
	}
	if r.JobRef != "" && r.JobRef != f.JobRef {
		return false
	}
	if r.PositionRef != "" && r.PositionRef != f.PositionRef {
		return false
	}
	if r.Category != "" && r.Category != f.Category {
		return false
	}
	if r.LocationRef != "" && r.LocationRef != f.LocationRef {
		return false
	}
	if r.LegalEntityRef != "" && r.LegalEntityRef != f.LegalEntityRef {
		return false
	}
	return true
}

// Resolve picks the one TimeProfile that applies to an assignment as of at.
// A rule that fails validation, or whose profile does not cover at, is never
// a candidate: a forged or stale rule can never be resolved to. No match is
// ErrNoProfile: the assignment cannot record time. Two matches tied on the
// highest priority is ErrAmbiguousProfile.
//
// Resolve is pure and reads nothing but its arguments: the caller decides
// when to call it. A session or period run must resolve once at its own
// start and keep that result for its lifetime; calling Resolve again for an
// open run and swapping in a new answer is a defect in the caller, not a
// case Resolve compensates for. A profile version change is visible only the
// next time a caller resolves, which is what "applies from the next session
// or period, never repins an open run" means at this pure layer.
func Resolve(rules []EligibilityRule, facts AssignmentFacts, at values.Instant) (TimeProfile, error) {
	if err := facts.validate(); err != nil {
		return TimeProfile{}, err
	}
	if err := at.Validate(); err != nil {
		return TimeProfile{}, invalidProfile("at", err.Error())
	}

	var (
		best         []EligibilityRule
		bestPriority int
		found        bool
	)
	for _, r := range rules {
		if r.validate() != nil {
			continue
		}
		if !r.matches(facts) {
			continue
		}
		if !r.Profile.coversInstant(at) {
			continue
		}
		switch {
		case !found:
			best = []EligibilityRule{r}
			bestPriority = r.Priority
			found = true
		case r.Priority > bestPriority:
			best = []EligibilityRule{r}
			bestPriority = r.Priority
		case r.Priority == bestPriority:
			best = append(best, r)
		}
	}
	if !found {
		return TimeProfile{}, ErrNoProfile
	}
	if len(best) > 1 {
		return TimeProfile{}, ErrAmbiguousProfile
	}
	return best[0].Profile, nil
}
