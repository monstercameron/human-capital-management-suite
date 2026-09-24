// Package configparamstore persists WF-DATA-037 tenant parameter revisions.
package configparamstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var ErrUnavailable = errors.New("configparamstore: unavailable")

// Store persists immutable parameter value revisions under database RLS.
// TenantUUID maps the authenticated logical tenant key to its physical UUID.
type Store struct {
	db         dbport.Beginner
	tenantUUID func(string) uuid.UUID
}

var _ config.ParameterRevisionRepository = (*Store)(nil)

func New(db dbport.Beginner, tenantUUID func(string) uuid.UUID) *Store {
	return &Store{db: db, tenantUUID: tenantUUID}
}

func (s *Store) AppendParameterRevision(ctx context.Context, tenant string, snapshot config.Snapshot, change config.ParameterValueChange) (config.ParameterValueRevision, error) {
	if s == nil || s.db == nil {
		return config.ParameterValueRevision{}, ErrUnavailable
	}
	definition, ok := snapshot.ParameterDefinition(change.Key)
	if !ok {
		return config.ParameterValueRevision{}, fmt.Errorf("configparamstore: parameter definition %q is absent", change.Key)
	}
	if err := config.ValidateParameterValueChange(change, definition); err != nil {
		return config.ParameterValueRevision{}, err
	}
	tenantID, err := s.resolveTenant(tenant)
	if err != nil {
		return config.ParameterValueRevision{}, err
	}
	if len(change.Path) == 0 || change.Path[0] != (config.ParameterScope{Kind: config.ScopeTenant, ID: tenant}) {
		return config.ParameterValueRevision{}, config.ErrParameterScopePathInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return config.ParameterValueRevision{}, fmt.Errorf("configparamstore: begin append: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return config.ParameterValueRevision{}, err
	}
	// Lock the tenant-to-target chain in order. A descendant append and an
	// ancestor lock change therefore serialize on the same ancestor key.
	for _, scope := range change.Path {
		lockKey := parameterLockKey(tenantID, change.Key, scope, change.Environment)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
			return config.ParameterValueRevision{}, err
		}
	}
	for _, ancestor := range change.Path[:len(change.Path)-1] {
		var locked bool
		err := tx.QueryRow(ctx, `SELECT locked FROM tenant_parameter_value_revision WHERE tenant_id=$1 AND parameter_key=$2 AND scope_kind=$3 AND scope_id=$4 AND environment=$5 ORDER BY revision DESC LIMIT 1`, tenantID, change.Key, ancestor.Kind, ancestor.ID, change.Environment).Scan(&locked)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return config.ParameterValueRevision{}, err
		}
		if err == nil && locked {
			return config.ParameterValueRevision{}, config.ErrParameterScopeLocked
		}
	}
	var current uint64
	var last time.Time
	err = tx.QueryRow(ctx, `SELECT revision,recorded_at FROM tenant_parameter_value_revision WHERE tenant_id=$1 AND parameter_key=$2 AND scope_kind=$3 AND scope_id=$4 AND environment=$5 ORDER BY revision DESC LIMIT 1`, tenantID, change.Key, change.Scope.Kind, change.Scope.ID, change.Environment).Scan(&current, &last)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return config.ParameterValueRevision{}, err
	}
	if err == nil && !change.RecordedAt.After(last) {
		return config.ParameterValueRevision{}, fmt.Errorf("%w: recorded time must advance beyond revision %d", config.ErrParameterValueInvalid, current)
	}
	if change.ExpectedRevision != current {
		return config.ParameterValueRevision{}, fmt.Errorf("%w: expected %d, current %d", config.ErrParameterRevisionConflict, change.ExpectedRevision, current)
	}
	typ, err := json.Marshal(definition.Type)
	if err != nil {
		return config.ParameterValueRevision{}, err
	}
	revision := config.ParameterValueRevision{
		Key: change.Key, DefinitionName: snapshot.Name, DefinitionVersion: snapshot.Version,
		Scope: change.Scope, Environment: change.Environment, Revision: current + 1,
		Value: change.Value, ValueType: definition.Type, Author: change.Author, Reason: change.Reason,
		RecordedAt: change.RecordedAt.UTC(), Locked: change.Locked,
	}
	_, err = tx.Exec(ctx, `INSERT INTO tenant_parameter_value_revision (tenant_id,parameter_key,scope_kind,scope_id,environment,revision,value_text,value_type,definition_name,definition_version,author,reason,recorded_at,locked) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14)`, tenantID, change.Key, change.Scope.Kind, change.Scope.ID, change.Environment, revision.Revision, change.Value, string(typ), snapshot.Name, snapshot.Version, change.Author, change.Reason, revision.RecordedAt, change.Locked)
	if err != nil {
		return config.ParameterValueRevision{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return config.ParameterValueRevision{}, fmt.Errorf("configparamstore: commit append: %w", err)
	}
	return revision, nil
}

