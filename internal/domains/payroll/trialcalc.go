package payroll

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// trialMoneyScale is the exact decimal scale of every trial amount. Trial
// arithmetic never uses binary floating point.
const trialMoneyScale int32 = 2

var (
	// ErrInvalidTrialRequest identifies a malformed trial calculation request.
	ErrInvalidTrialRequest = errors.New("payroll: invalid trial calculation request")
	// ErrTrialUnknown identifies a request whose jurisdiction, rules, or
	// balances are unknown. Unknown input blocks; it is never answered zero.
	ErrTrialUnknown = errors.New("payroll: trial calculation input is unknown")
	// ErrTrialRejected is the typed PAYRUN-004 refusal boundary.
	ErrTrialRejected = errors.New("PAYRUN_004_REJECTED")
)

// TrialCalcError reports the offending field and reason without creating an
// authoritative side effect.
type TrialCalcError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *TrialCalcError) Error() string {
	return fmt.Sprintf("%s: field=%s: %s", e.Code, e.Field, e.Reason)
}

// Is reports the typed PAYRUN-004 boundary and any wrapped cause.
func (e *TrialCalcError) Is(target error) bool {
	return target == ErrTrialRejected || target == e.Cause
}

// Unwrap returns the wrapped cause, if any.
func (e *TrialCalcError) Unwrap() error { return e.Cause }

func trialRefusal(field, reason string, cause error) error {
	return &TrialCalcError{Code: ErrTrialRejected.Error(), Field: field, Reason: reason, Cause: cause}
}

// TrialExceptionSeverity is the closed severity vocabulary for trial
// exceptions. Only BLOCKING stops a calculation or recalculation.
type TrialExceptionSeverity string

const (
	TrialExceptionBlocking TrialExceptionSeverity = "BLOCKING"
	TrialExceptionWarning  TrialExceptionSeverity = "WARNING"
	TrialExceptionInfo     TrialExceptionSeverity = "INFO"
)

// TrialExceptionStatus is the closed lifecycle vocabulary for one exception.
type TrialExceptionStatus string

const (
	TrialExceptionOpen     TrialExceptionStatus = "OPEN"
	TrialExceptionResolved TrialExceptionStatus = "RESOLVED"
)

// Trial exception reason codes.
const (
	TrialExceptionUnknownCode    = "UNKNOWN_EARNING_CODE"
	TrialExceptionUnknownBalance = "UNKNOWN_BALANCE"
)

// TrialExceptionOwner is the accountable owner bound to every trial exception.
const TrialExceptionOwner = "payroll-operations"

// TrialEarningLine is one submitted earning amount under a ruled code.
type TrialEarningLine struct {
	Code   string
	Amount values.Decimal
}

// TrialWorkerInput is one worker's submitted trial scope: the earning lines
// plus the owning balance snapshot the calculation must observe.
type TrialWorkerInput struct {
	WorkerRef     string
	BalanceDigest string
	Lines         []TrialEarningLine
}

// TrialRuleSet is the exact versioned rule binding a trial calculation:
// jurisdiction, rule version and digest, and integer basis-point rates.
type TrialRuleSet struct {
	Version      string
	Digest       string
	Jurisdiction string
	DeductionBps int64
	TaxBps       int64
}

// Validate reports whether the rule set is complete and its rates are lawful.
func (r TrialRuleSet) Validate() error {
	if strings.TrimSpace(r.Version) == "" {
		return trialRefusal("rules.version", "rule version is required", ErrTrialUnknown)
	}
	if strings.TrimSpace(r.Digest) == "" {
		return trialRefusal("rules.digest", "rule digest is required", ErrTrialUnknown)
	}
	if strings.TrimSpace(r.Jurisdiction) == "" {
		return trialRefusal("rules.jurisdiction", "jurisdiction is required", ErrTrialUnknown)
	}
	if r.DeductionBps < 0 || r.DeductionBps > 10000 {
		return trialRefusal("rules.deduction_bps", "deduction rate must be within 0..10000 basis points", ErrInvalidTrialRequest)
	}
	if r.TaxBps < 0 || r.TaxBps > 10000 {
		return trialRefusal("rules.tax_bps", "tax rate must be within 0..10000 basis points", ErrInvalidTrialRequest)
	}
	return nil
}

