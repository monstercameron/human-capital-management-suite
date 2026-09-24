package config

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var (
	ErrParameterValueInvalid      = errors.New("config: invalid parameter value revision")
	ErrParameterRevisionConflict  = errors.New("config: parameter value revision conflict")
	ErrParameterValueMissing      = errors.New("config: required parameter value is missing")
	ErrParameterValueTypeMismatch = errors.New("config: parameter value type differs from its definition")
	ErrParameterScopePathInvalid  = errors.New("config: parameter scope path is invalid")
	ErrParameterScopeLocked       = errors.New("config: ancestor parameter value locks this scope path")
)

// ParameterScopeKind identifies one supported scope in a tenant's value
// hierarchy. Scope order and parentage are supplied by the caller's trusted
// hierarchy resolver; this package validates the path and applies its values.
type ParameterScopeKind string

const (
	ScopeTenant       ParameterScopeKind = "TENANT"
	ScopeCompany      ParameterScopeKind = "COMPANY"
	ScopeLegalEntity  ParameterScopeKind = "LEGAL_ENTITY"
	ScopeOrganization ParameterScopeKind = "ORGANIZATION"
)

// ParameterScope identifies one node in the caller-provided scope path.
type ParameterScope struct {
	Kind ParameterScopeKind
	ID   string
}

// ParameterScopePath is ordered from tenant root to the requested scope.
type ParameterScopePath []ParameterScope

// ParameterEnvironment keeps non-production values in a separate namespace.
type ParameterEnvironment string

const (
	EnvironmentSandbox    ParameterEnvironment = "SANDBOX"
	EnvironmentProduction ParameterEnvironment = "PRODUCTION"
)

// ParameterValueChange is one append request. ExpectedRevision is the
// current revision for the exact (parameter, scope, environment) tuple, or
// zero when creating its first revision. ValueType must exactly match the
// current definition's workflow type; Value is that type's canonical string
// representation and is not decoded by this package.
type ParameterValueChange struct {
	Key              string
	Scope            ParameterScope
	Environment      ParameterEnvironment
	Value            string
	ValueType        workflow.ValueType
	ExpectedRevision uint64
	Author           string
	Reason           string
	RecordedAt       time.Time
	Locked           bool
	Path             ParameterScopePath
}

// ParameterValueRevision is one immutable-by-value authored value revision.
// DefinitionVersion records which config snapshot supplied its declared type.
type ParameterValueRevision struct {
	Key               string
	DefinitionName    string
	DefinitionVersion string
	Scope             ParameterScope
	Environment       ParameterEnvironment
	Revision          uint64
	Value             string
	ValueType         workflow.ValueType
	Author            string
	Reason            string
	RecordedAt        time.Time
	Locked            bool
}

// ParameterValueResolution reports the selected value and its scope source.
// Overridden lists broader configured scopes displaced by the selected value.
type ParameterValueResolution struct {
	Key               string
	Environment       ParameterEnvironment
	Value             string
	ValueType         workflow.ValueType
	Found             bool
	FromDefault       bool
	Source            ParameterScope
	HasSource         bool
	Revision          uint64
	DefinitionVersion string
	Author            string
	Reason            string
	Locked            bool
	Overridden        []ParameterScope
}

type parameterValueKey struct {
	key         string
	scopeKind   ParameterScopeKind
	scopeID     string
	environment ParameterEnvironment
}

// ParameterValueStore is an append-only in-memory reference ledger. Its
// state belongs to the store instance; callers can replace it with durable
// storage without changing revision, scope, or resolution semantics.
type ParameterValueStore struct {
	mu        sync.RWMutex
	revisions map[parameterValueKey][]ParameterValueRevision
}

// NewParameterValueStore creates an empty value revision ledger.
func NewParameterValueStore() *ParameterValueStore {
	return &ParameterValueStore{revisions: make(map[parameterValueKey][]ParameterValueRevision)}
}

