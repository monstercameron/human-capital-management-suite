package revalidate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Source tokens the historical [decision.Inputs] must carry exactly one
// [decision.Subdecision] for, so [Revalidate] knows which subdecision to
// replace with one freshly built from current [Facts]. They are internal
// wiring, not a public vocabulary: a caller never needs to spell these.
const (
	sourceAuthZ    = "authz"
	sourceSession  = "session"
	sourceBudget   = "budget"
	sourcePosition = "position"
	sourceConflict = "conflict"
)

// Clock supplies the current instant used only to timestamp a [Result].
// Revalidate never reads the wall clock itself; a nil Clock is refused
// rather than silently falling back to time.Now.
type Clock func() values.Instant

// Errors. All are matchable with errors.Is.
var (
	// ErrInvalidInput means a required identity, digest or fact is absent or
	// malformed, or the historical record does not carry every subdecision
	// source revalidation depends on.
	ErrInvalidInput = errors.New("revalidate: invalid input")
	// ErrTampered means recomposing the historical inputs does not reproduce
	// the historical decision's own recorded digest: the historical record
	// handed to Revalidate is not the actual product of GOVERN-002
	// composition and is refused outright rather than revalidated as if it
	// were.
	ErrTampered = errors.New("revalidate: historical decision record does not reproduce from its own inputs")
	// ErrPlanChanged is returned by [Result.VerifyBoundPlan] when the
	// prepared transaction plan's digest no longer matches the one this
	// result was computed against.
	ErrPlanChanged = errors.New("revalidate: prepared transaction plan changed since this result was computed")
)

// validState reports whether s is one of [decision]'s five declared states.
// decision.State's own validity check is unexported, so this package
// declares the same closed set rather than trusting an unchecked value
// through to decision.Compose (which would still fail closed, but with a
// less specific error).
func validState(s decision.State) bool {
	switch s {
	case decision.Allow, decision.AllowWithObligations, decision.Deny, decision.ContradictoryRequirements, decision.UnknownFailClosed:
		return true
	default:
		return false
	}
}

// AuthZFact is the current AuthZ verdict for the exact subject, fields and
// purpose the proposal's material writes touch.
type AuthZFact struct {
	Effect        decision.State
	PolicyVersion string
	FiredRules    []decision.RuleRef
	Restrictions  []string
	Obligations   []decision.Obligation
}

func (f AuthZFact) digest() string {
	return hashParts(string(f.Effect), f.PolicyVersion, ruleRefParts(f.FiredRules), stringParts(f.Restrictions), obligationParts(f.Obligations))
}

// SessionFact is the current state of the session backing the acting
// principal.
type SessionFact struct {
	// Effect is Allow when the session is live and its recorded assurance
	// satisfies the proposal's requirement; Deny or UnknownFailClosed
	// otherwise. A caller never reports Allow for an expired or revoked
	// session.
	Effect     decision.State
	SessionID  string
	Assurance  string
	FiredRules []decision.RuleRef
}

func (f SessionFact) digest() string {
	return hashParts(string(f.Effect), f.SessionID, f.Assurance, ruleRefParts(f.FiredRules))
}

// SourceAuthorityFact is the current per-field source-authority decision the
// proposal's planned writes were authorized under (see
// [github.com/monstercameron/human-capital-management-suite/internal/intent.PlannedWrite.SourceAuthorityDecision]).
type SourceAuthorityFact struct {
	Decision string
}

func (f SourceAuthorityFact) digest() string { return hashParts(f.Decision) }

// FieldClassificationFact is the current field/data classification taxonomy
// and label-set version in force for the proposal's governed fields.
type FieldClassificationFact struct {
	Version string
}

func (f FieldClassificationFact) digest() string { return hashParts(f.Version) }

// LegalPolicyFact is the current legal rule-pack and policy-bundle versions.
// The two are revalidated together as one input: a republished legal
// context and a republished policy bundle are both "the law or policy that
// applied has moved," and GOVERN-003 tracks that as a single category.
type LegalPolicyFact struct {
	LegalPackVersion  string
	PolicyPackVersion string
}

