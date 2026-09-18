// CUSTOM-007: version, migrate and retire custom-object types.
//
// A type migration moves one Kind+Namespace from version N to exactly N+1.
// The plan carries the full field diff (added, removed, renamed, retyped),
// the impact counts, the rollback reference and the adoption evidence, so
// expand, backfill, shadow, cutover and retire each have something to point
// at. Breaking work is refused up front: removed fields need retained
// history plus hold clearance, retyped fields need an explicit mapping, and
// live references need an explicit migrate action — otherwise history would
// be orphaned or held data destroyed. Every migration ships its inverse via
// RollbackPlan, and the superseded version is retired explicitly with
// MarkRetired. Record movement itself stays pure; committing it is the
// EventStore's job.
package custom

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrInvalidMigration  = errors.New("custom: invalid type migration")
	ErrMigrationVersion  = errors.New("custom: migration must advance exactly one version")
	ErrMigrationBreaking = errors.New("custom: breaking migration lacks an explicit mapping")
	ErrMigrationBlocked  = errors.New("custom: breaking migration blocked by live references")
	ErrMigrationHeldData = errors.New("custom: migration would remove held data")
)

// MigrationImpact counts one migration's field diff.
type MigrationImpact struct {
	Added   int
	Removed int
	Renamed int
	Retyped int
}

// TypeMigration is the governed N to N+1 plan for one custom-object type.
type TypeMigration struct {
	Kind           string
	Namespace      string
	FromVersion    uint64
	ToVersion      uint64
	FromFields     []string
	ToFields       []string
	Added          []string
	AddedDefaults  map[string]TypedValue
	Removed        []string
	Renamed        map[string]string
	Retyped        map[string]string
	Impact         MigrationImpact
	RollbackRef    string
	EvidenceDigest string
	HoldClearance  string
	LiveReferences int
	EffectiveAt    time.Time
	// inverse marks a rollback plan, whose versions step down instead of up.
	// It is set only by RollbackPlan.
	inverse bool
}

// Breaking reports whether the plan alters or drops existing fields.
func (m TypeMigration) Breaking() bool {
	return len(m.Removed) > 0 || len(m.Retyped) > 0 || len(m.Renamed) > 0
}

// Validate reports whether the plan is internally consistent and carries
// its rollback and adoption evidence.
func (m TypeMigration) Validate() error {
	if m.Kind == "" || m.Namespace == "" {
		return fmt.Errorf("%w: kind and namespace are required", ErrInvalidMigration)
	}
	if m.inverse {
		if m.ToVersion+1 != m.FromVersion {
			return fmt.Errorf("%w: %d -> %d", ErrMigrationVersion, m.FromVersion, m.ToVersion)
		}
	} else if m.ToVersion != m.FromVersion+1 {
		return fmt.Errorf("%w: %d -> %d", ErrMigrationVersion, m.FromVersion, m.ToVersion)
	}
	if m.RollbackRef == "" || m.EvidenceDigest == "" {
		return fmt.Errorf("%w: rollback and adoption evidence are required", ErrInvalidMigration)
	}
	if m.EffectiveAt.IsZero() {
		return fmt.Errorf("%w: effective time is required", ErrInvalidMigration)
	}
	for _, a := range m.Added {
		d, ok := m.AddedDefaults[a]
		if !ok || d.FieldName != a {
			return fmt.Errorf("%w: added field %q lacks a default", ErrInvalidMigration, a)
		}
	}
	return nil
}

// MigrationOptions carries the caller-declared authorities one migration
// needs before a breaking plan may exist.
type MigrationOptions struct {
	LiveReferences      int
	LiveReferenceAction string
	HistoryRetained     bool
	HoldClearanceRef    string
	TypeMappings        map[string]string
	Renames             map[string]string
	RollbackRef         string
	EvidenceDigest      string
	EffectiveAt         time.Time
}

