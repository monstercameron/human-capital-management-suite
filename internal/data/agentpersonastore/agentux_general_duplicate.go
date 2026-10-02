package agentpersonastore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

const retiredSuspendedDuplicateReason = "Retired duplicate after a runnable installation was created"

// RetireSuspendedDuplicateInstallations performs the one-time repair for old
// restore suspensions. It never touches an active installation and is safe to
// replay after the duplicate rows have been retired.
func (s *TenantStore) RetireSuspendedDuplicateInstallations(ctx context.Context, personaID, conversationID string) (int, error) {
	if s == nil || ctx == nil || strings.TrimSpace(personaID) == "" || strings.TrimSpace(conversationID) == "" {
		return 0, fmt.Errorf("%w: persona and conversation are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('persona-installation:' || $1 || ':' || $2,0))`, s.tenantID.String(), conversationID); err != nil {
		return 0, err
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM persona_installations WHERE tenant_id=$1 AND persona_id=$2 AND conversation_id=$3 AND state='ACTIVE')`, s.tenantID, personaID, conversationID).Scan(&active); err != nil {
		return 0, err
	}
	if !active {
		return 0, commit(ctx, tx)
	}
	retired, err := s.retireSuspendedDuplicateInstallationsTx(ctx, tx, personaID, conversationID)
	if err != nil {
		return 0, err
	}
	return retired, commit(ctx, tx)
}

func (s *TenantStore) retireSuspendedDuplicateInstallationsTx(ctx context.Context, tx dbport.Tx, personaID, conversationID string) (int, error) {
	changed, err := tx.Exec(ctx, `UPDATE persona_installations
		SET state='RETIRED',suspension_reason=$4,revision=revision+1,revocation_epoch=revocation_epoch+1,updated_at=now()
		WHERE tenant_id=$1 AND persona_id=$2 AND conversation_id=$3 AND state='SUSPENDED'`,
		s.tenantID, personaID, conversationID, retiredSuspendedDuplicateReason)
	if err != nil {
		return 0, fmt.Errorf("agentpersonastore: retire suspended duplicate installations: %w", err)
	}
	return int(changed), nil
}

// Reinstall uses the same publication proof as first installation. In
// particular a stopped row is never retired for an unpublished replacement.
func (s *TenantStore) checkPublishedReplacementTx(ctx context.Context, tx dbport.Tx, in PersonaInstallation) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2 || ':' || $3::text,0))`, s.tenant.String(), in.PersonaID, fmt.Sprint(in.PersonaVersion)); err != nil {
		return err
	}
	var published bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM persona_versions v JOIN LATERAL (
 SELECT to_state,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest FROM persona_lifecycle_events
 WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 ORDER BY event_sequence DESC LIMIT 1
 ) l ON true WHERE v.tenant_id=$1 AND v.persona_id=$2 AND v.version=$3 AND l.to_state='PUBLISHED' AND l.profile_digest=v.content_digest AND l.profile_digest<>'' AND l.review_digest<>'' AND l.reviewer_id<>'' AND l.evaluation_digest<>'' AND l.evaluation_profile_digest=l.profile_digest AND l.evaluation_suite_digest<>'')`, s.tenantID, in.PersonaID, in.PersonaVersion).Scan(&published)
	if errors.Is(err, dbport.ErrNoRows) || err == nil && !published {
		return fmt.Errorf("%w: replacement version is not published", ErrConflict)
	}
	return err
}
