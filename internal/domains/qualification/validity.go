// Validity, equivalency and substitution evaluation: QUAL-003 evaluates
// every required item against held credentials at an as-of instant,
// consulting declared equivalence rules within one jurisdiction.
//
// A covering direct credential satisfies; a covering substitute satisfies
// only through exactly one matching rule and the chain names that rule.
// Ambiguous equivalency, a future or expired credential, or a missing
// jurisdiction resolves to CONDITIONAL/UNKNOWN rather than a false
// SATISFIED, and every item records the exact rule chain consulted.
// Evaluation is pure: malformed inputs reject with QUAL_003_REJECTED
// naming field/version and persist nothing.
package qualification

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ValidityRejectedCode is the stable machine-readable refusal code.
const ValidityRejectedCode = "QUAL_003_REJECTED"

// ValidityRejectedError names the offending field and version.
type ValidityRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *ValidityRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// AsValidityRejected unwraps a QUAL_003_REJECTED refusal.
func AsValidityRejected(err error) (*ValidityRejectedError, bool) {
	if err == nil {
		return nil, false
	}
	if rejected, ok := err.(*ValidityRejectedError); ok && rejected.Code == ValidityRejectedCode {
		return rejected, true
	}
	return nil, false
}

func validityRejected(field, state string) *ValidityRejectedError {
	return &ValidityRejectedError{Code: ValidityRejectedCode, Field: field, State: state, Version: schemaVersion}
}

// EquivalenceRule declares that holding SubstituteRef may satisfy
// RequiredRef inside Jurisdiction. Rules from any other jurisdiction
// never apply to an evaluation.
type EquivalenceRule struct {
	RuleID        string
	RequiredRef   string
	SubstituteRef string
	Jurisdiction  string
}

func (r EquivalenceRule) Validate() error {
	if strings.TrimSpace(r.RuleID) == "" || strings.TrimSpace(r.RequiredRef) == "" ||
		strings.TrimSpace(r.SubstituteRef) == "" || strings.TrimSpace(r.Jurisdiction) == "" {
		return fmt.Errorf("%w: equivalence rule needs id, required ref, substitute ref and jurisdiction", ErrInvalidRequirement)
	}
	if r.RequiredRef == r.SubstituteRef {
		return fmt.Errorf("%w: equivalence rule %q substitutes itself", ErrInvalidRequirement, r.RuleID)
	}
	return nil
}

// ValidityOutcome is the only QUAL-003 outcome vocabulary.
type ValidityOutcome string

// The validity outcomes.
const (
	ValiditySatisfied   ValidityOutcome = "SATISFIED"
	ValidityConditional ValidityOutcome = "CONDITIONAL"
	ValidityUnknown     ValidityOutcome = "UNKNOWN"
	ValidityUnsatisfied ValidityOutcome = "UNSATISFIED"
)

func (o ValidityOutcome) Valid() bool {
	return o == ValiditySatisfied || o == ValidityConditional || o == ValidityUnknown || o == ValidityUnsatisfied
}

// ValidityItem is the outcome for one required item. Reason is a stable
// token and RuleChain is the exact consultation trace: refs and rule
// IDs only, never evidence content.
type ValidityItem struct {
	Kind          RequirementKind
	Ref           string
	RequiredLevel int
	Outcome       ValidityOutcome
	Reason        string
	RuleChain     []string
}

// ValidityEvaluation is the detached per-item validity result.
type ValidityEvaluation struct {
	RequirementID   string
	Revision        uint64
	Jurisdiction    string
	AsOf            values.Instant
	Items           []ValidityItem
	CanonicalDigest string
}

