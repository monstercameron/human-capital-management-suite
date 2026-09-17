package balance

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrCorrectionInvalid  = errors.New("balance: invalid retroactive correction")
	ErrCorrectionConflict = errors.New("balance: correction conflicts with existing lineage")
)

type RecalculationState string

const (
	RecalculationPending   RecalculationState = "PENDING"
	RecalculationComplete  RecalculationState = "RECALCULATED"
	ReconciliationRequired RecalculationState = "RECONCILIATION_REQUIRED"
)

type CorrectionDependency struct {
	ID        string
	Owner     string
	Version   string
	DependsOn []string
	State     RecalculationState
}

type RetroCorrectionRequest struct {
	Definition AccumulatorDefinition
	Original   BalanceEntry
	Corrected  BalanceEntry
	Ledger     []BalanceEntry
	Opening    values.Decimal
	// ExpectedDependencies is the trusted, version-pinned impact manifest.
	// Dependencies supplies the requested states for exactly that manifest.
	ExpectedDependencies []CorrectionDependency
	Dependencies         []CorrectionDependency
	// Recalculators recomputes external dependents, keyed by manifest ID.
	// A nil entry (or a missing one) means that dependent has no
	// implementation: it stays RECONCILIATION_REQUIRED rather than being
	// claimed done. A key with no manifest member fails the correction.
	Recalculators map[string]DependentRecalculator
}

// RecalculationEvidence proves one dependent recomputed against this exact
// correction. It binds the corrected canonical balance (InputDigest) and the
// manifest-pinned artifact version (Version) so a stale or drifted
// recomputation cannot be mistaken for a reconciled one.
type RecalculationEvidence struct {
	InputDigest string
	Version     string
	Digest      string
}

// RecalculationContext is the read-only input one dependent recalculator
// observes. Upstream holds the already-settled impacts its declared
// dependencies produced, in dependency order.
type RecalculationContext struct {
	Definition AccumulatorDefinition
	Dependent  CorrectionDependency
	Correction BalanceEntry
	Balance    AuthorizedBalance
	Upstream   []CorrectionImpact
}

// DependentRecalculator recomputes the dependent named in ctx.Dependent and
// returns evidence for it. Any error fails the correction closed: a
// dependent that cannot prove its recomputation stays unreconciled only when
// no implementation was registered at all.
type DependentRecalculator func(ctx RecalculationContext) (RecalculationEvidence, error)

type CorrectionImpact struct {
	ID        string
	Owner     string
	Version   string
	DependsOn []string
	State     RecalculationState
	// EvidenceDigest identifies the recomputed dependent artifact. It is set
	// only for RECALCULATED impacts whose evidence reconciled; empty means
	// the dependent still requires reconciliation.
	EvidenceDigest string
}

type RetroCorrectionResult struct {
	OriginalDigest string
	Correction     BalanceEntry
	Balance        AuthorizedBalance
	Impacts        []CorrectionImpact
	Digest         string
}

