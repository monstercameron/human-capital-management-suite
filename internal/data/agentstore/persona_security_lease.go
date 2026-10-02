package agentstore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrPersonaSecurityLeaseInvalid = errors.New("agentstore: invalid persona security lease")
	ErrPersonaSecurityLeaseDenied  = errors.New("agentstore: persona security lease denied")
	ErrPersonaSecurityLeaseRevoked = errors.New("agentstore: persona security lease revoked")
	ErrPersonaSecurityLeaseExpired = errors.New("agentstore: persona security lease expired")
	ErrPersonaSecurityStepConflict = errors.New("agentstore: persona security step conflict")
	ErrPersonaSecurityRecovery     = errors.New("agentstore: persona security recovery required")
)

// PersonaSecurityLeaseRequest names a committed admission. Authority is
// recovered from that immutable admission, never accepted from this request.
type PersonaSecurityLeaseRequest struct {
	TenantID    uuid.UUID
	AdmissionID string
	IssuedAt    time.Time
	ExpiresAt   time.Time
}

// PersonaSecurityLease aliases the trust-layer lease contract recovered from
// durable agent-store evidence.
type PersonaSecurityLease = agentsecurity.DurablePersonaLease

// PersonaSecurityScope identifies a permanent revocation boundary.
type PersonaSecurityScope struct {
	Kind string
	Key  string
}

