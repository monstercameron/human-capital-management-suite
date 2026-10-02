package chatstore

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type FilterStore struct{ Store *Store }

func NewFilterStore(s *Store) *FilterStore { return &FilterStore{Store: s} }
func (s *FilterStore) tx(ctx context.Context, tenantID string, f func(dbport.Tx) error) error {
	if s == nil || s.Store == nil || strings.TrimSpace(tenantID) == "" {
		return chatfilter.ErrInvalid
	}
	return s.Store.RunTenantTx(ctx, tenantID, f)
}
func (s *FilterStore) Definitions(ctx context.Context, tenantID string) ([]chatfilter.Definition, error) {
	out := []chatfilter.Definition{}
	err := s.tx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT definition FROM chat_filter_version WHERE tenant_id=$1 ORDER BY rule_id,version`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				return err
			}
			var d chatfilter.Definition
			if err = json.Unmarshal(raw, &d); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}
func (s *FilterStore) CreateVersion(ctx context.Context, tenantID string, d chatfilter.Definition) error {
	if d.ID == "" || d.Name == "" || !chatfilter.ValidVersion(d.Version) {
		return chatfilter.ErrInvalid
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	err = s.tx(ctx, tenantID, func(tx dbport.Tx) error {
		// Serialize versions of one rule, including concurrent first versions.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenantID+":"+d.ID); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_filter_version WHERE tenant_id=$1 AND rule_id=$2 AND string_to_array(version,'.')::bigint[] >= string_to_array($3,'.')::bigint[])`, tenantID, d.ID, d.Version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return chatfilter.ErrConflict
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_filter_version(tenant_id,rule_id,version,definition) VALUES($1,$2,$3,$4::jsonb)`, tenantID, d.ID, d.Version, raw)
		return err
	})
	return err
}
func (s *FilterStore) Enablements(ctx context.Context, tenantID string) ([]chatfilter.Enablement, error) {
	out := []chatfilter.Enablement{}
	err := s.tx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT rule_id,channel_id,enabled,dry_run_until,action FROM chat_filter_enablement WHERE tenant_id=$1 ORDER BY rule_id,channel_id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e chatfilter.Enablement
			var until *time.Time
			if err = rows.Scan(&e.RuleID, &e.Channel, &e.Enabled, &until, &e.Action); err != nil {
				return err
			}
			if until != nil {
				e.DryRunUntil = *until
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}
func (s *FilterStore) PutEnablement(ctx context.Context, tenantID string, e chatfilter.Enablement) error {
	if e.RuleID == "" {
		return chatfilter.ErrInvalid
	}
	var until *time.Time
	if !e.DryRunUntil.IsZero() {
		until = &e.DryRunUntil
	}
	return s.tx(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_filter_enablement(tenant_id,rule_id,channel_id,enabled,dry_run_until,action) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,rule_id,channel_id) DO UPDATE SET enabled=EXCLUDED.enabled,dry_run_until=EXCLUDED.dry_run_until,action=EXCLUDED.action,revision=chat_filter_enablement.revision+1`, tenantID, e.RuleID, e.Channel, e.Enabled, until, e.Action)
		return err
	})
}
func (s *FilterStore) RecordHits(ctx context.Context, tenantID string, records []chatfilter.Record) error {
	return s.tx(ctx, tenantID, func(tx dbport.Tx) error {
		for _, r := range records {
			if r.Tenant != tenantID || r.Hit.Masked != "[removed word]" {
				return chatfilter.ErrInvalid
			}
			raw, err := json.Marshal(r.Hit)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO chat_filter_hit(tenant_id,channel_id,subject_id,created_at,rule_id,rule_version,hit) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, tenantID, r.Channel, r.Subject, r.At, r.Hit.RuleID, r.Hit.Version, raw); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *FilterStore) Hits(ctx context.Context, tenantID string) ([]chatfilter.Record, error) {
	return s.SearchHitRecords(ctx, tenantID, "", "", 0)
}
func (s *FilterStore) SearchHitRecords(ctx context.Context, tenantID, query, channel string, before int64) ([]chatfilter.Record, error) {
	out := []chatfilter.Record{}
	err := s.tx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,channel_id,subject_id,created_at,hit FROM chat_filter_hit WHERE tenant_id=$1 AND ($2=0 OR id<$2) AND ($3='' OR strpos(lower(concat_ws(' ',hit->>'RuleName',hit->>'RuleID',hit->>'Version',hit->>'Action')),lower($3))>0) AND ($4='' OR channel_id=$4) ORDER BY id DESC LIMIT 200`, tenantID, before, query, channel)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r := chatfilter.Record{Tenant: tenantID}
			var raw []byte
			if err = rows.Scan(&r.ID, &r.Channel, &r.Subject, &r.At, &raw); err != nil {
				return err
			}
			if err = json.Unmarshal(raw, &r.Hit); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// A flagged hit is delivered by being in chat_filter_hit: the moderation queue
// reads it from there. A "notify" hit is told to the managers of the channel
// the filter names (chatmod003_notify.go).
func (s *FilterStore) DeliverFilterHit(ctx context.Context, r chatfilter.Record) error {
	if s == nil || s.Store == nil {
		return chatfilter.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Tenant == "" || r.Hit.RuleID == "" {
		return chatfilter.ErrInvalid
	}
	if r.Hit.Action != "notify" {
		return nil
	}
	return s.notifyFilterTarget(ctx, r)
}

var _ chatfilter.Store = (*FilterStore)(nil)

// SearchFilters and SearchFilterHits authorize before loading tenant rows.
func (s *FilterStore) SearchFilters(ctx context.Context, actor chatfilter.Actor, authority chatfilter.Authority, query string) ([]chatfilter.Definition, error) {
	service := chatfilter.Service{Store: s, Registry: chatfilter.NewRegistry(), Authority: authority}
	return service.SearchFilters(ctx, actor, query)
}
func (s *FilterStore) SearchFilterHits(ctx context.Context, actor chatfilter.Actor, authority chatfilter.Authority, query string) ([]chatfilter.Record, error) {
	service := chatfilter.Service{Store: s, Registry: chatfilter.NewRegistry(), Authority: authority}
	return service.ReadHits(ctx, actor, query)
}
