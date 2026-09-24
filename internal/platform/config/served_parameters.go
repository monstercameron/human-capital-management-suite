package config

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var (
	ErrParameterAuthorityUnavailable = errors.New("config: parameter authority unavailable")
	ErrParameterIdentityUnavailable  = errors.New("config: authenticated parameter identity unavailable")
)

// ParameterIdentity is a projection of a verified principal. It is returned
// only by the composition-owned PrincipalResolver; callers never supply it to
// [ServedParameters]. Tenant and subject must come from authenticated context.
type ParameterIdentity struct {
	Tenant   string
	Subject  string
	OrgScope string
}

// PrincipalResolver obtains the authenticated identity from the request or
// execution context. Production composition binds it to trust.FromContext.
type PrincipalResolver interface {
	ResolveParameterIdentity(context.Context) (ParameterIdentity, error)
}

// ParameterDefinitionSource serves the active immutable parameter catalog for
// the authenticated tenant from the running cell's published configuration.
type ParameterDefinitionSource interface {
	LoadParameterSnapshot(context.Context, string) (Snapshot, error)
}

// ParameterScopeAuthority derives a trusted scope path for the authenticated
// actor. It must resolve ancestry from tenant data and authorization state;
// implementations must not echo a path supplied by a request.
type ParameterScopeAuthority interface {
	ResolveParameterPath(context.Context, ParameterIdentity) (ParameterScopePath, error)
	AuthorizeParameterWrite(context.Context, ParameterIdentity, ParameterDefinition, ParameterScope, bool) (ParameterScopePath, error)
}

// ParameterRunEnvironment resolves the environment from server-owned run
// context. It must not accept a caller-selected environment as authority.
type ParameterRunEnvironment interface {
	ResolveParameterEnvironment(context.Context) (ParameterEnvironment, error)
}

// ParameterClock returns trusted server time for authored revision evidence.
type ParameterClock interface {
	Now(context.Context) (time.Time, error)
}

// ParameterRevisionRepository is the durable append-only value store. Tenant
// is passed only after ServedParameters derives it from PrincipalResolver.
type ParameterRevisionRepository interface {
	AppendParameterRevision(context.Context, string, Snapshot, ParameterValueChange) (ParameterValueRevision, error)
	ResolveParameterValue(context.Context, string, Snapshot, string, ParameterScopePath, ParameterEnvironment, Consumer, workflow.ValueType) (ParameterValueResolution, error)
}

// ServedParameters joins the running cell's active definition source to the
// durable revision repository, deriving tenant, actor, hierarchy and
// environment from trusted composition ports.
type ServedParameters struct {
	principal   PrincipalResolver
	definitions ParameterDefinitionSource
	scopes      ParameterScopeAuthority
	environment ParameterRunEnvironment
	clock       ParameterClock
	revisions   ParameterRevisionRepository
}

// NewServedParameters creates the production parameter service. All authority
// dependencies are mandatory; nil dependencies fail closed at construction.
func NewServedParameters(principal PrincipalResolver, definitions ParameterDefinitionSource, scopes ParameterScopeAuthority, environment ParameterRunEnvironment, clock ParameterClock, revisions ParameterRevisionRepository) (*ServedParameters, error) {
	if principal == nil || definitions == nil || scopes == nil || environment == nil || clock == nil || revisions == nil {
		return nil, ErrParameterAuthorityUnavailable
	}
	return &ServedParameters{principal: principal, definitions: definitions, scopes: scopes, environment: environment, clock: clock, revisions: revisions}, nil
}

// Resolve reads one authorized parameter from the authenticated tenant's
// active snapshot, trusted scope path, and server-resolved run environment.
func (s *ServedParameters) Resolve(ctx context.Context, key string, consumer Consumer, requestedType workflow.ValueType) (ParameterValueResolution, error) {
	return s.resolve(ctx, key, consumer, &requestedType)
}

// ResolveDeclared resolves using the active definition's declared type. It is
// intended for fixed application consumers that must not let a request choose
// its own type assertion.
func (s *ServedParameters) ResolveDeclared(ctx context.Context, key string, consumer Consumer) (ParameterValueResolution, error) {
	return s.resolve(ctx, key, consumer, nil)
}