// bpsRate converts integer basis points to an exact scale-4 decimal rate.
func bpsRate(bps int64) (values.Decimal, error) {
	num := values.MustDecimal(strconv.FormatInt(bps, 10)+".0000", 4, values.RoundingHalfUp)
	den := values.MustDecimal("10000.0000", 4, values.RoundingHalfUp)
	return num.Div(den, 4, values.RoundingHalfUp)
}

// TrialCalcRequest is the complete material for one deterministic trial
// calculation: tenant, idempotency key, run and period binding, the exact
// rule set, the known earning-code vocabulary, and every worker's inputs.
type TrialCalcRequest struct {
	Tenant         string
	CalculationKey string
	RunID          string
	PeriodDigest   string
	Rules          TrialRuleSet
	KnownCodes     []string
	Workers        []TrialWorkerInput
}

// TrialException is one blocking or advisory finding bound to its worker,
// owner, severity, and repair action.
type TrialException struct {
	ExceptionID  string
	WorkerRef    string
	Code         string
	Detail       string
	Severity     TrialExceptionSeverity
	Owner        string
	RepairAction string
	Status       TrialExceptionStatus
}

// TrialWorkerTotal is the exact per-worker answer with its rule trace.
type TrialWorkerTotal struct {
	WorkerRef string
	Gross     values.Decimal
	Deduction values.Decimal
	Tax       values.Decimal
	Net       values.Decimal
	LineCount int
	Trace     string
}

// RecalculationAttempt preserves one superseded calculation attempt: its
// revision, the digest it superseded, the exceptions it resolved, and its
// own digest. A fresh trial calculation carries no attempts.
type RecalculationAttempt struct {
	Attempt            uint64
	SupersedesDigest   string
	ResolvedExceptions []string
	CalculationDigest  string
}

// TrialCalculation is the immutable, digested answer for one trial revision.
type TrialCalculation struct {
	CalculationID     string
	CalculationKey    string
	Tenant            string
	RunID             string
	PeriodDigest      string
	Rules             TrialRuleSet
	Revision          uint64
	SupersedesDigest  string
	Workers           []TrialWorkerTotal
	Inputs            []TrialWorkerInput
	KnownCodes        []string
	RunGross          values.Decimal
	RunDeduction      values.Decimal
	RunTax            values.Decimal
	RunNet            values.Decimal
	Exceptions        []TrialException
	Attempts          []RecalculationAttempt
	CalculationDigest string
}

func trialZero2() values.Decimal {
	return values.MustDecimal("0.00", trialMoneyScale, values.RoundingHalfUp)
}

func trialKnownSet(known []string) map[string]bool {
	set := make(map[string]bool, len(known))
	for _, code := range known {
		set[code] = true
	}
	return set
}