// IssuePersonaSecurityLease creates or recovers a lease only for an accepted,
// started persona invocation whose durable execution and authority snapshot
// agree. The caller does not supply persona authority or run identity.
func (s *Store) IssuePersonaSecurityLease(ctx context.Context, req PersonaSecurityLeaseRequest) (PersonaSecurityLease, error) {
	if s == nil || ctx == nil || req.TenantID == uuid.Nil || strings.TrimSpace(req.AdmissionID) == "" ||
		req.IssuedAt.IsZero() || !req.ExpiresAt.After(req.IssuedAt) {
		return PersonaSecurityLease{}, personaSecurityLeaseInvalidHere()
	}
	var lease PersonaSecurityLease
	err := s.RunTenantTx(ctx, req.TenantID, func(tx dbport.Tx) error {
		var invocationID, runID, authorityRef, policyDigest, issuerID, mode, decision string
		var principalID, personaID, personaVersion, installationID string
		var deadline time.Time
		err := tx.QueryRow(ctx, `SELECT pi.invocation_id, re.run_id,
			ar.authority_snapshot->>'grant_ref', ar.authority_snapshot->>'policy_digest',
			current_user, ar.principal_chain->>'invoker_id', pi.persona_id, pi.persona_version, pi.installation_id,
			ar.decision, ar.principal_chain->>'mode',
			ar.deadline
			FROM agent_run_request ar
			JOIN agent_run_execution re ON re.tenant_id=ar.tenant_id AND re.admission_id=ar.request_id
			JOIN persona_invocations pi ON pi.tenant_id=ar.tenant_id AND pi.post_id=ar.source_ref
				AND pi.persona_id=ar.request_payload->'persona'->>'id'
				AND pi.persona_version=ar.request_payload->'persona'->>'version'
				AND pi.installation_id=ar.installation_id
				AND pi.invoker_id=ar.principal_chain->>'invoker_id'
				AND pi.conversation_id=ar.request_payload->'audience'->>'audience_id'
				AND pi.thread_id=ar.request_payload->'context_scope'->>'scope_id'
			JOIN persona_versions pv ON pv.tenant_id=pi.tenant_id AND pv.persona_id=pi.persona_id
				AND pv.version=pi.persona_version::bigint AND pv.agent_version=ar.agent_id || '@' || ar.agent_version
			JOIN persona_installations ins ON ins.tenant_id=pi.tenant_id AND ins.installation_id=pi.installation_id
				AND ins.persona_id=pi.persona_id AND ins.persona_version=pv.version AND ins.state='ACTIVE'
			WHERE ar.tenant_id=$1 AND ar.request_id=$2 AND ar.decision='ACCEPTED'
			AND ar.source_kind='PERSONA_MENTION' AND ar.authority_snapshot IS NOT NULL
			AND ar.principal_chain->>'mode'='ON_BEHALF_OF'
			AND pi.mode='ON_BEHALF_OF' AND pi.state IN ('CLAIMED','STARTED') AND pi.grant_payload IS NOT NULL
			AND (pi.admission_id IS NULL OR pi.admission_id=ar.request_id)
			AND ar.authority_snapshot->>'grant_ref' IS NOT NULL
			AND btrim(ar.authority_snapshot->>'grant_ref') <> ''
			AND ar.authority_snapshot->>'policy_digest' ~ '^sha256:[0-9a-f]{64}$'
			AND EXISTS (SELECT 1 FROM persona_lifecycle_events ple
				WHERE ple.tenant_id=pi.tenant_id AND ple.persona_id=pi.persona_id
				AND ple.persona_version=pv.version AND ple.to_state='PUBLISHED'
				AND ple.event_sequence=(SELECT max(last.event_sequence) FROM persona_lifecycle_events last
					WHERE last.tenant_id=ple.tenant_id AND last.persona_id=ple.persona_id
					AND last.persona_version=ple.persona_version))`, req.TenantID, req.AdmissionID).
			Scan(&invocationID, &runID, &authorityRef, &policyDigest, &issuerID,
				&principalID, &personaID, &personaVersion, &installationID, &decision, &mode, &deadline)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrPersonaSecurityLeaseDenied
		}
		if err != nil {
			return fmt.Errorf("agentstore: resolve accepted persona admission for security lease: %w", err)
		}
		if req.ExpiresAt.After(deadline) {
			return personaSecurityLeaseInvalidHere()
		}
		lease.TenantID, lease.AdmissionID = req.TenantID.String(), req.AdmissionID
		lease.IssuerID, lease.AuthorityRef, lease.PolicyDigest = issuerID, authorityRef, policyDigest
		lease.AdmissionDecision, lease.Mode, lease.AdmissionDeadline = decision, mode, deadline.UTC()
		lease.InvocationID, lease.RunID = invocationID, runID
		lease.PrincipalID, lease.PersonaID, lease.PersonaVersion = principalID, personaID, personaVersion
		lease.InstallationID, lease.IssuedAt, lease.ExpiresAt = installationID, req.IssuedAt.UTC(), req.ExpiresAt.UTC()
		scopes := personaLeaseScopes(lease)
		epochs := make([]int64, len(scopes))
		for i, scope := range scopes {
			epoch, err := ensureActivePersonaScope(ctx, tx, req.TenantID, scope)
			if err != nil {
				return err
			}
			epochs[i] = epoch
		}
		lease.TenantEpoch, lease.PrincipalEpoch, lease.PersonaEpoch = epochs[0], epochs[1], epochs[2]
		lease.VersionEpoch, lease.InstallationEpoch, lease.RunEpoch = epochs[3], epochs[4], epochs[5]

		var existing PersonaSecurityLease
		err = tx.QueryRow(ctx, `SELECT lease_id,issuer_id,authority_ref,policy_digest,invocation_id,run_id,
			principal_id,persona_id,persona_version,installation_id,tenant_epoch,principal_epoch,persona_epoch,
			version_epoch,installation_epoch,run_epoch,issued_at,expires_at
			FROM persona_security_lease WHERE tenant_id=$1 AND admission_id=$2`, req.TenantID, req.AdmissionID).
			Scan(&existing.LeaseID, &existing.IssuerID, &existing.AuthorityRef, &existing.PolicyDigest,
				&existing.InvocationID, &existing.RunID, &existing.PrincipalID, &existing.PersonaID,
				&existing.PersonaVersion, &existing.InstallationID, &existing.TenantEpoch, &existing.PrincipalEpoch,
				&existing.PersonaEpoch, &existing.VersionEpoch, &existing.InstallationEpoch, &existing.RunEpoch,
				&existing.IssuedAt, &existing.ExpiresAt)
		if err == nil {
			existing.TenantID, existing.AdmissionID = lease.TenantID, lease.AdmissionID
			if !samePersonaSecurityLease(existing, lease) {
				return personaSecurityLeaseInvalidHere()
			}
			lease.LeaseID = existing.LeaseID
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("agentstore: read persona security lease: %w", err)
		}
		lease.LeaseID = uuid.NewString()
		_, err = tx.Exec(ctx, `INSERT INTO persona_security_lease
			(tenant_id,lease_id,admission_id,invocation_id,run_id,issuer_id,authority_ref,policy_digest,
			principal_id,persona_id,persona_version,installation_id,tenant_epoch,principal_epoch,persona_epoch,
			version_epoch,installation_epoch,run_epoch,issued_at,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
			req.TenantID, lease.LeaseID, lease.AdmissionID, lease.InvocationID, lease.RunID, lease.IssuerID,
			lease.AuthorityRef, lease.PolicyDigest, lease.PrincipalID, lease.PersonaID, lease.PersonaVersion,
			lease.InstallationID, lease.TenantEpoch, lease.PrincipalEpoch, lease.PersonaEpoch, lease.VersionEpoch,
			lease.InstallationEpoch, lease.RunEpoch, lease.IssuedAt, lease.ExpiresAt)
		if err != nil {
			return fmt.Errorf("agentstore: insert persona security lease: %w", err)
		}
		return nil
	})
	if err != nil {
		return PersonaSecurityLease{}, err
	}
	return lease, nil
}

// LoadPersonaSecurityLease recovers the exact persisted lease after process restart.
func (s *Store) LoadPersonaSecurityLease(ctx context.Context, tenantID uuid.UUID, leaseID string) (PersonaSecurityLease, error) {
	if s == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(leaseID) == "" {
		return PersonaSecurityLease{}, personaSecurityLeaseInvalidHere()
	}
	return s.loadPersonaSecurityLease(ctx, tenantID, "l.lease_id", leaseID)
}

// ResolveActivePersonaSecurityLease recovers the persisted lease for one
// admission and checks every durable scope epoch before returning it.
func (s *Store) ResolveActivePersonaSecurityLease(ctx context.Context, tenantID uuid.UUID, admissionID string, at time.Time) (PersonaSecurityLease, error) {
	if s == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(admissionID) == "" || at.IsZero() {
		return PersonaSecurityLease{}, personaSecurityLeaseInvalidHere()
	}
	lease, err := s.loadPersonaSecurityLease(ctx, tenantID, "l.admission_id", admissionID)
	if err != nil {
		return PersonaSecurityLease{}, err
	}
	current := make(map[string]int64, len(personaLeaseScopes(lease)))
	err = s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		for _, scope := range personaLeaseScopes(lease) {
			var epoch int64
			var state string
			if err := tx.QueryRow(ctx, `SELECT epoch,state FROM persona_security_scope
				WHERE tenant_id=$1 AND scope_kind=$2 AND scope_key=$3`, tenantID, scope.Kind, scope.Key).Scan(&epoch, &state); err != nil {
				return ErrPersonaSecurityLeaseRevoked
			}
			if state != "ACTIVE" {
				return ErrPersonaSecurityLeaseRevoked
			}
			current[personaSecurityScopeIdentity(scope)] = epoch
		}
		var running int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM persona_security_step
			WHERE tenant_id=$1 AND lease_id=$2 AND state='STARTED'`, tenantID, lease.LeaseID).Scan(&running); err != nil {
			return err
		}
		if running != 0 {
			return ErrPersonaSecurityRecovery
		}
		return nil
	})
	if err != nil {
		return PersonaSecurityLease{}, err
	}
	if err := agentsecurity.CheckDurablePersonaLease(lease, current, at); err != nil {
		if !at.Before(lease.ExpiresAt) {
			return PersonaSecurityLease{}, ErrPersonaSecurityLeaseExpired
		}
		return PersonaSecurityLease{}, ErrPersonaSecurityLeaseRevoked
	}
	return lease, nil
}

