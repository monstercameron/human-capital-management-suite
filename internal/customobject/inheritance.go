// Attribute inheritance resolves one declared attribute along one or more
// relationship hierarchies under a declared policy and explains every
// decision. Org cascades, location defaults and policy inheritance are one
// generic mechanism: an attribute declares its paths, its policy, which
// levels may override and which roles may set it.
//
// Two seams are left for the dependencies of WF-DATA-022:
//   - HierarchySource supplies ancestor chains. WF-DATA-020 replaces the
//     in-memory StaticHierarchies with effective-dated closure tables.
//   - Projection is a rebuildable, watermarked cache. WF-DATA-027 drives
//     Rebuild asynchronously off fan-out watermarks; until then callers
//     rebuild synchronously after writes.
package customobject

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// InheritancePolicy is the closed set of resolution policies. Custom
// definitions cannot add executable policy behavior.
type InheritancePolicy string

const (
	// InheritanceNearestWins resolves to the nearest allowed candidate.
	InheritanceNearestWins InheritancePolicy = "NEAREST_WINS"
	// InheritanceMinimum resolves to the smallest allowed number.
	InheritanceMinimum InheritancePolicy = "MINIMUM"
	// InheritanceMaximum resolves to the largest allowed number.
	InheritanceMaximum InheritancePolicy = "MAXIMUM"
	// InheritanceAccumulate unions every allowed set, root first.
	InheritanceAccumulate InheritancePolicy = "ACCUMULATE"
	// InheritanceLocked pins the root-most value; nearer values are refused.
	InheritanceLocked InheritancePolicy = "LOCKED"
)

func (p InheritancePolicy) valid() bool {
	switch p {
	case InheritanceNearestWins, InheritanceMinimum, InheritanceMaximum,
		InheritanceAccumulate, InheritanceLocked:
		return true
	default:
		return false
	}
}

// ScalarKind is the closed set of inheritable value kinds.
type ScalarKind string

const (
	ScalarString ScalarKind = "STRING"
	ScalarNumber ScalarKind = "NUMBER"
	ScalarBool   ScalarKind = "BOOL"
	ScalarSet    ScalarKind = "SET"
)

func (k ScalarKind) valid() bool {
	switch k {
	case ScalarString, ScalarNumber, ScalarBool, ScalarSet:
		return true
	default:
		return false
	}
}

// Scalar is one inheritable value. Exactly one payload field applies per
// Kind; Set members are normalized to sorted, deduplicated order.
type Scalar struct {
	Kind ScalarKind `json:"kind"`
	Text string     `json:"text,omitempty"`
	Num  float64    `json:"num,omitempty"`
	Flag bool       `json:"flag,omitempty"`
	Set  []string   `json:"set,omitempty"`
}

// StringScalar builds a STRING value.
func StringScalar(s string) Scalar { return Scalar{Kind: ScalarString, Text: s} }

// NumberScalar builds a NUMBER value.
func NumberScalar(n float64) Scalar { return Scalar{Kind: ScalarNumber, Num: n} }

// BoolScalar builds a BOOL value.
func BoolScalar(b bool) Scalar { return Scalar{Kind: ScalarBool, Flag: b} }

// SetScalar builds a SET value with sorted, deduplicated members.
func SetScalar(members ...string) Scalar {
	return Scalar{Kind: ScalarSet, Set: normalizeSet(members)}
}

