// Package compensation owns the authorized, read-only compensation projection.
// It never decides authorization and never reads a denied field.
package compensation

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	ReadIntentType      = "hcmnext.compensation.read"
	ReadIntentVersion   = "v1"
	ReadRulePackVersion = "compensation.read.rules/1.0.0"
)

var (
	ErrRequestInvalid          = errors.New("compensation: read request is invalid")
	ErrUnknownField            = errors.New("compensation: unknown field")
	ErrAuthorizationIncomplete = errors.New("compensation: authorization decision is incomplete")
	ErrReaderFailed            = errors.New("compensation: reader failed")
	ErrSubjectMismatch         = errors.New("compensation: reader answered for another subject")
	ErrUnauthorizedFact        = errors.New("compensation: reader returned an unauthorized field")
	ErrFactIncomplete          = errors.New("compensation: fact is incomplete")
)

type FieldID string

// The closed field vocabulary deliberately excludes payroll account, bank,
// tax and other operational secrets. Exact decimal values are strings.
const (
	FieldPackageID         FieldID = "package.id"
	FieldComponentID       FieldID = "component.id"
	FieldComponentType     FieldID = "component.type"
	FieldAmount            FieldID = "component.amount"
	FieldCurrency          FieldID = "component.currency"
	FieldPayBasis          FieldID = "component.pay_basis"
	FieldFrequency         FieldID = "component.frequency"
	FieldEffectiveInterval FieldID = "component.effective_interval"
	FieldKnownAt           FieldID = "component.known_at"
	FieldAuthority         FieldID = "component.authority"
	FieldSource            FieldID = "component.source"
)

var knownFields = map[FieldID]struct{}{FieldPackageID: {}, FieldComponentID: {}, FieldComponentType: {}, FieldAmount: {}, FieldCurrency: {}, FieldPayBasis: {}, FieldFrequency: {}, FieldEffectiveInterval: {}, FieldKnownAt: {}, FieldAuthority: {}, FieldSource: {}}

func (f FieldID) Validate() error {
	if _, ok := knownFields[f]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownField, f)
	}
	return nil
}
func (f FieldID) String() string { return string(f) }
func AllFields() []FieldID {
	out := make([]FieldID, 0, len(knownFields))
	for f := range knownFields {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

type Effect uint8

const (
	EffectUnspecified Effect = iota
	EffectAllow
	EffectDeny
)

func (e Effect) Valid() bool { return e == EffectAllow || e == EffectDeny }
func (e Effect) String() string {
	if e == EffectAllow {
		return "ALLOW"
	}
	if e == EffectDeny {
		return "DENY"
	}
	return "EFFECT_UNSPECIFIED"
}

type FieldRuling struct {
	Effect Effect
	Reason string
}

func (r FieldRuling) Validate() error {
	if !r.Effect.Valid() {
		return fmt.Errorf("%w: invalid effect", ErrRequestInvalid)
	}
	if r.Effect == EffectDeny && r.Reason == "" {
		return fmt.Errorf("%w: denial reason required", ErrRequestInvalid)
	}
	return nil
}

type AuthorizationDecision struct {
	PolicyVersion       string
	Purpose             string
	SubjectDisclosable  bool
	SubjectDenialReason string
	Fields              map[FieldID]FieldRuling
}

func (d AuthorizationDecision) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	fs := make([]FieldID, 0, len(d.Fields))
	for f := range d.Fields {
		fs = append(fs, f)
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i] < fs[j] })
	w := canonicalbytes.New("hcmnext.domains.compensation.AuthorizationDecision", 1).String("policy", d.PolicyVersion).String("purpose", d.Purpose).Bool("subject", d.SubjectDisclosable).String("denial", d.SubjectDenialReason).Count("fields", len(fs))
	for _, f := range fs {
		w.String("field", string(f)).String("effect", d.Fields[f].Effect.String()).String("reason", d.Fields[f].Reason)
	}
	b, _ := w.Bytes()
	return b
}

