package balance

// This file owns BAL-008: the complete, redacted explanation of one balance
// derivation. It reads the BAL-003 authorized balance, the BAL-004 rule
// application, the BAL-005 lifecycle transition and the BAL-006 correction
// impacts against the BAL-001 definition, and returns a single derivation
// story covering opening, every counted entry, cap/floor treatment, expiry,
// rollover, corrections and the authority source.
//
// The function is pure: it receives values only, keeps no state, and offers
// no sink for rows, events, outbox entries, human work or provider requests,
// so a rejected explanation and an accepted one alike persist nothing.
//
// Contributors the caller may not see (CanView) are redacted by identity --
// idempotency key, source transaction and dimension values become REDACTED --
// while kind and amount are always retained, so redacted totals still
// reconcile exactly. Digests never leak: they bind content without revealing
// it, and stay bound in both full and redacted explanations.
import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrExplanationInvalid identifies a malformed explanation request.
	ErrExplanationInvalid = errors.New("balance: invalid explanation request")
	// ErrExplanationRejected identifies a derivation that cannot be
	// explained: a missing dimension, correction, rule, lifecycle or
	// authority section, an unverifiable digest, or a total that does not
	// reconcile. It always carries a BAL_008_REJECTED marker with the
	// offending field, state and definition version.
	ErrExplanationRejected = errors.New("balance: explanation rejected")
)

// Closed vocabulary of explanation rejection states.
const (
	stateDefinitionInvalid     = "DEFINITION_INVALID"
	stateMissingDimension      = "MISSING_DIMENSION"
	stateMissingCorrection     = "MISSING_CORRECTION"
	stateMissingRules          = "MISSING_RULES"
	stateMissingLifecycle      = "MISSING_LIFECYCLE"
	stateDigestMismatch        = "DIGEST_MISMATCH"
	stateTotalMismatch         = "TOTAL_MISMATCH"
	stateRevisionMismatch      = "REVISION_MISMATCH"
	stateAccountMismatch       = "ACCOUNT_MISMATCH"
	stateUnevidencedCompletion = "UNEVIDENCED_COMPLETION"
	stateInvalidImpact         = "INVALID_IMPACT"
)

// redactedToken replaces identity a caller may not see.
const redactedToken = "REDACTED"

// ExplanationRejectedError names the exact field, derivation state and
// definition version that made a balance unexplained.
type ExplanationRejectedError struct {
	Field   string
	State   string
	Version string
}

func (e *ExplanationRejectedError) Error() string {
	return fmt.Sprintf("%v: BAL_008_REJECTED definition=%s: field=%s state=%s version=%s",
		ErrExplanationRejected, e.Version, e.Field, e.State, e.Version)
}

func (e *ExplanationRejectedError) Unwrap() error { return ErrExplanationRejected }

func rejectExplanation(field, state, version string) error {
	return &ExplanationRejectedError{Field: field, State: state, Version: version}
}

// ExplanationContributor is one counted ledger movement with its derivation
// role. Adjustment marks BAL-006 correction entries. A redacted contributor
// keeps digest, kind, amount, entry type and lineage so totals and coverage
// still verify; only human-meaningful identity is masked.
type ExplanationContributor struct {
	Digest              string
	Kind                EntryKind
	Amount              values.Decimal
	EntryType           string
	Adjustment          bool
	IdempotencyKey      string
	SourceTransactionID string
	Dimensions          map[string]string
	SupersedesDigest    string
	Redacted            bool
}

// ExplanationRequest binds every derivation an explanation must cover.
// Rules carries the BAL-004 cap/floor section, Lifecycle the BAL-005
// expiry/rollover section, Impacts the BAL-006 correction section. CanView
// reports whether the caller may see a contributor's identity; nil means
// full visibility.
type ExplanationRequest struct {
	Definition AccumulatorDefinition
	Balance    AuthorizedBalance
	Rules      RuleApplicationResult
	Lifecycle  LifecycleResult
	Impacts    []CorrectionImpact
	CanView    func(BalanceEntry) bool
}

// BalanceExplanation is the complete derivation story. Every section is
// always present: an empty ledger explains zero contributors, a NONE policy
// explains an inactive concern, and corrections with no adjustments explain
// an uncorrected balance. Redacted counts masked contributor and excluded
// identity records; derived decision and lifecycle key masking is not
// counted twice.
type BalanceExplanation struct {
	AccountID           string
	DefinitionID        string
	DefinitionVersion   string
	Authority           Authority
	Opening             values.Decimal
	Contributors        []ExplanationContributor
	Excluded            []ExcludedEntry
	Floor               PolicyRule
	Cap                 PolicyRule
	RuleDecisions       []RuleDecision
	RuleAdjustments     []RuleAdjustment
	RulesDigest         string
	Expiry              PolicyRule
	Rollover            PolicyRule
	Expired             values.Decimal
	Carried             values.Decimal
	LifecycleEntries    []LifecycleEntry
	LifecycleExposition []LifecycleExplanation
	LifecycleDigest     string
	Correction          PolicyRule
	CorrectionDigests   []string
	Impacts             []CorrectionImpact
	Ending              values.Decimal
	Redacted            int
	Digest              string
}

