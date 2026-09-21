package meritstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/merit"
)

type compensationIntentPayload struct {
	IntentID       string `json:"intent_id"`
	IntentType     string `json:"intent_type"`
	IntentVersion  uint64 `json:"intent_version"`
	CycleID        string `json:"cycle_id"`
	CycleRevision  uint64 `json:"cycle_revision"`
	ParticipantID  string `json:"participant_id"`
	CurrentBasePay string `json:"current_base_pay"`
	TargetBasePay  string `json:"target_base_pay"`
	EffectiveAt    string `json:"effective_at"`
	Reason         string `json:"reason"`
	SourceDigest   string `json:"source_digest"`
}

func marshalCompensationIntent(i merit.CompensationChangeIntent) ([]byte, error) {
	current, err := textMoney(i.CurrentBasePay)
	if err != nil {
		return nil, fmt.Errorf("meritstore: marshal current base pay: %w", err)
	}
	target, err := textMoney(i.TargetBasePay)
	if err != nil {
		return nil, fmt.Errorf("meritstore: marshal target base pay: %w", err)
	}
	b, err := json.Marshal(compensationIntentPayload{i.IntentID, i.IntentType, i.IntentVersion, i.CycleID, i.CycleRevision, i.ParticipantID, current, target, i.EffectiveAt.String(), i.Reason, i.SourceDigest})
	if err != nil {
		return nil, fmt.Errorf("meritstore: marshal compensation intent: %w", err)
	}
	return b, nil
}

// EnqueueCompensationChangeIntentsTx durably fences child intents inside a
// transaction owned by the caller. The caller may atomically add its own
// downstream handoff before committing. Only newly inserted intents return.
func (s *Store) EnqueueCompensationChangeIntentsTx(ctx context.Context, tx dbport.Tx, tenantID string, cycle merit.MeritCycle) ([]merit.CompensationChangeIntent, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, invalid("transaction capability is required")
	}
	children, err := cycle.CompensationChangeIntents()
	if err != nil {
		return nil, invalid(err.Error())
	}
	var persistedDigest, persistedState string
	if err := tx.QueryRow(ctx, `SELECT canonical_digest,state FROM merit_cycle_revision
		WHERE tenant_id=$1 AND cycle_id=$2 AND revision=$3`, tid, cycle.CycleID, int64(cycle.Revision)).Scan(&persistedDigest, &persistedState); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return nil, notFound("finalized merit cycle revision is absent")
		}
		return nil, fmt.Errorf("meritstore: verify finalized cycle: %w", err)
	}
	if persistedState != string(merit.CycleFinalized) || domainDigest(persistedDigest) != cycle.CanonicalDigest {
		return nil, invalid("cycle does not match the persisted finalized revision")
	}
	for _, child := range children {
		var digest string
		err := tx.QueryRow(ctx, `SELECT canonical_digest FROM merit_compensation_intent_emission WHERE tenant_id=$1 AND intent_id=$2`, tid, child.IntentID).Scan(&digest)
		if err == nil && domainDigest(digest) != child.CanonicalDigest {
			return nil, merit.ErrIntentConflict
		}
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return nil, fmt.Errorf("meritstore: inspect compensation intent: %w", err)
		}
	}
	fresh := make([]merit.CompensationChangeIntent, 0, len(children))
	statements := make([]dbport.Statement, 0, len(children))
	for _, child := range children {
		payload, err := marshalCompensationIntent(child)
		if err != nil {
			return nil, err
		}
		statements = append(statements, dbport.Statement{SQL: `INSERT INTO merit_compensation_intent_emission
			(tenant_id,intent_id,cycle_id,cycle_revision,participant_id,payload,canonical_digest,source_digest)
			VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8) ON CONFLICT (tenant_id,intent_id) DO NOTHING`,
			Args: []any{tid, child.IntentID, child.CycleID, int64(child.CycleRevision), child.ParticipantID, string(payload), storageDigest(child.CanonicalDigest), storageDigest(child.SourceDigest)},
		})
	}
	counts, err := dbport.ExecAll(ctx, tx, statements)
	if err != nil {
		if failed := dbport.FailedStatement(counts, len(children)); failed >= 0 && failed < len(children) {
			return nil, fmt.Errorf("meritstore: enqueue compensation intent %s: %w", children[failed].IntentID, err)
		}
		return nil, fmt.Errorf("meritstore: enqueue compensation intent: %w", err)
	}
	for i, child := range children {
		if counts[i] == 1 {
			fresh = append(fresh, child)
			continue
		}
		var storedDigest string
		if err := tx.QueryRow(ctx, `SELECT canonical_digest FROM merit_compensation_intent_emission WHERE tenant_id=$1 AND intent_id=$2`, tid, child.IntentID).Scan(&storedDigest); err != nil {
			return nil, fmt.Errorf("meritstore: verify compensation intent retry: %w", err)
		}
		if domainDigest(storedDigest) != child.CanonicalDigest {
			return nil, merit.ErrIntentConflict
		}
	}
	return fresh, nil
}

// EnqueueCompensationChangeIntents is the convenience transaction boundary.
func (s *Store) EnqueueCompensationChangeIntents(ctx context.Context, tenantID string, cycle merit.MeritCycle) ([]merit.CompensationChangeIntent, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var fresh []merit.CompensationChangeIntent
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var err error
		fresh, err = s.EnqueueCompensationChangeIntentsTx(ctx, tx, tenantID, cycle)
		return err
	})
	return fresh, err
}

// StoredCompensationChangeIntents returns the durable, append-only children
// for one finalized cycle revision.
func (s *Store) StoredCompensationChangeIntents(ctx context.Context, tenantID, cycleID string, revision uint64) ([]merit.CompensationChangeIntent, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var out []merit.CompensationChangeIntent
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT payload::text,canonical_digest FROM merit_compensation_intent_emission
			WHERE tenant_id=$1 AND cycle_id=$2 AND cycle_revision=$3 ORDER BY participant_id`, tid, cycleID, int64(revision))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var digest string
			if err := rows.Scan(&raw, &digest); err != nil {
				return err
			}
			var p compensationIntentPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return invalid("stored compensation intent payload is invalid")
			}
			current, err := parseMoney(p.CurrentBasePay)
			if err != nil {
				return invalid(fmt.Sprintf("stored current base pay is invalid: %s: %v", p.CurrentBasePay, err))
			}
			target, err := parseMoney(p.TargetBasePay)
			if err != nil {
				return invalid("stored target base pay is invalid")
			}
			effective, err := parseInstant(p.EffectiveAt)
			if err != nil {
				return invalid("stored compensation effective time is invalid")
			}
			i := merit.CompensationChangeIntent{IntentID: p.IntentID, IntentType: p.IntentType, IntentVersion: p.IntentVersion, CycleID: p.CycleID, CycleRevision: p.CycleRevision, ParticipantID: p.ParticipantID, CurrentBasePay: current, TargetBasePay: target, EffectiveAt: effective, Reason: p.Reason, SourceDigest: p.SourceDigest, CanonicalDigest: domainDigest(digest)}
			if err := i.Validate(); err != nil {
				return invalid(err.Error())
			}
			out = append(out, i)
		}
		return rows.Err()
	})
	return out, err
}
