package agentpersonastore

import (
	"context"
	"fmt"
	"strings"
)

// AvailableInstallation is a trusted viewer-eligible installation tuple. It
// must come from current conversation membership and audience resolution.
type AvailableInstallation struct {
	PersonaID      string
	PersonaVersion int64
	InstallationID string
	ConversationID string
}

// ListAvailable returns each exact published persona version when that
// exact installation tuple is active in the tenant. An empty set returns no
// candidates. The query remains tenant-scoped through both the explicit
// predicate and the transaction's RLS setting.
func (s *TenantStore) ListAvailable(ctx context.Context, allowed []AvailableInstallation) ([]PersonaVersion, error) {
	if s == nil || ctx == nil {
		return nil, fmt.Errorf("%w: available persona context and store are required", ErrInvalid)
	}
	allowed = normalizeAvailableInstallations(allowed)
	if len(allowed) == 0 {
		return []PersonaVersion{}, nil
	}
	personaIDs := make([]string, 0, len(allowed))
	versions := make([]int64, 0, len(allowed))
	installationIDs := make([]string, 0, len(allowed))
	conversationIDs := make([]string, 0, len(allowed))
	for _, item := range allowed {
		personaIDs = append(personaIDs, item.PersonaID)
		versions = append(versions, item.PersonaVersion)
		installationIDs = append(installationIDs, item.InstallationID)
		conversationIDs = append(conversationIDs, item.ConversationID)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH allowed(persona_id,persona_version,installation_id,conversation_id) AS (
		SELECT * FROM unnest($2::text[],$3::bigint[],$4::text[],$5::text[])
	), current_lifecycle AS (
		SELECT DISTINCT ON (persona_id,persona_version)
			persona_id,persona_version,to_state,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest
		FROM persona_lifecycle_events
		WHERE tenant_id=$1
		ORDER BY persona_id,persona_version,event_sequence DESC
	), current_versions AS (
		SELECT v.*,l.to_state
		FROM persona_versions v
		JOIN current_lifecycle l ON l.persona_id=v.persona_id AND l.persona_version=v.version
		WHERE v.tenant_id=$1 AND v.persona_id=ANY($2::text[]) AND l.to_state='PUBLISHED' AND l.profile_digest=v.content_digest AND l.profile_digest<>'' AND l.review_digest<>'' AND l.reviewer_id<>'' AND l.evaluation_digest<>'' AND l.evaluation_profile_digest=l.profile_digest AND l.evaluation_suite_digest<>''
	)
	SELECT DISTINCT ON (v.persona_id COLLATE "C",v.version) v.persona_id,v.version,v.agent_version,v.handle,v.display_name,v.profile,v.content_digest,v.created_at
	FROM current_versions v
	JOIN allowed a ON a.persona_id=v.persona_id AND a.persona_version=v.version
	JOIN persona_installations i ON i.tenant_id=$1 AND i.persona_id=a.persona_id AND i.persona_version=a.persona_version AND i.installation_id=a.installation_id AND i.conversation_id=a.conversation_id AND i.state='ACTIVE'
	WHERE v.to_state='PUBLISHED'
	ORDER BY v.persona_id COLLATE "C",v.version`, s.tenantID, personaIDs, versions, installationIDs, conversationIDs)
	if err != nil {
		return nil, fmt.Errorf("agentpersonastore: query available personas: %w", err)
	}
	defer rows.Close()
	out := make([]PersonaVersion, 0, len(allowed))
	for rows.Next() {
		item, err := scanVersion(rows, s.tenant)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentpersonastore: read available personas: %w", err)
	}
	if err := commit(ctx, tx); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeAvailableInstallations(items []AvailableInstallation) []AvailableInstallation {
	seen := make(map[AvailableInstallation]struct{}, len(items))
	out := make([]AvailableInstallation, 0, len(items))
	for _, item := range items {
		item.PersonaID = strings.TrimSpace(item.PersonaID)
		item.InstallationID = strings.TrimSpace(item.InstallationID)
		item.ConversationID = strings.TrimSpace(item.ConversationID)
		if item.PersonaID == "" || item.PersonaVersion <= 0 || item.InstallationID == "" || item.ConversationID == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