// trialCore classifies every worker input and prices the known scope with
// exact decimal arithmetic. Workers with an unknown balance or unknown
// earning codes are excluded with blocking exceptions; they are never
// priced as zero.
func trialCore(runID string, rules TrialRuleSet, known map[string]bool, workers []TrialWorkerInput) ([]TrialWorkerTotal, []TrialException, values.Decimal, values.Decimal, values.Decimal, values.Decimal, error) {
	deductionRate, err := bpsRate(rules.DeductionBps)
	if err != nil {
		return nil, nil, values.Decimal{}, values.Decimal{}, values.Decimal{}, values.Decimal{}, trialRefusal("rules.deduction_bps", "deduction rate is not computable", err)
	}
	taxRate, err := bpsRate(rules.TaxBps)
	if err != nil {
		return nil, nil, values.Decimal{}, values.Decimal{}, values.Decimal{}, values.Decimal{}, trialRefusal("rules.tax_bps", "tax rate is not computable", err)
	}
	var totals []TrialWorkerTotal
	var exceptions []TrialException
	runGross, runDeduction, runTax, runNet := trialZero2(), trialZero2(), trialZero2(), trialZero2()
	for _, worker := range workers {
		if strings.TrimSpace(worker.BalanceDigest) == "" {
			exceptions = append(exceptions, TrialException{
				ExceptionID:  "trial-exception/" + runID + "/" + worker.WorkerRef + "/" + TrialExceptionUnknownBalance,
				WorkerRef:    worker.WorkerRef,
				Code:         TrialExceptionUnknownBalance,
				Severity:     TrialExceptionBlocking,
				Owner:        TrialExceptionOwner,
				RepairAction: "bind a balance snapshot and recalculate",
				Status:       TrialExceptionOpen,
			})
			continue
		}
		var unknown []string
		for _, line := range worker.Lines {
			if !known[line.Code] {
				unknown = append(unknown, line.Code)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			exceptions = append(exceptions, TrialException{
				ExceptionID:  "trial-exception/" + runID + "/" + worker.WorkerRef + "/" + TrialExceptionUnknownCode,
				WorkerRef:    worker.WorkerRef,
				Code:         TrialExceptionUnknownCode,
				Detail:       strings.Join(unknown, ","),
				Severity:     TrialExceptionBlocking,
				Owner:        TrialExceptionOwner,
				RepairAction: "map earning code to a ruled account and recalculate",
				Status:       TrialExceptionOpen,
			})
			continue
		}
		ordered := append([]TrialEarningLine(nil), worker.Lines...)
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].Code != ordered[j].Code {
				return ordered[i].Code < ordered[j].Code
			}
			return ordered[i].Amount.String() < ordered[j].Amount.String()
		})
		gross := trialZero2()
		for _, line := range ordered {
			gross, err = gross.Add(line.Amount)
			if err != nil {
				return nil, nil, values.Decimal{}, values.Decimal{}, values.Decimal{}, values.Decimal{}, trialRefusal("workers.lines.amount", "earning amount is not summable", err)
			}
		}
		deduction, err := gross.Mul(deductionRate, trialMoneyScale, values.RoundingHalfUp)
		if err != nil {
			return nil, nil, values.Decimal{}, values.Decimal{}, values.Decimal{}, values.Decimal{}, trialRefusal("workers.deduction", "deduction is not computable", err)
		}
		tax, err := gross.Mul(taxRate, trialMoneyScale, values.RoundingHalfUp)
		if err != nil {
			return nil, nil, values.Decimal{}, values.Decimal{}, values.Decimal{}, values.Decimal{}, trialRefusal("workers.tax", "tax is not computable", err)
		}
		afterDeduction, err := gross.Sub(deduction)
		if err != nil {
			return nil, nil, values.Decimal{}, values.Decimal{}, values.Decimal{}, values.Decimal{}, trialRefusal("workers.net", "net is not computable", err)
		}
		net, err := afterDeduction.Sub(tax)
		if err != nil {
			return nil, nil, values.Decimal{}, values.Decimal{}, values.Decimal{}, values.Decimal{}, trialRefusal("workers.net", "net is not computable", err)
		}
		totals = append(totals, TrialWorkerTotal{
			WorkerRef: worker.WorkerRef,
			Gross:     gross, Deduction: deduction, Tax: tax, Net: net,
			LineCount: len(ordered),
			Trace: fmt.Sprintf("worker=%s gross=%s deduction=%s@%dbps tax=%s@%dbps net=%s lines=%d",
				worker.WorkerRef, gross.String(), deduction.String(), rules.DeductionBps, tax.String(), rules.TaxBps, net.String(), len(ordered)),
		})
		runGross, _ = runGross.Add(gross)
		runDeduction, _ = runDeduction.Add(deduction)
		runTax, _ = runTax.Add(tax)
		runNet, _ = runNet.Add(net)
	}
	sort.Slice(totals, func(i, j int) bool { return totals[i].WorkerRef < totals[j].WorkerRef })
	sort.Slice(exceptions, func(i, j int) bool { return exceptions[i].ExceptionID < exceptions[j].ExceptionID })
	return totals, exceptions, runGross, runDeduction, runTax, runNet, nil
}