// Verify recomputes the explained total from the opening balance and every
// contributor's signed amount. Redaction masks identity only, so a redacted
// explanation verifies exactly like the full one.
func (e BalanceExplanation) Verify() bool {
	if err := e.Opening.Validate(); err != nil {
		return false
	}
	scale, mode := e.Opening.Scale(), e.Opening.Rounding()
	running := e.Opening
	for _, c := range e.Contributors {
		delta, err := contribution(BalanceEntry{Kind: c.Kind, Amount: c.Amount}, scale, mode)
		if err != nil {
			return false
		}
		running, err = running.Add(delta)
		if err != nil {
			return false
		}
	}
	ending, err := running.Quantize(scale, mode)
	if err != nil {
		return false
	}
	return ending.Equal(e.Ending)
}

// ExplainBalanceDerivation explains the whole derivation or rejects it with
// BAL_008_REJECTED. It never persists, queues or emits anything.
func ExplainBalanceDerivation(req ExplanationRequest) (BalanceExplanation, error) {
	def := req.Definition
	if err := def.Validate(); err != nil {
		var detail *DefinitionError
		if errors.As(err, &detail) {
			return BalanceExplanation{}, rejectExplanation(detail.Field, stateDefinitionInvalid, def.Version)
		}
		return BalanceExplanation{}, rejectExplanation("definition", stateDefinitionInvalid, def.Version)
	}
	bal := req.Balance
	if strings.TrimSpace(bal.Digest) == "" {
		return BalanceExplanation{}, rejectExplanation("balance.digest", stateDigestMismatch, def.Version)
	}
	recomputed, err := bal.canonicalDigest()
	if err != nil || recomputed != bal.Digest {
		return BalanceExplanation{}, rejectExplanation("balance.digest", stateDigestMismatch, def.Version)
	}
	if strings.TrimSpace(bal.AccountID) == "" {
		return BalanceExplanation{}, rejectExplanation("balance.account_id", stateAccountMismatch, def.Version)
	}
	if err := bal.Opening.Validate(); err != nil {
		return BalanceExplanation{}, rejectExplanation("balance.opening", stateTotalMismatch, def.Version)
	}
	if err := bal.Ending.Validate(); err != nil {
		return BalanceExplanation{}, rejectExplanation("balance.ending", stateTotalMismatch, def.Version)
	}
	if strings.TrimSpace(req.Rules.Digest) == "" {
		return BalanceExplanation{}, rejectExplanation("rules", stateMissingRules, def.Version)
	}
	if req.Rules.AccountID != bal.AccountID {
		return BalanceExplanation{}, rejectExplanation("rules.account_id", stateAccountMismatch, def.Version)
	}
	if strings.TrimSpace(req.Lifecycle.Digest) == "" {
		return BalanceExplanation{}, rejectExplanation("lifecycle", stateMissingLifecycle, def.Version)
	}
	if req.Lifecycle.AccountID != bal.AccountID {
		return BalanceExplanation{}, rejectExplanation("lifecycle.account_id", stateAccountMismatch, def.Version)
	}
	if err := checkExplanationCoherence(def, bal); err != nil {
		return BalanceExplanation{}, err
	}
	if len(bal.Adjustments) > 0 && len(req.Impacts) == 0 {
		return BalanceExplanation{}, rejectExplanation("corrections", stateMissingCorrection, def.Version)
	}
	if err := checkExplanationImpacts(req.Impacts, def.Version); err != nil {
		return BalanceExplanation{}, err
	}
	if err := checkExplanationTotal(bal, def.Version); err != nil {
		return BalanceExplanation{}, err
	}
	return buildExplanation(req), nil
}

