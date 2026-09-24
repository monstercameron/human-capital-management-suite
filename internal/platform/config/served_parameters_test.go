package config

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

type servedIdentityStub struct {
	identity ParameterIdentity
	err      error
}

func (s servedIdentityStub) ResolveParameterIdentity(context.Context) (ParameterIdentity, error) {
	return s.identity, s.err
}

type servedDefinitionStub struct {
	tenant   string
	snapshot Snapshot
	err      error
}

func (s *servedDefinitionStub) LoadParameterSnapshot(_ context.Context, tenant string) (Snapshot, error) {
	s.tenant = tenant
	return s.snapshot, s.err
}

type servedScopeStub struct {
	identity ParameterIdentity
	path     ParameterScopePath
	err      error
}

func (s *servedScopeStub) ResolveParameterPath(_ context.Context, identity ParameterIdentity) (ParameterScopePath, error) {
	s.identity = identity
	return cloneParameterScopes(s.path), s.err
}
func (s *servedScopeStub) AuthorizeParameterWrite(_ context.Context, identity ParameterIdentity, _ ParameterDefinition, scope ParameterScope, _ bool) (ParameterScopePath, error) {
	s.identity = identity
	for i, candidate := range s.path {
		if candidate == scope {
			return cloneParameterScopes(s.path[:i+1]), s.err
		}
	}
	return nil, ErrParameterScopePathInvalid
}

type servedEnvironmentStub struct {
	environment ParameterEnvironment
	err         error
}

func (s servedEnvironmentStub) ResolveParameterEnvironment(context.Context) (ParameterEnvironment, error) {
	return s.environment, s.err
}

type servedClockStub struct {
	at  time.Time
	err error
}

func (s servedClockStub) Now(context.Context) (time.Time, error) { return s.at, s.err }

type servedRepositoryStub struct {
	store  *ParameterValueStore
	tenant string
}

func (s *servedRepositoryStub) AppendParameterRevision(_ context.Context, tenant string, snapshot Snapshot, change ParameterValueChange) (ParameterValueRevision, error) {
	s.tenant = tenant
	return s.store.Append(snapshot, change)
}
func (s *servedRepositoryStub) ResolveParameterValue(_ context.Context, tenant string, snapshot Snapshot, key string, path ParameterScopePath, environment ParameterEnvironment, consumer Consumer, typ workflow.ValueType) (ParameterValueResolution, error) {
	s.tenant = tenant
	return s.store.Resolve(snapshot, key, path, environment, consumer, typ)
}

