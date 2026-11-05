// WF-DATA-021: derived-field publication checks and the propagation engine.
//
// ValidateDerived checks one definition's derived fields against the
// object set and relationships. DerivedRecords wraps an event port:
// every write computes the record's derived fields before it commits,
// and every value-changing commit maintains the derived fields of
// linked subscribers. Maintenance is change-gated and depth-bounded, so
// re-running it converges instead of looping; a maintenance failure
// reports although the triggering write committed, and retrying the
// trigger resumes the maintenance.
package custom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	// derivedPropagationActor is the provenance actor of maintenance
	// commits. Humans never author derived values.
	derivedPropagationActor = "system:derived-propagation"
	// derivedPropagationPurpose is the purpose carried by maintenance
	// reads and commits.
	derivedPropagationPurpose = "custom.derived-maintenance"
	// defaultMaxPropagationDepth bounds subscriber chains. Three covers
	// the documented topologies (record -> aggregate -> rollup) while
	// keeping every commit finite.
	defaultMaxPropagationDepth = 3
)

// ValidateDerived checks one definition's derived fields against the
// object set and relationships: every link resolves to a relationship
// involving the definition's kind, the join field exists on both ends,
// the value field exists on the target, and every declared
// classification dominates the join of its inputs. Types outside the
// object set fail closed: an unknown input cannot be trusted.
func ValidateDerived(def CustomObjectDefinition, objects map[TypeID]CustomObjectDefinition, rels map[CustomRelationshipKey]CustomRelationshipDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	for name, fieldDef := range def.Fields {
		if fieldDef.Derived == nil {
			continue
		}
		join := fieldDef.Classification
		seenVars := map[string]bool{}
		elements := make(map[string]rules.ExpressionType, len(fieldDef.Derived.Links))
		for _, link := range fieldDef.Derived.Links {
			seenVars[link.Input] = true
			targetField, err := resolveLinkValue(def, link, objects, rels)
			if err != nil {
				return fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
			}
			element, err := expressionTypeFor(targetField.Type)
			if err != nil {
				return fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
			}
			elements[link.Input] = element
			joined, ok := maxClassification(join, targetField.Classification)
			if !ok {
				return fmt.Errorf("%w: field %q joins an unknown input label", ErrInvalidDerived, name)
			}
			join = joined
		}
		_, vars, err := compileDerived(name, def.Fields, fieldDef.Derived, elements)
		if err != nil {
			return err
		}
		for _, v := range vars {
			if seenVars[v] {
				continue
			}
			input, ok := def.Fields[v]
			if !ok {
				return fmt.Errorf("%w: field %q references undeclared %q", ErrInvalidDerived, name, v)
			}
			joined, ok := maxClassification(join, input.Classification)
			if !ok {
				return fmt.Errorf("%w: field %q joins an unknown input label", ErrInvalidDerived, name)
			}
			join = joined
		}
		need := join.Classification
		if _, ok := labelRank(fieldDef.Classification.Classification); !ok {
			return fmt.Errorf("%w: field %q classification is unknown", ErrInvalidDerived, name)
		}
		if _, ok := labelRank(need); !ok {
			return fmt.Errorf("%w: field %q joins an unknown input label", ErrInvalidDerived, name)
		}
		if higher, _ := joinLabel(fieldDef.Classification.Classification, need); higher != fieldDef.Classification.Classification {
			return fmt.Errorf("%w: field %q is %s but its inputs need %s",
				ErrInvalidDerived, name, fieldDef.Classification.Classification, need)
		}
	}
	return nil
}

// CustomRelationshipKey identifies one relationship by namespace and name.
type CustomRelationshipKey struct {
	Namespace string
	Name      string
}

// resolveLinkRelationship resolves one link's relationship reference: a
// bare name in the definition's namespace, or namespace.Name qualified.
func resolveLinkRelationship(namespace, ref string, rels map[CustomRelationshipKey]CustomRelationshipDefinition) (CustomRelationshipDefinition, error) {
	name, ns := ref, namespace
	if dot := strings.LastIndexByte(ref, '.'); dot >= 0 {
		ns, name = ref[:dot], ref[dot+1:]
	}
	rel, ok := rels[CustomRelationshipKey{Namespace: ns, Name: name}]
	if !ok {
		return CustomRelationshipDefinition{}, fmt.Errorf("relationship %q is not declared", ref)
	}
	return rel, nil
}