func (f LegalPolicyFact) digest() string { return hashParts(f.LegalPackVersion, f.PolicyPackVersion) }

// ResourceFact is one scarce-resource authority observation: available
// (Effect allow) or not, identified by an observation reference stable
// enough to detect a re-baselined reading even when availability itself did
// not change.
type ResourceFact struct {
	Effect        decision.State
	ObservationID string
	FiredRules    []decision.RuleRef
	Obligations   []decision.Obligation
}

func (f ResourceFact) digest() string {
	return hashParts(string(f.Effect), f.ObservationID, ruleRefParts(f.FiredRules), obligationParts(f.Obligations))
}

// BudgetPositionFact is the current budget-authority and position-capacity
// observation the proposal's reservations depend on. Budget and position are
// revalidated together as one input, per the GOVERN-003 contract.
type BudgetPositionFact struct {
	Budget   ResourceFact
	Position ResourceFact
}

func (f BudgetPositionFact) digest() string { return hashParts(f.Budget.digest(), f.Position.digest()) }

// ConflictFact is the current [conflict.ClassifyConflict]-shaped
// classification for the proposal's write footprint against everything else
// pending, approved, future-dated or executed.
type ConflictFact struct {
	Effect         decision.State
	Classification string
	EvidenceRef    string
	FiredRules     []decision.RuleRef
}

func (f ConflictFact) digest() string {
	return hashParts(string(f.Effect), f.Classification, f.EvidenceRef, ruleRefParts(f.FiredRules))
}

// Facts is the complete set of the seven live inputs GOVERN-003 revalidates.
// Every field is required: an absent fact never resolves to confirmation, it
// fails closed with [ErrInvalidInput].
type Facts struct {
	AuthZ               AuthZFact
	Session             SessionFact
	SourceAuthority     SourceAuthorityFact
	FieldClassification FieldClassificationFact
	LegalPolicy         LegalPolicyFact
	BudgetPosition      BudgetPositionFact
	Conflict            ConflictFact
}

func (f Facts) validate() error {
	if !validState(f.AuthZ.Effect) || f.AuthZ.PolicyVersion == "" {
		return fmt.Errorf("%w: authz fact is incomplete", ErrInvalidInput)
	}
	if !validState(f.Session.Effect) || f.Session.SessionID == "" || f.Session.Assurance == "" {
		return fmt.Errorf("%w: session fact is incomplete", ErrInvalidInput)
	}
	if f.SourceAuthority.Decision == "" {
		return fmt.Errorf("%w: source authority fact is incomplete", ErrInvalidInput)
	}
	if f.FieldClassification.Version == "" {
		return fmt.Errorf("%w: field classification fact is incomplete", ErrInvalidInput)
	}
	if f.LegalPolicy.LegalPackVersion == "" || f.LegalPolicy.PolicyPackVersion == "" {
		return fmt.Errorf("%w: legal/policy fact is incomplete", ErrInvalidInput)
	}
	if !validState(f.BudgetPosition.Budget.Effect) || f.BudgetPosition.Budget.ObservationID == "" {
		return fmt.Errorf("%w: budget fact is incomplete", ErrInvalidInput)
	}
	if !validState(f.BudgetPosition.Position.Effect) || f.BudgetPosition.Position.ObservationID == "" {
		return fmt.Errorf("%w: position fact is incomplete", ErrInvalidInput)
	}
	if !validState(f.Conflict.Effect) || f.Conflict.Classification == "" || f.Conflict.EvidenceRef == "" {
		return fmt.Errorf("%w: conflict fact is incomplete", ErrInvalidInput)
	}
	return nil
}

// HistoricalApproval is the durable record of governance approval as it
// stood when the proposal was approved: the exact [decision.Inputs]
// GOVERN-002 composed, the [decision.Decision] that composition produced,
// the seven [Facts] as ports reported them at that time, and the prepared
// [TransactionPlan] digest the decision authorized.
type HistoricalApproval struct {
	Inputs     decision.Inputs
	Decision   decision.Decision
	Facts      Facts
	PlanDigest string
}

