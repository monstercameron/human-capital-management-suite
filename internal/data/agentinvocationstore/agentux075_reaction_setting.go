package agentinvocationstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ReactionSetting is an agent's owner's choice about the reaction the agent puts
// on the questions it is asked (AGENTUX-075). An agent with no stored choice
// reacts.
type ReactionSetting struct {
	TenantID  string
	PersonaID string
	// React is true when the agent reacts to questions with an emoji.
	React     bool
	Revision  uint64
	UpdatedBy string
	UpdatedAt time.Time
}

func cleanReactionPersona(persona string) bool {
	return persona != "" && persona == strings.TrimSpace(persona) && len(persona) <= 512
}

// PersonaReactionsEnabled reports whether the persona reacts to questions: true
// when its owner has not turned that off.
func (s *Store) PersonaReactionsEnabled(ctx context.Context, tenant, personaID string) (bool, error) {
	tid, err := s.resolveTenant(tenant)
	if ctx == nil || err != nil || !cleanReactionPersona(personaID) {
		return true, ErrInvalid
	}
	react := true
	err = s.db.RunTenantTx(ctx, tid, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT react_to_questions FROM persona_reaction_setting WHERE tenant_id=$1 AND persona_id=$2`, tid, personaID).Scan(&react)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if errors.Is(err, dbport.ErrNoRows) {
			react = true
		}
		return nil
	})
	if err != nil {
		return true, err
	}
	return react, nil
}

// SetPersonaReactions stores the owner's choice. The caller has already proved
// that actor may change this agent's setup.
func (s *Store) SetPersonaReactions(ctx context.Context, tenant, personaID, actor string, react bool, at time.Time) (ReactionSetting, error) {
	tid, err := s.resolveTenant(tenant)
	if ctx == nil || err != nil || !cleanReactionPersona(personaID) || strings.TrimSpace(actor) == "" || at.IsZero() {
		return ReactionSetting{}, ErrInvalid
	}
	result := ReactionSetting{TenantID: tenant, PersonaID: personaID}
	err = s.db.RunTenantTx(ctx, tid, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO persona_reaction_setting(tenant_id,persona_id,react_to_questions,revision,updated_by,updated_at)
			VALUES ($1,$2,$3,1,$4,$5)
			ON CONFLICT (tenant_id,persona_id) DO UPDATE SET react_to_questions=EXCLUDED.react_to_questions,
			  revision=persona_reaction_setting.revision+1,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at
			RETURNING react_to_questions,revision,updated_by,updated_at`, tid, personaID, react, actor, at.UTC()).
			Scan(&result.React, &result.Revision, &result.UpdatedBy, &result.UpdatedAt)
	})
	if err != nil {
		return ReactionSetting{}, err
	}
	return result, nil
}
