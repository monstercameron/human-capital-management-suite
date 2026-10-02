// Package agenttriggerstore persists native schedule publications and outbox
// rows in the Scheduling source database, and governed subscription journals
// in the isolated Agent database. Admission and execution stay Agent-owned.
package agenttriggerstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type TenantTxRunner interface {
	RunTenantTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
}

func (s *Store) fenceTx(ctx context.Context, fn func(dbport.Tx) error) error {
	runner, ok := s.runner.(interface {
		RunTenantFenceTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
	})
	if !ok {
		return scheduled.ErrInvalidSchedule
	}
	return runner.RunTenantFenceTx(ctx, s.tenantID, fn)
}

type Store struct {
	runner     TenantTxRunner
	tenantID   uuid.UUID
	tenant     string
	executions ScheduleExecutions
}

func New(runner TenantTxRunner, tenantID uuid.UUID, tenant string) (*Store, error) {
	if runner == nil || tenantID == uuid.Nil || tenant == "" || strings.TrimSpace(tenant) != tenant {
		return nil, scheduled.ErrInvalidSchedule
	}
	return &Store{runner: runner, tenantID: tenantID, tenant: tenant}, nil
}
func (s *Store) scoped(tenant string) error {
	if s == nil || tenant != s.tenant {
		return scheduled.ErrAuthority
	}
	return nil
}
func (s *Store) Load(ctx context.Context, tenant, id string) (scheduled.Schedule, bool, error) {
	if err := s.scoped(tenant); err != nil {
		return scheduled.Schedule{}, false, err
	}
	var result scheduled.Schedule
	found := false
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var payload []byte
		var rev uint64
		err := tx.QueryRow(ctx, `SELECT revision,payload FROM agent_schedule_state WHERE tenant_id=$1 AND schedule_id=$2`, s.tenantID, id).Scan(&rev, &payload)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = json.Unmarshal(payload, &result); err != nil {
			return err
		}
		if result.Revision != rev || result.Trigger.Definition.TenantID != tenant || result.Trigger.Definition.ID != id {
			return scheduled.ErrRevision
		}
		if err = scheduled.ValidateSchedule(result); err != nil {
			return err
		}
		found = true
		return nil
	})
	return result, found, err
}
func (s *Store) List(ctx context.Context, tenant string) ([]scheduled.Schedule, error) {
	if err := s.scoped(tenant); err != nil {
		return nil, err
	}
	var result []scheduled.Schedule
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT payload FROM agent_schedule_state WHERE tenant_id=$1 ORDER BY schedule_id`, s.tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var payload []byte
			var record scheduled.Schedule
			if err := rows.Scan(&payload); err != nil {
				return err
			}
			if err := json.Unmarshal(payload, &record); err != nil {
				return err
			}
			if record.Trigger.Definition.TenantID != tenant {
				return scheduled.ErrAuthority
			}
			if err := scheduled.ValidateSchedule(record); err != nil {
				return err
			}
			result = append(result, record)
		}
		return rows.Err()
	})
	return result, err
}
func (s *Store) Save(ctx context.Context, record scheduled.Schedule, expected uint64, audit scheduled.Audit) error {
	if err := s.scoped(record.Trigger.Definition.TenantID); err != nil {
		return err
	}
	if err := scheduled.ValidateSchedule(record); err != nil {
		return err
	}
	if expected >= math.MaxInt64 || record.Revision != expected+1 || audit.Revision != record.Revision || audit.TenantID != s.tenant || audit.ScheduleID != record.Trigger.Definition.ID || audit.ActorID == "" || audit.At.IsZero() {
		return scheduled.ErrRevision
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	auditPayload, err := json.Marshal(audit)
	if err != nil {
		return err
	}
	return s.fenceTx(ctx, func(tx dbport.Tx) error {
		var n int64
		var err error
		if expected == 0 {
			n, err = tx.Exec(ctx, `INSERT INTO agent_schedule_state(tenant_id,schedule_id,revision,payload) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT DO NOTHING`, s.tenantID, record.Trigger.Definition.ID, record.Revision, string(payload))
		} else {
			n, err = tx.Exec(ctx, `UPDATE agent_schedule_state SET revision=$3,payload=$4::jsonb WHERE tenant_id=$1 AND schedule_id=$2 AND revision=$5`, s.tenantID, record.Trigger.Definition.ID, record.Revision, string(payload), expected)
		}
		if err != nil {
			return err
		}
		if n != 1 {
			return scheduled.ErrRevision
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_schedule_audit(tenant_id,schedule_id,revision,actor_id,action,occurred_at,payload) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, s.tenantID, audit.ScheduleID, audit.Revision, audit.ActorID, string(audit.Action), audit.At, string(auditPayload))
		return err
	})
}
func (s *Store) AppendWindow(ctx context.Context, record scheduled.Schedule, end values.Instant, deliveries []scheduled.Delivery) error {
	if err := s.scoped(record.Trigger.Definition.TenantID); err != nil {
		return err
	}
	if record.State != scheduled.StateActive || record.Cursor.Validate() != nil || end.Validate() != nil || end.Compare(record.Cursor) <= 0 {
		return scheduled.ErrInvalidSchedule
	}
	return s.fenceTx(ctx, func(tx dbport.Tx) error {
		var payload []byte
		var revision uint64
		if err := tx.QueryRow(ctx, `SELECT revision,payload FROM agent_schedule_state WHERE tenant_id=$1 AND schedule_id=$2 FOR UPDATE`, s.tenantID, record.Trigger.Definition.ID).Scan(&revision, &payload); err != nil {
			return err
		}
		var stored scheduled.Schedule
		if err := json.Unmarshal(payload, &stored); err != nil {
			return err
		}
		if revision != record.Revision || stored.State != scheduled.StateActive || stored.Cursor.Compare(record.Cursor) != 0 {
			return scheduled.ErrRevision
		}
		storm := stored.Trigger.Definition.Storm
		if storm.MaxFiringsPerWindow == 0 {
			storm = stored.Trigger.Definition.StormPolicy
		}
		if len(deliveries) > 0 {
			var prior int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_schedule_outbox WHERE tenant_id=$1 AND schedule_id=$2 AND enqueued_at>$3`, s.tenantID, record.Trigger.Definition.ID, end.Time().Add(-storm.Window)).Scan(&prior); err != nil {
				return err
			}
			if uint64(prior)+uint64(len(deliveries)) > uint64(storm.MaxFiringsPerWindow) {
				return schedule.ErrStorm
			}
			outstanding, err := s.outstandingKeys(ctx, tx, record.Trigger.Definition.ID)
			if err != nil {
				return err
			}
			if uint64(len(outstanding))+uint64(len(deliveries)) > uint64(storm.MaxFiringsPerWindow) {
				return schedule.ErrStorm
			}
		}
		for _, delivery := range deliveries {
			if err := validateDelivery(delivery, stored); err != nil {
				return err
			}
			digest, err := agentrun.AdmissionRequestDigest(delivery.Request)
			if err != nil {
				return err
			}
			data, err := json.Marshal(delivery)
			if err != nil {
				return err
			}
			n, err := tx.Exec(ctx, `INSERT INTO agent_schedule_outbox(tenant_id,source_key,schedule_id,request_digest,enqueued_at,payload) VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT DO NOTHING`, s.tenantID, delivery.Key, delivery.ScheduleID, digest, delivery.EnqueuedAt, string(data))
			if err != nil {
				return err
			}
			if n != 1 {
				return scheduled.ErrReceiptConflict
			}
		}
		stored.Cursor = end
		payload, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agent_schedule_state SET payload=$3::jsonb WHERE tenant_id=$1 AND schedule_id=$2 AND revision=$4`, s.tenantID, record.Trigger.Definition.ID, string(payload), revision)
		return err
	})
}

// WithExecutionFence holds the source control row while the enclosed durable
// common-run claim commits. Concurrent claims observe the preceding committed
// run state before deciding overlap; model/network work runs after this returns.
func (s *Store) WithExecutionFence(ctx context.Context, tenant, key string, claim func(error) (runstate.Run, error)) (runstate.Run, error) {
	if err := s.scoped(tenant); err != nil {
		return runstate.Run{}, err
	}
	if claim == nil {
		return runstate.Run{}, scheduled.ErrInvalidFiring
	}
	delivery, found, err := s.GetDelivery(ctx, tenant, key)
	if err != nil {
		return runstate.Run{}, err
	}
	if !found {
		return runstate.Run{}, scheduled.ErrInvalidFiring
	}
	var result runstate.Run
	err = s.fenceTx(ctx, func(tx dbport.Tx) error {
		var payload []byte
		if err := tx.QueryRow(ctx, `SELECT payload FROM agent_schedule_state WHERE tenant_id=$1 AND schedule_id=$2 FOR UPDATE`, s.tenantID, delivery.ScheduleID).Scan(&payload); err != nil {
			return err
		}
		var current scheduled.Schedule
		if err := json.Unmarshal(payload, &current); err != nil {
			return err
		}
		if current.State != scheduled.StateActive || current.Trigger.Ref() != delivery.Firing.Occurrence.Trigger {
			result, err = claim(scheduled.ErrInactive)
			return err
		}
		outstanding, readErr := s.outstandingKeys(ctx, tx, delivery.ScheduleID)
		if readErr != nil {
			return readErr
		}
		var firstKey string
		if len(outstanding) > 0 {
			firstKey = outstanding[0]
		}
		if firstKey != "" && firstKey != key {
			policy := current.Trigger.Definition.Overlap
			if policy == "" {
				policy = current.Trigger.Definition.OverlapPolicy
			}
			switch policy {
			case schedule.OverlapQueue:
				result, err = claim(scheduled.ErrOverlapQueued)
			case schedule.OverlapSkip:
				result, err = claim(scheduled.ErrOverlapSkipped)
			default:
				result, err = claim(scheduled.ErrOverlapRefused)
			}
			return err
		}
		result, err = claim(nil)
		return err
	})
	return result, err
}
func validateDelivery(d scheduled.Delivery, s scheduled.Schedule) error {
	source, err := d.Firing.SourceIdentity()
	if err != nil {
		return err
	}
	source.Ref = source.Key
	if s.SourceKind == agentrun.SourceAnnouncement {
		source.Kind = agentrun.SourceAnnouncement
	}
	if d.TenantID != s.Trigger.Definition.TenantID || d.ScheduleID != s.Trigger.Definition.ID || d.ControlRevision != s.Revision || d.Key != source.Key || d.Request.Source != source || d.EnqueuedAt.IsZero() || !d.Request.Deadline.After(d.EnqueuedAt) || d.Request.InstallationID != s.InstallationID || d.Request.Agent.AgentID != d.Firing.Target.Agent.ID || d.Firing.Occurrence.Trigger != s.Trigger.Ref() || s.Trigger.Definition.AgentRun == nil || d.Firing.Target != *s.Trigger.Definition.AgentRun {
		return scheduled.ErrInvalidFiring
	}
	target := d.Firing.Target
	r := d.Request
	principal := agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: s.AgentPrincipalID, SponsorID: target.SponsorID}
	if s.SourceKind == agentrun.SourceAnnouncement {
		principal.RequesterID = s.RequesterID
		if r.Persona == nil || s.Persona == nil || *r.Persona != *s.Persona {
			return scheduled.ErrInvalidFiring
		}
	}
	if r.Agent != (agentrun.VersionRef{AgentID: target.Agent.ID, Version: target.Agent.Version, Digest: target.Agent.Digest}) || r.LegalEntity != s.LegalEntity || r.Context != s.Context || r.Principal != principal || r.Purpose != target.Purpose || r.Audience != (agentrun.AudienceScope{ID: target.Destination.AudienceID, SnapshotID: target.Destination.AudienceSnapshotID, Digest: target.Destination.AudienceDigest}) || r.Budget != (agentrun.Budget{MaxCostMicros: target.Budget.MaxCostMicros, MaxInputTokens: target.Budget.MaxInputTokens, MaxOutputTokens: target.Budget.MaxOutputTokens}) || !r.Deadline.Equal(d.EnqueuedAt.Add(s.RunTimeout)) {
		return scheduled.ErrInvalidFiring
	}
	return nil
}
func (s *Store) Pending(ctx context.Context, tenant string, limit int) ([]scheduled.Delivery, error) {
	if err := s.scoped(tenant); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, scheduled.ErrInvalidFiring
	}
	var deliveries []scheduled.Delivery
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT o.payload FROM agent_schedule_outbox o LEFT JOIN agent_schedule_receipt r ON r.tenant_id=o.tenant_id AND r.source_key=o.source_key WHERE o.tenant_id=$1 AND r.source_key IS NULL ORDER BY o.enqueued_at,o.source_key LIMIT $2`, s.tenantID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var data []byte
			var delivery scheduled.Delivery
			if err := rows.Scan(&data); err != nil {
				return err
			}
			if err := json.Unmarshal(data, &delivery); err != nil {
				return err
			}
			if delivery.TenantID != tenant {
				return scheduled.ErrAuthority
			}
			deliveries = append(deliveries, delivery)
		}
		return rows.Err()
	})
	return deliveries, err
}
func (s *Store) GetDelivery(ctx context.Context, tenant, key string) (scheduled.Delivery, bool, error) {
	if err := s.scoped(tenant); err != nil {
		return scheduled.Delivery{}, false, err
	}
	var d scheduled.Delivery
	found := false
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var data []byte
		var digest string
		err := tx.QueryRow(ctx, `SELECT payload,request_digest FROM agent_schedule_outbox WHERE tenant_id=$1 AND source_key=$2`, s.tenantID, key).Scan(&data, &digest)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &d); err != nil {
			return err
		}
		got, err := agentrun.AdmissionRequestDigest(d.Request)
		if err != nil || got != digest || d.TenantID != tenant || d.Key != key {
			return scheduled.ErrReceiptConflict
		}
		found = true
		return nil
	})
	return d, found, err
}
func (s *Store) Acknowledge(ctx context.Context, tenant, key string, receipt scheduled.Receipt) error {
	if err := s.scoped(tenant); err != nil {
		return err
	}
	if receipt.SourceKey != key || receipt.RunRequestID == "" || receipt.RequestDigest == "" {
		return scheduled.ErrReceiptConflict
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var digest string
		if err := tx.QueryRow(ctx, `SELECT request_digest FROM agent_schedule_outbox WHERE tenant_id=$1 AND source_key=$2`, s.tenantID, key).Scan(&digest); err != nil {
			return err
		}
		if digest != receipt.RequestDigest {
			return scheduled.ErrReceiptConflict
		}
		n, err := tx.Exec(ctx, `INSERT INTO agent_schedule_receipt(tenant_id,source_key,payload) VALUES($1,$2,$3::jsonb) ON CONFLICT DO NOTHING`, s.tenantID, key, string(data))
		if err != nil || n == 1 {
			return err
		}
		var prior []byte
		if err := tx.QueryRow(ctx, `SELECT payload FROM agent_schedule_receipt WHERE tenant_id=$1 AND source_key=$2`, s.tenantID, key).Scan(&prior); err != nil {
			return err
		}
		var r scheduled.Receipt
		if err := json.Unmarshal(prior, &r); err != nil {
			return err
		}
		if r != receipt {
			return scheduled.ErrReceiptConflict
		}
		return nil
	})
}
func (s *Store) LoadReceipt(ctx context.Context, tenant, key string) (scheduled.Receipt, bool, error) {
	if err := s.scoped(tenant); err != nil {
		return scheduled.Receipt{}, false, err
	}
	var receipt scheduled.Receipt
	found := false
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var data []byte
		err := tx.QueryRow(ctx, `SELECT payload FROM agent_schedule_receipt WHERE tenant_id=$1 AND source_key=$2`, s.tenantID, key).Scan(&data)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &receipt); err != nil {
			return err
		}
		if receipt.SourceKey != key {
			return fmt.Errorf("%w: receipt identity", scheduled.ErrReceiptConflict)
		}
		found = true
		return nil
	})
	return receipt, found, err
}