func (s *Store) loadPersonaSecurityLease(ctx context.Context, tenantID uuid.UUID, lookupColumn, lookupID string) (PersonaSecurityLease, error) {
	var lease PersonaSecurityLease
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT l.lease_id,l.admission_id,l.invocation_id,l.run_id,l.issuer_id,l.authority_ref,
			l.policy_digest,l.principal_id,l.persona_id,l.persona_version,l.installation_id,l.tenant_epoch,l.principal_epoch,
			l.persona_epoch,l.version_epoch,l.installation_epoch,l.run_epoch,l.issued_at,l.expires_at,
			ar.decision,ar.principal_chain->>'mode',ar.deadline
			FROM persona_security_lease l JOIN agent_run_request ar
			ON ar.tenant_id=l.tenant_id AND ar.request_id=l.admission_id
			WHERE l.tenant_id=$1 AND `+lookupColumn+`=$2`, tenantID, lookupID).
			Scan(&lease.LeaseID, &lease.AdmissionID, &lease.InvocationID, &lease.RunID, &lease.IssuerID,
				&lease.AuthorityRef, &lease.PolicyDigest, &lease.PrincipalID, &lease.PersonaID,
				&lease.PersonaVersion, &lease.InstallationID, &lease.TenantEpoch, &lease.PrincipalEpoch,
				&lease.PersonaEpoch, &lease.VersionEpoch, &lease.InstallationEpoch, &lease.RunEpoch,
				&lease.IssuedAt, &lease.ExpiresAt, &lease.AdmissionDecision, &lease.Mode, &lease.AdmissionDeadline)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrPersonaSecurityLeaseDenied
		}
		if err != nil {
			return fmt.Errorf("agentstore: load persona security lease: %w", err)
		}
		lease.TenantID = tenantID.String()
		return nil
	})
	if err != nil {
		return PersonaSecurityLease{}, err
	}
	return lease, nil
}

// RevokePersonaSecurityScope permanently revokes a scope. The transaction lock
// matches the session locks held by RunPersonaSecurityStep, so revocation waits
// for a running step and commits before any later step can start.
func (s *Store) RevokePersonaSecurityScope(ctx context.Context, tenantID uuid.UUID, scope PersonaSecurityScope, reason string, revokedAt time.Time) (int64, error) {
	if s == nil || ctx == nil || tenantID == uuid.Nil || !validPersonaSecurityScope(scope) ||
		strings.TrimSpace(reason) == "" || strings.ContainsAny(reason, "\r\n") || revokedAt.IsZero() {
		return 0, personaSecurityLeaseInvalidHere()
	}
	var epoch int64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		lockKey := personaScopeLockKey(tenantID, scope)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
			return fmt.Errorf("agentstore: lock persona revocation scope: %w", err)
		}
		n, err := tx.Exec(ctx, `INSERT INTO persona_security_scope
			(tenant_id,scope_kind,scope_key,epoch,state,reason,revoked_at)
			VALUES ($1,$2,$3,2,'REVOKED',$4,$5)
			ON CONFLICT (tenant_id,scope_kind,scope_key) DO UPDATE
			SET epoch=persona_security_scope.epoch+1,state='REVOKED',reason=EXCLUDED.reason,revoked_at=EXCLUDED.revoked_at
			WHERE persona_security_scope.state='ACTIVE'`, tenantID, scope.Kind, scope.Key, reason, revokedAt.UTC())
		if err != nil {
			return fmt.Errorf("agentstore: revoke persona security scope: %w", err)
		}
		err = tx.QueryRow(ctx, `SELECT epoch FROM persona_security_scope WHERE tenant_id=$1 AND scope_kind=$2 AND scope_key=$3`, tenantID, scope.Kind, scope.Key).Scan(&epoch)
		if err != nil || n == 0 {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO persona_security_scope_event(tenant_id,event_id,scope_kind,scope_key,epoch,state,reason,occurred_at)
			VALUES ($1,$2,$3,$4,$5,'REVOKED',$6,$7)`, tenantID, uuid.NewString(), scope.Kind, scope.Key, epoch, reason, revokedAt.UTC())
		return err
	})
	return epoch, err
}

