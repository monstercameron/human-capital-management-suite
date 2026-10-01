package agenttriggerstore

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/subscription"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func (s *Store) LoadSubscription(ctx context.Context, tenant, id string) (subscription.Definition, bool, error) {
	if err := s.scoped(tenant); err != nil {
		return subscription.Definition{}, false, err
	}
	var d subscription.Definition
	found := false
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var data []byte
		var revision uint64
		err := tx.QueryRow(ctx, `SELECT revision,payload FROM agent_subscription_state WHERE tenant_id=$1 AND subscription_id=$2`, s.tenantID, id).Scan(&revision, &data)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &d); err != nil {
			return err
		}
		if d.Revision != revision || d.Declaration.TenantID != tenant || d.Declaration.ID != id {
			return subscription.ErrRevision
		}
		if err := subscription.ValidateDefinition(d); err != nil {
			return err
		}
		found = true
		return nil
	})
	return d, found, err
}
func (s *Store) SaveSubscription(ctx context.Context, d subscription.Definition, expected uint64, a subscription.Audit) error {
	if err := s.scoped(d.Declaration.TenantID); err != nil {
		return err
	}
	if err := subscription.ValidateDefinition(d); err != nil {
		return err
	}
	if expected >= math.MaxInt64 || d.Revision != expected+1 || a.Revision != d.Revision || a.TenantID != s.tenant || a.SubscriptionID != d.Declaration.ID || a.ActorID == "" || a.At.IsZero() {
		return subscription.ErrRevision
	}
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	audit, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var n int64
		var err error
		if expected == 0 {
			n, err = tx.Exec(ctx, `INSERT INTO agent_subscription_state(tenant_id,subscription_id,revision,payload) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT DO NOTHING`, s.tenantID, d.Declaration.ID, d.Revision, string(data))
		} else {
			n, err = tx.Exec(ctx, `UPDATE agent_subscription_state SET revision=$3,payload=$4::jsonb WHERE tenant_id=$1 AND subscription_id=$2 AND revision=$5`, s.tenantID, d.Declaration.ID, d.Revision, string(data), expected)
		}
		if err != nil {
			return err
		}
		if n != 1 {
			return subscription.ErrRevision
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_subscription_audit(tenant_id,subscription_id,revision,payload) VALUES($1,$2,$3,$4::jsonb)`, s.tenantID, d.Declaration.ID, d.Revision, string(audit))
		return err
	})
}
func (s *Store) ReserveEvent(ctx context.Context, event subscription.Delivery, debounce time.Duration) (subscription.Delivery, bool, error) {
	if err := s.scoped(event.TenantID); err != nil {
		return subscription.Delivery{}, false, err
	}
	if event.Key == "" || event.At.IsZero() || debounce < 0 || event.Candidate.Request.Source.Key != event.Key || event.Candidate.Request.Source.Ref != event.Key || event.Candidate.Request.Source.TenantID != s.tenant || event.Candidate.Request.Source.Kind != agentrun.SourceEvent {
		return subscription.Delivery{}, false, subscription.ErrInvalid
	}
	var result subscription.Delivery
	created := false
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var revision uint64
		var stateData []byte
		if err := tx.QueryRow(ctx, `SELECT revision,payload FROM agent_subscription_state WHERE tenant_id=$1 AND subscription_id=$2 FOR UPDATE`, s.tenantID, event.SubscriptionID).Scan(&revision, &stateData); err != nil {
			return err
		}
		var d subscription.Definition
		if err := json.Unmarshal(stateData, &d); err != nil {
			return err
		}
		if d.State != "ACTIVE" || revision != event.Revision {
			return subscription.ErrInactive
		}
		var prior []byte
		err := tx.QueryRow(ctx, `SELECT payload FROM agent_subscription_event WHERE tenant_id=$1 AND source_key=$2`, s.tenantID, event.Key).Scan(&prior)
		if err == nil {
			if err := json.Unmarshal(prior, &result); err != nil {
				return err
			}
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		var latest time.Time
		err = tx.QueryRow(ctx, `SELECT admitted_at FROM agent_subscription_event WHERE tenant_id=$1 AND subscription_id=$2 ORDER BY admitted_at DESC LIMIT 1`, s.tenantID, event.SubscriptionID).Scan(&latest)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if err == nil && (latest.After(event.At) || event.At.Sub(latest) < debounce) {
			return subscription.ErrDebounced
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_subscription_event(tenant_id,source_key,subscription_id,admitted_at,payload) VALUES($1,$2,$3,$4,$5::jsonb)`, s.tenantID, event.Key, event.SubscriptionID, event.At, string(payload))
		if err != nil {
			return err
		}
		result = event
		created = true
		return nil
	})
	return result, created, err
}
func (s *Store) GetEvent(ctx context.Context, tenant, key string) (subscription.Delivery, bool, error) {
	if err := s.scoped(tenant); err != nil {
		return subscription.Delivery{}, false, err
	}
	var event subscription.Delivery
	found := false
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var data []byte
		err := tx.QueryRow(ctx, `SELECT payload FROM agent_subscription_event WHERE tenant_id=$1 AND source_key=$2`, s.tenantID, key).Scan(&data)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		if event.TenantID != tenant || event.Key != key {
			return subscription.ErrInvalid
		}
		found = true
		return nil
	})
	return event, found, err
}
func (s *Store) PendingEvents(ctx context.Context, tenant string, limit int) ([]subscription.Delivery, error) {
	if err := s.scoped(tenant); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, subscription.ErrInvalid
	}
	var events []subscription.Delivery
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT e.payload FROM agent_subscription_event e LEFT JOIN agent_subscription_receipt r ON r.tenant_id=e.tenant_id AND r.source_key=e.source_key WHERE e.tenant_id=$1 AND r.source_key IS NULL ORDER BY e.admitted_at,e.source_key LIMIT $2`, s.tenantID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var data []byte
			var event subscription.Delivery
			if err := rows.Scan(&data); err != nil {
				return err
			}
			if err := json.Unmarshal(data, &event); err != nil {
				return err
			}
			if event.TenantID != tenant {
				return subscription.ErrInvalid
			}
			events = append(events, event)
		}
		return rows.Err()
	})
	return events, err
}
func (s *Store) AcknowledgeEvent(ctx context.Context, tenant, key string, record agentrun.Record) error {
	if err := s.scoped(tenant); err != nil {
		return err
	}
	if record.Request.Source.Key != key || record.Request.Source.TenantID != tenant || agentrun.ValidateAdmissionRecord(record) != nil {
		return subscription.ErrInvalid
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		var eventData []byte
		if err := tx.QueryRow(ctx, `SELECT payload FROM agent_subscription_event WHERE tenant_id=$1 AND source_key=$2`, s.tenantID, key).Scan(&eventData); err != nil {
			return err
		}
		var event subscription.Delivery
		if err := json.Unmarshal(eventData, &event); err != nil {
			return err
		}
		want, err := agentrun.AdmissionRequestDigest(event.Candidate.Request)
		if err != nil || want != record.RequestDigest {
			return subscription.ErrInvalid
		}
		n, err := tx.Exec(ctx, `INSERT INTO agent_subscription_receipt(tenant_id,source_key,payload) VALUES($1,$2,$3::jsonb) ON CONFLICT DO NOTHING`, s.tenantID, key, string(data))
		if err != nil || n == 1 {
			return err
		}
		var priorData []byte
		if err := tx.QueryRow(ctx, `SELECT payload FROM agent_subscription_receipt WHERE tenant_id=$1 AND source_key=$2`, s.tenantID, key).Scan(&priorData); err != nil {
			return err
		}
		var prior agentrun.Record
		if err := json.Unmarshal(priorData, &prior); err != nil {
			return err
		}
		if prior.ID != record.ID || prior.RequestDigest != record.RequestDigest || prior.Decision != record.Decision || prior.RefusalCode != record.RefusalCode {
			return subscription.ErrInvalid
		}
		return nil
	})
}