// ApplyRetroCorrection appends a signed delta for an immutable prior entry.
// The original is never rewritten. Dependents with a registered recalculator
// are recomputed in dependency order and marked RECALCULATED only when their
// evidence binds this corrected balance digest and their pinned version;
// consumers without an owner/implementation remain explicitly
// reconciliation-required rather than being claimed done.
func ApplyRetroCorrection(req RetroCorrectionRequest) (RetroCorrectionResult, error) {
	if err := req.Definition.Validate(); err != nil {
		return RetroCorrectionResult{}, fmt.Errorf("%w: definition: %v", ErrCorrectionInvalid, err)
	}
	if err := req.Original.Validate(req.Definition); err != nil {
		return RetroCorrectionResult{}, fmt.Errorf("%w: original: %v", ErrCorrectionInvalid, err)
	}
	if err := req.Corrected.Validate(req.Definition); err != nil {
		return RetroCorrectionResult{}, fmt.Errorf("%w: corrected: %v", ErrCorrectionInvalid, err)
	}
	if req.Original.AccountID != req.Corrected.AccountID || req.Original.Digest() == req.Corrected.Digest() {
		return RetroCorrectionResult{}, ErrCorrectionInvalid
	}
	foundOriginal := false
	var priorCorrection *BalanceEntry
	for _, e := range req.Ledger {
		if e.Digest() == req.Original.Digest() && e.RecordedAt.Compare(req.Original.RecordedAt) == 0 {
			if foundOriginal {
				return RetroCorrectionResult{}, ErrCorrectionInvalid
			}
			foundOriginal = true
		}
		if e.SupersedesDigest == req.Original.Digest() {
			if priorCorrection != nil {
				return RetroCorrectionResult{}, ErrCorrectionConflict
			}
			copy := e.copy()
			priorCorrection = &copy
		}
	}
	if !foundOriginal {
		return RetroCorrectionResult{}, fmt.Errorf("%w: original is not an exact ledger member", ErrCorrectionInvalid)
	}
	oldSigned, err := signedContribution(req.Original.Kind, req.Original.Amount)
	if err != nil {
		return RetroCorrectionResult{}, err
	}
	newSigned, err := signedContribution(req.Corrected.Kind, req.Corrected.Amount)
	if err != nil {
		return RetroCorrectionResult{}, err
	}
	delta, err := newSigned.Sub(oldSigned)
	if err != nil || delta.IsZero() {
		return RetroCorrectionResult{}, ErrCorrectionInvalid
	}
	kind := Credit
	if delta.Sign() < 0 {
		kind = Debit
		delta, err = delta.Neg()
		if err != nil {
			return RetroCorrectionResult{}, err
		}
	}
	correction := req.Corrected.copy()
	correction.Kind = kind
	correction.Amount = delta
	correction.EntryType = AdjustmentEntryType
	correction.SupersedesDigest = req.Original.Digest()
	if !contains(req.Definition.EntryTypes, correction.EntryType) {
		return RetroCorrectionResult{}, fmt.Errorf("%w: definition does not permit correction entries", ErrCorrectionInvalid)
	}
	ledger := append([]BalanceEntry(nil), req.Ledger...)
	if priorCorrection != nil {
		if priorCorrection.Digest() != correction.Digest() {
			return RetroCorrectionResult{}, ErrCorrectionConflict
		}
	} else {
		ledger = append(ledger, correction)
	}
	known := req.Corrected.RecordedAt
	if !known.IsSet() || !req.Corrected.AuthorizedAt.IsSet() || !req.Corrected.EffectiveAt.IsSet() {
		return RetroCorrectionResult{}, ErrCorrectionInvalid
	}
	if req.Corrected.AuthorizedAt.After(known) {
		known = req.Corrected.AuthorizedAt
	}
	if err := req.Opening.Validate(); err != nil {
		return RetroCorrectionResult{}, fmt.Errorf("%w: opening: %v", ErrCorrectionInvalid, err)
	}
	balance, err := CalculateAuthorizedBalance(AuthorizedBalanceRequest{AccountID: correction.AccountID, EffectiveAsOf: req.Corrected.EffectiveAt, KnownAt: known, Opening: req.Opening, Scale: req.Corrected.Amount.Scale(), Rounding: req.Corrected.Amount.Rounding()}, ledger)
	if err != nil {
		return RetroCorrectionResult{}, err
	}
	impacts, err := orderImpacts(req.ExpectedDependencies, req.Dependencies)
	if err != nil {
		return RetroCorrectionResult{}, err
	}
	if err := recalculateDependents(req, balance, correction, impacts); err != nil {
		return RetroCorrectionResult{}, err
	}
	result := RetroCorrectionResult{OriginalDigest: req.Original.Digest(), Correction: correction, Balance: balance, Impacts: impacts}
	w := canonicalbytes.New("hcmnext.domains.balance.RetroCorrectionResult", 1).String("original", result.OriginalDigest).String("correction", correction.Digest()).String("balance", balance.Digest).Count("impacts", len(impacts))
	for _, i := range impacts {
		w.String("impact.id", i.ID).String("impact.owner", i.Owner).String("impact.version", i.Version).String("impact.state", string(i.State)).String("impact.evidence", i.EvidenceDigest).Count("impact.depends_on", len(i.DependsOn))
		for _, dependency := range i.DependsOn {
			w.String("impact.dependency", dependency)
		}
	}
	result.Digest, err = w.Digest()
	if err != nil {
		return RetroCorrectionResult{}, err
	}
	return result, nil
}

