// Package envelope is the one immutable deterministic request/result
// contract shared by every HCM engine (ENGINE-CONF-001).
//
// It lives at the kernel/engine boundary: domain-specific request and result
// semantics stay engine-owned, but every engine pins the same execution
// context (definition, revision, tenant, org, purpose, authority,
// effective/known time, source watermarks, classification, provenance), the
// same uncertainty/taint propagation and the same canonical result and
// explanation digest. Engines are pure functions over a Request: there is no
// ambient clock, no ambient config and no effect channel, so replaying an
// identical request yields byte-identical digests with zero persistence or
// side effects by construction.
package envelope

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// Engine contract version for the envelope framing itself.
const contractVersion = 1

// Version reports the envelope contract version.
func Version() int { return contractVersion }

var (
	// ErrContextIncomplete reports a request whose pinned context omits a
	// required dimension: definition, revision, tenant, org, purpose,
	// authority, effective/known time, watermarks, classification,
	// provenance or the input digest.
	ErrContextIncomplete = errors.New("envelope: execution context is incomplete")
	// ErrUncertaintyDowngrade reports an engine that converts UNKNOWN or
	// PARTIAL inputs into a confident result.
	ErrUncertaintyDowngrade = errors.New("envelope: engine downgraded input uncertainty")
	// ErrClassificationLeak reports a result that drops the input
	// classification or provenance taint.
	ErrClassificationLeak = errors.New("envelope: engine dropped classification or provenance")
)

// Uncertainty is the closed epistemic vocabulary. UNKNOWN dominates PARTIAL
// dominates CONFIDENT: combining inputs never lowers uncertainty.
type Uncertainty string

const (
	UncertaintyConfident Uncertainty = "CONFIDENT"
	UncertaintyPartial   Uncertainty = "PARTIAL"
	UncertaintyUnknown   Uncertainty = "UNKNOWN"
)

// Valid reports whether u is a declared uncertainty level.
func (u Uncertainty) Valid() bool {
	switch u {
	case UncertaintyConfident, UncertaintyPartial, UncertaintyUnknown:
		return true
	}
	return false
}

// Combine returns the dominant (least confident) uncertainty.
func Combine(a, b Uncertainty) Uncertainty {
	rank := map[Uncertainty]int{
		UncertaintyConfident: 0,
		UncertaintyPartial:   1,
		UncertaintyUnknown:   2,
	}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

// Context is the pinned execution context every engine request carries.
// Times are caller-supplied: engines never read the ambient clock.
type Context struct {
	EngineDefinition string
	EngineRevision   string
	Tenant           string
	Org              string
	Purpose          string
	Authority        string
	EffectiveAt      time.Time
	KnownAt          time.Time
	SourceWatermarks []string
	Classification   string
	Provenance       string
}

func (c Context) validate() error {
	for field, value := range map[string]string{
		"engine_definition": c.EngineDefinition,
		"engine_revision":   c.EngineRevision,
		"tenant":            c.Tenant,
		"org":               c.Org,
		"purpose":           c.Purpose,
		"authority":         c.Authority,
		"classification":    c.Classification,
		"provenance":        c.Provenance,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrContextIncomplete, field)
		}
	}
	if c.EffectiveAt.IsZero() || c.KnownAt.IsZero() {
		return fmt.Errorf("%w: effective and known time are required", ErrContextIncomplete)
	}
	if len(c.SourceWatermarks) == 0 {
		return fmt.Errorf("%w: at least one source watermark is required", ErrContextIncomplete)
	}
	for _, mark := range c.SourceWatermarks {
		if strings.TrimSpace(mark) == "" {
			return fmt.Errorf("%w: source watermark is blank", ErrContextIncomplete)
		}
	}
	return nil
}

// Input is one named engine input with its own uncertainty and taint.
type Input struct {
	Name           string
	Value          string
	Uncertainty    Uncertainty
	Classification string
	Provenance     string
}

func (in Input) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: input name is required", ErrContextIncomplete)
	}
	if !in.Uncertainty.Valid() {
		return fmt.Errorf("%w: input %q uncertainty %q is not declared", ErrContextIncomplete, in.Name, in.Uncertainty)
	}
	if strings.TrimSpace(in.Classification) == "" || strings.TrimSpace(in.Provenance) == "" {
		return fmt.Errorf("%w: input %q taint is incomplete", ErrContextIncomplete, in.Name)
	}
	return nil
}

// Request is the immutable engine invocation. InputsDigest binds the exact
// input set so replay and result verification share one value.
type Request struct {
	Context      Context
	Inputs       []Input
	InputsDigest string
}