// validateTrialRequest refuses malformed requests before any arithmetic.
func validateTrialRequest(req TrialCalcRequest) error {
	if strings.TrimSpace(req.Tenant) == "" {
		return trialRefusal("tenant", "tenant is required", ErrInvalidTrialRequest)
	}
	if strings.TrimSpace(req.CalculationKey) == "" {
		return trialRefusal("calculation_key", "calculation key is required", ErrInvalidTrialRequest)
	}
	if strings.TrimSpace(req.RunID) == "" {
		return trialRefusal("run_id", "run id is required", ErrInvalidTrialRequest)
	}
	if strings.TrimSpace(req.PeriodDigest) == "" {
		return trialRefusal("period_digest", "period digest is required", ErrTrialUnknown)
	}
	if err := req.Rules.Validate(); err != nil {
		return err
	}
	if len(req.KnownCodes) == 0 {
		return trialRefusal("known_codes", "at least one known earning code is required", ErrInvalidTrialRequest)
	}
	seenCode := map[string]bool{}
	for _, code := range req.KnownCodes {
		if strings.TrimSpace(code) == "" || seenCode[code] {
			return trialRefusal("known_codes", "known earning codes must be unique and non-blank", ErrInvalidTrialRequest)
		}
		seenCode[code] = true
	}
	if len(req.Workers) == 0 {
		return trialRefusal("workers", "at least one worker input is required", ErrInvalidTrialRequest)
	}
	seenWorker := map[string]bool{}
	for _, worker := range req.Workers {
		if strings.TrimSpace(worker.WorkerRef) == "" || seenWorker[worker.WorkerRef] {
			return trialRefusal("workers.worker_ref", "worker refs must be unique and non-blank", ErrInvalidTrialRequest)
		}
		seenWorker[worker.WorkerRef] = true
		if len(worker.Lines) == 0 {
			return trialRefusal("workers.lines", "worker "+worker.WorkerRef+" has no earning lines", ErrInvalidTrialRequest)
		}
		for _, line := range worker.Lines {
			if strings.TrimSpace(line.Code) == "" {
				return trialRefusal("workers.lines.code", "earning code is required", ErrInvalidTrialRequest)
			}
			if err := line.Amount.Validate(); err != nil {
				return trialRefusal("workers.lines.amount", "earning amount is invalid", err)
			}
			if line.Amount.Scale() != trialMoneyScale {
				return trialRefusal("workers.lines.amount", "earning amount must use scale 2", ErrInvalidTrialRequest)
			}
			if line.Amount.Sign() < 0 {
				return trialRefusal("workers.lines.amount", "earning amount must not be negative", ErrInvalidTrialRequest)
			}
		}
	}
	return nil
}

func (c TrialCalculation) body() *canonicalbytes.Writer {
	known := append([]string(nil), c.KnownCodes...)
	sort.Strings(known)
	w := canonicalbytes.New("hcmnext.domains.payroll.TrialCalculation", 1).
		String("calculation_id", c.CalculationID).
		String("calculation_key", c.CalculationKey).
		String("tenant", c.Tenant).
		String("run_id", c.RunID).
		String("period_digest", c.PeriodDigest).
		String("rules.version", c.Rules.Version).
		String("rules.digest", c.Rules.Digest).
		String("rules.jurisdiction", c.Rules.Jurisdiction).
		Int("rules.deduction_bps", c.Rules.DeductionBps).
		Int("rules.tax_bps", c.Rules.TaxBps).
		Int("revision", int64(c.Revision)).
		String("supersedes_digest", c.SupersedesDigest).
		String("known_codes", strings.Join(known, ",")).
		String("run_gross", c.RunGross.String()).
		String("run_deduction", c.RunDeduction.String()).
		String("run_tax", c.RunTax.String()).
		String("run_net", c.RunNet.String()).
		Int("workers", int64(len(c.Workers))).
		Int("exceptions", int64(len(c.Exceptions))).
		Int("attempts", int64(len(c.Attempts)))
	for _, worker := range c.Workers {
		prefix := "worker." + worker.WorkerRef + "."
		w = w.String(prefix+"gross", worker.Gross.String()).
			String(prefix+"deduction", worker.Deduction.String()).
			String(prefix+"tax", worker.Tax.String()).
			String(prefix+"net", worker.Net.String()).
			Int(prefix+"lines", int64(worker.LineCount)).
			String(prefix+"trace", worker.Trace)
	}
	for _, in := range c.Inputs {
		prefix := "input." + in.WorkerRef + "."
		w = w.String(prefix+"balance_digest", in.BalanceDigest)
		ordered := append([]TrialEarningLine(nil), in.Lines...)
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].Code != ordered[j].Code {
				return ordered[i].Code < ordered[j].Code
			}
			return ordered[i].Amount.String() < ordered[j].Amount.String()
		})
		for _, line := range ordered {
			w = w.String(prefix+"line."+line.Code, line.Amount.String())
		}
	}
	for _, e := range c.Exceptions {
		prefix := "exception." + e.ExceptionID + "."
		w = w.String(prefix+"worker", e.WorkerRef).
			String(prefix+"code", e.Code).
			String(prefix+"detail", e.Detail).
			String(prefix+"severity", string(e.Severity)).
			String(prefix+"owner", e.Owner).
			String(prefix+"repair", e.RepairAction).
			String(prefix+"status", string(e.Status))
	}
	for _, a := range c.Attempts {
		prefix := fmt.Sprintf("attempt.%d.", a.Attempt)
		w = w.String(prefix+"supersedes", a.SupersedesDigest).
			String(prefix+"resolved", strings.Join(a.ResolvedExceptions, ","))
	}
	return w
}