// linkTarget returns the linked end of a relationship from the source
// kind: the other endpoint. Self-kind relationships are refused because
// the join direction would be ambiguous.
func linkTarget(sourceKind string, rel CustomRelationshipDefinition) (string, error) {
	if rel.SourceKind == rel.TargetKind {
		return "", fmt.Errorf("relationship %q joins a kind to itself", rel.Name)
	}
	if sourceKind == rel.SourceKind {
		return rel.TargetKind, nil
	}
	if sourceKind == rel.TargetKind {
		return rel.SourceKind, nil
	}
	return "", fmt.Errorf("relationship %q involves neither %s end", rel.Name, sourceKind)
}

// resolveLinkValue resolves one link to the target value field it
// collects: the relationship, the linked end inside the object set, the
// join field on both ends and the value field on the target.
func resolveLinkValue(def CustomObjectDefinition, link DerivedLink, objects map[TypeID]CustomObjectDefinition, rels map[CustomRelationshipKey]CustomRelationshipDefinition) (FieldDefinition, error) {
	rel, err := resolveLinkRelationship(def.Namespace, link.Relationship, rels)
	if err != nil {
		return FieldDefinition{}, err
	}
	target, err := linkTarget(def.Kind, rel)
	if err != nil {
		return FieldDefinition{}, err
	}
	targetDef, ok := objects[TypeID{Namespace: rel.Namespace, Kind: target}]
	if !ok {
		return FieldDefinition{}, fmt.Errorf("target %s is outside the object set", target)
	}
	if _, ok := def.Fields[link.Field]; !ok {
		return FieldDefinition{}, fmt.Errorf("join field %q is not declared", link.Field)
	}
	targetField, ok := targetDef.Fields[link.Value]
	if !ok {
		return FieldDefinition{}, fmt.Errorf("target field %q is not declared", link.Value)
	}
	if _, ok := targetDef.Fields[link.Field]; !ok {
		return FieldDefinition{}, fmt.Errorf("join field %q is missing on %s", link.Field, target)
	}
	return targetField, nil
}

// linkElementTypes resolves every link input of one derived field to
// the expression element type of its target value field.
func linkElementTypes(def CustomObjectDefinition, spec *DerivedSpec, objects map[TypeID]CustomObjectDefinition, rels map[CustomRelationshipKey]CustomRelationshipDefinition) (map[string]rules.ExpressionType, error) {
	out := make(map[string]rules.ExpressionType, len(spec.Links))
	for _, link := range spec.Links {
		targetField, err := resolveLinkValue(def, link, objects, rels)
		if err != nil {
			return nil, err
		}
		element, err := expressionTypeFor(targetField.Type)
		if err != nil {
			return nil, err
		}
		out[link.Input] = element
	}
	return out, nil
}

// compiledDerived is one validated derived field with its compiled
// expression, evaluation inputs and topological rank.
type compiledDerived struct {
	name     string
	spec     *DerivedSpec
	compiled rules.CompiledExpression
	vars     []string
	order    int
}

// compiledDef is one indexed definition with its derived fields compiled
// in evaluation order.
type compiledDef struct {
	def    CustomObjectDefinition
	fields []*compiledDerived
}

// DerivedDeps wires derived computation to an event port, the object set
// and relationships it validates against, and the maintenance identity.
type DerivedDeps struct {
	Port          EventProjectionPort
	Definitions   []CustomObjectDefinition
	Relationships []CustomRelationshipDefinition
	Actor         string
	Purpose       string
	MaxDepth      int
	Clock         func() time.Time
}

// DerivedRecords computes derived fields on every write through the
// wrapped port and maintains linked subscribers after value-changing
// commits. Types outside the indexed object set pass through untouched.
type DerivedRecords struct {
	port     EventProjectionPort
	defs     map[TypeID]map[uint64]*compiledDef
	rels     map[CustomRelationshipKey]CustomRelationshipDefinition
	actor    string
	purpose  string
	maxDepth int
	clock    func() time.Time
}