// PlanMigration diffs from against to and authorizes the result under opts.
// from and to must name the same type exactly one version apart.
func PlanMigration(from, to CustomObjectDefinition, opts MigrationOptions) (TypeMigration, error) {
	if err := from.Validate(); err != nil {
		return TypeMigration{}, fmt.Errorf("%w: source: %v", ErrInvalidMigration, err)
	}
	if err := to.Validate(); err != nil {
		return TypeMigration{}, fmt.Errorf("%w: target: %v", ErrInvalidMigration, err)
	}
	if from.Kind != to.Kind || from.Namespace != to.Namespace {
		return TypeMigration{}, fmt.Errorf("%w: migration must stay within one type", ErrInvalidMigration)
	}
	if to.Version != from.Version+1 {
		return TypeMigration{}, fmt.Errorf("%w: %d -> %d", ErrMigrationVersion, from.Version, to.Version)
	}
	renames := opts.Renames
	if renames == nil {
		renames = map[string]string{}
	}
	for old, new := range renames {
		if _, ok := from.Fields[old]; !ok {
			return TypeMigration{}, fmt.Errorf("%w: rename source %q is not declared", ErrInvalidMigration, old)
		}
		if _, ok := to.Fields[new]; !ok {
			return TypeMigration{}, fmt.Errorf("%w: rename target %q is not declared", ErrInvalidMigration, new)
		}
	}
	renamedFrom := map[string]bool{}
	renamedTo := map[string]bool{}
	for old, new := range renames {
		renamedFrom[old] = true
		renamedTo[new] = true
	}
	plan := TypeMigration{
		Kind: from.Kind, Namespace: from.Namespace,
		FromVersion: from.Version, ToVersion: to.Version,
		AddedDefaults: map[string]TypedValue{}, Renamed: map[string]string{}, Retyped: map[string]string{},
		RollbackRef: opts.RollbackRef, EvidenceDigest: opts.EvidenceDigest,
		HoldClearance: opts.HoldClearanceRef, LiveReferences: opts.LiveReferences,
		EffectiveAt: opts.EffectiveAt,
	}
	for name := range from.Fields {
		plan.FromFields = append(plan.FromFields, name)
	}
	for name := range to.Fields {
		plan.ToFields = append(plan.ToFields, name)
	}
	sort.Strings(plan.FromFields)
	sort.Strings(plan.ToFields)
	for name, fd := range to.Fields {
		if renamedTo[name] {
			continue
		}
		if _, ok := from.Fields[name]; !ok {
			plan.Added = append(plan.Added, name)
			plan.AddedDefaults[name] = TypedValue{FieldName: name, Type: fd.Type, Value: zeroFieldValue(fd.Type)}
		} else if from.Fields[name].Type != fd.Type {
			plan.Retyped[name] = from.Fields[name].Type + "->" + fd.Type
		}
	}
	for name := range from.Fields {
		if renamedFrom[name] {
			continue
		}
		if _, ok := to.Fields[name]; !ok {
			plan.Removed = append(plan.Removed, name)
		}
	}
	for old, new := range renames {
		plan.Renamed[old] = new
		if from.Fields[old].Type != to.Fields[new].Type {
			plan.Retyped[old] = from.Fields[old].Type + "->" + to.Fields[new].Type
		}
	}
	sort.Strings(plan.Added)
	sort.Strings(plan.Removed)
	plan.Impact = MigrationImpact{Added: len(plan.Added), Removed: len(plan.Removed), Renamed: len(plan.Renamed), Retyped: len(plan.Retyped)}
	for field := range plan.Retyped {
		if _, ok := opts.TypeMappings[field]; !ok {
			// A rename carrying a retype names its own mapping through the
			// rename declaration itself.
			if _, renamed := renames[field]; !renamed {
				return TypeMigration{}, fmt.Errorf("%w: field %q", ErrMigrationBreaking, field)
			}
		}
	}
	if len(plan.Removed) > 0 && (!opts.HistoryRetained || opts.HoldClearanceRef == "") {
		return TypeMigration{}, fmt.Errorf("%w: fields %v", ErrMigrationHeldData, plan.Removed)
	}
	if plan.Breaking() && opts.LiveReferences > 0 && opts.LiveReferenceAction != "migrate" {
		return TypeMigration{}, fmt.Errorf("%w: %d live references", ErrMigrationBlocked, opts.LiveReferences)
	}
	if err := plan.Validate(); err != nil {
		return TypeMigration{}, err
	}
	return plan, nil
}

func zeroFieldValue(fieldType string) any {
	switch fieldType {
	case "string":
		return ""
	case "bool":
		return false
	case "integer":
		return 0
	}
	return nil
}

