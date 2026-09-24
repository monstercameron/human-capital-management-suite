package workflowversionstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// DefinitionPublication binds an immutable tenant-authored definition revision
// to the exact compiled record produced from it.
type DefinitionPublication struct {
	TenantID   uuid.UUID
	Definition workflow.Definition
	Options    workflow.Options
	Metadata   version.PublishMeta
}

// PublishedDefinition is the durable authoring form and the compiled version
// minted from it.
type PublishedDefinition struct {
	Definition workflow.Definition
	Compiled   version.CompiledVersion
}

// ActiveDefinition is a tenant-selected definition and its verified compiled
// version/plan. Callers pin Compiled.CompiledPlanDigest on workflow start.
type ActiveDefinition struct {
	Definition workflow.Definition
	Compiled   version.CompiledVersion
	Plan       *workflow.CompiledWorkflow
}

// PublishDefinition compiles the canonical authoring document and persists
// the definition and compiled version atomically under the tenant's RLS scope.
func (s Store) PublishDefinition(ctx context.Context, req DefinitionPublication) (PublishedDefinition, error) {
	d := req.Definition
	d.WorkflowID = strings.TrimSpace(d.WorkflowID)
	d.IntentType = strings.TrimSpace(d.IntentType)
	if s.DB == nil || req.TenantID == uuid.Nil || d.WorkflowID == "" || d.Version == 0 || d.IntentType == "" {
		return PublishedDefinition{}, fmt.Errorf("%w: tenant, workflow id, positive definition version and intent type are required", ErrInvalid)
	}
	for key, value := range d.MatchPredicate {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return PublishedDefinition{}, fmt.Errorf("%w: match predicate keys and values must be non-empty", ErrInvalid)
		}
	}
	raw, err := workflow.Marshal(d)
	if err != nil {
		return PublishedDefinition{}, err
	}
	plan, err := workflow.Compile(d, req.Options)
	if err != nil {
		return PublishedDefinition{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return PublishedDefinition{}, fmt.Errorf("workflowversionstore: begin definition publication: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, req.TenantID); err != nil {
		return PublishedDefinition{}, err
	}
	b := bound{ctx: ctx, ex: tx}
	compiled, err := version.Publish(b, d, plan, req.Options, req.Metadata)
	if err != nil {
		return PublishedDefinition{}, err
	}
	digest := version.DefinitionDigest(d)
	var priorDigest string
	err = tx.QueryRow(ctx, `SELECT definition_digest FROM definition_version
		WHERE tenant_id=$1 AND definition_kind='WORKFLOW' AND definition_key=$2 AND version=$3`,
		req.TenantID, d.WorkflowID, d.Version).Scan(&priorDigest)
	if err == nil {
		if priorDigest != digest {
			return PublishedDefinition{}, fmt.Errorf("%w: tenant workflow revision is immutable", ErrImmutable)
		}
		var boundDigest string
		if err := tx.QueryRow(ctx, `SELECT compiled_plan_digest FROM workflow_definition_compilation
			WHERE tenant_id=$1 AND definition_key=$2 AND definition_version=$3`, req.TenantID, d.WorkflowID, d.Version).Scan(&boundDigest); err != nil {
			return PublishedDefinition{}, fmt.Errorf("workflowversionstore: read definition compilation binding: %w", err)
		}
		if boundDigest != compiled.CompiledPlanDigest {
			return PublishedDefinition{}, fmt.Errorf("%w: definition revision is bound to another compiled plan", ErrImmutable)
		}
	} else if !errors.Is(err, dbport.ErrNoRows) {
		return PublishedDefinition{}, fmt.Errorf("workflowversionstore: read definition revision: %w", err)
	} else {
		var previous *uint32
		var previousVersion uint32
		err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM definition_version
			WHERE tenant_id=$1 AND definition_kind='WORKFLOW' AND definition_key=$2`, req.TenantID, d.WorkflowID).Scan(&previousVersion)
		if err != nil {
			return PublishedDefinition{}, fmt.Errorf("workflowversionstore: read definition lineage: %w", err)
		}
		if previousVersion > 0 {
			if d.Version <= previousVersion {
				return PublishedDefinition{}, fmt.Errorf("%w: definition version %d does not advance %d", ErrInvalid, d.Version, previousVersion)
			}
			previous = &previousVersion
		}
		if _, err := tx.Exec(ctx, `INSERT INTO definition_version
			(tenant_id,definition_kind,definition_key,version,definition_digest,source_ref,body,supersedes_version,published_by,published_at)
			VALUES ($1,'WORKFLOW',$2,$3,$4,$5,$6,$7,$8,$9)`, req.TenantID, d.WorkflowID, d.Version,
			digest, "workflow-definition:"+d.WorkflowID, raw, previous, req.Metadata.PublishedBy, req.Metadata.PublishedAt.UTC()); err != nil {
			return PublishedDefinition{}, fmt.Errorf("workflowversionstore: persist workflow definition: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO workflow_definition_compilation
			(tenant_id,definition_key,definition_version,compiled_plan_digest) VALUES ($1,$2,$3,$4)`,
			req.TenantID, d.WorkflowID, d.Version, compiled.CompiledPlanDigest); err != nil {
			return PublishedDefinition{}, fmt.Errorf("workflowversionstore: bind compiled plan: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PublishedDefinition{}, fmt.Errorf("workflowversionstore: commit definition publication: %w", err)
	}
	return PublishedDefinition{Definition: d, Compiled: compiled}, nil
}

// ActivateTenantDefinition points one tenant's workflow selector at a
// published definition whose compiled version is ACTIVE and approved.
func (s Store) ActivateTenantDefinition(ctx context.Context, tenantID uuid.UUID, workflowID string, definitionVersion uint32, effectiveFrom time.Time) error {
	workflowID = strings.TrimSpace(workflowID)
	if s.DB == nil || tenantID == uuid.Nil || workflowID == "" || definitionVersion == 0 || effectiveFrom.IsZero() {
		return fmt.Errorf("%w: tenant, workflow, definition version and effective instant are required", ErrInvalid)
	}
	err := s.tx(ctx, func(b bound) error {
		if err := tenancy.WithTenant(ctx, b.ex, tenantID); err != nil {
			return err
		}
		var digest string
		if err := b.ex.QueryRow(ctx, `SELECT c.compiled_plan_digest FROM workflow_definition_compilation c
			JOIN workflow_compiled_version v ON v.compiled_plan_digest=c.compiled_plan_digest
			WHERE c.tenant_id=$1 AND c.definition_key=$2 AND c.definition_version=$3 AND v.status='ACTIVE'`,
			tenantID, workflowID, definitionVersion).Scan(&digest); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return fmt.Errorf("%w: definition has no ACTIVE compiled version", ErrInvalid)
			}
			return err
		}
		_, err := b.ex.Exec(ctx, `INSERT INTO definition_active_pointer
			(tenant_id,definition_kind,definition_key,active_version,effective_from,effective_to,pointer_version,updated_at)
			VALUES ($1,'WORKFLOW',$2,$3,$4,NULL,1,$5)
			ON CONFLICT (tenant_id,definition_kind,definition_key) DO UPDATE SET
			active_version=EXCLUDED.active_version,effective_from=EXCLUDED.effective_from,effective_to=NULL,
			pointer_version=definition_active_pointer.pointer_version+1,updated_at=EXCLUDED.updated_at`,
			tenantID, workflowID, definitionVersion, effectiveFrom.UTC(), time.Now().UTC())
		return err
	})
	return err
}