// NewHistoricalApproval composes the record a caller must keep for a decision
// it is about to authorize: base carries every input GOVERN-002 composed that
// this package does not revalidate, facts carry the seven it does, and
// planDigest pins the prepared plan the decision authorizes. The five
// revalidated subdecisions are built here, by exactly the same construction
// [Revalidate] uses for current facts, so an unchanged world recomposes this
// record's own digest byte for byte instead of a near-miss a caller assembled
// by hand.
func NewHistoricalApproval(base decision.Inputs, facts Facts, planDigest string) (HistoricalApproval, error) {
	if err := facts.validate(); err != nil {
		return HistoricalApproval{}, err
	}
	if strings.TrimSpace(planDigest) == "" {
		return HistoricalApproval{}, fmt.Errorf("%w: no prepared transaction plan digest supplied", ErrInvalidInput)
	}
	inputs := buildCurrentInputs(base, facts)
	composed, err := decision.Compose(inputs)
	if err != nil && !errors.Is(err, decision.ErrUnresolvedConflict) {
		return HistoricalApproval{}, fmt.Errorf("%w: approval inputs do not compose: %v", ErrInvalidInput, err)
	}
	out := HistoricalApproval{Inputs: inputs, Decision: composed, Facts: facts, PlanDigest: planDigest}
	if err := out.validate(); err != nil {
		return HistoricalApproval{}, err
	}
	return out, nil
}

// Validate reports whether a stored record is usable: complete, composing
// from its own inputs, and reproducing its own recorded digest.
func (h HistoricalApproval) Validate() error { return h.validate() }

// Allows reports whether the recorded decision authorizes the effect at all.
// A record that reproduces exactly but denies is confirmed and still blocking.
func (h HistoricalApproval) Allows() bool {
	return h.Decision.State == decision.Allow || h.Decision.State == decision.AllowWithObligations
}

func (h HistoricalApproval) validate() error {
	if h.Decision.Digest == "" {
		return fmt.Errorf("%w: historical decision carries no digest", ErrInvalidInput)
	}
	if h.PlanDigest == "" {
		return fmt.Errorf("%w: historical decision is not bound to a prepared transaction plan", ErrInvalidInput)
	}
	if err := h.Facts.validate(); err != nil {
		return fmt.Errorf("historical: %w", err)
	}
	if err := requireSubdecisionSources(h.Inputs.Subdecisions); err != nil {
		return err
	}
	replayed, err := decision.Compose(h.Inputs)
	if err != nil && !errors.Is(err, decision.ErrUnresolvedConflict) {
		return fmt.Errorf("%w: historical inputs do not compose: %v", ErrInvalidInput, err)
	}
	if replayed.Digest != h.Decision.Digest {
		return fmt.Errorf("%w: recomposing historical inputs yields digest %q, recorded decision is %q",
			ErrTampered, replayed.Digest, h.Decision.Digest)
	}
	return nil
}

func requireSubdecisionSources(subs []decision.Subdecision) error {
	required := []string{sourceAuthZ, sourceSession, sourceBudget, sourcePosition, sourceConflict}
	counts := make(map[string]int, len(required))
	for _, s := range subs {
		counts[s.Source]++
	}
	for _, want := range required {
		if counts[want] != 1 {
			return fmt.Errorf("%w: historical decision must carry exactly one %q subdecision, has %d",
				ErrInvalidInput, want, counts[want])
		}
	}
	return nil
}

// RequirementKind is the typed outcome [Revalidate] returns when the current
// facts do not reproduce the historical decision exactly.
type RequirementKind string