// ReactivatePersonaSecurityScope admits fresh leases for an exact published
// version after independent publication. Epoch advancement keeps every old
// lease revoked. Retrying an already active scope preserves its current epoch.
func (s *Store) ReactivatePersonaSecurityScope(ctx context.Context, tenantID uuid.UUID, scope PersonaSecurityScope, reason string, at time.Time) (int64, error) {
	if s == nil || ctx == nil || tenantID == uuid.Nil || !validPersonaSecurityScope(scope) || scope.Kind != "VERSION" || strings.TrimSpace(reason) == "" || strings.ContainsAny(reason, "\r\n") || at.IsZero() {
		return 0, personaSecurityLeaseInvalidHere()
	}
	var epoch int64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, personaScopeLockKey(tenantID, scope)); err != nil {
			return err
		}
		var published bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM persona_versions v
			JOIN LATERAL (SELECT to_state,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest
				FROM persona_lifecycle_events WHERE tenant_id=v.tenant_id AND persona_id=v.persona_id AND persona_version=v.version
				ORDER BY event_sequence DESC LIMIT 1) l ON true
			WHERE v.tenant_id=$1 AND v.persona_id || ':' || v.version::text=$2 AND l.to_state='PUBLISHED'
			AND l.profile_digest=v.content_digest AND l.review_digest<>'' AND l.reviewer_id<>''
			AND l.evaluation_digest<>'' AND l.evaluation_profile_digest=v.content_digest AND l.evaluation_suite_digest<>'')`, tenantID, scope.Key).Scan(&published); err != nil {
			return err
		}
		if !published {
			return ErrPersonaSecurityLeaseDenied
		}
		n, err := tx.Exec(ctx, `INSERT INTO persona_security_scope(tenant_id,scope_kind,scope_key)
			VALUES ($1,$2,$3) ON CONFLICT (tenant_id,scope_kind,scope_key) DO UPDATE
			SET epoch=persona_security_scope.epoch+1,state='ACTIVE',reason='',revoked_at=NULL
			WHERE persona_security_scope.state='REVOKED'`, tenantID, scope.Kind, scope.Key)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT epoch FROM persona_security_scope WHERE tenant_id=$1 AND scope_kind=$2 AND scope_key=$3`, tenantID, scope.Kind, scope.Key).Scan(&epoch); err != nil {
			return err
		}
		if n != 0 {
			_, err = tx.Exec(ctx, `INSERT INTO persona_security_scope_event(tenant_id,event_id,scope_kind,scope_key,epoch,state,reason,occurred_at)
				VALUES ($1,$2,$3,$4,$5,'ACTIVE',$6,$7)`, tenantID, uuid.NewString(), scope.Kind, scope.Key, epoch, reason, at.UTC())
		}
		return err
	})
	return epoch, err
}