func (s *Store) ResolveParameterValue(ctx context.Context, tenant string, snapshot config.Snapshot, key string, path config.ParameterScopePath, environment config.ParameterEnvironment, consumer config.Consumer, requestedType workflow.ValueType) (config.ParameterValueResolution, error) {
	if s == nil || s.db == nil {
		return config.ParameterValueResolution{}, ErrUnavailable
	}
	definition, found := snapshot.ParameterDefinition(key)
	if !found {
		return config.ParameterValueResolution{}, fmt.Errorf("configparamstore: parameter definition %q is absent", key)
	}
	if err := definition.CheckRead(consumer, requestedType); err != nil {
		return config.ParameterValueResolution{}, err
	}
	if !validEnvironment(environment) {
		return config.ParameterValueResolution{}, fmt.Errorf("%w: unsupported environment %q", config.ErrParameterValueInvalid, environment)
	}
	tenantID, err := s.resolveTenant(tenant)
	if err != nil {
		return config.ParameterValueResolution{}, err
	}
	if len(path) == 0 || path[0] != (config.ParameterScope{Kind: config.ScopeTenant, ID: tenant}) {
		return config.ParameterValueResolution{}, config.ErrParameterScopePathInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return config.ParameterValueResolution{}, fmt.Errorf("configparamstore: begin resolve: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return config.ParameterValueResolution{}, err
	}
	revisions := make([]config.ParameterValueRevision, 0, len(path))
	for _, scope := range path {
		var revision config.ParameterValueRevision
		var valueType []byte
		err := tx.QueryRow(ctx, `SELECT parameter_key,definition_name,definition_version,scope_kind,scope_id,environment,revision,value_text,value_type,author,reason,recorded_at,locked FROM tenant_parameter_value_revision WHERE tenant_id=$1 AND parameter_key=$2 AND scope_kind=$3 AND scope_id=$4 AND environment=$5 ORDER BY revision DESC LIMIT 1`, tenantID, key, scope.Kind, scope.ID, environment).Scan(&revision.Key, &revision.DefinitionName, &revision.DefinitionVersion, &revision.Scope.Kind, &revision.Scope.ID, &revision.Environment, &revision.Revision, &revision.Value, &valueType, &revision.Author, &revision.Reason, &revision.RecordedAt, &revision.Locked)
		if errors.Is(err, dbport.ErrNoRows) {
			continue
		}
		if err != nil {
			return config.ParameterValueResolution{}, err
		}
		if err := json.Unmarshal(valueType, &revision.ValueType); err != nil {
			return config.ParameterValueResolution{}, fmt.Errorf("configparamstore: decode parameter value type: %w", err)
		}
		revisions = append(revisions, revision)
	}
	if err := tx.Commit(ctx); err != nil {
		return config.ParameterValueResolution{}, fmt.Errorf("configparamstore: commit resolve: %w", err)
	}
	return config.ResolveParameterRevisions(snapshot, key, path, environment, consumer, requestedType, revisions)
}

func validEnvironment(environment config.ParameterEnvironment) bool {
	return environment == config.EnvironmentSandbox || environment == config.EnvironmentProduction
}

func parameterLockKey(tenant uuid.UUID, key string, scope config.ParameterScope, environment config.ParameterEnvironment) string {
	parts := []string{tenant.String(), key, string(scope.Kind), scope.ID, string(environment)}
	var lockKey string
	for _, part := range parts {
		lockKey += fmt.Sprintf("%d:%s", len(part), part)
	}
	return lockKey
}

func (s *Store) resolveTenant(key string) (uuid.UUID, error) {
	if s == nil || s.tenantUUID == nil {
		return uuid.Nil, ErrUnavailable
	}
	id := s.tenantUUID(key)
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("configparamstore: logical tenant %q is not mapped", key)
	}
	return id, nil
}
