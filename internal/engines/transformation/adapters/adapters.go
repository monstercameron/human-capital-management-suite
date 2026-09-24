// Package adapters lowers the mapping form of each site that currently owns
// its own field-mapping runtime -- a workflow TRANSFORM step, a connectivity
// mapping profile (and the connectivity mapping IR underneath it), and a
// DataOps import mapping -- onto the shared transformation engine
// (XFORM-008).
//
// # Direction of dependency
//
// This package deliberately does NOT import internal/workflow,
// internal/connectivity/mapping or internal/domains/dataops. An engine that
// imported a domain would break ARCH-GO-008 outright, and an engine that
// imported workflow would add an "engines -> workflow" edge that the
// architecture's committed layer graph does not contain. So each site's
// mapping form is mirrored here as a plain, adapters-owned struct, and the
// site (or a migration proof written as a test) adapts its own values into
// it. The mirrors are field-for-field restatements of the site types; the
// per-site files name every mirrored field and every field that is
// deliberately not lowered.
//
// # What "lowering" means here
//
// A lowering never hand-builds an [ir.Program]. It builds a
// [transformation.TransformationDefinition] -- the XFORM-001 contract, with
// its typed schemas, closed operation vocabulary, declared resource limits,
// declared failure policy and its refusal of ambient dependencies -- and
// hands it to [ir.Compile]. Everything the shared engine already refuses is
// therefore refused on the way in, and the resulting Program carries the
// engine's own canonical digest.
//
// # Nothing is dropped silently
//
// A site feature with no IR equivalent is a typed [Refusal] naming the site,
// the feature and the exact target it applies to; the closed vocabulary of
// those feature names is [RefusableFeatures]. A feature that IS lowered but
// whose lowering is not literally the same construct is recorded on the
// result: a [Carrier] when a site type rides an IR type that carries its
// canonical text rather than its full semantics, and a [Divergence] when the
// lowered program's observable behaviour differs from the site's current
// behaviour on some input vector. Both are rendered by [Lowered.Explain] and
// both are covered by [Lowered.Digest], so neither can change unnoticed.
package adapters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/exec"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ContractVersion is this package's own contract version (ARCH-GO-009),
// independent of the IR version or of any site's own versioning.
const ContractVersion = 1

// DigestAlgorithm names the hash used by [Lowered.Digest].
const DigestAlgorithm = "sha256"

// Version reports this package's contract version.
func Version() int { return ContractVersion }

// Site names one migrating mapping site. A site is one mapping *form*, not
// one Go package: connectivity owns two forms (a versioned profile that
// references IR digests, and the executable rule IR underneath it), and each
// gets its own site identity because their operation sets differ.
type Site string

// The declared sites.
const (
	// SiteWorkflowTransform is a workflow TRANSFORM step's input mapping:
	// the node's declared input fields plus the mappings that bind them.
	SiteWorkflowTransform Site = "workflow.transform_step"
	// SiteConnectivityProfile is a connectivity MappingProfileVersion: a
	// versioned source-to-target field profile that names, per field, either
	// the identity transformation or a transformation IR digest.
	SiteConnectivityProfile Site = "connectivity.mapping_profile"
	// SiteConnectivityRules is the connectivity mapping IR: the rule list
	// the connectivity package executes with its own interpreter today.
	SiteConnectivityRules Site = "connectivity.mapping_ir"
	// SiteDataOpsImport is a DataOps import mapping: source columns bound to
	// canonical properties through the closed import transform set.
	SiteDataOpsImport Site = "dataops.import_mapping"
)

// Sites returns every declared site, in a fixed order.
func Sites() []Site {
	return []Site{SiteWorkflowTransform, SiteConnectivityProfile, SiteConnectivityRules, SiteDataOpsImport}
}

// ErrNoIREquivalent is the sentinel every [Refusal] unwraps to.
var ErrNoIREquivalent = errors.New("XFORM_008_NO_IR_EQUIVALENT")

// RefusalCode is the stable code every refusal carries.
const RefusalCode = "XFORM_008_REFUSED"

// Refusal is the typed refusal returned when a site's mapping form uses a
// feature the shared IR has no equivalent for. It always names the feature
// and the exact target it applies to, so a refusal is actionable without
// re-reading the mapping.
type Refusal struct {
	Code    string `json:"code"`
	Site    Site   `json:"site"`
	Feature string `json:"feature"`
	Target  string `json:"target,omitempty"`
	Reason  string `json:"reason"`
}

