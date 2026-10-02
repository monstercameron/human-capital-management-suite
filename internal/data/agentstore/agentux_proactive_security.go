package agentstore

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// WithAnnouncementSecurityFence uses the same tenant, principal, persona,
// installation and run stop scopes as mention execution. A stop waits for the
// bounded current step, then prevents the next model/output/delivery step.
func (s *Store) WithAnnouncementSecurityFence(ctx context.Context, tenant uuid.UUID, id string, fn func() error) error {
	if s == nil || ctx == nil || tenant == uuid.Nil || id == "" || fn == nil {
		return ErrPersonaSecurityLeaseDenied
	}
	return s.RunTenantFenceTx(ctx, tenant, func(tx dbport.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT request_payload FROM agent_run_request WHERE tenant_id=$1 AND request_id=$2 AND source_kind='ANNOUNCEMENT' AND decision='ACCEPTED'`, tenant, id).Scan(&raw); err != nil {
			return err
		}
		var request agentrun.Request
		if json.Unmarshal(raw, &request) != nil || request.Persona == nil || request.Principal.Mode != agentrun.ModeSponsored || request.Principal.InvokerID != "" {
			return ErrPersonaSecurityLeaseDenied
		}
		lease := PersonaSecurityLease{TenantID: tenant.String(), PrincipalID: request.Principal.AgentPrincipalID, PersonaID: request.Persona.ID, PersonaVersion: request.Persona.Version, InstallationID: request.InstallationID, RunID: id}
		scopes := personaLeaseScopes(lease)
		if request.Principal.SponsorID != lease.PrincipalID {
			scopes = append(scopes, PersonaSecurityScope{Kind: "PRINCIPAL", Key: request.Principal.SponsorID})
		}
		for _, scope := range scopes {
			if _, err := ensureActivePersonaScope(ctx, tx, tenant, scope); err != nil {
				return err
			}
			var state string
			if err := tx.QueryRow(ctx, `SELECT state FROM persona_security_scope WHERE tenant_id=$1 AND scope_kind=$2 AND scope_key=$3 FOR SHARE`, tenant, scope.Kind, scope.Key).Scan(&state); err != nil {
				return err
			}
			if state != "ACTIVE" {
				return ErrPersonaSecurityLeaseRevoked
			}
		}
		return fn()
	})
}