func (d AuthorizationDecision) Validate() error {
	if d.PolicyVersion == "" || d.Purpose == "" {
		return fmt.Errorf("%w: policy and purpose required", ErrRequestInvalid)
	}
	if !d.SubjectDisclosable && d.SubjectDenialReason == "" {
		return fmt.Errorf("%w: subject denial reason required", ErrRequestInvalid)
	}
	for f, r := range d.Fields {
		if f.Validate() != nil {
			return f.Validate()
		}
		if err := r.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (d AuthorizationDecision) Covers(fs []FieldID) error {
	for _, f := range fs {
		if _, ok := d.Fields[f]; !ok {
			return fmt.Errorf("%w: %s", ErrAuthorizationIncomplete, f)
		}
	}
	return nil
}

type Fact struct {
	Field       FieldID
	Value       values.Presence[string]
	Effective   values.EffectiveInterval
	KnownAt     values.KnownAt
	Revision    values.RevisionToken
	Authority   evidence.SourceAuthority
	Provenance  evidence.Provenance
	Observation bool
}

func (f Fact) Validate() error {
	if err := f.Field.Validate(); err != nil {
		return err
	}
	if err := f.Value.Validate(); err != nil {
		return err
	}
	if err := f.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrFactIncomplete, err)
	}
	if f.KnownAt.Canonical() == nil || !f.Revision.IsSpecified() {
		return ErrFactIncomplete
	}
	if err := f.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: authority: %v", ErrFactIncomplete, err)
	}
	if err := f.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: provenance: %v", ErrFactIncomplete, err)
	}
	return nil
}

type FactSet struct {
	Worker    values.EntityRef
	Exists    bool
	Facts     []Fact
	Watermark values.RevisionToken
}

func (s FactSet) Validate() error {
	if err := s.Worker.Validate(); err != nil {
		return err
	}
	if !s.Exists {
		return nil
	}
	if !s.Watermark.IsSpecified() {
		return ErrFactIncomplete
	}
	seen := map[FieldID]bool{}
	for _, f := range s.Facts {
		if err := f.Validate(); err != nil {
			return err
		}
		if seen[f.Field] {
			return fmt.Errorf("%w: duplicate", ErrFactIncomplete)
		}
		seen[f.Field] = true
	}
	return nil
}

type Query struct {
	Tenant values.TenantId
	Worker values.EntityRef
	AsOf   people.AsOf
	Fields []FieldID
}
type Reader interface {
	CompensationAt(context.Context, Query) (FactSet, error)
}
type Request struct {
	Tenant        values.TenantId
	Worker        values.EntityRef
	AsOf          people.AsOf
	Fields        []FieldID
	Authorization AuthorizationDecision
}
type DisclosedFact struct {
	Field        FieldID
	Access       Effect
	DenialReason string
	Value        values.Presence[string]
	Effective    values.EffectiveInterval
	KnownAt      values.KnownAt
	Revision     values.RevisionToken
	Authority    evidence.SourceAuthority
	Provenance   evidence.Provenance
}
type Result struct {
	IntentType      string
	IntentVersion   string
	Worker          values.EntityRef
	Disclosure      string
	Presence        string
	WithheldReason  string
	Fields          []DisclosedFact
	Watermark       values.RevisionToken
	PolicyVersion   string
	RulePackVersion string
	InputsDigest    string
	ResultDigest    string
	Effects         evidence.EffectCounters
	Receipt         evidence.ZeroEffectReceipt
}

func (r Request) inputsDigest() string {
	w := canonicalbytes.New("hcmnext.domains.compensation.ReadRequest", 1).String("intent", ReadIntentType).String("version", ReadIntentVersion).String("tenant", string(r.Tenant)).Value("worker", r.Worker).Value("as_of", r.AsOf).Count("fields", len(r.projection()))
	for _, f := range r.projection() {
		w.String("field", string(f))
	}
	w.Field("authorization", r.Authorization.Canonical())
	d, _ := w.Digest()
	return d
}

func (r Result) resultDigest() string {
	w := canonicalbytes.New("hcmnext.domains.compensation.ReadResult", 1).String("intent", r.IntentType).String("version", r.IntentVersion).Value("worker", r.Worker).String("disclosure", r.Disclosure).String("presence", r.Presence).String("withheld", r.WithheldReason).Count("fields", len(r.Fields))
	for _, f := range r.Fields {
		encoded, _ := values.MarshalPresence(f.Value, values.StringCodec{})
		w.String("field", string(f.Field)).String("access", f.Access.String()).String("reason", f.DenialReason).Field("value", encoded)
	}
	w.Value("effects", r.Effects)
	d, _ := w.Digest()
	return d
}

func finish(r Result, input string) (Result, error) {
	r.InputsDigest = input
	r.ResultDigest = r.resultDigest()
	receipt, err := evidence.NewZeroEffectReceipt(r.IntentType, r.IntentVersion, evidence.ModeSimulate, evidence.RequestStateSimulated, []evidence.ControlVersion{{Name: "authorization_policy", Version: r.PolicyVersion}, {Name: "read_rule_pack", Version: r.RulePackVersion}}, r.InputsDigest, r.ResultDigest, r.Effects)
	if err != nil {
		return Result{}, err
	}
	r.Receipt = receipt
	return r, nil
}

