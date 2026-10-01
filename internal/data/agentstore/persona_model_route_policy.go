package agentstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	// ErrPersonaModelRouteNotFound marks the absence of a current tenant route policy.
	ErrPersonaModelRouteNotFound = errors.New("agentstore: persona model route policy not found")
	// ErrPersonaModelRouteAmbiguous marks overlapping policy revisions.
	ErrPersonaModelRouteAmbiguous = errors.New("agentstore: persona model route policy is ambiguous")
)

// PersonaModelRoutePolicy is one immutable exact-manifest route authority.
type PersonaModelRoutePolicy struct {
	TenantID            uuid.UUID
	LegalEntityID       string
	PolicyID            string
	PolicyVersion       int64
	PolicySchemaVersion int64
	PolicyDigest        string
	Revision            int64
	EffectiveFrom       time.Time
	EffectiveUntil      *time.Time
	RoutePayload        []byte
	RoutePayloadDigest  string
	PolicyPayload       []byte
	AgentVersionDigest  string
}

// CurrentPersonaModelRoutePolicy resolves one policy revision in the tenant's
// isolated database. Missing and overlapping rows fail closed.
func (s *Store) CurrentPersonaModelRoutePolicy(ctx context.Context, tenantID uuid.UUID, entity, policyID string, policyVersion, policySchemaVersion int64, policyDigest string, at time.Time) (PersonaModelRoutePolicy, error) {
	return s.currentPersonaModelRoutePolicy(ctx, tenantID, entity, policyID, policyVersion, policySchemaVersion, policyDigest, "", at)
}

// CurrentPersonaModelRoutePolicyForAgent resolves only the exact admitted manifest deployment.
func (s *Store) CurrentPersonaModelRoutePolicyForAgent(ctx context.Context, tenantID uuid.UUID, entity, policyID string, policyVersion, policySchemaVersion int64, policyDigest, agentVersionDigest string, at time.Time) (PersonaModelRoutePolicy, error) {
	if !validStoreDigest(agentVersionDigest) {
		return PersonaModelRoutePolicy{}, ErrInvalidConfig
	}
	return s.currentPersonaModelRoutePolicy(ctx, tenantID, entity, policyID, policyVersion, policySchemaVersion, policyDigest, agentVersionDigest, at)
}

func (s *Store) currentPersonaModelRoutePolicy(ctx context.Context, tenantID uuid.UUID, entity, policyID string, policyVersion, policySchemaVersion int64, policyDigest, agentVersionDigest string, at time.Time) (PersonaModelRoutePolicy, error) {
	if s == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(entity) == "" || strings.TrimSpace(entity) != entity ||
		strings.TrimSpace(policyID) == "" || strings.TrimSpace(policyID) != policyID || policyVersion <= 0 || policySchemaVersion <= 0 || !validStoreDigest(policyDigest) || at.IsZero() {
		return PersonaModelRoutePolicy{}, fmt.Errorf("%w: tenant, entity, exact policy reference and time are required", ErrInvalidConfig)
	}
	var policy PersonaModelRoutePolicy
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		query := `SELECT policy_digest,revision,effective_from,effective_until,route_payload,COALESCE(route_payload_digest,''),policy_payload,''
			FROM persona_model_route_policy
			WHERE tenant_id=$1 AND legal_entity_id=$2 AND policy_id=$3 AND policy_version=$4 AND policy_schema_version=$5
			  AND effective_from <= $6 AND (effective_until IS NULL OR effective_until > $6)
			ORDER BY effective_from DESC LIMIT 2`
		args := []any{tenantID, entity, policyID, policyVersion, policySchemaVersion, at.UTC()}
		if agentVersionDigest != "" {
			query = `SELECT policy_digest,revision,effective_from,effective_until,route_payload,route_payload_digest,policy_payload,agent_version_digest
				FROM persona_model_route_deployment WHERE tenant_id=$1 AND legal_entity_id=$2 AND policy_id=$3 AND policy_version=$4 AND policy_schema_version=$5
				AND effective_from<=$6 AND effective_until>$6 AND agent_version_digest=$7 ORDER BY effective_from DESC LIMIT 2`
			args = append(args, agentVersionDigest)
		}
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("agentstore: query persona model route policy: %w", err)
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return ErrPersonaModelRouteNotFound
		}
		if err := rows.Scan(&policy.PolicyDigest, &policy.Revision, &policy.EffectiveFrom, &policy.EffectiveUntil, &policy.RoutePayload, &policy.RoutePayloadDigest, &policy.PolicyPayload, &policy.AgentVersionDigest); err != nil {
			return err
		}
		if rows.Next() {
			return ErrPersonaModelRouteAmbiguous
		}
		return rows.Err()
	})
	if err != nil {
		return PersonaModelRoutePolicy{}, err
	}
	policy.TenantID, policy.LegalEntityID, policy.PolicyID, policy.PolicyVersion, policy.PolicySchemaVersion = tenantID, entity, policyID, policyVersion, policySchemaVersion
	policy.EffectiveFrom = policy.EffectiveFrom.UTC()
	if policy.EffectiveUntil != nil {
		value := policy.EffectiveUntil.UTC()
		policy.EffectiveUntil = &value
	}
	if policy.PolicyDigest != policyDigest || policy.Revision <= 0 || len(policy.RoutePayload) == 0 || !jsonObject(policy.RoutePayload) {
		return PersonaModelRoutePolicy{}, fmt.Errorf("%w: persisted route authority is inconsistent", ErrInvalidConfig)
	}
	route, routeDigest, routeErr := CanonicalPersonaModelRoutePayload(policy.RoutePayload)
	semantic, semanticDigest, semanticErr := CanonicalPersonaModelRoutePayload(policy.PolicyPayload)
	if routeErr != nil || semanticErr != nil || routeDigest != policy.RoutePayloadDigest || semanticDigest != policy.PolicyDigest ||
		!personaPolicyIdentity(semantic, policyID, policyVersion, policySchemaVersion) {
		return PersonaModelRoutePolicy{}, fmt.Errorf("%w: persisted route or semantic policy digest is inconsistent", ErrInvalidConfig)
	}
	policy.RoutePayload, policy.PolicyPayload = route, semantic
	if agentVersionDigest != "" {
		pinned, err := personaRouteManifestDigest(route)
		if err != nil || pinned != agentVersionDigest || policy.AgentVersionDigest != agentVersionDigest || policy.EffectiveUntil == nil {
			return PersonaModelRoutePolicy{}, fmt.Errorf("%w: persisted manifest deployment is inconsistent", ErrInvalidConfig)
		}
	}
	return policy, nil
}

func validStoreDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, c := range value[7:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func jsonObject(value []byte) bool {
	return len(value) > 1 && value[0] == '{' && value[len(value)-1] == '}'
}