func (r Refusal) Error() string {
	if r.Target == "" {
		return fmt.Sprintf("%s site=%s feature=%s: %s", r.Code, r.Site, r.Feature, r.Reason)
	}
	return fmt.Sprintf("%s site=%s feature=%s target=%q: %s", r.Code, r.Site, r.Feature, r.Target, r.Reason)
}

// Unwrap makes every refusal matchable with errors.Is(err, ErrNoIREquivalent).
func (r Refusal) Unwrap() error { return ErrNoIREquivalent }

func refuse(site Site, feature, target, format string, args ...any) error {
	return Refusal{Code: RefusalCode, Site: site, Feature: feature, Target: target, Reason: fmt.Sprintf(format, args...)}
}

// The closed vocabulary of refusable feature names. Every refusal this
// package can produce names one of these, and [RefusableFeatures] lists them
// all, so a new silent drop cannot be introduced without extending the list.
const (
	// FeatureAmbientSource is a mapping source resolved from state outside
	// the mapping's own typed inputs (a workflow context read). The
	// transformation contract forbids ambient dependencies outright.
	FeatureAmbientSource = "ambient_source"
	// FeatureCompositeValueType is a LIST or MESSAGE typed field: the IR's
	// type vocabulary is scalar-only.
	FeatureCompositeValueType = "composite_value_type"
	// FeatureUnrepresentableName is a schema, field or mapping name outside
	// the transformation contract's name vocabulary.
	FeatureUnrepresentableName = "unrepresentable_name"
	// FeatureStringNormalization is a trim / upper-case / lower-case
	// transform. The IR function vocabulary has no string normalizer.
	FeatureStringNormalization = "string_normalization"
	// FeatureCrosswalkLookup is a value lookup through a crosswalk table.
	// The IR instruction set has no lookup instruction.
	FeatureCrosswalkLookup = "crosswalk_lookup"
	// FeatureLayoutDateParse is a date parse under a layout other than
	// RFC 3339. The IR's coercion to timestamp parses RFC 3339 only; it
	// takes no layout parameter.
	FeatureLayoutDateParse = "layout_date_parse"
	// FeatureMoneyParse is a money parse: currency-token stripping, group
	// separators and a declared currency. The IR's decimal coercion accepts
	// plain signed decimal text and nothing else.
	FeatureMoneyParse = "money_parse"
	// FeatureTemplateCompose is a template-style value composition
	// (substituting the source value into a literal template).
	FeatureTemplateCompose = "template_compose"
	// FeatureNullPolicy is a per-rule null policy that changes the shape of
	// the output (omitting a target, or emitting a delete marker). The IR
	// always produces one property per instruction.
	FeatureNullPolicy = "null_policy"
	// FeatureEmptyLiteral is a constant mapping with an empty literal. The
	// transformation contract requires a default operation's literal to be
	// non-empty, so an empty constant has no representation.
	FeatureEmptyLiteral = "empty_literal"
	// FeaturePinnedLookup is a workflow transform's pinned reference-data
	// lookup. The IR reads only its declared typed inputs.
	FeaturePinnedLookup = "pinned_lookup"
	// FeatureNondeterministicTransform is a transform declaring that it
	// reads the clock, randomness or the network, or carrying inline code.
	FeatureNondeterministicTransform = "nondeterministic_transform"
	// FeatureDuplicateTarget is two mappings writing one target: the IR
	// refuses an ambiguously ordered write.
	FeatureDuplicateTarget = "duplicate_target"
	// FeatureInvalidMapping is a mapping form the site's own compiler would
	// itself reject (no source, no target, no fields, an unknown operation
	// kind). It is a refusal rather than a lowering because a mapping that
	// does not compile at its own site has nothing to lower.
	FeatureInvalidMapping = "invalid_mapping"
)

// RefusableFeatures returns the closed vocabulary of feature names a
// [Refusal] can carry, sorted.
func RefusableFeatures() []string {
	out := []string{
		FeatureAmbientSource, FeatureCompositeValueType, FeatureUnrepresentableName,
		FeatureStringNormalization, FeatureCrosswalkLookup, FeatureLayoutDateParse,
		FeatureMoneyParse, FeatureTemplateCompose, FeatureNullPolicy, FeatureEmptyLiteral,
		FeaturePinnedLookup, FeatureNondeterministicTransform, FeatureDuplicateTarget,
		FeatureInvalidMapping,
	}
	sort.Strings(out)
	return out
}