func (r Request) projection() []FieldID {
	fs := r.Fields
	if len(fs) == 0 {
		fs = AllFields()
	}
	out := append([]FieldID(nil), fs...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func (r Request) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return err
	}
	if err := r.Worker.Validate(); err != nil {
		return err
	}
	if r.Worker.Tenant != r.Tenant {
		return fmt.Errorf("%w: worker outside tenant", ErrRequestInvalid)
	}
	if err := r.AsOf.Validate(); err != nil {
		return err
	}
	seen := map[FieldID]bool{}
	for _, f := range r.Fields {
		if err := f.Validate(); err != nil {
			return err
		}
		if seen[f] {
			return fmt.Errorf("%w: duplicate field", ErrRequestInvalid)
		}
		seen[f] = true
	}
	return r.Authorization.Validate()
}

func Read(ctx context.Context, reader Reader, req Request) (Result, error) {
	if reader == nil {
		return Result{}, ErrRequestInvalid
	}
	if err := req.Validate(); err != nil {
		return Result{}, err
	}
	inputDigest := req.inputsDigest()
	fs := req.projection()
	if err := req.Authorization.Covers(fs); err != nil {
		return Result{}, err
	}
	if !req.Authorization.SubjectDisclosable {
		return finish(Result{IntentType: ReadIntentType, IntentVersion: ReadIntentVersion, Worker: req.Worker, Disclosure: "WITHHELD", WithheldReason: req.Authorization.SubjectDenialReason, PolicyVersion: req.Authorization.PolicyVersion, RulePackVersion: ReadRulePackVersion, Effects: evidence.ZeroEffects()}, inputDigest)
	}
	allowed := []FieldID{}
	for _, f := range fs {
		if req.Authorization.Fields[f].Effect == EffectAllow {
			allowed = append(allowed, f)
		}
	}
	set, err := reader.CompensationAt(ctx, Query{Tenant: req.Tenant, Worker: req.Worker, AsOf: req.AsOf, Fields: allowed})
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrReaderFailed, err)
	}
	if err := set.Validate(); err != nil {
		return Result{}, err
	}
	if set.Worker != req.Worker {
		return Result{}, ErrSubjectMismatch
	}
	if !set.Exists {
		// Keep an absent worker compensation record distinct from an existing
		// record whose requested fields happen to be unknown. In particular,
		// don't manufacture per-field facts or a watermark for an empty read.
		return finish(Result{IntentType: ReadIntentType, IntentVersion: ReadIntentVersion, Worker: req.Worker, Disclosure: "FULL", Presence: "ABSENT", PolicyVersion: req.Authorization.PolicyVersion, RulePackVersion: ReadRulePackVersion, Effects: evidence.ZeroEffects()}, inputDigest)
	}
	by := map[FieldID]Fact{}
	for _, f := range set.Facts {
		if req.Authorization.Fields[f.Field].Effect != EffectAllow {
			return Result{}, fmt.Errorf("%w: %s", ErrUnauthorizedFact, f.Field)
		}
		by[f.Field] = f
	}
	out := Result{IntentType: ReadIntentType, IntentVersion: ReadIntentVersion, Worker: req.Worker, Disclosure: "FULL", Presence: "PRESENT", PolicyVersion: req.Authorization.PolicyVersion, RulePackVersion: ReadRulePackVersion, Watermark: set.Watermark, Effects: evidence.ZeroEffects()}
	for _, field := range fs {
		ruling := req.Authorization.Fields[field]
		if ruling.Effect == EffectDeny {
			out.Disclosure = "PARTIAL"
			out.Fields = append(out.Fields, DisclosedFact{Field: field, Access: EffectDeny, DenialReason: ruling.Reason, Value: values.Redacted[string](ruling.Reason)})
			continue
		}
		f, ok := by[field]
		if !ok {
			out.Fields = append(out.Fields, DisclosedFact{Field: field, Access: EffectAllow, Value: values.Unknown[string]("not_asserted_at_requested_coordinate")})
			continue
		}
		if f.Observation {
			out.Fields = append(out.Fields, DisclosedFact{Field: field, Access: EffectAllow, Value: f.Value, Effective: f.Effective, KnownAt: f.KnownAt, Revision: f.Revision, Authority: f.Authority, Provenance: f.Provenance})
			continue
		}
		out.Fields = append(out.Fields, DisclosedFact{Field: field, Access: EffectAllow, Value: f.Value, Effective: f.Effective, KnownAt: f.KnownAt, Revision: f.Revision, Authority: f.Authority, Provenance: f.Provenance})
	}
	return finish(out, inputDigest)
}