// Append creates the next revision for one parameter/scope/environment. A
// locked ancestor prevents a descendant from introducing an override. The
// exact same scope may publish a later revision under its own authority.
func (s *ParameterValueStore) Append(snapshot Snapshot, change ParameterValueChange) (ParameterValueRevision, error) {
	if s == nil {
		return ParameterValueRevision{}, fmt.Errorf("%w: nil store", ErrParameterValueInvalid)
	}
	definition, found := snapshot.ParameterDefinition(change.Key)
	if !found {
		return ParameterValueRevision{}, fmt.Errorf("%w: definition %q is absent", ErrParameterValueInvalid, change.Key)
	}
	if err := validateParameterChange(change, definition); err != nil {
		return ParameterValueRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkAncestorLocksLocked(change); err != nil {
		return ParameterValueRevision{}, err
	}
	key := keyFor(change.Key, change.Scope, change.Environment)
	history := s.revisions[key]
	var current uint64
	if len(history) > 0 {
		last := history[len(history)-1]
		current = last.Revision
		if !change.RecordedAt.After(last.RecordedAt) {
			return ParameterValueRevision{}, fmt.Errorf("%w: recorded time must advance beyond revision %d", ErrParameterValueInvalid, current)
		}
	}
	if change.ExpectedRevision != current {
		return ParameterValueRevision{}, fmt.Errorf("%w: expected %d, current %d", ErrParameterRevisionConflict, change.ExpectedRevision, current)
	}
	revision := ParameterValueRevision{
		Key: change.Key, DefinitionName: snapshot.Name, DefinitionVersion: snapshot.Version,
		Scope: change.Scope, Environment: change.Environment, Revision: current + 1,
		Value: change.Value, ValueType: cloneWorkflowValueType(definition.Type),
		Author: change.Author, Reason: change.Reason, RecordedAt: change.RecordedAt.UTC(), Locked: change.Locked,
	}
	s.revisions[key] = append(history, revision)
	return cloneParameterValueRevision(revision), nil
}

// Resolve selects the most specific authored value on path for exactly one
// environment. It never falls back from SANDBOX to PRODUCTION or vice versa.
// The definition and consumer are supplied by the current immutable snapshot,
// so a stale value type or unauthorized consumer fails closed.
func (s *ParameterValueStore) Resolve(snapshot Snapshot, key string, path ParameterScopePath, environment ParameterEnvironment, consumer Consumer, requestedType workflow.ValueType) (ParameterValueResolution, error) {
	if s == nil {
		return ParameterValueResolution{}, fmt.Errorf("%w: nil store", ErrParameterValueInvalid)
	}
	definition, found := snapshot.ParameterDefinition(key)
	if !found {
		return ParameterValueResolution{}, fmt.Errorf("%w: definition %q is absent", ErrParameterValueInvalid, key)
	}
	if err := validateParameterScopePath(path); err != nil {
		return ParameterValueResolution{}, err
	}
	if !validParameterEnvironment(environment) {
		return ParameterValueResolution{}, fmt.Errorf("%w: unsupported environment %q", ErrParameterValueInvalid, environment)
	}
	if err := definition.CheckRead(consumer, requestedType); err != nil {
		return ParameterValueResolution{}, err
	}
	s.mu.RLock()
	revisions := make([]ParameterValueRevision, 0, len(path))
	for _, scope := range path {
		history := s.revisions[keyFor(key, scope, environment)]
		if len(history) > 0 {
			revisions = append(revisions, cloneParameterValueRevision(history[len(history)-1]))
		}
	}
	s.mu.RUnlock()
	return ResolveParameterRevisions(snapshot, key, path, environment, consumer, requestedType, revisions)
}

// ResolveParameterRevisions resolves from latest revisions supplied by a
// durable repository. One or zero latest revisions for each path scope must
// be supplied; older rows must be filtered by the repository.
func ResolveParameterRevisions(snapshot Snapshot, key string, path ParameterScopePath, environment ParameterEnvironment, consumer Consumer, requestedType workflow.ValueType, revisions []ParameterValueRevision) (ParameterValueResolution, error) {
	if err := validateParameterScopePath(path); err != nil {
		return ParameterValueResolution{}, err
	}
	if !validParameterEnvironment(environment) {
		return ParameterValueResolution{}, fmt.Errorf("%w: unsupported environment %q", ErrParameterValueInvalid, environment)
	}
	definition, found := snapshot.ParameterDefinition(key)
	if !found {
		return ParameterValueResolution{}, fmt.Errorf("%w: definition %q is absent", ErrParameterValueInvalid, key)
	}
	if err := definition.CheckRead(consumer, requestedType); err != nil {
		return ParameterValueResolution{}, err
	}
	byScope := make(map[ParameterScope]ParameterValueRevision, len(revisions))
	for _, revision := range revisions {
		if revision.Key != key || revision.Environment != environment || !containsParameterScope(path, revision.Scope) || revision.Revision == 0 {
			return ParameterValueResolution{}, fmt.Errorf("%w: repository returned a revision outside the requested key, environment, or path", ErrParameterValueInvalid)
		}
		if _, duplicate := byScope[revision.Scope]; duplicate {
			return ParameterValueResolution{}, fmt.Errorf("%w: repository returned duplicate latest revisions", ErrParameterValueInvalid)
		}
		byScope[revision.Scope] = cloneParameterValueRevision(revision)
	}
	var selected *ParameterValueRevision
	var overridden []ParameterScope
	for _, scope := range path {
		current, exists := byScope[scope]
		if !exists {
			continue
		}
		if current.ValueType.String() != definition.Type.String() {
			return ParameterValueResolution{}, fmt.Errorf("%w: %q changed from %s to %s; publish a value revision", ErrParameterValueTypeMismatch, key, current.ValueType, definition.Type)
		}
		if selected != nil {
			if selected.Locked {
				return ParameterValueResolution{}, fmt.Errorf("%w: %s/%s locks %s/%s", ErrParameterScopeLocked, selected.Scope.Kind, selected.Scope.ID, scope.Kind, scope.ID)
			}
			overridden = append(overridden, selected.Scope)
		}
		copy := cloneParameterValueRevision(current)
		selected = &copy
	}
	if selected != nil {
		return ParameterValueResolution{
			Key: key, Environment: environment, Value: selected.Value, ValueType: cloneWorkflowValueType(selected.ValueType),
			Found: true, Source: selected.Scope, HasSource: true, Revision: selected.Revision,
			DefinitionVersion: selected.DefinitionVersion, Author: selected.Author, Reason: selected.Reason,
			Locked: selected.Locked, Overridden: cloneParameterScopes(overridden),
		}, nil
	}
	if definition.Default != nil {
		if definition.Default.Type.String() != definition.Type.String() {
			return ParameterValueResolution{}, fmt.Errorf("%w: default type %s differs from definition %s", ErrParameterValueTypeMismatch, definition.Default.Type, definition.Type)
		}
		return ParameterValueResolution{
			Key: key, Environment: environment, Value: definition.Default.Value,
			ValueType: cloneWorkflowValueType(definition.Type), Found: true, FromDefault: true,
			DefinitionVersion: snapshot.Version,
		}, nil
	}
	if definition.Required {
		return ParameterValueResolution{}, fmt.Errorf("%w: %s has no value in %s", ErrParameterValueMissing, key, environment)
	}
	return ParameterValueResolution{Key: key, Environment: environment, ValueType: cloneWorkflowValueType(definition.Type)}, nil
}

func containsParameterScope(path ParameterScopePath, scope ParameterScope) bool {
	for _, candidate := range path {
		if candidate == scope {
			return true
		}
	}
	return false
}

// Revisions returns a defensive copy of one exact revision history.
func (s *ParameterValueStore) Revisions(key string, scope ParameterScope, environment ParameterEnvironment) []ParameterValueRevision {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := s.revisions[keyFor(key, scope, environment)]
	out := make([]ParameterValueRevision, len(history))
	for i, revision := range history {
		out[i] = cloneParameterValueRevision(revision)
	}
	return out
}

func (s *ParameterValueStore) checkAncestorLocksLocked(change ParameterValueChange) error {
	for _, ancestor := range change.Path[:len(change.Path)-1] {
		history := s.revisions[keyFor(change.Key, ancestor, change.Environment)]
		if len(history) > 0 && history[len(history)-1].Locked {
			return fmt.Errorf("%w: %s/%s", ErrParameterScopeLocked, ancestor.Kind, ancestor.ID)
		}
	}
	return nil
}

func validateParameterChange(change ParameterValueChange, definition ParameterDefinition) error {
	if strings.TrimSpace(change.Key) == "" || change.Key != definition.Key ||
		!validParameterEnvironment(change.Environment) || !scopeKindValid(change.Scope.Kind) ||
		strings.TrimSpace(change.Scope.ID) == "" || strings.TrimSpace(change.Scope.ID) != change.Scope.ID || strings.ContainsRune(change.Scope.ID, '\x00') ||
		strings.TrimSpace(change.Author) == "" || strings.TrimSpace(change.Reason) == "" || change.RecordedAt.IsZero() {
		return fmt.Errorf("%w: identity, environment, author, reason, and recorded time are required", ErrParameterValueInvalid)
	}
	if err := validateParameterScopePath(change.Path); err != nil {
		return err
	}
	if change.Path[len(change.Path)-1] != change.Scope {
		return fmt.Errorf("%w: path must end at the changed scope", ErrParameterScopePathInvalid)
	}
	if change.ValueType.String() != definition.Type.String() {
		return fmt.Errorf("%w: value type %s differs from definition %s", ErrParameterValueTypeMismatch, change.ValueType, definition.Type)
	}
	if definition.SecretReference && !strings.HasPrefix(change.Value, CredentialRefPrefix) {
		return fmt.Errorf("%w: secret parameters require an opaque credential reference", ErrParameterValueInvalid)
	}
	return nil
}

// ValidateParameterValueChange checks a revision against its active typed
// definition. Durable adapters call the same validation before opening writes.
func ValidateParameterValueChange(change ParameterValueChange, definition ParameterDefinition) error {
	return validateParameterChange(change, definition)
}

func validateParameterScopePath(path ParameterScopePath) error {
	if len(path) == 0 || path[0].Kind != ScopeTenant {
		return fmt.Errorf("%w: path must start at a tenant scope", ErrParameterScopePathInvalid)
	}
	seen := make(map[ParameterScope]bool, len(path))
	for _, scope := range path {
		if !scopeKindValid(scope.Kind) || strings.TrimSpace(scope.ID) == "" || strings.TrimSpace(scope.ID) != scope.ID || strings.ContainsRune(scope.ID, '\x00') || seen[scope] {
			return fmt.Errorf("%w: scope entries must be valid and unique", ErrParameterScopePathInvalid)
		}
		seen[scope] = true
	}
	return nil
}

func validParameterEnvironment(environment ParameterEnvironment) bool {
	return environment == EnvironmentSandbox || environment == EnvironmentProduction
}

func scopeKindValid(kind ParameterScopeKind) bool {
	switch kind {
	case ScopeTenant, ScopeCompany, ScopeLegalEntity, ScopeOrganization:
		return true
	default:
		return false
	}
}

func keyFor(key string, scope ParameterScope, environment ParameterEnvironment) parameterValueKey {
	return parameterValueKey{key: key, scopeKind: scope.Kind, scopeID: scope.ID, environment: environment}
}

func cloneParameterValueRevision(revision ParameterValueRevision) ParameterValueRevision {
	revision.ValueType = cloneWorkflowValueType(revision.ValueType)
	return revision
}

func cloneWorkflowValueType(valueType workflow.ValueType) workflow.ValueType {
	if valueType.Element != nil {
		element := cloneWorkflowValueType(*valueType.Element)
		valueType.Element = &element
	}
	return valueType
}

func cloneParameterScopes(scopes []ParameterScope) []ParameterScope {
	return append([]ParameterScope(nil), scopes...)
}