// Carrier records that a site type was lowered onto an IR type that carries
// its canonical text rather than its full semantics -- for example a
// workflow MONEY value riding IR string as "180000.00 USD". The lowered
// program's bytes are the site's bytes; what the carrier does not give is the
// arithmetic. A carrier is declared, digested and explained precisely so
// "this rode a string" is never an invisible decision.
type Carrier struct {
	Target   string              `json:"target"`
	SiteType string              `json:"site_type"`
	IRType   transformation.Type `json:"ir_type"`
	Loses    string              `json:"loses"`
}

// Divergence records an input vector on which the lowered program's
// observable behaviour differs from the site's current behaviour. It is a
// finding, not a patch: XFORM-008 forbids changing the site, so the
// difference is declared here, digested, and pinned by the golden test.
type Divergence struct {
	Feature string `json:"feature"`
	Vector  string `json:"vector"`
	Site    string `json:"site_behaviour"`
	Lowered string `json:"lowered_behaviour"`
}

// Delegation records a field whose value the site's mapping form does not
// itself compute: it names a transformation IR program by digest and defers
// to it. The lowered profile carries no instruction for that field; the
// delegation is what says so out loud.
type Delegation struct {
	Target        string `json:"target"`
	Source        string `json:"source"`
	ProgramDigest string `json:"program_digest"`
}

// Binding is one lowered field: the record key the lowered program reads and
// the record key it writes, both spelled as exec spells them
// ("Schema.Field").
type Binding struct {
	Target    string              `json:"target"`
	SourceKey string              `json:"source_key,omitempty"`
	TargetKey string              `json:"target_key"`
	Type      transformation.Type `json:"type"`
}

// LimitsMapping is the declared translation from a site's own resource
// bounds to the shared engine's two limit surfaces: the definition limits
// [ir.Compile] turns into the program's own bounds, and the execution limits
// [exec.Execute] enforces across a dataset. Notes says, per limit, exactly
// where the number came from, so no bound is silently invented.
type LimitsMapping struct {
	Definition transformation.ResourceLimits `json:"definition"`
	Execution  execLimits                    `json:"execution"`
	Notes      []string                      `json:"notes"`
}

// execLimits mirrors exec.Limits with JSON tags so a LimitsMapping
// canonicalizes deterministically. exec.Limits itself carries no tags.
type execLimits struct {
	MaxRows        int   `json:"max_rows"`
	MaxSteps       int   `json:"max_steps"`
	MaxOutputBytes int64 `json:"max_output_bytes"`
}

// Limits returns the execution limits in the shape exec.Execute takes.
func (l execLimits) Limits() exec.Limits {
	return exec.Limits{MaxRows: l.MaxRows, MaxSteps: l.MaxSteps, MaxOutputBytes: l.MaxOutputBytes}
}

// Lowered is one site mapping form expressed as a shared-engine program,
// with everything the lowering decided recorded alongside it.
type Lowered struct {
	Site          Site          `json:"site"`
	Name          string        `json:"name"`
	Program       ir.Program    `json:"program"`
	ProgramDigest string        `json:"program_digest"`
	Limits        LimitsMapping `json:"limits"`
	Bindings      []Binding     `json:"bindings"`
	Carriers      []Carrier     `json:"carriers,omitempty"`
	Divergences   []Divergence  `json:"divergences,omitempty"`
	Delegations   []Delegation  `json:"delegations,omitempty"`
}

// Digest is the canonical, content-addressed identity of the whole lowered
// artifact: the program's own digest plus the limits mapping, bindings,
// carriers, divergences and delegations. Two lowerings that agree on every
// one of those hash the same; changing any of them changes the digest.
func (l Lowered) Digest() (string, error) {
	c := l
	c.Program = ir.Program{}
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return DigestAlgorithm + ":" + hex.EncodeToString(h[:]), nil
}