func (e ValidityEvaluation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.qualification.ValidityEvaluation", schemaVersion).
		String("requirement_id", e.RequirementID).Int("revision", int64(e.Revision)).
		String("jurisdiction", e.Jurisdiction).Value("as_of", e.AsOf).Count("items", len(e.Items))
	for _, item := range e.Items {
		chain := append([]string(nil), item.RuleChain...)
		sort.Strings(chain)
		w.String("kind", string(item.Kind)).String("ref", item.Ref).Int("required_level", int64(item.RequiredLevel)).
			String("outcome", string(item.Outcome)).String("reason", item.Reason)
		w.Count("chain", len(chain))
		for _, step := range chain {
			w.String("rule", step)
		}
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Explain renders the bounded human-readable account: outcome counts
// only, never evidence content.
func (e ValidityEvaluation) Explain() string {
	counts := make(map[ValidityOutcome]int, 4)
	for _, item := range e.Items {
		counts[item.Outcome]++
	}
	return fmt.Sprintf("validity satisfied=%d conditional=%d unknown=%d unsatisfied=%d",
		counts[ValiditySatisfied], counts[ValidityConditional], counts[ValidityUnknown], counts[ValidityUnsatisfied])
}

// EvaluateValidity compares held credentials to every required item at
// as-of, consulting rules inside jurisdiction. It is pure: no rows,
// events, outbox entries, human work or provider requests.
func EvaluateValidity(requirement QualificationRequirement, held []HeldCredential, rules []EquivalenceRule, jurisdiction string, asOf values.Instant) (ValidityEvaluation, error) {
	if err := requirement.Validate(); err != nil {
		return ValidityEvaluation{}, validityRejected("requirement", "invalid")
	}
	for i, h := range held {
		if err := h.Validate(); err != nil {
			return ValidityEvaluation{}, validityRejected("held", fmt.Sprintf("item-%d-invalid", i))
		}
	}
	seenRules := make(map[string]struct{}, len(rules))
	for i, rule := range rules {
		if err := rule.Validate(); err != nil {
			return ValidityEvaluation{}, validityRejected("rule", fmt.Sprintf("item-%d-invalid", i))
		}
		if _, ok := seenRules[rule.RuleID]; ok {
			return ValidityEvaluation{}, validityRejected("rule", "duplicate-rule-id")
		}
		seenRules[rule.RuleID] = struct{}{}
	}
	if err := asOf.Validate(); err != nil {
		return ValidityEvaluation{}, validityRejected("as-of", "invalid")
	}
	allowed := make(map[EvidenceKind]bool, len(requirement.AcceptedEvidence))
	for _, k := range requirement.AcceptedEvidence {
		allowed[k] = true
	}
	evaluation := ValidityEvaluation{RequirementID: requirement.RequirementID, Revision: requirement.Revision, Jurisdiction: jurisdiction, AsOf: asOf}
	for _, req := range requirement.Credentials {
		evaluation.Items = append(evaluation.Items, evaluateValidityItem(RequirementCredential, req.Ref, req.Level, requirement.Validity, held, allowed, jurisdiction, asOf, rules,
			func(h HeldCredential) (string, bool) {
				if h.CredentialRef != "" {
					return h.CredentialRef, true
				}
				return "", false
			}))
	}
	for _, req := range requirement.Skills {
		evaluation.Items = append(evaluation.Items, evaluateValidityItem(RequirementSkill, req.Ref, req.Level, requirement.Validity, held, allowed, jurisdiction, asOf, rules,
			func(h HeldCredential) (string, bool) {
				if h.SkillRef != "" {
					return h.SkillRef, true
				}
				return "", false
			}))
	}
	evaluation.Items = append([]ValidityItem(nil), evaluation.Items...)
	evaluation.CanonicalDigest = canonicalbytes.Digest(evaluation.body())
	return evaluation, nil
}

// heldStanding classifies one usable held credential against as-of and
// the requirement validity.
type heldStanding int

const (
	standingAbsent heldStanding = iota
	standingCover
	standingPartial
	standingFuture
	standingExpired
)

func classifyHeld(h HeldCredential, ref string, level int, required values.EffectiveInterval, allowed map[EvidenceKind]bool, match func(HeldCredential) (string, bool), asOf values.Instant) heldStanding {
	heldRef, ok := match(h)
	if !ok || heldRef != ref || h.Level < level || !allowed[h.EvidenceKind] {
		return standingAbsent
	}
	contains, err := intervalContainsAt(h.Validity, asOf)
	if err != nil || !contains {
		if starts, err := intervalStartsAfter(h.Validity, asOf); err == nil && starts {
			return standingFuture
		}
		if ended, err := intervalEndedBefore(h.Validity, asOf); err == nil && ended {
			return standingExpired
		}
		return standingAbsent
	}
	if intervalCovers(h.Validity, required) {
		return standingCover
	}
	return standingPartial
}

func evaluateValidityItem(kind RequirementKind, ref string, level int, required values.EffectiveInterval, held []HeldCredential, allowed map[EvidenceKind]bool, jurisdiction string, asOf values.Instant, rules []EquivalenceRule, match func(HeldCredential) (string, bool)) ValidityItem {
	item := ValidityItem{Kind: kind, Ref: ref, RequiredLevel: level}
	if strings.TrimSpace(jurisdiction) == "" {
		item.Outcome, item.Reason, item.RuleChain = ValidityUnknown, "missing-jurisdiction", []string{"jurisdiction:missing"}
		return item
	}
	var direct heldStanding
	for _, h := range held {
		if standing := classifyHeld(h, ref, level, required, allowed, match, asOf); standing > direct {
			direct = standingPriority(direct, standing)
		}
	}
	if direct == standingCover {
		item.Outcome, item.Reason, item.RuleChain = ValiditySatisfied, "direct", []string{"direct:cover"}
		return item
	}
	// Consult jurisdiction-scoped substitution rules deterministically.
	type ruleStanding struct {
		ruleID   string
		standing heldStanding
	}
	consulted := make([]ruleStanding, 0)
	for _, rule := range rules {
		if rule.RequiredRef != ref || rule.Jurisdiction != jurisdiction {
			continue
		}
		var top heldStanding
		for _, h := range held {
			if standing := classifyHeld(h, rule.SubstituteRef, level, required, allowed, match, asOf); standingRank(standing) > standingRank(top) {
				top = standing
			}
		}
		consulted = append(consulted, ruleStanding{ruleID: rule.RuleID, standing: top})
	}
	sort.Slice(consulted, func(i, j int) bool { return consulted[i].ruleID < consulted[j].ruleID })
	covering := make([]string, 0)
	for _, c := range consulted {
		if c.standing == standingCover {
			covering = append(covering, c.ruleID)
		}
	}
	switch len(covering) {
	case 1:
		item.Outcome, item.Reason, item.RuleChain = ValiditySatisfied, "substitution", []string{"equiv:" + covering[0]}
		return item
	default:
		if len(covering) > 1 {
			chain := make([]string, 0, len(covering))
			for _, id := range covering {
				chain = append(chain, "equiv:"+id)
			}
			item.Outcome, item.Reason, item.RuleChain = ValidityConditional, "ambiguous-equivalency", chain
			return item
		}
	}
	if direct == standingPartial {
		item.Outcome, item.Reason, item.RuleChain = ValidityConditional, "expiring-validity", []string{"direct:partial"}
		return item
	}
	for _, c := range consulted {
		if c.standing == standingPartial {
			item.Outcome, item.Reason, item.RuleChain = ValidityConditional, "expiring-validity", []string{"equiv:" + c.ruleID, "validity:partial"}
			return item
		}
	}
	if direct == standingFuture {
		item.Outcome, item.Reason, item.RuleChain = ValidityConditional, "future-credential", []string{"direct:future"}
		return item
	}
	for _, c := range consulted {
		if c.standing == standingFuture {
			item.Outcome, item.Reason, item.RuleChain = ValidityConditional, "future-credential", []string{"equiv:" + c.ruleID, "validity:future"}
			return item
		}
	}
	if direct == standingExpired {
		item.Outcome, item.Reason, item.RuleChain = ValidityConditional, "expired-credential", []string{"direct:expired"}
		return item
	}
	for _, c := range consulted {
		if c.standing == standingExpired {
			item.Outcome, item.Reason, item.RuleChain = ValidityConditional, "expired-credential", []string{"equiv:" + c.ruleID, "validity:expired"}
			return item
		}
	}
	if len(consulted) > 0 {
		chain := make([]string, 0, len(consulted))
		for _, c := range consulted {
			chain = append(chain, "equiv:"+c.ruleID+":absent")
		}
		item.Outcome, item.Reason, item.RuleChain = ValidityUnsatisfied, "missing-evidence", chain
		return item
	}
	item.Outcome, item.Reason, item.RuleChain = ValidityUnsatisfied, "missing-evidence", []string{"direct:absent"}
	return item
}

// standingRank orders standings so the strongest evidence wins; absent
// stays weakest.
func standingRank(s heldStanding) int {
	switch s {
	case standingCover:
		return 4
	case standingPartial:
		return 3
	case standingFuture:
		return 2
	case standingExpired:
		return 1
	default:
		return 0
	}
}

func standingPriority(a, b heldStanding) heldStanding {
	if standingRank(b) > standingRank(a) {
		return b
	}
	return a
}

func intervalContainsAt(interval values.EffectiveInterval, asOf values.Instant) (bool, error) {
	if interval.Kind() == values.IntervalKindInstant {
		return interval.ContainsInstant(asOf)
	}
	at := asOf.Time()
	date, err := values.NewLocalDate(at.Year(), at.Month(), at.Day())
	if err != nil {
		return false, err
	}
	return interval.ContainsDate(date)
}

func intervalStartsAfter(interval values.EffectiveInterval, asOf values.Instant) (bool, error) {
	if interval.Kind() == values.IntervalKindInstant {
		start, _ := interval.StartInstant()
		return start.Compare(asOf) > 0, nil
	}
	at := asOf.Time()
	date, err := values.NewLocalDate(at.Year(), at.Month(), at.Day())
	if err != nil {
		return false, err
	}
	start, _ := interval.StartDate()
	return start.Compare(date) > 0, nil
}

func intervalEndedBefore(interval values.EffectiveInterval, asOf values.Instant) (bool, error) {
	if interval.Kind() == values.IntervalKindInstant {
		end, hasEnd := interval.EndInstant()
		if !hasEnd {
			return false, nil
		}
		return end.Compare(asOf) < 0, nil
	}
	at := asOf.Time()
	date, err := values.NewLocalDate(at.Year(), at.Month(), at.Day())
	if err != nil {
		return false, err
	}
	end, hasEnd := interval.EndDate()
	if !hasEnd {
		return false, nil
	}
	return end.Compare(date) < 0, nil
}