// Declared requirements. RequirementNone is the zero value, returned only
// alongside a confirmed result.
const (
	RequirementNone RequirementKind = ""
	// RequirementBlock means the decision recomposed from current facts no
	// longer allows at all: dispatch/commit must refuse outright.
	RequirementBlock RequirementKind = "BLOCK"
	// RequirementReapprovalRequired means governance still allows, but the
	// material basis for the historical approval has moved (AuthZ, session,
	// source authority, field classification or legal/policy version): a
	// human or process approval must be re-obtained before proceeding.
	RequirementReapprovalRequired RequirementKind = "REAPPROVAL_REQUIRED"
	// RequirementReplanRequired means a budget, position or conflict fact
	// moved out from under the prepared plan's reservations or
	// write-ordering assumptions: the plan itself must be recompiled, not
	// merely re-approved.
	RequirementReplanRequired RequirementKind = "REPLAN_REQUIRED"
)

// ChangedInput names one of the seven revalidated categories.
type ChangedInput string

// Declared changed-input tokens, one per GOVERN-003 RED input.
const (
	ChangedAuthZ               ChangedInput = "AUTHZ"
	ChangedSession             ChangedInput = "SESSION"
	ChangedSourceAuthority     ChangedInput = "SOURCE_AUTHORITY"
	ChangedFieldClassification ChangedInput = "FIELD_CLASSIFICATION"
	ChangedLegalPolicyVersion  ChangedInput = "LEGAL_POLICY_VERSION"
	ChangedBudgetPosition      ChangedInput = "BUDGET_POSITION"
	ChangedConflict            ChangedInput = "CONFLICT"
)

// Result is the typed outcome of one revalidation. A confirmed result means
// the exact historical decision digest was reproduced from current facts; an
// unconfirmed result names every changed input and the one typed
// requirement dispatch/commit must satisfy before proceeding.
type Result struct {
	Confirmed        bool
	Requirement      RequirementKind
	ChangedInputs    []ChangedInput
	HistoricalDigest string
	RecomposedDigest string
	RecomposedState  decision.State
	// PlanDigest pins the prepared transaction plan digest this result was
	// computed against. See [Result.VerifyBoundPlan].
	PlanDigest  string
	EvaluatedAt values.Instant
	Explanation string
}

// VerifyBoundPlan refuses to let a caller use this result against a plan
// other than the one it was computed against. Revalidation is only ever
// evidence about the plan that existed at the instant it ran; a plan
// recompiled afterwards (different participants, reservations or lock
// order) invalidates the binding even if none of the seven revalidated
// facts themselves changed again.
func (r Result) VerifyBoundPlan(currentPlanDigest string) error {
	if currentPlanDigest == "" || r.PlanDigest == "" || currentPlanDigest != r.PlanDigest {
		return fmt.Errorf("%w: revalidated for plan digest %q, asked to execute plan digest %q",
			ErrPlanChanged, r.PlanDigest, currentPlanDigest)
	}
	return nil
}