func (c TrialCalculation) computedDigest() string {
	digest, err := c.body().Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Validate checks identity, exact-decimal coherence, exception bindings, run
// sums, attempt history, and the self-digest.
func (c TrialCalculation) Validate() error {
	if strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.CalculationKey) == "" || strings.TrimSpace(c.RunID) == "" {
		return trialRefusal("calculation", "calculation identity is incomplete", ErrInvalidTrialRequest)
	}
	if c.CalculationID != "trial-calculation/"+c.CalculationKey {
		return trialRefusal("calculation_id", "calculation id is not key-bound", ErrInvalidTrialRequest)
	}
	if strings.TrimSpace(c.PeriodDigest) == "" {
		return trialRefusal("period_digest", "period digest is required", ErrTrialUnknown)
	}
	if err := c.Rules.Validate(); err != nil {
		return err
	}
	if c.Revision == 0 {
		return trialRefusal("revision", "revision is required", ErrInvalidTrialRequest)
	}
	if c.Revision == 1 && c.SupersedesDigest != "" {
		return trialRefusal("supersedes_digest", "a fresh calculation supersedes nothing", ErrInvalidTrialRequest)
	}
	if c.Revision > 1 && c.SupersedesDigest == "" {
		return trialRefusal("supersedes_digest", "a recalculation must link its prior digest", ErrInvalidTrialRequest)
	}
	if uint64(len(c.Attempts)) != c.Revision-1 {
		return trialRefusal("attempts", "attempt history must hold every prior revision", ErrInvalidTrialRequest)
	}
	for i, a := range c.Attempts {
		if a.Attempt != uint64(i+2) || a.SupersedesDigest == "" || a.CalculationDigest == "" || len(a.ResolvedExceptions) == 0 {
			return trialRefusal("attempts", "attempt record is incomplete", ErrInvalidTrialRequest)
		}
	}
	if len(c.Attempts) > 0 && c.Attempts[len(c.Attempts)-1].CalculationDigest != c.CalculationDigest {
		return trialRefusal("attempts", "latest attempt must bind the calculation digest", ErrInvalidTrialRequest)
	}
	known := trialKnownSet(c.KnownCodes)
	workers, open, runGross, runDeduction, runTax, runNet, err := trialCore(c.RunID, c.Rules, known, c.Inputs)
	if err != nil {
		return err
	}
	if len(workers) != len(c.Workers) {
		return trialRefusal("workers", "worker totals do not match the retained inputs", ErrInvalidTrialRequest)
	}
	for i := range workers {
		a, b := workers[i], c.Workers[i]
		if a.WorkerRef != b.WorkerRef || !a.Gross.Equal(b.Gross) || !a.Deduction.Equal(b.Deduction) || !a.Tax.Equal(b.Tax) || !a.Net.Equal(b.Net) || a.LineCount != b.LineCount || a.Trace != b.Trace {
			return trialRefusal("workers", "worker "+a.WorkerRef+" total does not match the retained inputs", ErrInvalidTrialRequest)
		}
	}
	var wantOpen, wantResolved []TrialException
	for _, e := range c.Exceptions {
		if e.Status == TrialExceptionOpen {
			wantOpen = append(wantOpen, e)
		} else if e.Status == TrialExceptionResolved {
			wantResolved = append(wantResolved, e)
		} else {
			return trialRefusal("exceptions.status", "exception status is unknown", ErrInvalidTrialRequest)
		}
	}
	if len(wantOpen) != len(open) {
		return trialRefusal("exceptions", "open exceptions do not match the retained inputs", ErrInvalidTrialRequest)
	}
	for i := range open {
		a, b := open[i], wantOpen[i]
		if a.ExceptionID != b.ExceptionID || a.WorkerRef != b.WorkerRef || a.Code != b.Code || a.Detail != b.Detail || a.Severity != b.Severity || a.Owner != b.Owner || a.RepairAction != b.RepairAction {
			return trialRefusal("exceptions", "open exception does not match the retained inputs", ErrInvalidTrialRequest)
		}
	}
	for _, e := range wantResolved {
		if e.ExceptionID == "" || e.WorkerRef == "" || e.Code == "" || e.Owner == "" || e.RepairAction == "" || (e.Code != TrialExceptionUnknownCode && e.Code != TrialExceptionUnknownBalance) {
			return trialRefusal("exceptions", "resolved exception record is incomplete", ErrInvalidTrialRequest)
		}
	}
	if !c.RunGross.Equal(runGross) || !c.RunDeduction.Equal(runDeduction) || !c.RunTax.Equal(runTax) || !c.RunNet.Equal(runNet) {
		return trialRefusal("run_totals", "run totals do not sum the worker totals", ErrInvalidTrialRequest)
	}
	if c.CalculationDigest == "" || c.CalculationDigest != c.computedDigest() {
		return trialRefusal("calculation_digest", "calculation digest mismatch", ErrInvalidTrialRequest)
	}
	return nil
}