// NewDerivedRecords indexes and validates the object set. Every indexed
// definition fully validates, including its derived links and
// classifications, so a wrapper never governs a type it cannot compute.
func NewDerivedRecords(deps DerivedDeps) (*DerivedRecords, error) {
	if deps.Port == nil {
		return nil, fmt.Errorf("%w: event port is required", ErrInvalidDerived)
	}
	actor := deps.Actor
	if actor == "" {
		actor = derivedPropagationActor
	}
	purpose := deps.Purpose
	if purpose == "" {
		purpose = derivedPropagationPurpose
	}
	maxDepth := deps.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxPropagationDepth
	}
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	rels := make(map[CustomRelationshipKey]CustomRelationshipDefinition, len(deps.Relationships))
	for _, rel := range deps.Relationships {
		if err := rel.Validate(); err != nil {
			return nil, err
		}
		key := CustomRelationshipKey{Namespace: rel.Namespace, Name: rel.Name}
		if _, dup := rels[key]; dup {
			return nil, fmt.Errorf("%w: relationship %s is declared twice", ErrInvalidDerived, rel.Name)
		}
		rels[key] = rel
	}
	objects := make(map[TypeID]CustomObjectDefinition, len(deps.Definitions))
	for _, def := range deps.Definitions {
		if err := def.Validate(); err != nil {
			return nil, err
		}
		objects[TypeID{Namespace: def.Namespace, Kind: def.Kind}] = def
	}
	indexed := map[TypeID]map[uint64]*compiledDef{}
	for _, def := range deps.Definitions {
		if err := ValidateDerived(def, objects, rels); err != nil {
			return nil, err
		}
		compiled, err := compileDerivedFields(def, objects, rels)
		if err != nil {
			return nil, err
		}
		id := TypeID{Namespace: def.Namespace, Kind: def.Kind}
		versions := indexed[id]
		if versions == nil {
			versions = map[uint64]*compiledDef{}
			indexed[id] = versions
		}
		versions[def.Version] = &compiledDef{def: def, fields: compiled}
	}
	return &DerivedRecords{
		port: deps.Port, defs: indexed, rels: rels,
		actor: actor, purpose: purpose, maxDepth: maxDepth, clock: clock,
	}, nil
}

// compileDerivedFields compiles one definition's derived fields in
// topological evaluation order against the object set.
func compileDerivedFields(def CustomObjectDefinition, objects map[TypeID]CustomObjectDefinition, rels map[CustomRelationshipKey]CustomRelationshipDefinition) ([]*compiledDerived, error) {
	order, err := derivedOrder(def.Fields)
	if err != nil {
		return nil, err
	}
	out := make([]*compiledDerived, 0, len(order))
	for rank, name := range order {
		fieldDef := def.Fields[name]
		elements, err := linkElementTypes(def, fieldDef.Derived, objects, rels)
		if err != nil {
			return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
		}
		compiled, vars, err := compileDerived(name, def.Fields, fieldDef.Derived, elements)
		if err != nil {
			return nil, err
		}
		out = append(out, &compiledDerived{name: name, spec: fieldDef.Derived, compiled: compiled, vars: vars, order: rank})
	}
	return out, nil
}