// inputsDigest computes the canonical digest over the sorted input set.
func inputsDigest(inputs []Input) string {
	ordered := append([]Input(nil), inputs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	w := canonicalbytes.New("hcmnext.engines.envelope.Inputs", contractVersion)
	for _, in := range ordered {
		w = w.String("name", in.Name).String("value", in.Value).String("uncertainty", string(in.Uncertainty)).String("classification", in.Classification).String("provenance", in.Provenance)
	}
	body, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(body)
}

// NewRequest pins a request: it validates the context and every input and
// binds the canonical input digest.
func NewRequest(ctx Context, inputs []Input) (Request, error) {
	if err := ctx.validate(); err != nil {
		return Request{}, err
	}
	if len(inputs) == 0 {
		return Request{}, fmt.Errorf("%w: at least one input is required", ErrContextIncomplete)
	}
	seen := map[string]bool{}
	for _, in := range inputs {
		if err := in.validate(); err != nil {
			return Request{}, err
		}
		if seen[in.Name] {
			return Request{}, fmt.Errorf("%w: duplicate input %q", ErrContextIncomplete, in.Name)
		}
		seen[in.Name] = true
	}
	digest := inputsDigest(inputs)
	if digest == "" {
		return Request{}, fmt.Errorf("%w: inputs have no canonical encoding", ErrContextIncomplete)
	}
	return Request{Context: ctx, Inputs: append([]Input(nil), inputs...), InputsDigest: digest}, nil
}

// CombinedUncertainty folds every input uncertainty: the floor an engine
// result must meet, never beat.
func (r Request) CombinedUncertainty() Uncertainty {
	combined := UncertaintyConfident
	for _, in := range r.Inputs {
		combined = Combine(combined, in.Uncertainty)
	}
	return combined
}

// Result is the immutable engine answer. EffectCount is always zero: the
// envelope exposes no persistence or side-effect channel, so an engine
// cannot report an effect even if it wanted to.
type Result struct {
	EngineDefinition  string
	EngineRevision    string
	InputsDigest      string
	Outputs           []Input
	Uncertainty       Uncertainty
	Classification    string
	Provenance        string
	EffectCount       int
	ResultDigest      string
	ExplanationDigest string
}

// Transform is the engine-owned pure computation. It receives only the
// pinned request and returns outputs with the uncertainty it claims; the
// envelope verifies the claim against the propagated floor.
type Transform func(Request) ([]Input, Uncertainty, error)

// Execute runs one engine transform under the shared contract: pinned
// context, exact uncertainty/taint propagation, canonical digests and zero
// effects. The explanation argument is the engine's human-readable account
// of how the outputs derive from the inputs; it is digested, never trusted.
func Execute(req Request, fn Transform, explanation string) (Result, error) {
	if err := req.Context.validate(); err != nil {
		return Result{}, err
	}
	if req.InputsDigest == "" || req.InputsDigest != inputsDigest(req.Inputs) {
		return Result{}, fmt.Errorf("%w: request inputs digest does not match pinned inputs", ErrContextIncomplete)
	}
	if strings.TrimSpace(explanation) == "" {
		return Result{}, fmt.Errorf("%w: explanation is required", ErrContextIncomplete)
	}
	outputs, claimed, err := fn(req)
	if err != nil {
		return Result{}, err
	}
	if !claimed.Valid() {
		return Result{}, fmt.Errorf("%w: result uncertainty %q is not declared", ErrContextIncomplete, claimed)
	}
	floor := req.CombinedUncertainty()
	if rank(claimed) < rank(floor) {
		return Result{}, fmt.Errorf("%w: inputs are %s but the engine claims %s", ErrUncertaintyDowngrade, floor, claimed)
	}
	for _, out := range outputs {
		if err := out.validate(); err != nil {
			return Result{}, err
		}
		if out.Classification != req.Context.Classification && !carriesInputTaint(req, out) {
			return Result{}, fmt.Errorf("%w: output %q drops the propagated taint", ErrClassificationLeak, out.Name)
		}
	}
	res := Result{
		EngineDefinition: req.Context.EngineDefinition,
		EngineRevision:   req.Context.EngineRevision,
		InputsDigest:     req.InputsDigest,
		Outputs:          append([]Input(nil), outputs...),
		Uncertainty:      claimed,
		Classification:   req.Context.Classification,
		Provenance:       req.Context.Provenance,
	}
	res.ResultDigest = res.computeResultDigest()
	res.ExplanationDigest = computeExplanationDigest(req, res, explanation)
	if res.ResultDigest == "" || res.ExplanationDigest == "" {
		return Result{}, fmt.Errorf("%w: result has no canonical encoding", ErrContextIncomplete)
	}
	return res, nil
}

func rank(u Uncertainty) int {
	switch u {
	case UncertaintyUnknown:
		return 2
	case UncertaintyPartial:
		return 1
	default:
		return 0
	}
}

// carriesInputTaint accepts an output whose classification matches one of
// the request inputs exactly: taint may narrow to a declared input, never
// to an undeclared value.
func carriesInputTaint(req Request, out Input) bool {
	for _, in := range req.Inputs {
		if out.Classification == in.Classification && out.Provenance == in.Provenance {
			return true
		}
	}
	return false
}

func (r Result) computeResultDigest() string {
	ordered := append([]Input(nil), r.Outputs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	w := canonicalbytes.New("hcmnext.engines.envelope.Result", contractVersion).
		String("engine_definition", r.EngineDefinition).
		String("engine_revision", r.EngineRevision).
		String("inputs_digest", r.InputsDigest).
		String("uncertainty", string(r.Uncertainty)).
		String("classification", r.Classification).
		String("provenance", r.Provenance)
	for _, out := range ordered {
		w = w.String("output_name", out.Name).String("output_value", out.Value).String("output_uncertainty", string(out.Uncertainty))
	}
	body, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(body)
}

func computeExplanationDigest(req Request, res Result, explanation string) string {
	w := canonicalbytes.New("hcmnext.engines.envelope.Explanation", contractVersion).
		String("result_digest", res.ResultDigest).
		String("explanation", explanation)
	body, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(body)
}

// VerifyReplay proves deterministic replay: re-executing the identical
// request through the same transform yields identical digests.
func VerifyReplay(req Request, fn Transform, explanation string, first Result) error {
	second, err := Execute(req, fn, explanation)
	if err != nil {
		return err
	}
	if first.ResultDigest != second.ResultDigest || first.ExplanationDigest != second.ExplanationDigest {
		return fmt.Errorf("%w: replay digests diverged", ErrContextIncomplete)
	}
	return nil
}