// ResolveActiveDefinition selects the one active tenant definition matching
// the intent type and all exact material facts in its predicate. Ambiguous
// selectors and corrupted definition or compiled bytes fail closed.
func (s Store) ResolveActiveDefinition(ctx context.Context, tenantID uuid.UUID, intentType string, facts map[string]string, at time.Time) (ActiveDefinition, bool, error) {
	if s.DB == nil || tenantID == uuid.Nil || strings.TrimSpace(intentType) == "" || at.IsZero() {
		return ActiveDefinition{}, false, fmt.Errorf("%w: tenant, intent type and resolution instant are required", ErrInvalid)
	}
	var result ActiveDefinition
	found := false
	err := s.tx(ctx, func(b bound) error {
		if err := tenancy.WithTenant(ctx, b.ex, tenantID); err != nil {
			return err
		}
		rows, err := b.ex.Query(ctx, `SELECT d.body,c.compiled_plan_digest,v.record,v.record_digest,v.status
			FROM definition_active_pointer p
			JOIN definition_version d ON d.tenant_id=p.tenant_id AND d.definition_kind=p.definition_kind
				AND d.definition_key=p.definition_key AND d.version=p.active_version
			JOIN workflow_definition_compilation c ON c.tenant_id=d.tenant_id AND c.definition_key=d.definition_key
				AND c.definition_version=d.version
			JOIN workflow_compiled_version v ON v.compiled_plan_digest=c.compiled_plan_digest
			WHERE p.tenant_id=$1 AND p.definition_kind='WORKFLOW' AND p.effective_from <= $2
				AND (p.effective_to IS NULL OR p.effective_to > $2) AND v.status='ACTIVE'
			ORDER BY p.definition_key`, tenantID, at.UTC())
		if err != nil {
			return fmt.Errorf("workflowversionstore: query active definitions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var body, record []byte
			var planDigest, recordDigest, status string
			if err := rows.Scan(&body, &planDigest, &record, &recordDigest, &status); err != nil {
				return err
			}
			var d workflow.Definition
			d, err = workflow.Load(body)
			if err != nil {
				return fmt.Errorf("workflowversionstore: decode stored definition: %w", err)
			}
			if !definitionMatches(d, intentType, facts) {
				continue
			}
			if found {
				return fmt.Errorf("%w: multiple active definitions match intent %s", ErrInvalid, intentType)
			}
			var compiled version.CompiledVersion
			if err := json.Unmarshal(record, &compiled); err != nil {
				return fmt.Errorf("workflowversionstore: decode compiled record: %w", err)
			}
			compiled.Status = version.ActivationStatus(status)
			compiled, err = version.Restore(compiled, recordDigest)
			if err != nil || compiled.CompiledPlanDigest != planDigest {
				return fmt.Errorf("%w: active compiled record does not match its tenant definition", ErrInvalid)
			}
			plan, err := workflow.DecodeCanonicalPlan(compiled.CanonicalPlanBytes)
			if err != nil || plan.Digest() != planDigest || plan.WorkflowID != d.WorkflowID {
				return fmt.Errorf("%w: active canonical plan does not match its definition", ErrInvalid)
			}
			result = ActiveDefinition{Definition: d, Compiled: compiled, Plan: plan}
			found = true
		}
		return rows.Err()
	})
	return result, found, err
}

func definitionMatches(d workflow.Definition, intentType string, facts map[string]string) bool {
	if d.IntentType != intentType {
		return false
	}
	for key, value := range d.MatchPredicate {
		if facts[key] != value {
			return false
		}
	}
	return true
}
