package agentpersonastore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CatalogInstallation is the durable placement fact stored for a persona.
type CatalogInstallation struct {
	ID                string
	PersonaID         string
	PersonaVersion    int64
	ConversationID    string
	ConversationClass ConversationClass
	ChannelPolicy     ChannelPolicy
	Revision          int64
	RevocationEpoch   int64
	State             string
	SuspensionReason  string
}

// CatalogEntry combines one immutable persona version with its durable
// lifecycle, owner and installation facts. It contains no task or review
// content.
type CatalogEntry struct {
	Version          PersonaVersion
	Lifecycle        LifecycleState
	PublicationProof PublicationEvidenceStatus
	BusinessOwner    string
	Steward          string
	Installations    []CatalogInstallation
}

// PublicationEvidenceStatus distinguishes historical PUBLISHED labels that
// predate evidence pins from publications with structurally complete pins.
// PINNED does not imply the upstream authority signatures were revalidated by
// this projection.
type PublicationEvidenceStatus string

const (
	PublicationEvidenceNotRequired PublicationEvidenceStatus = "NOT_REQUIRED"
	PublicationEvidenceMissing     PublicationEvidenceStatus = "MISSING"
	PublicationEvidencePinned      PublicationEvidenceStatus = "PINNED"
)

// ListCatalog returns tenant-scoped durable persona facts from one database
// statement. Every immutable version is returned separately with only its
// exact placements. No current-version pointer or evidence is synthesized.
func (s *TenantStore) ListCatalog(ctx context.Context) ([]CatalogEntry, error) {
	if s == nil || ctx == nil {
		return nil, fmt.Errorf("%w: catalog context and tenant store are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH latest_lifecycle AS (
		SELECT DISTINCT ON (persona_id, persona_version)
			persona_id, persona_version, to_state, profile_digest, review_digest, reviewer_id, evaluation_digest, evaluation_profile_digest, evaluation_suite_digest
		FROM persona_lifecycle_events
		WHERE tenant_id=$1
		ORDER BY persona_id, persona_version, event_sequence DESC
	)
	SELECT v.persona_id,v.version,v.agent_version,v.handle,v.display_name,v.profile,v.content_digest,v.created_at,
		l.to_state,l.profile_digest,l.review_digest,l.reviewer_id,l.evaluation_digest,l.evaluation_profile_digest,l.evaluation_suite_digest,
		business_owner.principal_id,technical_steward.principal_id,
		i.installation_id,i.persona_id,i.persona_version,i.conversation_id,i.conversation_class,
		i.channel_policy,i.revision,i.revocation_epoch,i.state,i.suspension_reason
	FROM persona_versions v
	LEFT JOIN latest_lifecycle l ON l.persona_id=v.persona_id AND l.persona_version=v.version
	LEFT JOIN persona_owners business_owner ON business_owner.tenant_id=v.tenant_id AND business_owner.persona_id=v.persona_id AND business_owner.owner_role='BUSINESS_OWNER'
	LEFT JOIN persona_owners technical_steward ON technical_steward.tenant_id=v.tenant_id AND technical_steward.persona_id=v.persona_id AND technical_steward.owner_role='TECHNICAL_STEWARD'
	LEFT JOIN persona_installations i ON i.tenant_id=v.tenant_id AND i.persona_id=v.persona_id AND i.persona_version=v.version
	WHERE v.tenant_id=$1
	ORDER BY v.persona_id COLLATE "C",v.version,i.installation_id`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("agentpersonastore: query admin persona catalog: %w", err)
	}
	defer rows.Close()
	entries := make([]CatalogEntry, 0)
	type versionKey struct {
		personaID string
		version   int64
	}
	indices := make(map[versionKey]int)
	for rows.Next() {
		var version PersonaVersion
		var lifecycle *LifecycleState
		var profileDigest, reviewDigest, reviewerID, evaluationDigest, evaluationProfileDigest, evaluationSuiteDigest *string
		var owner, steward *string
		var installID, installPersona, conversationID *string
		var conversationClass *ConversationClass
		var policyJSON []byte
		var installVersion *int64
		var revision, revocationEpoch *int64
		var installState *InstallationState
		var suspensionReason *string
		if err := rows.Scan(&version.PersonaID, &version.Version, &version.AgentVersion, &version.Handle, &version.DisplayName,
			&version.Profile, &version.ContentDigest, &version.CreatedAt, &lifecycle,
			&profileDigest, &reviewDigest, &reviewerID, &evaluationDigest, &evaluationProfileDigest, &evaluationSuiteDigest, &owner, &steward,
			&installID, &installPersona, &installVersion, &conversationID, &conversationClass,
			&policyJSON, &revision, &revocationEpoch, &installState, &suspensionReason); err != nil {
			return nil, fmt.Errorf("agentpersonastore: scan admin persona catalog: %w", err)
		}
		version.TenantID, version.CreatedAt = s.tenant, version.CreatedAt.UTC()
		if err := validateCatalogFacts(version, lifecycle, owner, steward); err != nil {
			return nil, err
		}
		key := versionKey{version.PersonaID, version.Version}
		index, exists := indices[key]
		if !exists {
			indices[key] = len(entries)
			entries = append(entries, CatalogEntry{Version: version, Lifecycle: *lifecycle, PublicationProof: publicationEvidenceStatus(*lifecycle, version.ContentDigest, profileDigest, reviewDigest, reviewerID, evaluationDigest, evaluationProfileDigest, evaluationSuiteDigest), BusinessOwner: *owner, Steward: *steward})
			index = len(entries) - 1
		}
		if installID != nil {
			var policy ChannelPolicy
			if err := json.Unmarshal(policyJSON, &policy); err != nil {
				return nil, fmt.Errorf("%w: invalid installation channel policy", ErrInvalid)
			}
			if strings.TrimSpace(*installID) == "" || installPersona == nil || *installPersona != version.PersonaID || installVersion == nil || *installVersion != version.Version || conversationID == nil || strings.TrimSpace(*conversationID) == "" || conversationClass == nil || !validConversationClass(*conversationClass) || !validChannelPolicy(policy) || revision == nil || *revision <= 0 || revocationEpoch == nil || *revocationEpoch <= 0 || installState == nil || !validInstallationState(string(*installState)) {
				return nil, fmt.Errorf("%w: invalid installation catalog facts", ErrInvalid)
			}
			entries[index].Installations = append(entries[index].Installations, CatalogInstallation{
				ID: *installID, PersonaID: *installPersona, PersonaVersion: *installVersion,
				ConversationID: *conversationID, ConversationClass: *conversationClass,
				ChannelPolicy: policy, Revision: *revision, RevocationEpoch: *revocationEpoch,
				State: string(*installState), SuspensionReason: nonnilCatalogString(suspensionReason),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentpersonastore: read admin persona catalog rows: %w", err)
	}
	if err := commit(ctx, tx); err != nil {
		return nil, err
	}
	return entries, nil
}

func nonnilCatalogString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func publicationEvidenceStatus(state LifecycleState, contentDigest string, fields ...*string) PublicationEvidenceStatus {
	if state != StatePublished {
		return PublicationEvidenceNotRequired
	}
	if len(fields) != 6 {
		return PublicationEvidenceMissing
	}
	for _, field := range fields {
		if field == nil || strings.TrimSpace(*field) == "" {
			return PublicationEvidenceMissing
		}
	}
	if *fields[0] != contentDigest || *fields[4] != *fields[0] {
		return PublicationEvidenceMissing
	}
	return PublicationEvidencePinned
}

func validateCatalogFacts(version PersonaVersion, lifecycle *LifecycleState, owner, steward *string) error {
	if version.PersonaID == "" || version.Version <= 0 || strings.TrimSpace(version.AgentVersion) == "" || strings.TrimSpace(version.Handle) == "" || strings.TrimSpace(version.DisplayName) == "" || len(version.Profile) == 0 || strings.TrimSpace(version.ContentDigest) == "" {
		return fmt.Errorf("%w: incomplete persona catalog version", ErrInvalid)
	}
	if lifecycle == nil || !validCatalogLifecycle(*lifecycle) || owner == nil || strings.TrimSpace(*owner) == "" || steward == nil || strings.TrimSpace(*steward) == "" {
		return fmt.Errorf("%w: lifecycle and both owner roles are required for catalog display", ErrInvalid)
	}
	return nil
}

func validCatalogLifecycle(state LifecycleState) bool {
	switch state {
	case StateDraft, StateInReview, StatePublished, StateSuspended, StateRetired:
		return true
	default:
		return false
	}
}

func validInstallationState(state string) bool {
	switch state {
	case string(InstallationActive), string(InstallationSuspended), string(InstallationRetired), "KILLED":
		return true
	default:
		return false
	}
}
