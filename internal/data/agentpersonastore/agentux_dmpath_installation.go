package agentpersonastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func (s *TenantStore) RetireActiveInstallation(ctx context.Context, personaID, conversationID, actor, reason string) (PersonaInstallation, bool, error) {
	if s == nil || ctx == nil || strings.TrimSpace(personaID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(actor) == "" {
		return PersonaInstallation{}, false, fmt.Errorf("%w: persona, conversation, and actor are required", ErrInvalid)
	}
	if strings.TrimSpace(reason) == "" {
		reason = "Persona installation removed by an authorized administrator"
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaInstallation{}, false, err
	}
	defer tx.Rollback(ctx)
	current, err := s.activeInstallationForUpdate(ctx, tx, personaID, conversationID)
	if errors.Is(err, dbport.ErrNoRows) {
		return PersonaInstallation{}, false, commit(ctx, tx)
	}
	if err != nil {
		return PersonaInstallation{}, false, err
	}
	var policyJSON []byte
	err = tx.QueryRow(ctx, `UPDATE persona_installations
		SET state='RETIRED',suspension_reason=$4,revision=revision+1,revocation_epoch=revocation_epoch+1,updated_at=now()
		WHERE tenant_id=$1 AND installation_id=$2 AND revision=$3 AND state='ACTIVE'
		RETURNING installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at`,
		s.tenantID, current.InstallationID, current.Revision, reason).Scan(&current.InstallationID, &current.PersonaID, &current.PersonaVersion, &current.ConversationID, &current.ConversationClass, &current.InstallerID, &policyJSON, &current.State, &current.SuspensionReason, &current.Revision, &current.RevocationEpoch, &current.CreatedAt, &current.UpdatedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return PersonaInstallation{}, false, fmt.Errorf("%w: installation changed", ErrConflict)
	}
	if err != nil {
		return PersonaInstallation{}, false, err
	}
	if err := json.Unmarshal(policyJSON, &current.ChannelPolicy); err != nil {
		return PersonaInstallation{}, false, fmt.Errorf("agentpersonastore: decode retired installation policy: %w", err)
	}
	current.TenantID, current.CreatedAt, current.UpdatedAt = s.tenant, current.CreatedAt.UTC(), current.UpdatedAt.UTC()
	return current, true, commit(ctx, tx)
}

func (s *TenantStore) ReplaceActiveInstallation(ctx context.Context, replacement PersonaInstallation) (PersonaInstallation, bool, error) {
	if err := validateInstallation(replacement, s.tenant); err != nil {
		return PersonaInstallation{}, false, err
	}
	if replacement.State != InstallationActive {
		return PersonaInstallation{}, false, fmt.Errorf("%w: replacement must be active", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaInstallation{}, false, err
	}
	defer tx.Rollback(ctx)
	if err := s.checkPublishedReplacementTx(ctx, tx, replacement); err != nil {
		return PersonaInstallation{}, false, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('persona-installation:' || $1 || ':' || $2,0))`, s.tenantID.String(), replacement.ConversationID); err != nil {
		return PersonaInstallation{}, false, err
	}
	if _, err := s.retireSuspendedDuplicateInstallationsTx(ctx, tx, replacement.PersonaID, replacement.ConversationID); err != nil {
		return PersonaInstallation{}, false, err
	}
	current, err := s.activeInstallationForUpdate(ctx, tx, replacement.PersonaID, replacement.ConversationID)
	if errors.Is(err, dbport.ErrNoRows) {
		if err := s.insertInstallationTx(ctx, tx, replacement); err != nil {
			return PersonaInstallation{}, false, err
		}
		return replacement, true, commit(ctx, tx)
	}
	if err != nil {
		return PersonaInstallation{}, false, err
	}
	if current.PersonaVersion == replacement.PersonaVersion && sameChannelPolicy(current.ChannelPolicy, replacement.ChannelPolicy) {
		return current, false, commit(ctx, tx)
	}
	if _, err := tx.Exec(ctx, `UPDATE persona_installations SET state='RETIRED',suspension_reason='Replaced by reviewed reinstall',revision=revision+1,revocation_epoch=revocation_epoch+1,updated_at=now() WHERE tenant_id=$1 AND installation_id=$2 AND state='ACTIVE'`, s.tenantID, current.InstallationID); err != nil {
		return PersonaInstallation{}, false, err
	}
	if err := s.insertInstallationTx(ctx, tx, replacement); err != nil {
		return PersonaInstallation{}, false, err
	}
	return replacement, true, commit(ctx, tx)
}

func (s *TenantStore) activeInstallationForUpdate(ctx context.Context, tx dbport.Tx, personaID, conversationID string) (PersonaInstallation, error) {
	var out PersonaInstallation
	var policyJSON []byte
	err := tx.QueryRow(ctx, `SELECT installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at
		FROM persona_installations WHERE tenant_id=$1 AND persona_id=$2 AND conversation_id=$3 AND state='ACTIVE'
		ORDER BY installation_id COLLATE "C" FOR UPDATE`, s.tenantID, personaID, conversationID).Scan(&out.InstallationID, &out.PersonaID, &out.PersonaVersion, &out.ConversationID, &out.ConversationClass, &out.InstallerID, &policyJSON, &out.State, &out.SuspensionReason, &out.Revision, &out.RevocationEpoch, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return PersonaInstallation{}, err
	}
	if err := json.Unmarshal(policyJSON, &out.ChannelPolicy); err != nil {
		return PersonaInstallation{}, fmt.Errorf("agentpersonastore: decode active installation policy: %w", err)
	}
	out.TenantID, out.CreatedAt, out.UpdatedAt = s.tenant, out.CreatedAt.UTC(), out.UpdatedAt.UTC()
	return out, nil
}

func (s *TenantStore) insertInstallationTx(ctx context.Context, tx dbport.Tx, installation PersonaInstallation) error {
	created, updated := installation.CreatedAt.UTC(), installation.UpdatedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if updated.IsZero() {
		updated = created
	}
	n, err := tx.Exec(ctx, `INSERT INTO persona_installations
		(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`,
		s.tenantID, installation.InstallationID, installation.PersonaID, installation.PersonaVersion,
		installation.ConversationID, installation.ConversationClass, installation.InstallerID, marshalChannelPolicy(installation.ChannelPolicy),
		installation.State, installation.SuspensionReason, installation.Revision, installation.RevocationEpoch, created, updated)
	if err != nil {
		return fmt.Errorf("agentpersonastore: insert replacement installation: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: installation already exists", ErrConflict)
	}
	return nil
}

func sameChannelPolicy(a, b ChannelPolicy) bool {
	return a.PlacementClass == b.PlacementClass && a.MaxTier == b.MaxTier && a.AlwaysPrivate == b.AlwaysPrivate &&
		a.ConversationSearchAllowed == b.ConversationSearchAllowed && a.AllowExternalMembers == b.AllowExternalMembers &&
		a.AllowCrossCompanyMembers == b.AllowCrossCompanyMembers && sameStrings(a.AllowedDataClasses, b.AllowedDataClasses) &&
		sameClasses(a.AllowedChannelClasses, b.AllowedChannelClasses)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, value := range a {
		found := false
		for _, other := range b {
			found = found || value == other
		}
		if !found {
			return false
		}
	}
	return true
}

func sameClasses(a, b []ConversationClass) bool {
	if len(a) != len(b) {
		return false
	}
	for _, value := range a {
		found := false
		for _, other := range b {
			found = found || value == other
		}
		if !found {
			return false
		}
	}
	return true
}