func (s *ServedParameters) resolve(ctx context.Context, key string, consumer Consumer, requestedType *workflow.ValueType) (ParameterValueResolution, error) {
	if s == nil {
		return ParameterValueResolution{}, ErrParameterAuthorityUnavailable
	}
	identity, err := s.principal.ResolveParameterIdentity(ctx)
	if err != nil {
		return ParameterValueResolution{}, fmt.Errorf("%w: %v", ErrParameterIdentityUnavailable, err)
	}
	if err := validateParameterIdentity(identity); err != nil {
		return ParameterValueResolution{}, err
	}
	snapshot, err := s.definitions.LoadParameterSnapshot(ctx, identity.Tenant)
	if err != nil {
		return ParameterValueResolution{}, err
	}
	definition, found := snapshot.ParameterDefinition(key)
	if !found {
		return ParameterValueResolution{}, fmt.Errorf("%w: definition %q is absent", ErrParameterValueInvalid, key)
	}
	readType := definition.Type
	if requestedType != nil {
		readType = *requestedType
	}
	if err := definition.CheckRead(consumer, readType); err != nil {
		return ParameterValueResolution{}, err
	}
	path, err := s.scopes.ResolveParameterPath(ctx, identity)
	if err != nil {
		return ParameterValueResolution{}, err
	}
	if err := validateAuthorizedParameterPath(identity, path, ParameterScope{}); err != nil {
		return ParameterValueResolution{}, err
	}
	environment, err := s.environment.ResolveParameterEnvironment(ctx)
	if err != nil {
		return ParameterValueResolution{}, err
	}
	return s.revisions.ResolveParameterValue(ctx, identity.Tenant, snapshot, key, path, environment, consumer, readType)
}

// Append records a value revision with author and scope taken from trusted
// identity and scope authority. Timestamp remains supplied by the service
// caller's trusted clock so deterministic evidence and replay tests can bind
// it; the durable repository enforces monotonic revisions per tuple.
func (s *ServedParameters) Append(ctx context.Context, change ParameterValueChange) (ParameterValueRevision, error) {
	if s == nil {
		return ParameterValueRevision{}, ErrParameterAuthorityUnavailable
	}
	identity, err := s.principal.ResolveParameterIdentity(ctx)
	if err != nil {
		return ParameterValueRevision{}, fmt.Errorf("%w: %v", ErrParameterIdentityUnavailable, err)
	}
	if err := validateParameterIdentity(identity); err != nil {
		return ParameterValueRevision{}, err
	}
	snapshot, err := s.definitions.LoadParameterSnapshot(ctx, identity.Tenant)
	if err != nil {
		return ParameterValueRevision{}, err
	}
	definition, ok := snapshot.ParameterDefinition(change.Key)
	if !ok {
		return ParameterValueRevision{}, fmt.Errorf("%w: definition %q is absent", ErrParameterValueInvalid, change.Key)
	}
	change.ValueType = definition.Type
	path, err := s.scopes.AuthorizeParameterWrite(ctx, identity, definition, change.Scope, change.Locked)
	if err != nil {
		return ParameterValueRevision{}, err
	}
	if err := validateAuthorizedParameterPath(identity, path, change.Scope); err != nil {
		return ParameterValueRevision{}, err
	}
	environment, err := s.environment.ResolveParameterEnvironment(ctx)
	if err != nil {
		return ParameterValueRevision{}, err
	}
	now, err := s.clock.Now(ctx)
	if err != nil {
		return ParameterValueRevision{}, err
	}
	if now.IsZero() {
		return ParameterValueRevision{}, ErrParameterAuthorityUnavailable
	}
	change.Path = cloneParameterScopes(path)
	change.Environment = environment
	change.Author = identity.Subject
	change.RecordedAt = now
	return s.revisions.AppendParameterRevision(ctx, identity.Tenant, snapshot, change)
}

func validateParameterIdentity(identity ParameterIdentity) error {
	if identity.Tenant == "" || identity.Subject == "" {
		return ErrParameterIdentityUnavailable
	}
	return nil
}

func validateAuthorizedParameterPath(identity ParameterIdentity, path ParameterScopePath, target ParameterScope) error {
	if err := validateParameterScopePath(path); err != nil {
		return err
	}
	if path[0] != (ParameterScope{Kind: ScopeTenant, ID: identity.Tenant}) {
		return fmt.Errorf("%w: scope authority returned a path outside the authenticated tenant", ErrParameterScopePathInvalid)
	}
	if target.Kind != "" && path[len(path)-1] != target {
		return fmt.Errorf("%w: authorized write path does not terminate at the requested scope", ErrParameterScopePathInvalid)
	}
	return nil
}
