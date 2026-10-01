package agentpersonastore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// InstallationAudience is the audience declaration carried by the exact
// persona version selected by an installation.
type InstallationAudience struct {
	Roles             []string `json:"roles"`
	Populations       []string `json:"populations"`
	OrganizationScope []string `json:"organization_scopes"`
}

// ActiveInstallation is the viewer-safe projection of one active placement.
// Every field is read from the same tenant-scoped transaction, so callers do
// not have to combine an installation row with a possibly newer version.
type ActiveInstallation struct {
	InstallationID    string
	PersonaID         string
	PersonaVersion    int64
	AgentVersion      string
	ConversationID    string
	ConversationClass ConversationClass
	InstallerID       string
	ChannelPolicy     ChannelPolicy
	Revision          int64
	RevocationEpoch   int64
	Audience          InstallationAudience
}

// ListVersionRolloutInstallations returns every active installation for a
// persona, including placements on versions other than the current published
// one. Rollout preview uses this exact tenant-scoped inventory.
func (s *TenantStore) ListVersionRolloutInstallations(ctx context.Context, personaID string) ([]PersonaInstallation, error) {
	if s == nil || ctx == nil || strings.TrimSpace(personaID) == "" {
		return nil, fmt.Errorf("%w: context, tenant store, and persona are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at
		FROM persona_installations WHERE tenant_id=$1 AND persona_id=$2 AND state='ACTIVE' ORDER BY installation_id COLLATE "C"`, s.tenantID, personaID)
	if err != nil {
		return nil, fmt.Errorf("agentpersonastore: list rollout installations: %w", err)
	}
	defer rows.Close()
	out := make([]PersonaInstallation, 0)
	for rows.Next() {
		var item PersonaInstallation
		var policy []byte
		if err := rows.Scan(&item.InstallationID, &item.PersonaID, &item.PersonaVersion, &item.ConversationID, &item.ConversationClass, &item.InstallerID, &policy, &item.State, &item.SuspensionReason, &item.Revision, &item.RevocationEpoch, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(policy, &item.ChannelPolicy); err != nil {
			return nil, fmt.Errorf("agentpersonastore: decode rollout installation policy: %w", err)
		}
		item.TenantID, item.CreatedAt, item.UpdatedAt = s.tenant, item.CreatedAt.UTC(), item.UpdatedAt.UTC()
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := commit(ctx, tx); err != nil {
		return nil, err
	}
	return out, nil
}

// ListActiveInstallations returns active installations in one conversation.
// The conversation predicate is tenant-bound and the version join is exact;
// an installation from another tenant or a missing version cannot appear.
func (s *TenantStore) ListActiveInstallations(ctx context.Context, conversationID string) ([]ActiveInstallation, error) {
	if s == nil || ctx == nil || strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("%w: context, tenant store, and conversation are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH current_lifecycle AS (
		SELECT DISTINCT ON (persona_id,persona_version) persona_id,persona_version,to_state,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest
		FROM persona_lifecycle_events WHERE tenant_id=$1
		ORDER BY persona_id,persona_version,event_sequence DESC
	)
	SELECT i.installation_id,i.persona_id,i.persona_version,
		i.conversation_id,i.conversation_class,i.installer_id,i.channel_policy,
		i.revision,i.revocation_epoch,v.agent_version,v.profile->'audience'
		FROM persona_installations i
		JOIN persona_versions v ON v.tenant_id=i.tenant_id
			AND v.persona_id=i.persona_id AND v.version=i.persona_version
		JOIN current_lifecycle l ON l.persona_id=i.persona_id AND l.persona_version=i.persona_version
		WHERE i.tenant_id=$1 AND i.conversation_id=$2 AND i.state='ACTIVE' AND l.to_state='PUBLISHED'
		AND l.profile_digest=v.content_digest AND l.profile_digest<>'' AND l.review_digest<>''
		AND l.evaluation_digest<>'' AND l.evaluation_profile_digest=l.profile_digest AND l.evaluation_suite_digest<>''
		ORDER BY i.installation_id COLLATE "C"`, s.tenantID, strings.TrimSpace(conversationID))
	if err != nil {
		return nil, fmt.Errorf("agentpersonastore: list active installations: %w", err)
	}
	defer rows.Close()
	out := make([]ActiveInstallation, 0)
	for rows.Next() {
		var item ActiveInstallation
		var policyJSON, audienceJSON []byte
		if err := rows.Scan(&item.InstallationID, &item.PersonaID, &item.PersonaVersion,
			&item.ConversationID, &item.ConversationClass, &item.InstallerID, &policyJSON,
			&item.Revision, &item.RevocationEpoch, &item.AgentVersion, &audienceJSON); err != nil {
			return nil, fmt.Errorf("agentpersonastore: scan active installation: %w", err)
		}
		if err := json.Unmarshal(policyJSON, &item.ChannelPolicy); err != nil {
			return nil, fmt.Errorf("agentpersonastore: decode active installation policy: %w", err)
		}
		if len(audienceJSON) != 0 && string(audienceJSON) != "null" {
			if err := json.Unmarshal(audienceJSON, &item.Audience); err != nil {
				return nil, fmt.Errorf("agentpersonastore: decode installation audience: %w", err)
			}
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentpersonastore: read active installations: %w", err)
	}
	if err := commit(ctx, tx); err != nil {
		return nil, err
	}
	return out, nil
}

// ListActiveByConversation is an explicit alias for callers that name the
// query by its filtering dimension.
func (s *TenantStore) ListActiveByConversation(ctx context.Context, conversationID string) ([]ActiveInstallation, error) {
	return s.ListActiveInstallations(ctx, conversationID)
}

// ReadCurrentPersonaAuthority returns one active installation bound to the
// exact published version installed in conversationID. Membership is
// intentionally absent: chat remains the source of truth and must recheck it
// before invocation. Ambiguous active placements fail closed.
func (s *TenantStore) ReadCurrentPersonaAuthority(ctx context.Context, conversationID, personaID string) (PersonaVersion, ActiveInstallation, error) {
	if s == nil || ctx == nil || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(personaID) == "" {
		return PersonaVersion{}, ActiveInstallation{}, fmt.Errorf("%w: context, tenant store, conversation, and persona are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaVersion{}, ActiveInstallation{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH latest_lifecycle AS (
		SELECT DISTINCT ON (persona_id,persona_version) persona_id,persona_version,to_state,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest
		FROM persona_lifecycle_events WHERE tenant_id=$1
		ORDER BY persona_id,persona_version,event_sequence DESC
	), current_published AS (
		SELECT v.persona_id,v.version,v.agent_version,v.handle,v.display_name,
			v.profile,v.content_digest,v.created_at
		FROM persona_versions v
		JOIN latest_lifecycle l ON l.persona_id=v.persona_id AND l.persona_version=v.version
		WHERE v.tenant_id=$1 AND v.persona_id=$2 AND l.to_state='PUBLISHED' AND l.profile_digest=v.content_digest AND l.profile_digest<>'' AND l.review_digest<>'' AND l.reviewer_id<>'' AND l.evaluation_digest<>'' AND l.evaluation_profile_digest=l.profile_digest AND l.evaluation_suite_digest<>''
	)
	SELECT v.persona_id,v.version,v.agent_version,v.handle,v.display_name,v.profile,v.content_digest,v.created_at,
		i.installation_id,i.conversation_id,i.conversation_class,i.installer_id,i.channel_policy,i.revision,i.revocation_epoch
	FROM current_published v
	JOIN persona_installations i ON i.tenant_id=$1 AND i.persona_id=v.persona_id AND i.persona_version=v.version
	WHERE i.tenant_id=$1 AND i.persona_id=$2 AND i.conversation_id=$3 AND i.state='ACTIVE'
	ORDER BY i.installation_id COLLATE "C"`, s.tenantID, strings.TrimSpace(personaID), strings.TrimSpace(conversationID))
	if err != nil {
		return PersonaVersion{}, ActiveInstallation{}, fmt.Errorf("agentpersonastore: read current persona authority: %w", err)
	}
	defer rows.Close()
	var version PersonaVersion
	var installation ActiveInstallation
	var policyJSON []byte
	var found int
	for rows.Next() {
		found++
		if found > 1 {
			return PersonaVersion{}, ActiveInstallation{}, fmt.Errorf("%w: multiple active installations", ErrConflict)
		}
		var profile []byte
		if err := rows.Scan(&version.PersonaID, &version.Version, &version.AgentVersion, &version.Handle,
			&version.DisplayName, &profile, &version.ContentDigest, &version.CreatedAt,
			&installation.InstallationID, &installation.ConversationID, &installation.ConversationClass,
			&installation.InstallerID, &policyJSON, &installation.Revision, &installation.RevocationEpoch); err != nil {
			return PersonaVersion{}, ActiveInstallation{}, fmt.Errorf("agentpersonastore: scan current persona authority: %w", err)
		}
		version.Profile = profile
		if err := json.Unmarshal(policyJSON, &installation.ChannelPolicy); err != nil {
			return PersonaVersion{}, ActiveInstallation{}, fmt.Errorf("agentpersonastore: decode authority policy: %w", err)
		}
		var profileFields struct {
			Audience InstallationAudience `json:"audience"`
		}
		if err := json.Unmarshal(profile, &profileFields); err != nil {
			return PersonaVersion{}, ActiveInstallation{}, fmt.Errorf("agentpersonastore: decode authority audience: %w", err)
		}
		installation.Audience = profileFields.Audience
	}
	if err := rows.Err(); err != nil {
		return PersonaVersion{}, ActiveInstallation{}, fmt.Errorf("agentpersonastore: read current persona authority rows: %w", err)
	}
	if found == 0 {
		return PersonaVersion{}, ActiveInstallation{}, fmt.Errorf("%w: current persona authority", ErrNotFound)
	}
	version.TenantID, installation.PersonaID = s.tenant, version.PersonaID
	installation.PersonaVersion, installation.AgentVersion = version.Version, version.AgentVersion
	if err := commit(ctx, tx); err != nil {
		return PersonaVersion{}, ActiveInstallation{}, err
	}
	return version, installation, nil
}