// Revalidate recomposes governance from current facts and compares the
// result to the historical decision. now supplies the instant [Result] is
// stamped with; currentPlanDigest is the prepared transaction plan's exact
// digest at the moment revalidation runs (see [Result.VerifyBoundPlan]).
//
// A confirmed result reproduces historical.Decision.Digest byte for byte
// from current, by calling [decision.Compose] again over historical.Inputs
// with exactly the seven revalidated categories substituted for current
// ones -- nothing else about the historical inputs is disturbed. An
// unconfirmed result names every input that changed and the single, typed
// requirement dispatch or commit must satisfy: BLOCK when the recomposed
// decision no longer allows at all, REPLAN_REQUIRED when a budget, position
// or conflict fact invalidated the prepared plan's own assumptions, or
// REAPPROVAL_REQUIRED when governance still allows but the material basis
// moved.
func Revalidate(now Clock, historical HistoricalApproval, current Facts, currentPlanDigest string) (Result, error) {
	if now == nil {
		return Result{}, fmt.Errorf("%w: no clock supplied", ErrInvalidInput)
	}
	if err := historical.validate(); err != nil {
		return Result{}, err
	}
	if err := current.validate(); err != nil {
		return Result{}, fmt.Errorf("current: %w", err)
	}
	if currentPlanDigest == "" {
		return Result{}, fmt.Errorf("%w: no current prepared transaction plan digest supplied", ErrInvalidInput)
	}

	newInputs := buildCurrentInputs(historical.Inputs, current)
	newDecision, err := decision.Compose(newInputs)
	if err != nil && !errors.Is(err, decision.ErrUnresolvedConflict) {
		return Result{}, fmt.Errorf("%w: current facts do not compose: %v", ErrInvalidInput, err)
	}

	changed := changedInputs(historical.Facts, current)

	result := Result{
		HistoricalDigest: historical.Decision.Digest,
		RecomposedDigest: newDecision.Digest,
		RecomposedState:  newDecision.State,
		PlanDigest:       currentPlanDigest,
		EvaluatedAt:      now(),
	}

	if newDecision.Digest == historical.Decision.Digest {
		result.Confirmed = true
		result.Explanation = fmt.Sprintf("revalidate: confirmed state=%s digest=%s", newDecision.State, newDecision.Digest)
		return result, nil
	}

	result.ChangedInputs = changed
	result.Requirement = requirement(newDecision.State, changed)
	result.Explanation = fmt.Sprintf("revalidate: %s state=%s changed=%v", result.Requirement, newDecision.State, changed)
	return result, nil
}

// requirement maps the recomposed state and the set of changed inputs to the
// one typed requirement dispatch/commit must satisfy. A decision that no
// longer allows always blocks outright, regardless of which input caused it
// -- GOVERN-002's own deny-dominant precedence table already decided that.
// Short of an outright block, a moved budget, position or conflict fact
// invalidates the prepared plan's own reservations or write ordering and
// demands a replan; every other change demands only reapproval.
func requirement(state decision.State, changed []ChangedInput) RequirementKind {
	if state != decision.Allow && state != decision.AllowWithObligations {
		return RequirementBlock
	}
	if slices.Contains(changed, ChangedBudgetPosition) || slices.Contains(changed, ChangedConflict) {
		return RequirementReplanRequired
	}
	return RequirementReapprovalRequired
}

// buildCurrentInputs returns a copy of historical carrying every field
// current facts do not govern, with the seven revalidated categories
// replaced by ones built fresh from current.
func buildCurrentInputs(historical decision.Inputs, current Facts) decision.Inputs {
	out := historical
	out.Context.Authority = current.SourceAuthority.Decision
	out.ControlSnapshot.Classification = current.FieldClassification.Version
	out.ControlSnapshot.LegalContext = current.LegalPolicy.LegalPackVersion
	out.ControlSnapshot.PolicyBundle = current.LegalPolicy.PolicyPackVersion

	subs := make([]decision.Subdecision, 0, len(historical.Subdecisions)+5)
	for _, s := range historical.Subdecisions {
		switch s.Source {
		case sourceAuthZ, sourceSession, sourceBudget, sourcePosition, sourceConflict:
			continue // replaced below with one built from current facts
		default:
			subs = append(subs, s)
		}
	}
	subs = append(subs,
		authzSubdecision(current.AuthZ),
		sessionSubdecision(current.Session),
		resourceSubdecision(sourceBudget, current.BudgetPosition.Budget),
		resourceSubdecision(sourcePosition, current.BudgetPosition.Position),
		conflictSubdecision(current.Conflict),
	)
	out.Subdecisions = subs
	return out
}

// The subdecision builders below each fold their fact's identity fields
// (policy version, session id/assurance, observation id, classification) into
// a deterministic marker [decision.RuleRef] before handing the subdecision to
// [decision.Compose]. decision.Decision.Canonical never encodes a
// subdecision directly, only the fired rules, restrictions, obligations and
// resolved state it contributes; without a marker, a fact whose identity
// changed but whose Effect/FiredRules a caller left untouched (a
// re-baselined budget reading that is still sufficient, say) could silently
// fail to move the recomposed digest. The marker closes that gap for every
// one of the seven inputs.

