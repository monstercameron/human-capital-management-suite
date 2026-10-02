// Package agentcoststore keeps agent spend limits, the audit of changes to
// them and the finished-run cost ledger in the agent database (migration
// 00047). Every call runs in a transaction bound to one tenant, so the
// tenant_isolation policies apply. It implements agentcost.Store.
package agentcoststore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentcost"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrInvalid is returned for a request that names no tenant or agent, or an
// unknown tenant.
var ErrInvalid = errors.New("agentcoststore: invalid request")

// callTimeout bounds each call, because agentcost.Store carries no context.
const callTimeout = 15 * time.Second

// Runner opens a tenant-bound transaction. *agentstore.Store satisfies it.
type Runner interface {
	RunTenantTx(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error
}

// Store implements agentcost.Store over the agent database.
type Store struct {
	db         Runner
	tenantUUID func(string) uuid.UUID
}

var _ agentcost.Store = (*Store)(nil)

// New builds a Store. tenantUUID maps a tenant key to its canonical UUID and
// must return uuid.Nil for an unknown tenant.
func New(db Runner, tenantUUID func(string) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, ErrInvalid
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

func (s *Store) tenant(key string) (uuid.UUID, error) {
	if s == nil || key == "" || key != strings.TrimSpace(key) {
		return uuid.Nil, ErrInvalid
	}
	id := s.tenantUUID(key)
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: unknown tenant", ErrInvalid)
	}
	return id, nil
}

func (s *Store) run(tenant string, fn func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error) error {
	id, err := s.tenant(tenant)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return s.db.RunTenantTx(ctx, id, func(tx dbport.Tx) error { return fn(ctx, tx, id) })
}

// LoadLimits returns the tenant's limits that are in force.
func (s *Store) LoadLimits(tenant string) ([]agentcost.Limit, error) {
	var out []agentcost.Limit
	err := s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT agent_id,conversation_id,max_runs,max_spend_micros FROM agent_spend_limits
			WHERE tenant_id=$1 AND (max_runs>0 OR max_spend_micros>0) ORDER BY agent_id,conversation_id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			limit := agentcost.Limit{TenantID: tenant}
			if err := rows.Scan(&limit.AgentID, &limit.ConversationID, &limit.MaxRuns, &limit.MaxSpendMicros); err != nil {
				return err
			}
			out = append(out, limit)
		}
		return rows.Err()
	})
	return out, err
}

type limitDoc struct {
	ConversationID string `json:"conversation_id"`
	MaxRuns        int64  `json:"max_runs"`
	MaxSpendMicros int64  `json:"max_spend_micros"`
}

func docOf(limit *agentcost.Limit) any {
	if limit == nil {
		return nil
	}
	raw, _ := json.Marshal(limitDoc{ConversationID: limit.ConversationID, MaxRuns: limit.MaxRuns, MaxSpendMicros: limit.MaxSpendMicros})
	return string(raw)
}

// SaveLimit upserts the limit and appends its audit row in one transaction.
func (s *Store) SaveLimit(event agentcost.AuditEvent, next agentcost.Limit, removed bool) error {
	if strings.TrimSpace(event.Actor) == "" || strings.TrimSpace(next.AgentID) == "" || event.Tenant != next.TenantID {
		return ErrInvalid
	}
	runs, micros := next.MaxRuns, next.MaxSpendMicros
	if removed {
		runs, micros = 0, 0
	}
	return s.run(next.TenantID, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		if _, err := tx.Exec(ctx, `INSERT INTO agent_spend_limits (tenant_id,agent_id,conversation_id,max_runs,max_spend_micros,updated_by,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (tenant_id,agent_id,conversation_id) DO UPDATE SET max_runs=EXCLUDED.max_runs,max_spend_micros=EXCLUDED.max_spend_micros,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`,
			id, next.AgentID, next.ConversationID, runs, micros, event.Actor, event.At.UTC()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agent_spend_limit_audit (tenant_id,agent_id,actor,at,before_json,after_json) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb)`,
			id, next.AgentID, event.Actor, event.At.UTC(), docOf(event.Before), docOf(event.After))
		return err
	})
}

// LimitAudit returns the audit trail of one agent, oldest first.
func (s *Store) LimitAudit(tenant, agentID string) ([]agentcost.AuditEvent, error) {
	var out []agentcost.AuditEvent
	err := s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT actor,at,before_json::text,after_json::text FROM agent_spend_limit_audit WHERE tenant_id=$1 AND agent_id=$2 ORDER BY id`, id, agentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			event := agentcost.AuditEvent{Tenant: tenant, Agent: agentID}
			var before, after *string
			if err := rows.Scan(&event.Actor, &event.At, &before, &after); err != nil {
				return err
			}
			event.At = event.At.UTC()
			event.Before, event.After = decodeLimit(tenant, agentID, before), decodeLimit(tenant, agentID, after)
			out = append(out, event)
		}
		return rows.Err()
	})
	return out, err
}