// recalculateDependents walks the topologically ordered impacts and invokes
// each registered recalculator with the impacts its dependencies already
// settled. Evidence must bind the corrected balance digest and the pinned
// manifest version; anything else -- an error, a drifted version, a stale
// input digest, a missing artifact digest, or a recalculator for an unknown
// dependent -- fails the correction rather than reporting a reconciled
// dependent that is not.
func recalculateDependents(req RetroCorrectionRequest, balance AuthorizedBalance, correction BalanceEntry, impacts []CorrectionImpact) error {
	byID := make(map[string]int, len(impacts))
	for i := range impacts {
		byID[impacts[i].ID] = i
	}
	for id := range req.Recalculators {
		if _, ok := byID[id]; !ok {
			return fmt.Errorf("%w: recalculator %s is not a member of the trusted dependency manifest", ErrCorrectionInvalid, id)
		}
	}
	for i := range impacts {
		recalculate := req.Recalculators[impacts[i].ID]
		if recalculate == nil {
			continue
		}
		dep := CorrectionDependency{ID: impacts[i].ID, Owner: impacts[i].Owner, Version: impacts[i].Version, DependsOn: append([]string(nil), impacts[i].DependsOn...)}
		evidence, err := recalculate(RecalculationContext{Definition: req.Definition, Dependent: dep, Correction: correction.copy(), Balance: balance, Upstream: append([]CorrectionImpact(nil), impacts[:i]...)})
		if err != nil {
			return fmt.Errorf("%w: recalculation of %s: %v", ErrCorrectionInvalid, impacts[i].ID, err)
		}
		if evidence.Version != impacts[i].Version {
			return fmt.Errorf("%w: recalculation of %s reports version %q, manifest pins %q", ErrCorrectionInvalid, impacts[i].ID, evidence.Version, impacts[i].Version)
		}
		if evidence.InputDigest == "" || evidence.InputDigest != balance.Digest {
			return fmt.Errorf("%w: recalculation of %s is not bound to the corrected balance", ErrCorrectionInvalid, impacts[i].ID)
		}
		if strings.TrimSpace(evidence.Digest) == "" {
			return fmt.Errorf("%w: recalculation of %s supplies no artifact digest", ErrCorrectionInvalid, impacts[i].ID)
		}
		impacts[i].State = RecalculationComplete
		impacts[i].EvidenceDigest = evidence.Digest
	}
	return nil
}

func orderImpacts(expected, in []CorrectionDependency) ([]CorrectionImpact, error) {
	if len(expected) == 0 || len(in) != len(expected) {
		return nil, fmt.Errorf("%w: requested impacts do not cover the trusted dependency manifest", ErrCorrectionInvalid)
	}
	by := map[string]CorrectionDependency{}
	for _, d := range in {
		if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Owner) == "" || strings.TrimSpace(d.Version) == "" || by[d.ID].ID != "" {
			return nil, ErrCorrectionInvalid
		}
		if d.State != "" && d.State != ReconciliationRequired {
			return nil, fmt.Errorf("%w: recalculation state %q has no completion evidence", ErrCorrectionInvalid, d.State)
		}
		by[d.ID] = d
	}
	expectedByID := make(map[string]CorrectionDependency, len(expected))
	requiredOwners := map[string]bool{"balance": false, "payroll": false, "tax": false, "benefits": false}
	for _, d := range expected {
		if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Owner) == "" || strings.TrimSpace(d.Version) == "" || expectedByID[d.ID].ID != "" || d.State != "" {
			return nil, fmt.Errorf("%w: trusted dependency manifest is invalid", ErrCorrectionInvalid)
		}
		expectedByID[d.ID] = d
		owner := strings.ToLower(strings.TrimSpace(d.Owner))
		if _, required := requiredOwners[owner]; required {
			requiredOwners[owner] = true
		}
	}
	for owner, found := range requiredOwners {
		if !found {
			return nil, fmt.Errorf("%w: trusted manifest omits required %s impact", ErrCorrectionInvalid, owner)
		}
	}
	for id, d := range by {
		want, ok := expectedByID[id]
		if !ok || d.Owner != want.Owner || d.Version != want.Version || !sameDependencies(d.DependsOn, want.DependsOn) {
			return nil, fmt.Errorf("%w: impact %s differs from trusted dependency manifest", ErrCorrectionInvalid, id)
		}
	}
	state := map[string]uint8{}
	ordered := make([]CorrectionImpact, 0, len(in))
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return ErrCorrectionInvalid
		}
		if state[id] == 2 {
			return nil
		}
		d, ok := by[id]
		if !ok {
			return ErrCorrectionInvalid
		}
		state[id] = 1
		deps := append([]string(nil), d.DependsOn...)
		sort.Strings(deps)
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = 2
		st := d.State
		if st == "" {
			st = ReconciliationRequired
		}
		ordered = append(ordered, CorrectionImpact{ID: d.ID, Owner: d.Owner, Version: d.Version, DependsOn: deps, State: st})
		return nil
	}
	ids := make([]string, 0, len(in))
	for id := range by {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func sameDependencies(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