// derivedOrder returns the derived field names in evaluation order:
// every field after the derived fields it references.
func derivedOrder(fields map[string]FieldDefinition) ([]string, error) {
	derived := map[string]bool{}
	for name, fieldDef := range fields {
		if fieldDef.Derived != nil {
			derived[name] = true
		}
	}
	edges := map[string][]string{}
	for name, fieldDef := range fields {
		if fieldDef.Derived == nil {
			continue
		}
		vars, err := parseDerivedVars(fieldDef.Derived)
		if err != nil {
			return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
		}
		for _, v := range vars {
			if derived[v] {
				edges[name] = append(edges[name], v)
			}
		}
	}
	for name := range edges {
		sort.Strings(edges[name])
	}
	var order []string
	done := map[string]bool{}
	var visit func(name string, stack []string) error
	visit = func(name string, stack []string) error {
		for _, frame := range stack {
			if frame == name {
				return fmt.Errorf("%w: derivation cycle %s", ErrInvalidDerived, strings.Join(append(stack, name), " -> "))
			}
		}
		if done[name] {
			return nil
		}
		for _, next := range edges[name] {
			if err := visit(next, append(stack, name)); err != nil {
				return err
			}
		}
		done[name] = true
		order = append(order, name)
		return nil
	}
	names := make([]string, 0, len(derived))
	for name := range derived {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// Commit computes the record's derived fields, delegates the write and
// maintains linked subscribers. Records whose type is not indexed pass
// through untouched.
func (d *DerivedRecords) Commit(ctx context.Context, req MutationRequest) (CommitReceipt, error) {
	compiled := d.compiled(req.Definition)
	if compiled != nil && req.Operation != OperationRetire {
		computed, err := d.compute(ctx, req.Tenant, compiled, req.Record.FieldValues, req.ObjectID)
		if err != nil {
			return CommitReceipt{}, err
		}
		fields := cloneFields(req.Record.FieldValues)
		for name, value := range computed {
			if provided, ok := fields[name]; ok && !nativeEqual(provided.Value, value.Value) {
				return CommitReceipt{}, fmt.Errorf("%w: field %q", ErrDerivedMismatch, name)
			}
			fields[name] = value
		}
		req.Record.FieldValues = fields
	}
	var preimage map[string]TypedValue
	if req.Operation == OperationChange || req.Operation == OperationCorrect || req.Operation == OperationRetire {
		preimage = d.currentFields(ctx, req)
	}
	receipt, err := d.port.Commit(ctx, req)
	if err != nil {
		return CommitReceipt{}, err
	}
	if req.Operation == OperationCreate || req.Operation == OperationChange ||
		req.Operation == OperationCorrect || req.Operation == OperationRetire {
		if err := d.maintain(ctx, req, receipt, 0, map[subscriberKey]bool{}); err != nil {
			return receipt, err
		}
		// A join value may have moved: subscribers of the old value
		// must drop the record exactly as subscribers of the new value
		// gain it. Retires carry no values at all, so the pre-image is
		// their only trigger. Each pass keeps its own visited set: a
		// record maintained for the new value is not a cycle when the
		// old value reaches it again, and change-gating keeps the
		// second visit read-only.
		if preimage != nil {
			old := req
			old.Record.FieldValues = preimage
			if err := d.maintain(ctx, old, receipt, 0, map[subscriberKey]bool{}); err != nil {
				return receipt, err
			}
		}
	}
	return receipt, nil
}

// currentFields returns the stored fields of one object, or nil when the
// object has no projection yet. Maintenance reads the pre-image so moved
// and retired records release their old subscribers.
func (d *DerivedRecords) currentFields(ctx context.Context, req MutationRequest) map[string]TypedValue {
	report, err := d.port.Search(ctx, SearchRequest{
		Tenant: req.Tenant, Definition: req.Definition,
		GrantedDomains: allDomains(req.Definition), Purpose: d.purpose,
	})
	if err != nil {
		return nil
	}
	for _, rec := range report.Records {
		if rec.ObjectID == req.ObjectID {
			return rec.Fields
		}
	}
	return nil
}

// Search delegates to the wrapped port.
func (d *DerivedRecords) Search(ctx context.Context, req SearchRequest) (SearchReport, error) {
	return d.port.Search(ctx, req)
}

// Rebuild delegates to the wrapped port.
func (d *DerivedRecords) Rebuild(ctx context.Context, tenant values.TenantId, kind, objectID string) (ProjectionReport, error) {
	return d.port.Rebuild(ctx, tenant, kind, objectID)
}

func (d *DerivedRecords) compiled(def CustomObjectDefinition) *compiledDef {
	versions := d.defs[TypeID{Namespace: def.Namespace, Kind: def.Kind}]
	if versions == nil {
		return nil
	}
	return versions[def.Version]
}

// compute evaluates one record's derived fields in topological order:
// own fields from the record, linked inputs from current targets, and
// earlier derived fields as they complete.
func (d *DerivedRecords) compute(ctx context.Context, tenant values.TenantId, compiled *compiledDef, fields map[string]TypedValue, objectID string) (map[string]TypedValue, error) {
	working := make(map[string]TypedValue, len(fields))
	for name, value := range fields {
		working[name] = value
	}
	out := map[string]TypedValue{}
	for _, field := range compiled.fields {
		name := field.name
		inputs := map[string]rules.Value{}
		for _, v := range field.vars {
			if link := linkFor(field.spec, v); link != nil {
				list, err := d.linkedValues(ctx, tenant, compiled.def, *link, working, objectID)
				if err != nil {
					return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
				}
				inputs[v] = rules.ListValue(list...)
				continue
			}
			fieldDef, ok := compiled.def.Fields[v]
			if !ok {
				return nil, fmt.Errorf("%w: field %q references undeclared %q", ErrInvalidDerived, name, v)
			}
			stored, ok := working[v]
			if !ok {
				return nil, fmt.Errorf("%w: field %q needs missing input %q", ErrInvalidDerived, name, v)
			}
			value, err := toRulesValue(fieldDef.Type, stored.Value)
			if err != nil {
				return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
			}
			inputs[v] = value
		}
		result, err := field.compiled.Evaluate(inputs)
		if err != nil {
			return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
		}
		if result.State != rules.EvalStatePresent {
			return nil, fmt.Errorf("%w: field %q is unknown", ErrInvalidDerived, name)
		}
		native, err := fromRulesValue(compiled.def.Fields[name].Type, result.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: field %q: %v", ErrInvalidDerived, name, err)
		}
		typed := TypedValue{FieldName: name, Type: compiled.def.Fields[name].Type, Value: native}
		working[name] = typed
		out[name] = typed
	}
	return out, nil
}

// linkFor returns the link declaring one expression variable, if any.
func linkFor(spec *DerivedSpec, input string) *DerivedLink {
	for i := range spec.Links {
		if spec.Links[i].Input == input {
			return &spec.Links[i]
		}
	}
	return nil
}

// linkedValues collects one linked input: the target records joining on
// the shared field, their value fields as a list. Cardinality and the
// explicit target bound refuse overgrown joins instead of truncating them.
func (d *DerivedRecords) linkedValues(ctx context.Context, tenant values.TenantId, def CustomObjectDefinition, link DerivedLink, fields map[string]TypedValue, objectID string) ([]rules.Value, error) {
	rel, err := resolveLinkRelationship(def.Namespace, link.Relationship, d.rels)
	if err != nil {
		return nil, err
	}
	target, err := linkTarget(def.Kind, rel)
	if err != nil {
		return nil, err
	}
	join, ok := fields[link.Field]
	if !ok {
		return nil, nil
	}
	key, err := renderJoinValue(join.Value)
	if err != nil {
		return nil, err
	}
	targetDef, ok := latestDef(d.defs[TypeID{Namespace: rel.Namespace, Kind: target}])
	if !ok {
		return nil, fmt.Errorf("target %s is not indexed", target)
	}
	report, err := d.port.Search(ctx, SearchRequest{
		Tenant: tenant, Definition: targetDef,
		Query: map[string]string{link.Field: key}, Purpose: d.purpose,
		GrantedDomains: allDomains(targetDef),
	})
	if err != nil {
		return nil, err
	}
	if rel.Cardinality == CardinalityOneToOne && len(report.Records) > 1 {
		return nil, fmt.Errorf("relationship %q allows one target, %d join", rel.Name, len(report.Records))
	}
	if len(report.Records) > link.MaxTargets {
		return nil, fmt.Errorf("link %q exceeds its %d-target bound with %d", link.Input, link.MaxTargets, len(report.Records))
	}
	_ = objectID
	out := make([]rules.Value, 0, len(report.Records))
	for _, rec := range report.Records {
		versions := d.defs[TypeID{Namespace: rel.Namespace, Kind: target}]
		recordDef := versions[rec.DefinitionVersion]
		if recordDef == nil {
			return nil, fmt.Errorf("target %s version %d is not indexed", target, rec.DefinitionVersion)
		}
		fieldDef, ok := recordDef.def.Fields[link.Value]
		if !ok {
			return nil, fmt.Errorf("target field %q is missing on version %d", link.Value, rec.DefinitionVersion)
		}
		stored, ok := rec.Fields[link.Value]
		if !ok {
			continue
		}
		value, err := toRulesValue(fieldDef.Type, stored.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

// latestDef returns the highest indexed version of one type, if any.
func latestDef(versions map[uint64]*compiledDef) (CustomObjectDefinition, bool) {
	var (
		best CustomObjectDefinition
		have bool
		top  uint64
	)
	for version, compiled := range versions {
		if !have || version > top {
			best, have, top = compiled.def, true, version
		}
	}
	return best, have
}

// allDomains grants every field domain of one definition for internal
// maintenance reads.
func allDomains(def CustomObjectDefinition) map[string]bool {
	policy, err := ResolvePolicy(def)
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(policy.Fields))
	for _, field := range policy.Fields {
		out[field.AuthZDomain] = true
	}
	return out
}

// renderJoinValue renders one stored join value in its canonical query
// text: strings verbatim, booleans as true/false, integers decimal,
// decimals canonical, dates verbatim.
func renderJoinValue(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		if v != float64(int64(v)) {
			return "", fmt.Errorf("%w: join value %v is fractional", ErrInvalidDerived, v)
		}
		return strconv.FormatInt(int64(v), 10), nil
	}
	return "", fmt.Errorf("%w: join value has no canonical text", ErrInvalidDerived)
}

// nativeEqual compares two stored natives across JSON round-trips:
// strings and booleans exactly, integers numerically.
func nativeEqual(a, b any) bool {
	switch left := a.(type) {
	case string:
		right, ok := b.(string)
		return ok && left == right
	case bool:
		right, ok := b.(bool)
		return ok && left == right
	case int:
		return intEqual(int64(left), b)
	case int64:
		return intEqual(left, b)
	case float64:
		right, ok := b.(float64)
		return ok && left == right
	}
	return false
}

func intEqual(left int64, other any) bool {
	switch right := other.(type) {
	case int:
		return left == int64(right)
	case int64:
		return left == right
	case float64:
		return float64(left) == right
	}
	return false
}

// subscriberKey identifies one maintained record within a propagation run.
type subscriberKey struct {
	kind     string
	objectID string
}

// maintain recomputes the derived fields of records linking to one
// committed record, then recurses while the chain stays within bounds.
// Every commit lands only when a value actually changed, so re-running
// converges; revisits honor the relationship's own cycle contract.
func (d *DerivedRecords) maintain(ctx context.Context, trigger MutationRequest, receipt CommitReceipt, depth int, visited map[subscriberKey]bool) error {
	if depth > d.maxDepth {
		return fmt.Errorf("%w: propagation exceeds depth %d", ErrInvalidDerived, d.maxDepth)
	}
	me := subscriberKey{kind: trigger.Definition.Kind, objectID: trigger.ObjectID}
	visited[me] = true
	links := d.subscriberLinks(trigger.Definition.Kind)
	if len(links) == 0 {
		return nil
	}
	var errs []error
	for _, sub := range links {
		targets, err := d.reverseTargets(ctx, trigger, sub)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, rec := range targets {
			if err := d.maintainRecord(ctx, trigger, receipt, sub, rec, depth, visited); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errorsJoin(errs)
}

// subscriberLink is one derived link whose relationship touches the
// committed kind from the other end.
type subscriberLink struct {
	def  CustomObjectDefinition
	rel  CustomRelationshipDefinition
	link DerivedLink
}

// subscriberLinks returns every indexed derived link traversing toward
// the committed kind: links whose relationship has the kind on either
// end while the subscriber sits on the other.
func (d *DerivedRecords) subscriberLinks(kind string) []subscriberLink {
	var out []subscriberLink
	for _, versions := range d.defs {
		for _, compiled := range versions {
			for _, field := range compiled.fields {
				for _, link := range field.spec.Links {
					rel, err := resolveLinkRelationship(compiled.def.Namespace, link.Relationship, d.rels)
					if err != nil {
						continue
					}
					if (rel.SourceKind == kind && compiled.def.Kind != kind) ||
						(rel.TargetKind == kind && compiled.def.Kind != kind) {
						out = append(out, subscriberLink{def: compiled.def, rel: rel, link: link})
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].def.Kind != out[j].def.Kind {
			return out[i].def.Kind < out[j].def.Kind
		}
		if out[i].def.Version != out[j].def.Version {
			return out[i].def.Version < out[j].def.Version
		}
		return out[i].link.Input < out[j].link.Input
	})
	return out
}

// reverseTargets finds the subscriber records joining to one committed
// record: indexed type, shared join field, committed join value.
func (d *DerivedRecords) reverseTargets(ctx context.Context, trigger MutationRequest, sub subscriberLink) ([]SearchRecord, error) {
	join, ok := trigger.Record.FieldValues[sub.link.Field]
	if !ok {
		return nil, nil
	}
	key, err := renderJoinValue(join.Value)
	if err != nil {
		return nil, err
	}
	report, err := d.port.Search(ctx, SearchRequest{
		Tenant: trigger.Tenant, Definition: sub.def,
		Query: map[string]string{sub.link.Field: key}, Purpose: d.purpose,
		GrantedDomains: allDomains(sub.def),
	})
	if err != nil {
		return nil, err
	}
	if sub.rel.Cardinality == CardinalityOneToOne && len(report.Records) > 1 {
		return nil, fmt.Errorf("%w: relationship %q allows one target, %d join",
			ErrInvalidDerived, sub.rel.Name, len(report.Records))
	}
	if len(report.Records) > sub.link.MaxTargets {
		return nil, fmt.Errorf("%w: link %q exceeds its %d-target bound with %d",
			ErrInvalidDerived, sub.link.Input, sub.link.MaxTargets, len(report.Records))
	}
	out := make([]SearchRecord, 0, len(report.Records))
	for _, rec := range report.Records {
		if rec.DefinitionVersion != sub.def.Version {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// maintainRecord recomputes one subscriber and commits only when a
// derived value actually changed, then continues the chain.
func (d *DerivedRecords) maintainRecord(ctx context.Context, trigger MutationRequest, receipt CommitReceipt, sub subscriberLink, rec SearchRecord, depth int, visited map[subscriberKey]bool) error {
	key := subscriberKey{kind: sub.def.Kind, objectID: rec.ObjectID}
	if visited[key] {
		if sub.rel.AllowCycles {
			return nil
		}
		return fmt.Errorf("%w: propagation revisits %s %s", ErrInvalidDerived, sub.def.Kind, rec.ObjectID)
	}
	visited[key] = true
	compiled := d.compiled(sub.def)
	if compiled == nil {
		return fmt.Errorf("%w: subscriber %s is not indexed", ErrInvalidDerived, sub.def.Kind)
	}
	computed, err := d.compute(ctx, trigger.Tenant, compiled, rec.Fields, rec.ObjectID)
	if err != nil {
		return err
	}
	changed := map[string]TypedValue{}
	for name, value := range computed {
		stored, ok := rec.Fields[name]
		if !ok || !nativeEqual(stored.Value, value.Value) {
			changed[name] = value
		}
	}
	if len(changed) == 0 {
		return nil
	}
	now := d.clock()
	fields := make(map[string]TypedValue, len(rec.Fields)+len(changed))
	for name, value := range rec.Fields {
		fields[name] = value
	}
	for name, value := range changed {
		fields[name] = value
	}
	effective, err := values.NewOpenInstantInterval(values.NewInstant(now))
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(receipt.Event.EventID + "\x00" + rec.ObjectID + "\x00" + strconv.FormatUint(rec.Revision, 10)))
	next, err := d.port.Commit(ctx, MutationRequest{
		Tenant: trigger.Tenant, ObjectID: rec.ObjectID, Definition: sub.def,
		Record: CustomRecordRevision{
			ObjectID: rec.ObjectID, ObjectKind: sub.def.Kind, Namespace: sub.def.Namespace,
			DefinitionVersion: sub.def.Version, Effective: effective, FieldValues: fields,
		},
		Operation: OperationChange, ExpectedHead: int64(rec.Revision), ExpectedRevision: rec.Revision,
		Actor: d.actor, Purpose: d.purpose, EvidenceDigest: "derived:" + hex.EncodeToString(sum[:]),
		GrantedDomains: allDomains(sub.def), OccurredAt: now, EffectiveAt: now,
	})
	if err != nil {
		return err
	}
	return d.maintain(ctx, MutationRequest{
		Tenant: trigger.Tenant, ObjectID: rec.ObjectID, Definition: sub.def,
		Record:    CustomRecordRevision{ObjectID: rec.ObjectID, FieldValues: fields},
		Operation: OperationChange,
	}, next, depth+1, visited)
}

// errorsJoin joins maintenance errors, or nil when every subscriber held.
func errorsJoin(errs []error) error {
	joined := errs[:0:0]
	for _, err := range errs {
		if err != nil {
			joined = append(joined, err)
		}
	}
	if len(joined) == 0 {
		return nil
	}
	if len(joined) == 1 {
		return joined[0]
	}
	text := make([]string, 0, len(joined))
	for _, err := range joined {
		text = append(text, err.Error())
	}
	return errors.New("custom: maintenance failures: " + strings.Join(text, "; "))
}