func authzSubdecision(f AuthZFact) decision.Subdecision {
	rules := append(slices.Clone(f.FiredRules), decision.RuleRef{ID: "authz.policy_version", Version: f.PolicyVersion})
	return decision.Subdecision{
		ID: sourceAuthZ, Source: sourceAuthZ, State: f.Effect,
		FiredRules: rules, Restrictions: slices.Clone(f.Restrictions), Obligations: slices.Clone(f.Obligations),
	}
}

func sessionSubdecision(f SessionFact) decision.Subdecision {
	rules := append(slices.Clone(f.FiredRules),
		decision.RuleRef{ID: "session.identity", Version: f.SessionID},
		decision.RuleRef{ID: "session.assurance", Version: f.Assurance},
	)
	return decision.Subdecision{ID: sourceSession, Source: sourceSession, State: f.Effect, FiredRules: rules}
}

func resourceSubdecision(source string, f ResourceFact) decision.Subdecision {
	rules := append(slices.Clone(f.FiredRules), decision.RuleRef{ID: source + ".observation", Version: f.ObservationID})
	return decision.Subdecision{ID: source, Source: source, State: f.Effect, FiredRules: rules, Obligations: slices.Clone(f.Obligations)}
}

func conflictSubdecision(f ConflictFact) decision.Subdecision {
	rules := append(slices.Clone(f.FiredRules),
		decision.RuleRef{ID: "conflict.classification", Version: f.Classification},
		decision.RuleRef{ID: "conflict.evidence", Version: f.EvidenceRef},
	)
	return decision.Subdecision{ID: sourceConflict, Source: sourceConflict, State: f.Effect, FiredRules: rules}
}

// changedInputs reports every one of the seven categories whose digest
// differs between the historical and current facts, in declared order.
func changedInputs(h, c Facts) []ChangedInput {
	var out []ChangedInput
	if h.AuthZ.digest() != c.AuthZ.digest() {
		out = append(out, ChangedAuthZ)
	}
	if h.Session.digest() != c.Session.digest() {
		out = append(out, ChangedSession)
	}
	if h.SourceAuthority.digest() != c.SourceAuthority.digest() {
		out = append(out, ChangedSourceAuthority)
	}
	if h.FieldClassification.digest() != c.FieldClassification.digest() {
		out = append(out, ChangedFieldClassification)
	}
	if h.LegalPolicy.digest() != c.LegalPolicy.digest() {
		out = append(out, ChangedLegalPolicyVersion)
	}
	if h.BudgetPosition.digest() != c.BudgetPosition.digest() {
		out = append(out, ChangedBudgetPosition)
	}
	if h.Conflict.digest() != c.Conflict.digest() {
		out = append(out, ChangedConflict)
	}
	return out
}

// hashParts returns a stable, length-prefixed sha256 digest over parts, so
// that no two distinct fact values can collide by concatenation ambiguity.
func hashParts(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:%s;", len(p), p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ruleRefParts(rules []decision.RuleRef) string {
	sorted := slices.Clone(rules)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ID != sorted[j].ID {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Version < sorted[j].Version
	})
	var b strings.Builder
	for _, r := range sorted {
		fmt.Fprintf(&b, "%s@%s;", r.ID, r.Version)
	}
	return b.String()
}

func obligationParts(obs []decision.Obligation) string {
	sorted := slices.Clone(obs)
	sort.Slice(sorted, func(i, j int) bool { return obligationKey(sorted[i]) < obligationKey(sorted[j]) })
	var b strings.Builder
	for _, o := range sorted {
		fmt.Fprintf(&b, "%s|%s|%s|%s;", o.ID, o.Version, o.Scope, o.Owner)
	}
	return b.String()
}

func obligationKey(o decision.Obligation) string {
	return o.ID + "\x00" + o.Version + "\x00" + o.Scope + "\x00" + o.Owner
}

func stringParts(ss []string) string {
	sorted := slices.Clone(ss)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}