// checkExplanationCoherence proves every counted and excluded entry belongs
// to the explained definition revision and carries exactly its dimensions.
// A definition that lost a dimension, or an entry from another revision,
// leaves the balance unexplained.
func checkExplanationCoherence(def AccumulatorDefinition, bal AuthorizedBalance) error {
	required := map[string]bool{}
	for _, d := range def.Dimensions {
		required[d.Name] = d.Required
	}
	check := func(e BalanceEntry, field string) error {
		if e.DefinitionID != def.ID || e.DefinitionVersion != def.Version {
			return rejectExplanation(field+".definition", stateRevisionMismatch, def.Version)
		}
		if len(e.Dimensions) != len(def.Dimensions) {
			return rejectExplanation(field+".dimensions", stateMissingDimension, def.Version)
		}
		for _, d := range def.Dimensions {
			v, ok := e.Dimensions[d.Name]
			if !ok || (d.Required && strings.TrimSpace(v) == "") {
				return rejectExplanation(field+".dimensions", stateMissingDimension, def.Version)
			}
		}
		for name := range e.Dimensions {
			found := false
			for _, d := range def.Dimensions {
				if d.Name == name {
					found = true
					break
				}
			}
			if !found {
				return rejectExplanation(field+".dimensions", stateMissingDimension, def.Version)
			}
		}
		return nil
	}
	for i, e := range bal.Entries {
		if err := check(e, fmt.Sprintf("contributors[%d]", i)); err != nil {
			return err
		}
	}
	for i, e := range bal.Adjustments {
		if err := check(e, fmt.Sprintf("contributors[%d]", len(bal.Entries)+i)); err != nil {
			return err
		}
	}
	for i, x := range bal.Excluded {
		if err := check(x.Entry, fmt.Sprintf("excluded[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}

// checkExplanationImpacts proves the correction section is honest: every
// impact names its owner and pinned version, and a claimed RECALCULATED
// without evidence digest is an unexplained balance, never a reconciled one.
func checkExplanationImpacts(impacts []CorrectionImpact, version string) error {
	for i, imp := range impacts {
		field := fmt.Sprintf("corrections[%d]", i)
		if strings.TrimSpace(imp.ID) == "" || strings.TrimSpace(imp.Owner) == "" || strings.TrimSpace(imp.Version) == "" {
			member := "id"
			if strings.TrimSpace(imp.ID) != "" {
				member = "owner"
				if strings.TrimSpace(imp.Owner) != "" {
					member = "version"
				}
			}
			return rejectExplanation(field+"."+member, stateInvalidImpact, version)
		}
		switch imp.State {
		case RecalculationPending, ReconciliationRequired:
		case RecalculationComplete:
			if strings.TrimSpace(imp.EvidenceDigest) == "" {
				return rejectExplanation(field+".evidence", stateUnevidencedCompletion, version)
			}
		default:
			return rejectExplanation(field+".state", stateInvalidImpact, version)
		}
	}
	return nil
}

// checkExplanationTotal recomputes opening plus every counted contribution at
// the opening's scale and rounding rule. Anything else is a falsified total.
func checkExplanationTotal(bal AuthorizedBalance, version string) error {
	scale, mode := bal.Opening.Scale(), bal.Opening.Rounding()
	running := bal.Opening
	for _, e := range append(append([]BalanceEntry(nil), bal.Entries...), bal.Adjustments...) {
		delta, err := contribution(e, scale, mode)
		if err != nil {
			return rejectExplanation("ending", stateTotalMismatch, version)
		}
		running, err = running.Add(delta)
		if err != nil {
			return rejectExplanation("ending", stateTotalMismatch, version)
		}
	}
	ending, err := running.Quantize(scale, mode)
	if err != nil || !ending.Equal(bal.Ending) {
		return rejectExplanation("ending", stateTotalMismatch, version)
	}
	return nil
}

// buildExplanation assembles the accepted story. Every caller-visible
// identity passes through CanView; amounts, digests and reasons never do.
func buildExplanation(req ExplanationRequest) BalanceExplanation {
	bal := req.Balance
	denied := map[string]bool{}
	visible := func(e BalanceEntry) bool {
		if req.CanView == nil {
			return true
		}
		return req.CanView(e)
	}
	for _, e := range append(append([]BalanceEntry(nil), bal.Entries...), bal.Adjustments...) {
		if !visible(e) {
			denied[e.IdempotencyKey] = true
		}
	}
	for _, x := range bal.Excluded {
		if !visible(x.Entry) {
			denied[x.Entry.IdempotencyKey] = true
		}
	}
	exp := BalanceExplanation{
		AccountID: bal.AccountID, DefinitionID: req.Definition.ID, DefinitionVersion: req.Definition.Version,
		Authority: req.Definition.Authority, Opening: bal.Opening,
		Floor: req.Definition.Floor, Cap: req.Definition.Cap, RulesDigest: req.Rules.Digest,
		Expiry: req.Definition.Expiry, Rollover: req.Definition.Rollover,
		Expired: req.Lifecycle.Expired, Carried: req.Lifecycle.Carried, LifecycleDigest: req.Lifecycle.Digest,
		Correction: req.Definition.Correction, Ending: bal.Ending,
	}
	for _, e := range bal.Entries {
		c := explainContributor(e, false, denied)
		if c.Redacted {
			exp.Redacted++
		}
		exp.Contributors = append(exp.Contributors, c)
	}
	for _, e := range bal.Adjustments {
		c := explainContributor(e, true, denied)
		if c.Redacted {
			exp.Redacted++
		}
		exp.Contributors = append(exp.Contributors, c)
	}
	for _, x := range bal.Excluded {
		masked := x
		if denied[x.Entry.IdempotencyKey] {
			masked.Entry = maskExplanationEntry(x.Entry)
			exp.Redacted++
		}
		exp.Excluded = append(exp.Excluded, masked)
	}
	for _, d := range req.Rules.Decisions {
		if denied[d.SourceEntry] {
			d.SourceEntry = redactedToken
		}
		exp.RuleDecisions = append(exp.RuleDecisions, d)
	}
	for _, a := range req.Rules.Adjustments {
		if !visible(a.Entry) {
			a.Entry = maskExplanationEntry(a.Entry)
		}
		exp.RuleAdjustments = append(exp.RuleAdjustments, a)
	}
	for _, l := range req.Lifecycle.Entries {
		masked := l
		masked.SourceEntries = append([]string(nil), l.SourceEntries...)
		if !visible(l.Entry) {
			masked.Entry = maskExplanationEntry(l.Entry)
		}
		for i, source := range masked.SourceEntries {
			if denied[source] {
				masked.SourceEntries[i] = redactedToken
			}
		}
		exp.LifecycleEntries = append(exp.LifecycleEntries, masked)
	}
	exp.LifecycleExposition = append([]LifecycleExplanation(nil), req.Lifecycle.Explanation...)
	for _, e := range bal.Adjustments {
		exp.CorrectionDigests = append(exp.CorrectionDigests, e.Digest())
	}
	exp.Impacts = append([]CorrectionImpact(nil), req.Impacts...)
	exp.Digest, _ = exp.canonicalDigest(req)
	return exp
}

func explainContributor(e BalanceEntry, adjustment bool, denied map[string]bool) ExplanationContributor {
	c := ExplanationContributor{
		Digest: e.Digest(), Kind: e.Kind, Amount: e.Amount, EntryType: e.EntryType, Adjustment: adjustment,
		IdempotencyKey: e.IdempotencyKey, SourceTransactionID: e.SourceTransactionID,
		Dimensions: mapsCopy(e.Dimensions), SupersedesDigest: e.SupersedesDigest,
	}
	if denied[e.IdempotencyKey] {
		c.IdempotencyKey, c.SourceTransactionID = redactedToken, redactedToken
		masked := make(map[string]string, len(c.Dimensions))
		for k := range c.Dimensions {
			masked[k] = redactedToken
		}
		c.Dimensions = masked
		c.Redacted = true
	}
	return c
}

// maskExplanationEntry strips caller-visible identity from a derived record
// while keeping amounts, digests and reasons intact.
func maskExplanationEntry(e BalanceEntry) BalanceEntry {
	masked := e.copy()
	masked.IdempotencyKey, masked.SourceTransactionID = redactedToken, redactedToken
	for k := range masked.Dimensions {
		masked.Dimensions[k] = redactedToken
	}
	return masked
}

func (e BalanceExplanation) canonicalDigest(req ExplanationRequest) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.balance.BalanceExplanation", 1).
		String("account_id", e.AccountID).
		String("definition_id", e.DefinitionID).
		String("definition_version", e.DefinitionVersion).
		String("authority.source_id", e.Authority.SourceID).
		String("authority.version", e.Authority.Version).
		Value("opening", e.Opening).
		Value("ending", e.Ending).
		String("balance", req.Balance.Digest).
		String("rules", req.Rules.Digest).
		String("lifecycle", req.Lifecycle.Digest).
		Count("contributors", len(e.Contributors))
	for _, c := range e.Contributors {
		w = w.String("contributor", c.Digest).Bool("contributor.redacted", c.Redacted)
	}
	w = w.Count("excluded", len(e.Excluded))
	for _, x := range e.Excluded {
		w = w.String("excluded.digest", x.Entry.Digest()).String("excluded.reason", string(x.Reason))
	}
	w = w.Count("impacts", len(e.Impacts))
	for _, imp := range e.Impacts {
		w = w.String("impact.id", imp.ID).String("impact.state", string(imp.State)).String("impact.evidence", imp.EvidenceDigest)
	}
	w = w.Int("redacted", int64(e.Redacted))
	return w.Digest()
}