// MigrateRecord moves rec from the plan's source version to its target
// version: removed fields drop, renames carry their values, added fields
// take plan defaults, retypes re-echo the mapped type. The record must
// otherwise match the source shape exactly: undeclared fields and missing
// live data refuse.
func MigrateRecord(plan TypeMigration, rec CustomRecordRevision) (CustomRecordRevision, error) {
	if err := plan.Validate(); err != nil {
		return CustomRecordRevision{}, err
	}
	if rec.ObjectKind != plan.Kind || rec.Namespace != plan.Namespace {
		return CustomRecordRevision{}, fmt.Errorf("%w: record is not %s/%s", ErrInvalidMigration, plan.Kind, plan.Namespace)
	}
	if rec.DefinitionVersion != plan.FromVersion {
		return CustomRecordRevision{}, fmt.Errorf("%w: record pins version %d, plan moves %d", ErrInvalidMigration, rec.DefinitionVersion, plan.FromVersion)
	}
	fromSet := map[string]bool{}
	for _, f := range plan.FromFields {
		fromSet[f] = true
	}
	for name := range rec.FieldValues {
		if !fromSet[name] {
			return CustomRecordRevision{}, fmt.Errorf("%w: undeclared field %q", ErrInvalidMigration, name)
		}
	}
	removed := map[string]bool{}
	for _, f := range plan.Removed {
		removed[f] = true
	}
	for _, f := range plan.FromFields {
		if !removed[f] {
			if _, ok := rec.FieldValues[f]; !ok {
				return CustomRecordRevision{}, fmt.Errorf("%w: live field %q is missing", ErrInvalidMigration, f)
			}
		}
	}
	out := rec
	out.DefinitionVersion = plan.ToVersion
	values := make(map[string]TypedValue, len(plan.ToFields))
	for name, tv := range rec.FieldValues {
		if removed[name] {
			continue
		}
		if dst, ok := plan.Renamed[name]; ok {
			tv.FieldName = dst
			values[dst] = tv
			continue
		}
		values[name] = tv
	}
	for _, a := range plan.Added {
		if _, ok := values[a]; !ok {
			values[a] = plan.AddedDefaults[a]
		}
	}
	for field := range plan.Retyped {
		dst := field
		if renamed, ok := plan.Renamed[field]; ok {
			dst = renamed
		}
		if tv, ok := values[dst]; ok {
			tv.Type = retypeTarget(plan.Retyped[field])
			values[dst] = tv
		}
	}
	out.FieldValues = values
	return out, nil
}

func retypeTarget(mapping string) string {
	for i := len(mapping) - 1; i >= 0; i-- {
		if mapping[i] == '>' {
			return mapping[i+1:]
		}
	}
	return mapping
}

// RollbackPlan derives the inverse migration: the exact plan that restores
// the source shape. Removed fields return as nil-valued tombstones the
// caller backfills from retained history; nothing is silently recreated.
func RollbackPlan(plan TypeMigration) (TypeMigration, error) {
	if err := plan.Validate(); err != nil {
		return TypeMigration{}, err
	}
	back := TypeMigration{
		Kind: plan.Kind, Namespace: plan.Namespace,
		FromVersion: plan.ToVersion, ToVersion: plan.FromVersion,
		FromFields:    append([]string(nil), plan.ToFields...),
		ToFields:      append([]string(nil), plan.FromFields...),
		AddedDefaults: map[string]TypedValue{}, Renamed: map[string]string{}, Retyped: map[string]string{},
		RollbackRef: plan.RollbackRef + ":inverse", EvidenceDigest: plan.EvidenceDigest,
		HoldClearance: plan.HoldClearance, EffectiveAt: plan.EffectiveAt,
		inverse: true,
	}
	back.Removed = append(back.Removed, plan.Added...)
	for _, r := range plan.Removed {
		back.Added = append(back.Added, r)
		back.AddedDefaults[r] = TypedValue{FieldName: r, Type: "unknown", Value: nil}
	}
	for old, new := range plan.Renamed {
		back.Renamed[new] = old
	}
	for field, mapping := range plan.Retyped {
		back.Retyped[field] = reverseMapping(mapping)
	}
	sort.Strings(back.Added)
	sort.Strings(back.Removed)
	back.Impact = MigrationImpact{Added: len(back.Added), Removed: len(back.Removed), Renamed: len(back.Renamed), Retyped: len(back.Retyped)}
	if err := back.Validate(); err != nil {
		return TypeMigration{}, err
	}
	return back, nil
}

func reverseMapping(mapping string) string {
	for i := 0; i < len(mapping); i++ {
		if mapping[i] == '>' {
			return mapping[i+1:] + "->" + mapping[:i-1]
		}
	}
	return mapping
}

// TypeRetirement retires one superseded type version in favor of its
// successor. Retirement is explicit evidence, never silence: the retired
// version, its successor and the authority stay pinned.
type TypeRetirement struct {
	Kind             string
	Namespace        string
	RetiredVersion   uint64
	SuccessorVersion uint64
	RetiredAt        time.Time
	EvidenceDigest   string
}

// MarkRetired retires plan's source version once its effective time has
// passed, pointing at the migration target as successor.
func MarkRetired(plan TypeMigration, at time.Time, evidence string) (TypeRetirement, error) {
	if err := plan.Validate(); err != nil {
		return TypeRetirement{}, err
	}
	if at.IsZero() || at.Before(plan.EffectiveAt) {
		return TypeRetirement{}, fmt.Errorf("%w: retirement precedes the migration", ErrInvalidMigration)
	}
	if evidence == "" {
		return TypeRetirement{}, fmt.Errorf("%w: retirement evidence is required", ErrInvalidMigration)
	}
	return TypeRetirement{Kind: plan.Kind, Namespace: plan.Namespace, RetiredVersion: plan.FromVersion, SuccessorVersion: plan.ToVersion, RetiredAt: at, EvidenceDigest: evidence}, nil
}