func normalizeSet(members []string) []string {
	seen := make(map[string]struct{}, len(members))
	out := make([]string, 0, len(members))
	for _, m := range members {
		if m == "" {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// clone deep-copies the Set members so a served resolution never shares a
// backing array with the cached projection.
func (s Scalar) clone() Scalar {
	s.Set = append([]string(nil), s.Set...)
	return s
}

// Equal reports whether two scalars carry the same value. Sets compare as
// sets, independent of member order.
func (s Scalar) Equal(o Scalar) bool {
	if s.Kind != o.Kind {
		return false
	}
	switch s.Kind {
	case ScalarString:
		return s.Text == o.Text
	case ScalarNumber:
		return s.Num == o.Num
	case ScalarBool:
		return s.Flag == o.Flag
	case ScalarSet:
		if len(s.Set) != len(o.Set) {
			return false
		}
		seen := make(map[string]struct{}, len(s.Set))
		for _, m := range s.Set {
			seen[m] = struct{}{}
		}
		for _, m := range o.Set {
			if _, ok := seen[m]; !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// MarshalJSON emits only the payload field that applies to the kind, so a
// zero number or false boolean is never dropped from the wire form.
func (s Scalar) MarshalJSON() ([]byte, error) {
	type wire struct {
		Kind ScalarKind `json:"kind"`
		Text string     `json:"text,omitempty"`
		Num  *float64   `json:"num,omitempty"`
		Flag *bool      `json:"flag,omitempty"`
		Set  []string   `json:"set,omitempty"`
	}
	w := wire{Kind: s.Kind}
	switch s.Kind {
	case ScalarString:
		w.Text = s.Text
	case ScalarNumber:
		w.Num = &s.Num
	case ScalarBool:
		w.Flag = &s.Flag
	case ScalarSet:
		w.Set = append([]string(nil), s.Set...)
	}
	return json.Marshal(w)
}

// UnmarshalJSON reads the wire form written by MarshalJSON.
func (s *Scalar) UnmarshalJSON(b []byte) error {
	var w struct {
		Kind ScalarKind `json:"kind"`
		Text string     `json:"text,omitempty"`
		Num  *float64   `json:"num,omitempty"`
		Flag *bool      `json:"flag,omitempty"`
		Set  []string   `json:"set,omitempty"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*s = Scalar{Kind: w.Kind, Text: w.Text, Set: w.Set}
	if w.Num != nil {
		s.Num = *w.Num
	}
	if w.Flag != nil {
		s.Flag = *w.Flag
	}
	return nil
}

var (
	ErrInvalidAttribute  = errors.New("customobject: invalid inherited attribute")
	ErrUnknownAttribute  = errors.New("customobject: unknown inherited attribute")
	ErrDuplicateAttr     = errors.New("customobject: duplicate inherited attribute")
	ErrAttributeMismatch = errors.New("customobject: value names a different attribute")
	ErrHierarchyMismatch = errors.New("customobject: value hierarchy is not a declared path")
	ErrKindMismatch      = errors.New("customobject: value kind does not match the declaration")
	ErrRoleNotPermitted  = errors.New("customobject: role may not set this attribute")
	ErrDuplicateValue    = errors.New("customobject: duplicate value for attribute, hierarchy and record")
	ErrUnknownHierarchy  = errors.New("customobject: unknown hierarchy")
	ErrHierarchyCycle    = errors.New("customobject: hierarchy cycle")
	ErrMultipleParents   = errors.New("customobject: record has more than one parent in a hierarchy")
	ErrConflictingLevel  = errors.New("customobject: record claims conflicting levels")
)

// Refusal reasons recorded on refused overrides.
const (
	RefuseLevelNotOverridable = "LEVEL_NOT_OVERRIDABLE"
	RefuseLocked              = "LOCKED"
)

// AttributePath binds one hierarchy into an attribute at an explicit
// precedence. Lower precedence wins; equal precedence with differing values
// resolves to AMBIGUOUS, never last-write-wins.
type AttributePath struct {
	Hierarchy  string `json:"hierarchy"`
	Precedence int    `json:"precedence"`
}

// InheritedAttribute declares how one attribute resolves along relationship
// paths. A path names one hierarchy: an ordered ancestor chain supplied by
// the HierarchySource, whatever parent relation defines it.
type InheritedAttribute struct {
	Name              string            `json:"name"`
	Paths             []AttributePath   `json:"paths"`
	Policy            InheritancePolicy `json:"policy"`
	ValueKind         ScalarKind        `json:"value_kind"`
	OverridableLevels []string          `json:"overridable_levels,omitempty"`
	SetRoles          []string          `json:"set_roles"`
	RestrictsAccess   bool              `json:"restricts_access,omitempty"`
}

// Validate rejects incoherent declarations before anything can use them.
func (a InheritedAttribute) Validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidAttribute)
	}
	if len(a.Paths) == 0 {
		return fmt.Errorf("%w: %s declares no path", ErrInvalidAttribute, a.Name)
	}
	seen := make(map[string]struct{}, len(a.Paths))
	for _, p := range a.Paths {
		if strings.TrimSpace(p.Hierarchy) == "" {
			return fmt.Errorf("%w: %s declares an empty hierarchy", ErrInvalidAttribute, a.Name)
		}
		if _, dup := seen[p.Hierarchy]; dup {
			return fmt.Errorf("%w: %s declares %s twice", ErrInvalidAttribute, a.Name, p.Hierarchy)
		}
		seen[p.Hierarchy] = struct{}{}
	}
	if !a.Policy.valid() {
		return fmt.Errorf("%w: %s uses unknown policy %q", ErrInvalidAttribute, a.Name, a.Policy)
	}
	if !a.ValueKind.valid() {
		return fmt.Errorf("%w: %s uses unknown value kind %q", ErrInvalidAttribute, a.Name, a.ValueKind)
	}
	switch a.Policy {
	case InheritanceMinimum, InheritanceMaximum:
		if a.ValueKind != ScalarNumber {
			return fmt.Errorf("%w: %s folds %s over non-numeric %s", ErrInvalidAttribute, a.Name, a.Policy, a.ValueKind)
		}
	case InheritanceAccumulate:
		if a.ValueKind != ScalarSet {
			return fmt.Errorf("%w: %s accumulates non-set %s", ErrInvalidAttribute, a.Name, a.ValueKind)
		}
	}
	if a.RestrictsAccess && a.Policy != InheritanceAccumulate {
		return fmt.Errorf("%w: %s restricts access but does not accumulate", ErrInvalidAttribute, a.Name)
	}
	if len(a.SetRoles) == 0 {
		return fmt.Errorf("%w: %s names no role that may set it", ErrInvalidAttribute, a.Name)
	}
	for _, r := range a.SetRoles {
		if strings.TrimSpace(r) == "" {
			return fmt.Errorf("%w: %s names an empty role", ErrInvalidAttribute, a.Name)
		}
	}
	for _, l := range a.OverridableLevels {
		if strings.TrimSpace(l) == "" {
			return fmt.Errorf("%w: %s names an empty level", ErrInvalidAttribute, a.Name)
		}
	}
	return nil
}

// Digest is the stable policy version stamped on every resolution the
// declaration produces.
func (a InheritedAttribute) Digest() string {
	paths := append([]AttributePath(nil), a.Paths...)
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].Precedence != paths[j].Precedence {
			return paths[i].Precedence < paths[j].Precedence
		}
		return paths[i].Hierarchy < paths[j].Hierarchy
	})
	levels := append([]string(nil), a.OverridableLevels...)
	sort.Strings(levels)
	roles := append([]string(nil), a.SetRoles...)
	sort.Strings(roles)
	b, _ := json.Marshal(struct {
		Name            string            `json:"name"`
		Paths           []AttributePath   `json:"paths"`
		Policy          InheritancePolicy `json:"policy"`
		ValueKind       ScalarKind        `json:"value_kind"`
		Overridable     []string          `json:"overridable_levels"`
		SetRoles        []string          `json:"set_roles"`
		RestrictsAccess bool              `json:"restricts_access"`
	}{a.Name, paths, a.Policy, a.ValueKind, levels, roles, a.RestrictsAccess})
	return canonicalbytes.Digest(b)
}

// StoredValue is one attribute value set on one record. Level is the setter's
// claim; the hierarchy chain level governs override permission.
type StoredValue struct {
	Attribute string `json:"attribute"`
	Hierarchy string `json:"hierarchy"`
	Record    string `json:"record"`
	Level     string `json:"level,omitempty"`
	Value     Scalar `json:"value"`
	SetByRole string `json:"set_by_role"`
}

// Validate enforces the declaration at write time and fails closed.
func (v StoredValue) Validate(attr InheritedAttribute) error {
	if err := attr.Validate(); err != nil {
		return err
	}
	if v.Attribute != attr.Name {
		return fmt.Errorf("%w: value for %q filed under %q", ErrAttributeMismatch, v.Attribute, attr.Name)
	}
	declared := false
	for _, p := range attr.Paths {
		if p.Hierarchy == v.Hierarchy {
			declared = true
			break
		}
	}
	if !declared {
		return fmt.Errorf("%w: %s is not a declared path of %s", ErrHierarchyMismatch, v.Hierarchy, attr.Name)
	}
	if strings.TrimSpace(v.Record) == "" {
		return fmt.Errorf("%w: record is required", ErrInvalidAttribute)
	}
	if v.Value.Kind != attr.ValueKind {
		return fmt.Errorf("%w: %s carries %s, declaration wants %s", ErrKindMismatch, attr.Name, v.Value.Kind, attr.ValueKind)
	}
	for _, r := range attr.SetRoles {
		if v.SetByRole == r {
			return nil
		}
	}
	return fmt.Errorf("%w: %s may not set %s", ErrRoleNotPermitted, v.SetByRole, attr.Name)
}

// ChainNode is one ancestor-chain entry. Depth 0 is the record itself;
// larger depths are further toward the root.
type ChainNode struct {
	Record string `json:"record"`
	Level  string `json:"level"`
	Depth  int    `json:"depth"`
}

// HierarchySource supplies ancestor chains. StaticHierarchies serves the
// current snapshot; WF-DATA-020 replaces it with effective-dated closure
// tables behind this same interface.
type HierarchySource interface {
	// Ancestors returns the chain from the record up to the root, nearest
	// first. A record with no links is its own chain of one.
	Ancestors(hierarchy, record string) ([]ChainNode, error)
	// Records lists every record known in a hierarchy in sorted order.
	Records(hierarchy string) ([]string, error)
}

// HierarchyLink is one parent-to-child edge inside one hierarchy.
type HierarchyLink struct {
	Hierarchy   string `json:"hierarchy"`
	Parent      string `json:"parent"`
	ParentLevel string `json:"parent_level"`
	Child       string `json:"child"`
	ChildLevel  string `json:"child_level"`
}

// StaticHierarchies is an in-memory HierarchySource built from links. A
// record has exactly one parent per hierarchy so every chain is linear.
type StaticHierarchies struct {
	parents map[string]map[string]string
	levels  map[string]map[string]string
}

// NewStaticHierarchies validates links and rejects cycles, multiple parents
// and conflicting level claims.
func NewStaticHierarchies(links []HierarchyLink) (*StaticHierarchies, error) {
	s := &StaticHierarchies{
		parents: map[string]map[string]string{},
		levels:  map[string]map[string]string{},
	}
	claim := func(hierarchy, record, level string) error {
		if level == "" {
			return nil
		}
		known := s.levels[hierarchy]
		if known == nil {
			known = map[string]string{}
			s.levels[hierarchy] = known
		}
		if prev, ok := known[record]; ok && prev != level {
			return fmt.Errorf("%w: %s claims %s and %s in %s", ErrConflictingLevel, record, prev, level, hierarchy)
		}
		known[record] = level
		return nil
	}
	for _, l := range links {
		if strings.TrimSpace(l.Hierarchy) == "" || strings.TrimSpace(l.Parent) == "" || strings.TrimSpace(l.Child) == "" {
			return nil, fmt.Errorf("%w: hierarchy, parent and child are required", ErrInvalidAttribute)
		}
		if l.Parent == l.Child {
			return nil, fmt.Errorf("%w: %s parents itself in %s", ErrHierarchyCycle, l.Parent, l.Hierarchy)
		}
		edges := s.parents[l.Hierarchy]
		if edges == nil {
			edges = map[string]string{}
			s.parents[l.Hierarchy] = edges
		}
		if prev, dup := edges[l.Child]; dup && prev != l.Parent {
			return nil, fmt.Errorf("%w: %s has parents %s and %s in %s", ErrMultipleParents, l.Child, prev, l.Parent, l.Hierarchy)
		}
		edges[l.Child] = l.Parent
		if err := claim(l.Hierarchy, l.Parent, l.ParentLevel); err != nil {
			return nil, err
		}
		if err := claim(l.Hierarchy, l.Child, l.ChildLevel); err != nil {
			return nil, err
		}
	}
	for hierarchy, edges := range s.parents {
		for child := range edges {
			seen := map[string]struct{}{child: {}}
			for parent, ok := edges[child]; ok; parent, ok = edges[parent] {
				if _, dup := seen[parent]; dup {
					return nil, fmt.Errorf("%w: %s loops through %s", ErrHierarchyCycle, hierarchy, parent)
				}
				seen[parent] = struct{}{}
			}
		}
	}
	return s, nil
}

// Ancestors implements HierarchySource.
func (s *StaticHierarchies) Ancestors(hierarchy, record string) ([]ChainNode, error) {
	edges, ok := s.parents[hierarchy]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownHierarchy, hierarchy)
	}
	chain := []ChainNode{{Record: record, Level: s.levels[hierarchy][record], Depth: 0}}
	for parent, ok := edges[record]; ok; parent, ok = edges[parent] {
		chain = append(chain, ChainNode{Record: parent, Level: s.levels[hierarchy][parent], Depth: len(chain)})
	}
	return chain, nil
}

// Records implements HierarchySource.
func (s *StaticHierarchies) Records(hierarchy string) ([]string, error) {
	edges, ok := s.parents[hierarchy]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownHierarchy, hierarchy)
	}
	seen := map[string]struct{}{}
	for child, parent := range edges {
		seen[child] = struct{}{}
		seen[parent] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out, nil
}

// ValueSource identifies one contributing or shadowed candidate.
type ValueSource struct {
	Hierarchy string `json:"hierarchy"`
	Record    string `json:"record"`
	Level     string `json:"level"`
	Depth     int    `json:"depth"`
}

// RefusedOverride records one override that was not applied, and why.
type RefusedOverride struct {
	Source ValueSource `json:"source"`
	Reason string      `json:"reason"`
}

// AttributeResolution is one explained inheritance result. Value is the zero
// Scalar when Found is false. Values carries every contributor, root first,
// under ACCUMULATE only.
type AttributeResolution struct {
	Attribute     string            `json:"attribute"`
	Record        string            `json:"record"`
	Found         bool              `json:"found"`
	Ambiguous     bool              `json:"ambiguous"`
	Value         Scalar            `json:"value"`
	Values        []Scalar          `json:"values,omitempty"`
	Sources       []ValueSource     `json:"sources,omitempty"`
	Overrode      []ValueSource     `json:"overrode,omitempty"`
	Refused       []RefusedOverride `json:"refused,omitempty"`
	Watermark     uint64            `json:"watermark"`
	PolicyVersion string            `json:"policy_version"`
}

func (r AttributeResolution) clone() AttributeResolution {
	r.Value = r.Value.clone()
	r.Values = append([]Scalar(nil), r.Values...)
	for i := range r.Values {
		r.Values[i] = r.Values[i].clone()
	}
	r.Sources = append([]ValueSource(nil), r.Sources...)
	r.Overrode = append([]ValueSource(nil), r.Overrode...)
	r.Refused = append([]RefusedOverride(nil), r.Refused...)
	return r
}

type compiledAttribute struct {
	decl     InheritedAttribute
	digest   string
	roles    map[string]struct{}
	levels   map[string]struct{}
	priority map[string]int
}

func compileAttribute(a InheritedAttribute) (compiledAttribute, error) {
	if err := a.Validate(); err != nil {
		return compiledAttribute{}, err
	}
	c := compiledAttribute{
		decl:     a,
		digest:   a.Digest(),
		roles:    make(map[string]struct{}, len(a.SetRoles)),
		levels:   make(map[string]struct{}, len(a.OverridableLevels)),
		priority: make(map[string]int, len(a.Paths)),
	}
	for _, r := range a.SetRoles {
		c.roles[r] = struct{}{}
	}
	for _, l := range a.OverridableLevels {
		c.levels[l] = struct{}{}
	}
	for _, p := range a.Paths {
		c.priority[p.Hierarchy] = p.Precedence
	}
	return c, nil
}

type candidate struct {
	source ValueSource
	value  Scalar
	nearer bool // true once a root-most base exists below this candidate
}

type singleResult struct {
	found    bool
	value    Scalar
	values   []Scalar
	sources  []ValueSource
	overrode []ValueSource
	refused  []RefusedOverride
}

// resolveOne resolves an attribute within a single hierarchy. The root-most
// candidate is the base; every nearer candidate is an override that applies
// only when its level may override, and never under LOCKED.
func resolveOne(attr compiledAttribute, hierarchy string, chain []ChainNode, byRecord map[string]StoredValue) singleResult {
	var ordered []candidate // root first
	for i := len(chain) - 1; i >= 0; i-- {
		node := chain[i]
		v, ok := byRecord[node.Record]
		if !ok {
			continue
		}
		level := node.Level
		if level == "" {
			level = v.Level
		}
		ordered = append(ordered, candidate{
			source: ValueSource{Hierarchy: hierarchy, Record: node.Record, Level: level, Depth: node.Depth},
			value:  v.Value,
			nearer: len(ordered) > 0,
		})
	}
	if len(ordered) == 0 {
		return singleResult{}
	}
	allowed := make([]candidate, 0, len(ordered))
	var refused []RefusedOverride
	for _, c := range ordered {
		if !c.nearer {
			allowed = append(allowed, c)
			continue
		}
		if attr.decl.Policy == InheritanceLocked {
			refused = append(refused, RefusedOverride{Source: c.source, Reason: RefuseLocked})
			continue
		}
		if _, ok := attr.levels[c.source.Level]; !ok {
			refused = append(refused, RefusedOverride{Source: c.source, Reason: RefuseLevelNotOverridable})
			continue
		}
		allowed = append(allowed, c)
	}
	// allowed always holds the base, so it is never empty here.
	shadowed := func(winner candidate) []ValueSource {
		out := make([]ValueSource, 0, len(allowed)-1)
		for i := len(allowed) - 1; i >= 0; i-- {
			if allowed[i].source == winner.source {
				continue
			}
			out = append(out, allowed[i].source)
		}
		return out
	}
	switch attr.decl.Policy {
	case InheritanceMinimum, InheritanceMaximum:
		winner := allowed[0]
		for _, c := range allowed[1:] {
			better := c.value.Num < winner.value.Num
			if attr.decl.Policy == InheritanceMaximum {
				better = c.value.Num > winner.value.Num
			}
			if better || (c.value.Num == winner.value.Num && c.source.Depth < winner.source.Depth) {
				winner = c
			}
		}
		return singleResult{found: true, value: winner.value, sources: []ValueSource{winner.source}, overrode: shadowed(winner), refused: refused}
	case InheritanceAccumulate:
		union := []string{}
		seen := map[string]struct{}{}
		values := make([]Scalar, 0, len(allowed))
		sources := make([]ValueSource, 0, len(allowed))
		for _, c := range allowed {
			values = append(values, c.value)
			sources = append(sources, c.source)
			for _, m := range c.value.Set {
				if _, dup := seen[m]; dup {
					continue
				}
				seen[m] = struct{}{}
				union = append(union, m)
			}
		}
		return singleResult{found: true, value: Scalar{Kind: ScalarSet, Set: union}, values: values, sources: sources, refused: refused}
	default: // NEAREST_WINS and LOCKED resolve to one candidate.
		winner := allowed[len(allowed)-1]
		if attr.decl.Policy == InheritanceLocked {
			winner = allowed[0]
		}
		return singleResult{found: true, value: winner.value, sources: []ValueSource{winner.source}, overrode: shadowed(winner), refused: refused}
	}
}

// Projection serves inheritance results from a rebuildable cache. Every
// served resolution stamps the watermark of the Rebuild that produced it.
type Projection struct {
	mu        sync.RWMutex
	watermark uint64
	attrs     map[string]compiledAttribute
	cached    map[projectionKey]AttributeResolution
}

type projectionKey struct {
	attr   string
	record string
}

// NewProjection returns an empty projection at watermark zero.
func NewProjection() *Projection {
	return &Projection{attrs: map[string]compiledAttribute{}, cached: map[projectionKey]AttributeResolution{}}
}

// Rebuild validates its inputs, recomputes every attribute-by-record
// resolution and publishes a new watermark. On error the previous
// projection is left untouched.
func (p *Projection) Rebuild(attrs []InheritedAttribute, src HierarchySource, values []StoredValue) (uint64, error) {
	compiled := make(map[string]compiledAttribute, len(attrs))
	for _, a := range attrs {
		c, err := compileAttribute(a)
		if err != nil {
			return p.Watermark(), err
		}
		if _, dup := compiled[a.Name]; dup {
			return p.Watermark(), fmt.Errorf("%w: %s", ErrDuplicateAttr, a.Name)
		}
		compiled[a.Name] = c
	}
	byAttrHierarchy := map[projectionKey]map[string]StoredValue{}
	records := map[string]struct{}{}
	for _, v := range values {
		c, ok := compiled[v.Attribute]
		if !ok {
			return p.Watermark(), fmt.Errorf("%w: %s", ErrUnknownAttribute, v.Attribute)
		}
		if err := v.Validate(c.decl); err != nil {
			return p.Watermark(), err
		}
		if v.Value.Kind == ScalarSet {
			v.Value.Set = normalizeSet(v.Value.Set)
		}
		k := projectionKey{attr: v.Attribute, record: v.Hierarchy}
		slot := byAttrHierarchy[k]
		if slot == nil {
			slot = map[string]StoredValue{}
			byAttrHierarchy[k] = slot
		}
		if _, dup := slot[v.Record]; dup {
			return p.Watermark(), fmt.Errorf("%w: %s in %s at %s", ErrDuplicateValue, v.Attribute, v.Hierarchy, v.Record)
		}
		slot[v.Record] = v
		records[v.Record] = struct{}{}
	}
	chains := map[projectionKey][]ChainNode{}
	chainFor := func(hierarchy, record string) ([]ChainNode, error) {
		k := projectionKey{attr: hierarchy, record: record}
		if chain, ok := chains[k]; ok {
			return chain, nil
		}
		chain, err := src.Ancestors(hierarchy, record)
		if err != nil {
			return nil, err
		}
		chains[k] = chain
		return chain, nil
	}
	for _, c := range compiled {
		for _, path := range c.decl.Paths {
			known, err := src.Records(path.Hierarchy)
			if err != nil {
				return p.Watermark(), err
			}
			for _, r := range known {
				records[r] = struct{}{}
			}
		}
	}
	next := p.Watermark() + 1
	cached := make(map[projectionKey]AttributeResolution, len(compiled)*len(records))
	for name, c := range compiled {
		for record := range records {
			res := AttributeResolution{Attribute: name, Record: record, Watermark: next, PolicyVersion: c.digest}
			type contender struct {
				path   AttributePath
				result singleResult
			}
			var contenders []contender
			for _, path := range c.decl.Paths {
				chain, err := chainFor(path.Hierarchy, record)
				if err != nil {
					return p.Watermark(), err
				}
				r := resolveOne(c, path.Hierarchy, chain, byAttrHierarchy[projectionKey{attr: name, record: path.Hierarchy}])
				if r.found {
					contenders = append(contenders, contender{path: path, result: r})
				}
			}
			if len(contenders) == 0 {
				cached[projectionKey{attr: name, record: record}] = res
				continue
			}
			best := contenders[0].path.Precedence
			for _, cd := range contenders[1:] {
				if cd.path.Precedence < best {
					best = cd.path.Precedence
				}
			}
			var winners []contender
			for _, cd := range contenders {
				if cd.path.Precedence == best {
					winners = append(winners, cd)
				} else {
					res.Overrode = append(res.Overrode, cd.result.sources...)
					res.Overrode = append(res.Overrode, cd.result.overrode...)
					res.Refused = append(res.Refused, cd.result.refused...)
				}
			}
			decided := true
			for _, w := range winners[1:] {
				if !w.result.value.Equal(winners[0].result.value) {
					decided = false
					break
				}
			}
			if !decided {
				res.Ambiguous = true
				for _, w := range winners {
					res.Sources = append(res.Sources, w.result.sources...)
					res.Overrode = append(res.Overrode, w.result.overrode...)
					res.Refused = append(res.Refused, w.result.refused...)
				}
				cached[projectionKey{attr: name, record: record}] = res
				continue
			}
			res.Found = true
			res.Value = winners[0].result.value
			for _, w := range winners {
				res.Values = append(res.Values, w.result.values...)
				res.Sources = append(res.Sources, w.result.sources...)
				res.Overrode = append(res.Overrode, w.result.overrode...)
				res.Refused = append(res.Refused, w.result.refused...)
			}
			cached[projectionKey{attr: name, record: record}] = res
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.watermark = next
	p.attrs = compiled
	p.cached = cached
	return next, nil
}

// Resolve serves one cached resolution. Records with no chain and no values
// resolve empty at the current watermark; unknown attributes error.
func (p *Projection) Resolve(attribute, record string) (AttributeResolution, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	c, ok := p.attrs[attribute]
	if !ok {
		return AttributeResolution{}, fmt.Errorf("%w: %s", ErrUnknownAttribute, attribute)
	}
	if res, ok := p.cached[projectionKey{attr: attribute, record: record}]; ok {
		return res.clone(), nil
	}
	return AttributeResolution{Attribute: attribute, Record: record, Watermark: p.watermark, PolicyVersion: c.digest}, nil
}

// Watermark reports the latest published rebuild watermark.
func (p *Projection) Watermark() uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.watermark
}
