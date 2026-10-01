package agentpersonastore

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaReferenceInstallation is the minimum tenant-scoped durable state
// needed to validate a registered persona reference for one conversation.
type PersonaReferenceInstallation struct {
	TenantID          values.TenantId
	InstallationID    string
	PersonaID         string
	PersonaVersion    int64
	CurrentVersion    int64
	ConversationID    string
	InstallationState InstallationState
	Lifecycle         LifecycleState
}

// LookupCurrentReferenceInstallation returns the sole active installation
// for personaID in conversationID only when the latest published persona
// version is current. Missing, stale, suspended, or ambiguous state fails
// closed as ErrNotFound or ErrConflict.
func (s *TenantStore) LookupCurrentReferenceInstallation(ctx context.Context, personaID, conversationID string) (PersonaReferenceInstallation, error) {
	if s == nil || strings.TrimSpace(personaID) == "" || strings.TrimSpace(conversationID) == "" {
		return PersonaReferenceInstallation{}, fmt.Errorf("%w: persona and conversation are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaReferenceInstallation{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH current_lifecycle AS (
		SELECT DISTINCT ON (e.persona_id,e.persona_version) e.persona_id,e.persona_version,e.to_state,e.profile_digest,e.review_digest,e.reviewer_id,e.evaluation_digest,e.evaluation_profile_digest,e.evaluation_suite_digest
		FROM persona_lifecycle_events e
		WHERE e.tenant_id=$1
		ORDER BY e.persona_id,e.persona_version,e.event_sequence DESC
	), current_versions AS (
		SELECT DISTINCT ON (v.persona_id) v.persona_id,v.version
		FROM persona_versions v
		JOIN current_lifecycle l ON l.persona_id=v.persona_id AND l.persona_version=v.version
		WHERE v.tenant_id=$1 AND v.persona_id=$2 AND l.to_state='PUBLISHED' AND l.profile_digest=v.content_digest AND l.profile_digest<>'' AND l.review_digest<>'' AND l.reviewer_id<>'' AND l.evaluation_digest<>'' AND l.evaluation_profile_digest=l.profile_digest AND l.evaluation_suite_digest<>''
		ORDER BY v.persona_id,v.version DESC
	)
	SELECT i.installation_id,i.persona_id,i.persona_version,i.conversation_id,i.state,l.to_state,v.version
	FROM persona_installations i
	JOIN current_versions v ON v.persona_id=i.persona_id AND v.version=i.persona_version
	JOIN current_lifecycle l ON l.persona_id=i.persona_id AND l.persona_version=i.persona_version
	WHERE i.tenant_id=$1 AND i.persona_id=$2 AND i.conversation_id=$3
	AND i.state='ACTIVE' AND l.to_state='PUBLISHED' AND l.profile_digest<>'' AND l.review_digest<>'' AND l.reviewer_id<>'' AND l.evaluation_digest<>'' AND l.evaluation_profile_digest=l.profile_digest AND l.evaluation_suite_digest<>''
	ORDER BY i.installation_id`, s.tenantID, personaID, conversationID)
	if err != nil {
		return PersonaReferenceInstallation{}, fmt.Errorf("agentpersonastore: lookup persona reference: %w", err)
	}
	defer rows.Close()
	var result PersonaReferenceInstallation
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return PersonaReferenceInstallation{}, fmt.Errorf("%w: ambiguous active persona installation", ErrConflict)
		}
		if err := rows.Scan(&result.InstallationID, &result.PersonaID, &result.PersonaVersion, &result.ConversationID, &result.InstallationState, &result.Lifecycle, &result.CurrentVersion); err != nil {
			return PersonaReferenceInstallation{}, fmt.Errorf("agentpersonastore: scan reference installation: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return PersonaReferenceInstallation{}, err
	}
	if count == 0 {
		return PersonaReferenceInstallation{}, fmt.Errorf("%w: current published installation", ErrNotFound)
	}
	result.TenantID = s.tenant
	if err := commit(ctx, tx); err != nil {
		return PersonaReferenceInstallation{}, err
	}
	return result, nil
}