func decodeLimit(tenant, agentID string, raw *string) *agentcost.Limit {
	if raw == nil {
		return nil
	}
	var doc limitDoc
	if json.Unmarshal([]byte(*raw), &doc) != nil {
		return nil
	}
	return &agentcost.Limit{TenantID: tenant, AgentID: agentID, ConversationID: doc.ConversationID, MaxRuns: doc.MaxRuns, MaxSpendMicros: doc.MaxSpendMicros}
}

// Usage counts one agent's runs and spend since a time, in one conversation
// when conversation is not empty.
func (s *Store) Usage(tenant, agentID, conversation string, since time.Time) (int64, int64, error) {
	var runs, micros int64
	err := s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		return tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(spend_micros),0)::bigint FROM agent_run_costs
			WHERE tenant_id=$1 AND agent_id=$2 AND ($3='' OR conversation_id=$3) AND at>=$4`, id, agentID, conversation, since.UTC()).Scan(&runs, &micros)
	})
	return runs, micros, err
}

// AppendRun records one finished run. A run id already stored is left as it is.
func (s *Store) AppendRun(run agentcost.Run) error {
	if strings.TrimSpace(run.AgentID) == "" || strings.TrimSpace(run.RunID) == "" || run.SpendMicros < 0 || run.MessagesRead < 0 {
		return ErrInvalid
	}
	switch run.Kind {
	case agentcost.KindAnswer, agentcost.KindScreening, agentcost.KindDecision:
	default:
		return ErrInvalid
	}
	return s.run(run.TenantID, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		_, err := tx.Exec(ctx, `INSERT INTO agent_run_costs (tenant_id,agent_id,conversation_id,run_id,at,kind,spend_micros,answered,messages_read)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (tenant_id,run_id) DO NOTHING`,
			id, run.AgentID, run.ConversationID, run.RunID, run.At.UTC(), string(run.Kind), run.SpendMicros, run.Answered, run.MessagesRead)
		return err
	})
}

// RunsSince returns the tenant's runs that finished at or after since.
func (s *Store) RunsSince(tenant string, since time.Time) ([]agentcost.Run, error) {
	var out []agentcost.Run
	err := s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT agent_id,conversation_id,run_id,at,kind,spend_micros,answered,messages_read FROM agent_run_costs
			WHERE tenant_id=$1 AND at>=$2 ORDER BY at,run_id`, id, since.UTC())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			run := agentcost.Run{TenantID: tenant}
			var kind string
			if err := rows.Scan(&run.AgentID, &run.ConversationID, &run.RunID, &run.At, &kind, &run.SpendMicros, &run.Answered, &run.MessagesRead); err != nil {
				return err
			}
			run.Kind, run.At = agentcost.Kind(kind), run.At.UTC()
			out = append(out, run)
		}
		return rows.Err()
	})
	return out, err
}

// IsBusinessOwner reports whether the principal is the agent's business owner.
// The spend limits of an agent are the owner's to raise.
func (s *Store) IsBusinessOwner(tenant, agentID, principalID string) bool {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(principalID) == "" {
		return false
	}
	found := false
	err := s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM persona_owners WHERE tenant_id=$1 AND persona_id=$2 AND owner_role='BUSINESS_OWNER' AND principal_id=$3`, id, agentID, principalID).Scan(&n); err != nil {
			return err
		}
		found = n > 0
		return nil
	})
	return err == nil && found
}

// OwnedAgent is one agent a person is the business owner of.
type OwnedAgent struct {
	ID   string
	Name string
}

// OwnedAgents lists the agents the principal is the business owner of, with the
// name people see (the latest version's display name).
func (s *Store) OwnedAgents(tenant, principalID string) ([]OwnedAgent, error) {
	if strings.TrimSpace(principalID) == "" {
		return nil, nil
	}
	var out []OwnedAgent
	err := s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT po.persona_id, COALESCE((SELECT pv.display_name FROM persona_versions pv WHERE pv.tenant_id=po.tenant_id AND pv.persona_id=po.persona_id ORDER BY pv.version DESC LIMIT 1), po.persona_id)
			FROM persona_owners po WHERE po.tenant_id=$1 AND po.owner_role='BUSINESS_OWNER' AND po.principal_id=$2 ORDER BY po.persona_id`, id, principalID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var agent OwnedAgent
			if err := rows.Scan(&agent.ID, &agent.Name); err != nil {
				return err
			}
			out = append(out, agent)
		}
		return rows.Err()
	})
	return out, err
}

// BusinessOwner returns the principal who owns the agent, or empty when it has
// no owner on record.
func (s *Store) BusinessOwner(tenant, agentID string) string {
	if strings.TrimSpace(agentID) == "" {
		return ""
	}
	owner := ""
	err := s.run(tenant, func(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
		return tx.QueryRow(ctx, `SELECT COALESCE((SELECT principal_id FROM persona_owners WHERE tenant_id=$1 AND persona_id=$2 AND owner_role='BUSINESS_OWNER'),'')`, id, agentID).Scan(&owner)
	})
	if err != nil {
		return ""
	}
	return owner
}