// Explain renders the lowering as a readable plan: audit-log and
// review-screen shaped, not for programmatic branching (branch on the typed
// Refusal instead).
func (l Lowered) Explain() string {
	var b strings.Builder
	digest, err := l.Digest()
	if err != nil {
		digest = "unavailable: " + err.Error()
	}
	fmt.Fprintf(&b, "lowered %s mapping %q (adapters v%d)", l.Site, l.Name, ContractVersion)
	fmt.Fprintf(&b, "\n  lowered digest: %s", digest)
	fmt.Fprintf(&b, "\n  program digest: %s", l.ProgramDigest)
	fmt.Fprintf(&b, "\n  limits: max_operations=%d max_input_bytes=%d max_output_bytes=%d max_expansion=%d;"+
		" exec max_rows=%d max_steps=%d max_output_bytes=%d",
		l.Limits.Definition.MaxOperations, l.Limits.Definition.MaxInputBytes,
		l.Limits.Definition.MaxOutputBytes, l.Limits.Definition.MaxExpansion,
		l.Limits.Execution.MaxRows, l.Limits.Execution.MaxSteps, l.Limits.Execution.MaxOutputBytes)
	for _, note := range l.Limits.Notes {
		fmt.Fprintf(&b, "\n    limit source: %s", note)
	}
	fmt.Fprintf(&b, "\n  bindings (%d):", len(l.Bindings))
	for _, bind := range l.Bindings {
		if bind.SourceKey == "" {
			fmt.Fprintf(&b, "\n    %s <- (literal) as %s:%s", bind.Target, bind.TargetKey, bind.Type)
			continue
		}
		fmt.Fprintf(&b, "\n    %s <- %s as %s:%s", bind.Target, bind.SourceKey, bind.TargetKey, bind.Type)
	}
	for _, c := range l.Carriers {
		fmt.Fprintf(&b, "\n  carrier: %s rides %s as %s (loses: %s)", c.Target, c.IRType, c.SiteType, c.Loses)
	}
	for _, d := range l.Delegations {
		fmt.Fprintf(&b, "\n  delegation: %s <- %s via program %s (no instruction lowered here)", d.Target, d.Source, d.ProgramDigest)
	}
	for _, d := range l.Divergences {
		fmt.Fprintf(&b, "\n  divergence [%s] on %s: site %s; lowered %s", d.Feature, d.Vector, d.Site, d.Lowered)
	}
	b.WriteString("\n  ")
	b.WriteString(strings.ReplaceAll(l.Program.Explain(), "\n", "\n  "))
	return b.String()
}

// SiteOperation is one operation a site's mapping form can express, together
// with the IR instruction (and function, where that instruction takes one)
// it lowers to. The union over every site is the migration's whole claim:
// every operation any site supports is one of the IR's instructions.
type SiteOperation struct {
	Site       Site        `json:"site"`
	Name       string      `json:"name"`
	IROp       ir.OpCode   `json:"ir_op"`
	IRFunction ir.Function `json:"ir_function,omitempty"`
	Via        string      `json:"via"`
}