// RunPersonaSecurityStep durably begins and finishes one step while holding
// PostgreSQL session advisory locks for every lease scope. It holds no SQL
// transaction while work runs; revocation transactions on any held scope wait
// until work returns and the session locks are released.
func (s *Store) RunPersonaSecurityStep(ctx context.Context, tenantID uuid.UUID, leaseID, stepID string, at time.Time, work func(context.Context) error) error {
	if s == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(leaseID) == "" ||
		strings.TrimSpace(stepID) == "" || at.IsZero() || work == nil {
		return personaSecurityLeaseInvalidHere()
	}
	lease, err := s.LoadPersonaSecurityLease(ctx, tenantID, leaseID)
	if err != nil {
		return err
	}
	scopes := personaLeaseScopes(lease)
	if s.fencePool == nil {
		return personaSecurityLeaseInvalidHere()
	}
	return s.fencePool.WithConn(ctx, func(conn dbport.Conn) error {
		unlock, err := acquirePersonaScopeLocks(ctx, conn, tenantID, scopes)
		if err != nil {
			return err
		}
		defer unlock()
		if err := s.checkAndStartPersonaSecurityStep(ctx, conn, lease, stepID, at.UTC()); err != nil {
			return err
		}
		workErr := work(ctx)
		state := "COMPLETED"
		if workErr != nil {
			state = "FAILED"
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.finishPersonaSecurityStep(finishCtx, conn, tenantID, leaseID, stepID, state); err != nil {
			return errors.Join(workErr, err)
		}
		return workErr
	})
}

// RecoverPersonaSecuritySteps terminally marks orphaned started steps. The
// same scope locks wait for any still-live worker before recovery proceeds.
func (s *Store) RecoverPersonaSecuritySteps(ctx context.Context, tenantID uuid.UUID, leaseID string, at time.Time) (int64, error) {
	if s == nil || ctx == nil || tenantID == uuid.Nil || strings.TrimSpace(leaseID) == "" || at.IsZero() {
		return 0, personaSecurityLeaseInvalidHere()
	}
	lease, err := s.LoadPersonaSecurityLease(ctx, tenantID, leaseID)
	if err != nil {
		return 0, err
	}
	scopes := personaLeaseScopes(lease)
	var recovered int64
	if s.fencePool == nil {
		return 0, personaSecurityLeaseInvalidHere()
	}
	err = s.fencePool.WithConn(ctx, func(conn dbport.Conn) error {
		unlock, err := acquirePersonaScopeLocks(ctx, conn, tenantID, scopes)
		if err != nil {
			return err
		}
		defer unlock()
		tx, err := beginPersonaSecurityConn(conn, ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if err := setPersonaTenant(ctx, tx, tenantID); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `UPDATE persona_security_step SET state='INTERRUPTED',finished_at=$1
			WHERE tenant_id=$2 AND lease_id=$3 AND state='STARTED'`, at.UTC(), tenantID, leaseID)
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		recovered = n
		return nil
	})
	return recovered, err
}

func (s *Store) checkAndStartPersonaSecurityStep(ctx context.Context, conn dbport.Conn, lease PersonaSecurityLease, stepID string, at time.Time) error {
	tx, err := beginPersonaSecurityConn(conn, ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tenantID, _ := uuid.Parse(lease.TenantID)
	if err := setPersonaTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	var admissionDecision, mode string
	var admissionDeadline time.Time
	err = tx.QueryRow(ctx, `SELECT l.expires_at,ar.decision,ar.principal_chain->>'mode',ar.deadline
		FROM persona_security_lease l JOIN agent_run_request ar
		ON ar.tenant_id=l.tenant_id AND ar.request_id=l.admission_id
		WHERE l.tenant_id=$1 AND l.lease_id=$2`, tenantID, lease.LeaseID).
		Scan(&lease.ExpiresAt, &admissionDecision, &mode, &admissionDeadline)
	if errors.Is(err, dbport.ErrNoRows) {
		return ErrPersonaSecurityLeaseDenied
	}
	if err != nil {
		return err
	}
	lease.AdmissionDecision, lease.Mode, lease.AdmissionDeadline = admissionDecision, mode, admissionDeadline
	current := make(map[string]int64, len(personaLeaseScopes(lease)))
	for _, scope := range personaLeaseScopes(lease) {
		var epoch int64
		var state string
		if err := tx.QueryRow(ctx, `SELECT epoch,state FROM persona_security_scope
			WHERE tenant_id=$1 AND scope_kind=$2 AND scope_key=$3 FOR SHARE`, tenantID, scope.Kind, scope.Key).Scan(&epoch, &state); err != nil {
			return ErrPersonaSecurityLeaseRevoked
		}
		if state == "ACTIVE" {
			current[personaSecurityScopeIdentity(scope)] = epoch
		}
	}
	if err := agentsecurity.CheckDurablePersonaLease(lease, current, at); err != nil {
		if !at.Before(lease.ExpiresAt) {
			return ErrPersonaSecurityLeaseExpired
		}
		return ErrPersonaSecurityLeaseRevoked
	}
	var running int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM persona_security_step
		WHERE tenant_id=$1 AND lease_id=$2 AND state='STARTED'`, tenantID, lease.LeaseID).Scan(&running); err != nil {
		return err
	}
	if running != 0 {
		return ErrPersonaSecurityRecovery
	}
	_, err = tx.Exec(ctx, `INSERT INTO persona_security_step(tenant_id,lease_id,step_id,state,started_at)
		VALUES ($1,$2,$3,'STARTED',$4)`, tenantID, lease.LeaseID, stepID, at)
	if err != nil {
		return errors.Join(ErrPersonaSecurityStepConflict, err)
	}
	return tx.Commit(ctx)
}

func (s *Store) finishPersonaSecurityStep(ctx context.Context, conn dbport.Conn, tenantID uuid.UUID, leaseID, stepID, state string) error {
	tx, err := beginPersonaSecurityConn(conn, ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := setPersonaTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	n, err := tx.Exec(ctx, `UPDATE persona_security_step SET state=$1,finished_at=now()
		WHERE tenant_id=$2 AND lease_id=$3 AND step_id=$4 AND state='STARTED'`, state, tenantID, leaseID, stepID)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrPersonaSecurityStepConflict
	}
	return tx.Commit(ctx)
}

func setPersonaTenant(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenantID.String())
	return err
}

// personaSecurityLeaseInvalidHere returns the invalid-lease sentinel with the
// source location of the check that refused, for operator diagnosis.
func personaSecurityLeaseInvalidHere() error {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		return ErrPersonaSecurityLeaseInvalid
	}
	return fmt.Errorf("%w (%s:%d)", ErrPersonaSecurityLeaseInvalid, filepath.Base(file), line)
}

func beginPersonaSecurityConn(conn dbport.Conn, ctx context.Context) (dbport.Tx, error) {
	beginner, ok := conn.(interface {
		dbport.Conn
		dbport.Beginner
	})
	if !ok {
		return nil, personaSecurityLeaseInvalidHere()
	}
	return beginner.Begin(ctx)
}

func ensureActivePersonaScope(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, scope PersonaSecurityScope) (int64, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO persona_security_scope(tenant_id,scope_kind,scope_key)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, tenantID, scope.Kind, scope.Key); err != nil {
		return 0, err
	}
	var epoch int64
	var state string
	err := tx.QueryRow(ctx, `SELECT epoch,state FROM persona_security_scope
		WHERE tenant_id=$1 AND scope_kind=$2 AND scope_key=$3 FOR SHARE`, tenantID, scope.Kind, scope.Key).Scan(&epoch, &state)
	if err != nil || state != "ACTIVE" || epoch <= 0 {
		return 0, ErrPersonaSecurityLeaseRevoked
	}
	return epoch, nil
}

func personaLeaseScopes(lease PersonaSecurityLease) []PersonaSecurityScope {
	return []PersonaSecurityScope{
		{Kind: "TENANT", Key: lease.TenantID},
		{Kind: "PRINCIPAL", Key: lease.PrincipalID},
		{Kind: "PERSONA", Key: lease.PersonaID},
		{Kind: "VERSION", Key: lease.PersonaID + ":" + lease.PersonaVersion},
		{Kind: "INSTALLATION", Key: lease.InstallationID},
		{Kind: "RUN", Key: lease.RunID},
	}
}

func personaLeaseEpochs(lease PersonaSecurityLease) []int64 {
	return []int64{lease.TenantEpoch, lease.PrincipalEpoch, lease.PersonaEpoch, lease.VersionEpoch, lease.InstallationEpoch, lease.RunEpoch}
}

func personaScopeLockKey(tenantID uuid.UUID, scope PersonaSecurityScope) string {
	return "persona-security:" + tenantID.String() + ":" + scope.Kind + ":" + scope.Key
}

func personaSecurityScopeIdentity(scope PersonaSecurityScope) string {
	return scope.Kind + ":" + scope.Key
}

func validPersonaSecurityScope(scope PersonaSecurityScope) bool {
	if strings.TrimSpace(scope.Key) == "" || strings.TrimSpace(scope.Key) != scope.Key {
		return false
	}
	switch scope.Kind {
	case "TENANT", "PRINCIPAL", "PERSONA", "VERSION", "INSTALLATION", "RUN":
		return true
	default:
		return false
	}
}

func acquirePersonaScopeLocks(ctx context.Context, conn dbport.Conn, tenantID uuid.UUID, scopes []PersonaSecurityScope) (func(), error) {
	keys := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		keys = append(keys, personaScopeLockKey(tenantID, scope))
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, key); err != nil {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			for i := len(keys) - 1; i >= 0; i-- {
				_, _ = conn.Exec(releaseCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, keys[i])
			}
			return nil, fmt.Errorf("agentstore: acquire persona scope fence: %w", err)
		}
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		for i := len(keys) - 1; i >= 0; i-- {
			_, _ = conn.Exec(releaseCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, keys[i])
		}
	}, nil
}

// samePersonaSecurityLease reports whether a request names the lease already
// issued for an admission. The issue time is not compared: a run asks for its
// lease again on every model turn, and the lease issued first is the lease.
// Expiry is compared at the precision the database stores.
func samePersonaSecurityLease(a, b PersonaSecurityLease) bool {
	expiresDelta := a.ExpiresAt.Sub(b.ExpiresAt)
	if expiresDelta < 0 {
		expiresDelta = -expiresDelta
	}
	return a.TenantID == b.TenantID && a.AdmissionID == b.AdmissionID && a.IssuerID == b.IssuerID &&
		a.AuthorityRef == b.AuthorityRef && a.PolicyDigest == b.PolicyDigest && a.InvocationID == b.InvocationID &&
		a.RunID == b.RunID && a.PrincipalID == b.PrincipalID && a.PersonaID == b.PersonaID &&
		a.PersonaVersion == b.PersonaVersion && a.InstallationID == b.InstallationID &&
		a.TenantEpoch == b.TenantEpoch && a.PrincipalEpoch == b.PrincipalEpoch && a.PersonaEpoch == b.PersonaEpoch &&
		a.VersionEpoch == b.VersionEpoch && a.InstallationEpoch == b.InstallationEpoch && a.RunEpoch == b.RunEpoch &&
		expiresDelta < time.Microsecond
}