// Canonical returns the calculation evidence bytes, or nil when invalid.
func (c TrialCalculation) Canonical() []byte {
	if err := c.Validate(); err != nil {
		return nil
	}
	raw, err := c.body().Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the calculation digest.
func (c TrialCalculation) Digest() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	return c.CalculationDigest, nil
}

// TrialCalculationExplanation is the read-only summary of one trial revision.
type TrialCalculationExplanation struct {
	CalculationID string
	RunID         string
	Revision      uint64
	Workers       int
	Exceptions    int
	Digest        string
}

// Explain returns the calculation facts.
func (c TrialCalculation) Explain() (TrialCalculationExplanation, error) {
	if err := c.Validate(); err != nil {
		return TrialCalculationExplanation{}, err
	}
	return TrialCalculationExplanation{
		CalculationID: c.CalculationID, RunID: c.RunID,
		Revision: c.Revision, Workers: len(c.Workers),
		Exceptions: len(c.Exceptions), Digest: c.CalculationDigest,
	}, nil
}

// ExecuteTrialCalculation prices one deterministic trial payroll revision.
// The same tenant, key, run, period, rules, codes, and worker inputs always
// yield the identical totals, traces, exceptions, and digest. Unknown
// jurisdiction, rules, or balances block with a typed refusal and yield no
// totals — never zeros. Nothing is mutated.
func ExecuteTrialCalculation(req TrialCalcRequest) (TrialCalculation, error) {
	if err := validateTrialRequest(req); err != nil {
		return TrialCalculation{}, err
	}
	known := trialKnownSet(req.KnownCodes)
	workers, exceptions, runGross, runDeduction, runTax, runNet, err := trialCore(req.RunID, req.Rules, known, req.Workers)
	if err != nil {
		return TrialCalculation{}, err
	}
	if len(workers) == 0 {
		return TrialCalculation{}, trialRefusal("workers", "no worker has known inputs; trial scope is unknown", ErrTrialUnknown)
	}
	inputs := append([]TrialWorkerInput(nil), req.Workers...)
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].WorkerRef < inputs[j].WorkerRef })
	calc := TrialCalculation{
		CalculationID:  "trial-calculation/" + req.CalculationKey,
		CalculationKey: req.CalculationKey, Tenant: req.Tenant,
		RunID: req.RunID, PeriodDigest: req.PeriodDigest, Rules: req.Rules,
		Revision:   1,
		Workers:    workers,
		Inputs:     inputs,
		KnownCodes: append([]string(nil), req.KnownCodes...),
		RunGross:   runGross, RunDeduction: runDeduction, RunTax: runTax, RunNet: runNet,
		Exceptions: exceptions,
	}
	calc.CalculationDigest = calc.computedDigest()
	if err := calc.Validate(); err != nil {
		return TrialCalculation{}, err
	}
	return calc, nil
}