// SupportedOperations returns every site operation this package lowers, in a
// fixed order. An operation a site supports but this package refuses is
// deliberately absent: it is a [Refusal] feature, not a supported operation.
func SupportedOperations() []SiteOperation {
	out := []SiteOperation{
		{SiteWorkflowTransform, "workflow_input_source", ir.OpProject, "", "transformation.OpCopy"},
		{SiteWorkflowTransform, "node_output_source", ir.OpProject, "", "transformation.OpCopy"},
		{SiteWorkflowTransform, "constant_source", ir.OpMap, ir.FuncDefault, "transformation.OpDefault"},
		{SiteConnectivityProfile, "identity", ir.OpProject, "", "transformation.OpCopy"},
		{SiteConnectivityRules, "IDENTITY", ir.OpProject, "", "transformation.OpCopy"},
		{SiteConnectivityRules, "CONSTANT", ir.OpMap, ir.FuncDefault, "transformation.OpDefault"},
		{SiteConnectivityRules, "TRIM", ir.OpMap, ir.FuncTrim, "transformation.OpTransform"},
		{SiteConnectivityRules, "UPPER", ir.OpMap, ir.FuncUpper, "transformation.OpTransform"},
		{SiteConnectivityRules, "LOWER", ir.OpMap, ir.FuncLower, "transformation.OpTransform"},
		{SiteConnectivityRules, "LOOKUP", ir.OpMap, ir.FuncLookup, "transformation.OpTransform"},
		{SiteConnectivityRules, "DATE(layout)", ir.OpMap, ir.FuncDateParse, "transformation.OpTransform"},
		{SiteConnectivityRules, "MONEY(currency)", ir.OpMap, ir.FuncMoneyParse, "transformation.OpTransform"},
		{SiteConnectivityRules, "COMPOSE", ir.OpMap, ir.FuncCompose, "transformation.OpTransform"},
		{SiteDataOpsImport, "IDENTITY", ir.OpProject, "", "transformation.OpCopy"},
		{SiteDataOpsImport, "CONSTANT", ir.OpMap, ir.FuncDefault, "transformation.OpDefault"},
		{SiteDataOpsImport, "TRIM", ir.OpMap, ir.FuncTrim, "transformation.OpTransform"},
		{SiteDataOpsImport, "CASE_UPPER", ir.OpMap, ir.FuncUpper, "transformation.OpTransform"},
		{SiteDataOpsImport, "CASE_LOWER", ir.OpMap, ir.FuncLower, "transformation.OpTransform"},
		{SiteDataOpsImport, "LOOKUP", ir.OpMap, ir.FuncLookup, "transformation.OpTransform"},
		{SiteDataOpsImport, "DATE_PARSE(layout)", ir.OpMap, ir.FuncDateParse, "transformation.OpTransform"},
		{SiteDataOpsImport, "MONEY_PARSE(currency)", ir.OpMap, ir.FuncMoneyParse, "transformation.OpTransform"},
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Site != out[j].Site {
			return out[i].Site < out[j].Site
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// nameRE mirrors the transformation contract's own name vocabulary. It is
// restated (not imported: the contract keeps it unexported) so a lowering can
// refuse an unrepresentable name with a named feature instead of letting
// transformation.Validate reject it with a generic diagnostic.
var nameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:/-]*$`)

// ValidName reports whether s is spellable as a transformation schema,
// field, or definition name.
func ValidName(s string) bool { return nameRE.MatchString(s) }

// builder accumulates one lowering and finishes it through the real XFORM-001
// contract and the real XFORM-002 compiler. It never hand-builds a Program.
type builder struct {
	site         Site
	name         string
	owner        string
	phase        string
	sourceSchema string
	destSchema   string
	sourceFields map[string]transformation.Type
	destFields   []transformation.Field
	operations   []transformation.Operation
	bindings     []Binding
	carriers     []Carrier
	divergences  []Divergence
	delegations  []Delegation
	limits       LimitsMapping
	maxSources   int
}

func newBuilder(site Site, name, sourceSchema, destSchema string) (*builder, error) {
	for label, candidate := range map[string]string{"name": name, "source schema": sourceSchema, "destination schema": destSchema} {
		if !ValidName(candidate) {
			return nil, refuse(site, FeatureUnrepresentableName, candidate,
				"%s %q is outside the transformation contract's name vocabulary", label, candidate)
		}
	}
	return &builder{
		site: site, name: name, owner: string(site), phase: "MAP",
		sourceSchema: sourceSchema, destSchema: destSchema,
		sourceFields: map[string]transformation.Type{},
		maxSources:   1,
	}, nil
}

// source declares (idempotently) one source field and returns its typed path.
func (b *builder) source(field string, typ transformation.Type) (transformation.Path, error) {
	if !ValidName(field) {
		return transformation.Path{}, refuse(b.site, FeatureUnrepresentableName, field,
			"source field %q is outside the transformation contract's name vocabulary", field)
	}
	if existing, ok := b.sourceFields[field]; ok && existing != typ {
		return transformation.Path{}, refuse(b.site, FeatureInvalidMapping, field,
			"source field %q is read as both %s and %s", field, existing, typ)
	}
	b.sourceFields[field] = typ
	return transformation.Path{Schema: b.sourceSchema, Field: field, Type: typ}, nil
}

func (b *builder) destination(field string, typ transformation.Type) (transformation.Path, error) {
	if !ValidName(field) {
		return transformation.Path{}, refuse(b.site, FeatureUnrepresentableName, field,
			"target field %q is outside the transformation contract's name vocabulary", field)
	}
	for _, f := range b.destFields {
		if f.Name == field {
			return transformation.Path{}, refuse(b.site, FeatureDuplicateTarget, field,
				"target field %q is written by more than one mapping", field)
		}
	}
	b.destFields = append(b.destFields, transformation.Field{Name: field, Type: typ})
	return transformation.Path{Schema: b.destSchema, Field: field, Type: typ}, nil
}

// project lowers "copy this source value to this target unchanged".
func (b *builder) project(target, sourceField string, typ transformation.Type) error {
	src, err := b.source(sourceField, typ)
	if err != nil {
		return err
	}
	dst, err := b.destination(target, typ)
	if err != nil {
		return err
	}
	b.operations = append(b.operations, transformation.Operation{Kind: transformation.OpCopy, Source: &src, Destination: dst})
	b.bindings = append(b.bindings, Binding{Target: target, SourceKey: key(src), TargetKey: key(dst), Type: typ})
	return nil
}

// convert lowers "coerce this source value to the target's declared type".
func (b *builder) convert(target, sourceField string, from, to transformation.Type) error {
	src, err := b.source(sourceField, from)
	if err != nil {
		return err
	}
	dst, err := b.destination(target, to)
	if err != nil {
		return err
	}
	b.operations = append(b.operations, transformation.Operation{
		Kind: transformation.OpConvert, Source: &src, Destination: dst, TargetType: to,
	})
	b.bindings = append(b.bindings, Binding{Target: target, SourceKey: key(src), TargetKey: key(dst), Type: to})
	return nil
}

// transform lowers one closed, source-based engine function. The site adapter
// supplies the named operation and its declarative arguments; execution still
// runs in transformation/exec.
func (b *builder) transform(target, sourceField, function, argument string, lookup map[string]string) error {
	src, err := b.source(sourceField, transformation.TypeString)
	if err != nil {
		return err
	}
	dst, err := b.destination(target, transformation.TypeString)
	if err != nil {
		return err
	}
	copyLookup := make(map[string]string, len(lookup))
	for k, v := range lookup {
		copyLookup[k] = v
	}
	b.operations = append(b.operations, transformation.Operation{Kind: transformation.OpTransform, Source: &src, Destination: dst, Function: function, Argument: argument, Lookup: copyLookup})
	b.bindings = append(b.bindings, Binding{Target: target, SourceKey: key(src), TargetKey: key(dst), Type: transformation.TypeString})
	return nil
}

// literal lowers "this target always holds this fixed value".
func (b *builder) literal(target, value string, typ transformation.Type) error {
	if value == "" {
		return refuse(b.site, FeatureEmptyLiteral, target,
			"target %q declares a constant with an empty value; the transformation contract requires a non-empty literal", target)
	}
	dst, err := b.destination(target, typ)
	if err != nil {
		return err
	}
	b.operations = append(b.operations, transformation.Operation{Kind: transformation.OpDefault, Destination: dst, Literal: value})
	b.bindings = append(b.bindings, Binding{Target: target, TargetKey: key(dst), Type: typ})
	return nil
}

func (b *builder) carrier(c Carrier)         { b.carriers = append(b.carriers, c) }
func (b *builder) diverge(d Divergence)      { b.divergences = append(b.divergences, d) }
func (b *builder) delegate(d Delegation)     { b.delegations = append(b.delegations, d) }
func (b *builder) setLimits(l LimitsMapping) { b.limits = l }

func key(p transformation.Path) string { return p.Schema + "." + p.Field }

// finish builds the XFORM-001 definition, compiles it with the XFORM-002
// compiler, and assembles the Lowered result.
func (b *builder) finish() (Lowered, error) {
	if len(b.operations) == 0 && len(b.delegations) == 0 {
		return Lowered{}, refuse(b.site, FeatureInvalidMapping, b.name, "mapping lowers to no operations")
	}
	if len(b.operations) == 0 {
		// Every field of this mapping is delegated to a program identified
		// only by digest. There is nothing for this package to compile, and
		// inventing a filler instruction would misreport what runs.
		return Lowered{}, refuse(b.site, FeatureInvalidMapping, b.name,
			"every field delegates to a transformation IR digest; this mapping form compiles no instruction of its own")
	}

	// Canonicalize the operation order before the definition is built.
	// ir.Compile canonicalizes the compiled instructions, but the definition
	// digest it embeds in the Program comes from
	// transformation.CanonicalBytes, which sorts schema fields and NOT
	// operations -- so two definitions differing only in declared operation
	// order still compile to different Program digests. Sorting here (by
	// destination field, which every lowering keeps unique) makes a lowering
	// independent of the order the site happened to declare its mappings in.
	sort.SliceStable(b.operations, func(i, j int) bool {
		return b.operations[i].Destination.Field < b.operations[j].Destination.Field
	})
	sort.SliceStable(b.destFields, func(i, j int) bool { return b.destFields[i].Name < b.destFields[j].Name })

	sourceNames := make([]string, 0, len(b.sourceFields))
	for name := range b.sourceFields {
		sourceNames = append(sourceNames, name)
	}
	sort.Strings(sourceNames)
	sourceFields := make([]transformation.Field, 0, len(sourceNames))
	for _, name := range sourceNames {
		sourceFields = append(sourceFields, transformation.Field{Name: name, Type: b.sourceFields[name]})
	}
	if len(sourceFields) == 0 {
		// A definition's source schema must declare at least one field even
		// when every operation is a constant. Declaring the destination's
		// own first field name in the source schema would be a lie, so a
		// dedicated, never-read sentinel column is declared instead and named
		// as such.
		sourceFields = append(sourceFields, transformation.Field{Name: UnusedSourceField, Type: transformation.TypeString})
	}

	limits := b.limits
	if limits.Definition.MaxOperations < len(b.operations) {
		limits.Definition.MaxOperations = len(b.operations)
		limits.Notes = append(limits.Notes,
			fmt.Sprintf("max_operations raised to %d: the site declares a smaller operation ceiling than its own mapping needs", len(b.operations)))
	}
	if limits.Definition.MaxExpansion < b.maxSources {
		limits.Definition.MaxExpansion = b.maxSources
	}

	def := transformation.TransformationDefinition{
		Version: transformation.ContractVersion,
		Name:    b.name,
		Owner:   b.owner,
		Phase:   b.phase,
		Source: transformation.Schema{
			Name: b.sourceSchema, Version: 1, Fields: sourceFields,
		},
		Destination: transformation.Schema{
			Name: b.destSchema, Version: 1, Fields: append([]transformation.Field(nil), b.destFields...),
		},
		Operations:    b.operations,
		Compatibility: transformation.Compatibility{MinimumSourceVersion: 1},
		Limits:        limits.Definition,
		// Every migrating site refuses the whole unit of work when a mapping
		// cannot be applied; none of them skips a bad row and keeps going.
		Failure:     transformation.FailureReject,
		SideEffects: transformation.SideEffectsNone,
	}

	program, err := ir.Compile(def)
	if err != nil {
		return Lowered{}, fmt.Errorf("%w: lowering %s mapping %q: %w", ErrNoIREquivalent, b.site, b.name, err)
	}
	digest, err := program.Digest()
	if err != nil {
		return Lowered{}, err
	}

	bindings := append([]Binding(nil), b.bindings...)
	sort.SliceStable(bindings, func(i, j int) bool { return bindings[i].Target < bindings[j].Target })
	carriers := append([]Carrier(nil), b.carriers...)
	sort.SliceStable(carriers, func(i, j int) bool { return carriers[i].Target < carriers[j].Target })
	delegations := append([]Delegation(nil), b.delegations...)
	sort.SliceStable(delegations, func(i, j int) bool { return delegations[i].Target < delegations[j].Target })
	divergences := append([]Divergence(nil), b.divergences...)
	sort.SliceStable(divergences, func(i, j int) bool {
		if divergences[i].Feature != divergences[j].Feature {
			return divergences[i].Feature < divergences[j].Feature
		}
		return divergences[i].Vector < divergences[j].Vector
	})

	return Lowered{
		Site: b.site, Name: b.name, Program: program, ProgramDigest: digest,
		Limits: limits, Bindings: bindings, Carriers: carriers,
		Divergences: divergences, Delegations: delegations,
	}, nil
}

// UnusedSourceField is the sentinel source column declared when a mapping
// consists only of constants. Nothing reads it; it exists because the
// transformation contract requires a source schema to declare at least one
// field, and naming a real column that is never read would be worse.
const UnusedSourceField = "adapters.unused_source"

// Record encodes one row of site values -- keyed by the source keys the
// bindings name, each in its site's canonical text form -- into the typed
// exec record the lowered program reads. A source key the row does not carry
// is left out of the record entirely, which is exactly how exec represents
// ABSENT; it is never defaulted to a zero value.
func (l Lowered) Record(row map[string]string) (exec.Record, error) {
	out := make(exec.Record, len(row))
	for _, instr := range l.Program.Instructions {
		for _, s := range instr.Sources {
			text, ok := row[key(s)]
			if !ok {
				continue
			}
			data, err := encode(s.Type, text)
			if err != nil {
				return nil, fmt.Errorf("%w: source %s: %w", ErrNoIREquivalent, key(s), err)
			}
			out[key(s)] = exec.Present(s.Type, data)
		}
	}
	for k := range row {
		if _, used := out[k]; !used {
			if !l.reads(k) {
				return nil, fmt.Errorf("%w: row carries %q, which no lowered instruction reads", ErrNoIREquivalent, k)
			}
		}
	}
	return out, nil
}

func (l Lowered) reads(recordKey string) bool {
	for _, instr := range l.Program.Instructions {
		for _, s := range instr.Sources {
			if key(s) == recordKey {
				return true
			}
		}
	}
	return false
}

// Run encodes each row, executes the lowered program under the declared
// execution limits, and returns the raw typed results.
func (l Lowered) Run(rows []map[string]string) ([]exec.Record, error) {
	dataset := make([]exec.Record, 0, len(rows))
	for i, row := range rows {
		r, err := l.Record(row)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", i, err)
		}
		dataset = append(dataset, r)
	}
	return exec.Execute(l.Program, dataset, l.Limits.Execution.Limits())
}

// Texts renders one executed record back into the site's canonical text form,
// keyed by the site's own target name. A property that is not in the VALUE
// state is omitted rather than rendered as empty text: an absent value and an
// empty string are different answers, and collapsing them is the exact defect
// the shared engine's presence states exist to prevent.
func (l Lowered) Texts(record exec.Record) (map[string]string, error) {
	byKey := make(map[string]Binding, len(l.Bindings))
	for _, b := range l.Bindings {
		byKey[b.TargetKey] = b
	}
	out := make(map[string]string, len(record))
	keys := make([]string, 0, len(record))
	for k := range record {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := record[k]
		binding, ok := byKey[k]
		if !ok {
			return nil, fmt.Errorf("%w: executed record carries %q, which no binding declares", ErrNoIREquivalent, k)
		}
		if v.State != values.PresenceValue {
			continue
		}
		text, err := decode(v.Type, v.Data)
		if err != nil {
			return nil, fmt.Errorf("%w: target %s: %w", ErrNoIREquivalent, binding.Target, err)
		}
		out[binding.Target] = text
	}
	return out, nil
}

// encode turns a site's canonical text into the Go representation exec holds
// for an IR type. string, decimal, date and timestamp are carried verbatim,
// so a projection of any of them is byte-preserving; int and bool are the two
// types exec holds as Go values rather than text, and are therefore the two
// that re-render through a canonical form on the way out.
func encode(typ transformation.Type, text string) (any, error) {
	switch typ {
	case transformation.TypeString, transformation.TypeDecimal, transformation.TypeDate, transformation.TypeTimestamp:
		return text, nil
	case transformation.TypeInt:
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("int %q: %w", text, err)
		}
		return n, nil
	case transformation.TypeBool:
		v, err := strconv.ParseBool(text)
		if err != nil {
			return nil, fmt.Errorf("bool %q: %w", text, err)
		}
		return v, nil
	default:
		return nil, fmt.Errorf("type %q is not declared", typ)
	}
}

func decode(typ transformation.Type, data any) (string, error) {
	switch typ {
	case transformation.TypeString, transformation.TypeDecimal, transformation.TypeDate, transformation.TypeTimestamp:
		s, ok := data.(string)
		if !ok {
			return "", fmt.Errorf("%s value is %T, want string", typ, data)
		}
		return s, nil
	case transformation.TypeInt:
		n, ok := data.(int64)
		if !ok {
			return "", fmt.Errorf("int value is %T, want int64", data)
		}
		return strconv.FormatInt(n, 10), nil
	case transformation.TypeBool:
		v, ok := data.(bool)
		if !ok {
			return "", fmt.Errorf("bool value is %T, want bool", data)
		}
		return strconv.FormatBool(v), nil
	default:
		return "", fmt.Errorf("type %q is not declared", typ)
	}
}