func TestTodo_WF_DATA_036_ServedSnapshotFromAuthenticatedCell(t *testing.T) {
	typ := workflow.ValueType{Kind: workflow.KindString}
	consumer := Consumer{Kind: ConsumerWorkflow, ID: "workflow.payroll"}
	definition := ParameterDefinition{Key: "payroll.currency", Type: typ, Classification: "TENANT", Owner: "payroll", Required: true, AllowedConsumers: []Consumer{consumer}}
	snapshot, err := NewSnapshotWithDefinitions("active", "published-v3", nil, []ParameterDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	identity := ParameterIdentity{Tenant: "tenant-authenticated", Subject: "subject-1", OrgScope: "org-9"}
	path := ParameterScopePath{{Kind: ScopeTenant, ID: identity.Tenant}, {Kind: ScopeOrganization, ID: identity.OrgScope}}
	definitions := &servedDefinitionStub{snapshot: snapshot}
	scopes := &servedScopeStub{path: path}
	repository := &servedRepositoryStub{store: NewParameterValueStore()}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	service, err := NewServedParameters(servedIdentityStub{identity: identity}, definitions, scopes, servedEnvironmentStub{environment: EnvironmentSandbox}, servedClockStub{at: at}, repository)
	if err != nil {
		t.Fatal(err)
	}

	change := ParameterValueChange{Key: definition.Key, Scope: path[0], Value: "CAD", ValueType: workflow.ValueType{Kind: workflow.KindBool}, Reason: "sandbox setup", RecordedAt: time.Time{}}
	if _, err := service.Append(context.Background(), change); err != nil {
		t.Fatal(err)
	}
	got, err := service.ResolveDeclared(context.Background(), definition.Key, consumer)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "CAD" || got.Environment != EnvironmentSandbox || definitions.tenant != identity.Tenant || repository.tenant != identity.Tenant {
		t.Fatalf("served value=%+v definition tenant=%q repository tenant=%q", got, definitions.tenant, repository.tenant)
	}
	revision := repository.store.Revisions(definition.Key, path[0], EnvironmentSandbox)[0]
	if scopes.identity.Subject != identity.Subject || revision.Author != identity.Subject || revision.RecordedAt != at || revision.Environment != EnvironmentSandbox {
		t.Fatal("scope resolver or revision author did not use the authenticated identity")
	}
}

func TestTodo_WF_DATA_037_ServedParametersFailClosedOnMissingAuthority(t *testing.T) {
	if _, err := NewServedParameters(nil, nil, nil, nil, nil, nil); !errors.Is(err, ErrParameterAuthorityUnavailable) {
		t.Fatalf("incomplete constructor error=%v", err)
	}
	typ := workflow.ValueType{Kind: workflow.KindString}
	snapshot, err := NewSnapshotWithDefinitions("active", "v1", nil, []ParameterDefinition{{Key: "payroll.currency", Type: typ, Classification: "TENANT", Owner: "payroll", AllowedConsumers: []Consumer{{Kind: ConsumerWorkflow, ID: "workflow.payroll"}}}})
	if err != nil {
		t.Fatal(err)
	}
	definitions := &servedDefinitionStub{snapshot: snapshot}
	service, err := NewServedParameters(servedIdentityStub{err: errors.New("no authenticated principal")}, definitions, &servedScopeStub{}, servedEnvironmentStub{}, servedClockStub{}, &servedRepositoryStub{store: NewParameterValueStore()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(context.Background(), "payroll.currency", Consumer{Kind: ConsumerWorkflow, ID: "workflow.payroll"}, typ); !errors.Is(err, ErrParameterIdentityUnavailable) {
		t.Fatalf("missing principal resolve error=%v", err)
	}
	if definitions.tenant != "" {
		t.Fatalf("definition source was called before authentication with tenant %q", definitions.tenant)
	}
}

func TestTodo_WF_DATA_037_Security_RejectForeignResolvedScope(t *testing.T) {
	typ := workflow.ValueType{Kind: workflow.KindString}
	consumer := Consumer{Kind: ConsumerWorkflow, ID: "workflow.payroll"}
	snapshot, err := NewSnapshotWithDefinitions("active", "v1", nil, []ParameterDefinition{{Key: "payroll.currency", Type: typ, Classification: "TENANT", Owner: "payroll", AllowedConsumers: []Consumer{consumer}}})
	if err != nil {
		t.Fatal(err)
	}
	definitions := &servedDefinitionStub{snapshot: snapshot}
	repository := &servedRepositoryStub{store: NewParameterValueStore()}
	service, err := NewServedParameters(
		servedIdentityStub{identity: ParameterIdentity{Tenant: "tenant-a", Subject: "subject-a"}},
		definitions,
		&servedScopeStub{path: ParameterScopePath{{Kind: ScopeTenant, ID: "tenant-b"}}},
		servedEnvironmentStub{environment: EnvironmentSandbox}, servedClockStub{at: time.Now()}, repository,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Resolve(context.Background(), "payroll.currency", consumer, typ); !errors.Is(err, ErrParameterScopePathInvalid) {
		t.Fatalf("foreign scope path error=%v, want ErrParameterScopePathInvalid", err)
	}
	if repository.tenant != "" {
		t.Fatalf("repository reached with tenant %q despite foreign path", repository.tenant)
	}
}
