// Package agentmemorystore owns tenant-scoped durable agent copy inventories,
// tombstones, and explicit current policy decisions. Missing decisions deny.
package agentmemorystore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/memory"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type Store struct {
	db     dbport.Beginner
	mapper func(values.TenantId) uuid.UUID
	now    func() time.Time
}
type Copy struct {
	store *Store
	name  string
}

func New(db dbport.Beginner, mapper func(values.TenantId) uuid.UUID, now func() time.Time) (*Store, error) {
	if db == nil || mapper == nil || now == nil {
		return nil, memory.ErrInvalid
	}
	return &Store{db, mapper, now}, nil
}
func (s *Store) Copy(name string) (*Copy, error) {
	if s == nil || strings.TrimSpace(name) == "" {
		return nil, memory.ErrInvalid
	}
	return &Copy{s, name}, nil
}
func (c *Copy) Name() string { return c.name }
func (s *Store) begin(ctx context.Context, tenant string) (dbport.Tx, uuid.UUID, error) {
	if s == nil || ctx == nil || values.TenantId(tenant).Validate() != nil {
		return nil, uuid.Nil, memory.ErrInvalid
	}
	id := s.mapper(values.TenantId(tenant))
	if id == uuid.Nil {
		return nil, id, memory.ErrDenied
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, id, err
	}
	if err = tenancy.WithTenant(ctx, tx, id); err != nil {
		_ = tx.Rollback(ctx)
		return nil, id, err
	}
	return tx, id, nil
}

// Put serializes writers and tombstone creation for the tenant so revocation
// cannot race a new copy into existence after the invalidation commits.
func (c *Copy) Put(ctx context.Context, item memory.Item) error {
	tx, id, err := c.store.begin(ctx, item.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lock(ctx, tx, id); err != nil {
		return err
	}
	var blocked bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memory_invalidation WHERE tenant_id=$1 AND ((target_kind='ITEM' AND target_id=$2) OR (target_kind='SOURCE' AND target_id=$3)))`, id, item.ID, memory.SourceTarget(item.SourceOwner, item.SourceID)).Scan(&blocked)
	if err != nil {
		return err
	}
	if blocked {
		return memory.ErrStale
	}
	body, err := json.Marshal(item)
	if err != nil {
		return err
	}
	n, err := tx.Exec(ctx, `INSERT INTO agent_memory_copy(tenant_id,store_name,item_id,source_owner,source_id,item) VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT DO NOTHING`, id, c.name, item.ID, item.SourceOwner, item.SourceID, body)
	if err != nil {
		return err
	}
	if n == 0 {
		var prior []byte
		if err = tx.QueryRow(ctx, `SELECT item FROM agent_memory_copy WHERE tenant_id=$1 AND store_name=$2 AND item_id=$3`, id, c.name, item.ID).Scan(&prior); err != nil {
			return err
		}
		var previous memory.Item
		if json.Unmarshal(prior, &previous) != nil {
			return memory.ErrIncomplete
		}
		a, _ := json.Marshal(previous)
		if string(a) != string(body) {
			return memory.ErrStale
		}
	}
	return tx.Commit(ctx)
}
func lock(ctx context.Context, tx dbport.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-memory:"+id.String())
	return err
}
func (c *Copy) Get(ctx context.Context, tenant, itemID string) (memory.Item, error) {
	tx, id, err := c.store.begin(ctx, tenant)
	if err != nil {
		return memory.Item{}, err
	}
	defer tx.Rollback(ctx)
	var body []byte
	err = tx.QueryRow(ctx, `SELECT item FROM agent_memory_copy WHERE tenant_id=$1 AND store_name=$2 AND item_id=$3`, id, c.name, itemID).Scan(&body)
	if errors.Is(err, dbport.ErrNoRows) {
		return memory.Item{}, memory.ErrNotFound
	}
	if err != nil {
		return memory.Item{}, err
	}
	var item memory.Item
	err = json.Unmarshal(body, &item)
	return item, err
}
func (c *Copy) ListTenant(ctx context.Context, tenant string) (memory.Inventory, error) {
	return c.list(ctx, tenant, "", "", "")
}
func (c *Copy) ListSource(ctx context.Context, tenant, owner, source string) (memory.Inventory, error) {
	return c.list(ctx, tenant, owner, source, "")
}
func (c *Copy) ListItem(ctx context.Context, tenant, item string) (memory.Inventory, error) {
	return c.list(ctx, tenant, "", "", item)
}
func (c *Copy) list(ctx context.Context, tenant, owner, source, itemID string) (memory.Inventory, error) {
	tx, id, err := c.store.begin(ctx, tenant)
	if err != nil {
		return memory.Inventory{}, err
	}
	defer tx.Rollback(ctx)
	if err = lock(ctx, tx, id); err != nil {
		return memory.Inventory{}, err
	}
	rows, err := tx.Query(ctx, `SELECT item FROM agent_memory_copy WHERE tenant_id=$1 AND store_name=$2 AND ($3='' OR source_owner=$3) AND ($4='' OR source_id=$4) AND ($5='' OR item_id=$5) ORDER BY item_id`, id, c.name, owner, source, itemID)
	if err != nil {
		return memory.Inventory{}, err
	}
	defer rows.Close()
	inventory := memory.Inventory{Items: []memory.Item{}}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return memory.Inventory{}, err
		}
		var item memory.Item
		if err = json.Unmarshal(body, &item); err != nil {
			return memory.Inventory{}, err
		}
		inventory.Items = append(inventory.Items, item)
	}
	if err = rows.Err(); err != nil {
		return memory.Inventory{}, err
	}
	rows.Close()
	var watermark string
	err = tx.QueryRow(ctx, `SELECT txid_current_snapshot()::text`).Scan(&watermark)
	if err != nil {
		return memory.Inventory{}, err
	}
	inventory.Complete = true
	inventory.Watermark = watermark
	return inventory, nil
}
func (c *Copy) Delete(ctx context.Context, tenant, item string) error {
	tx, id, err := c.store.begin(ctx, tenant)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lock(ctx, tx, id); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM agent_memory_copy WHERE tenant_id=$1 AND store_name=$2 AND item_id=$3`, id, c.name, item)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (c *Copy) RecordInvalidation(ctx context.Context, in memory.Invalidation) error {
	tx, id, err := c.store.begin(ctx, in.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lock(ctx, tx, id); err != nil {
		return err
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_memory_invalidation(tenant_id,target_kind,target_id,receipt) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT DO NOTHING`, id, in.TargetKind, in.TargetID, body)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (c *Copy) Invalidated(ctx context.Context, tenant, kind, target string) (bool, error) {
	tx, id, err := c.store.begin(ctx, tenant)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var yes bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memory_invalidation WHERE tenant_id=$1 AND target_kind=$2 AND target_id=$3)`, id, kind, target).Scan(&yes)
	return yes, err
}

func (s *Store) ResolveMemoryPolicy(ctx context.Context, tenant, owner, purpose string) (memory.Policy, error) {
	tx, id, err := s.begin(ctx, tenant)
	if err != nil {
		return memory.Policy{}, err
	}
	defer tx.Rollback(ctx)
	var body []byte
	err = tx.QueryRow(ctx, `SELECT policy FROM agent_memory_policy WHERE tenant_id=$1 AND owner_id=$2 AND purpose=$3 AND expires_at>$4`, id, owner, purpose, s.now().UTC()).Scan(&body)
	if err != nil {
		return memory.Policy{}, errors.Join(memory.ErrDenied, err)
	}
	var result memory.Policy
	err = json.Unmarshal(body, &result)
	return result, err
}
func (s *Store) CheckMemorySource(ctx context.Context, pin memory.SourcePin, purpose string) (memory.SourceDecision, error) {
	tx, id, err := s.begin(ctx, pin.TenantID)
	if err != nil {
		return memory.SourceDecision{}, err
	}
	defer tx.Rollback(ctx)
	var body []byte
	err = tx.QueryRow(ctx, `SELECT decision FROM agent_memory_source WHERE tenant_id=$1 AND source_owner=$2 AND source_id=$3 AND purpose=$4 AND expires_at>$5`, id, pin.Owner, pin.ID, purpose, s.now().UTC()).Scan(&body)
	if err != nil {
		return memory.SourceDecision{}, errors.Join(memory.ErrDenied, err)
	}
	var result memory.SourceDecision
	err = json.Unmarshal(body, &result)
	return result, err
}
func (s *Store) ResolveMemoryDisposition(ctx context.Context, item memory.Item, now time.Time) (memory.DispositionDecision, error) {
	tx, id, err := s.begin(ctx, item.TenantID)
	if err != nil {
		return memory.DispositionDecision{}, err
	}
	defer tx.Rollback(ctx)
	var body []byte
	err = tx.QueryRow(ctx, `SELECT d.decision FROM agent_memory_disposition d JOIN agent_memory_policy p ON p.tenant_id=d.tenant_id WHERE d.tenant_id=$1 AND d.item_id=$2 AND d.expires_at>$3 AND p.owner_id=$4 AND p.purpose=$5 AND p.expires_at>$3 AND p.policy->>'RetentionPolicyID'=$6 AND p.policy->>'RetentionVersion'=$7`, id, item.ID, now.UTC(), item.OwnerID, item.Purpose, item.RetentionPolicyID, item.RetentionVersion).Scan(&body)
	if err != nil {
		return memory.DispositionDecision{}, errors.Join(memory.ErrDisposition, err)
	}
	var result memory.DispositionDecision
	err = json.Unmarshal(body, &result)
	return result, err
}
func (s *Store) AuthorizeMemory(ctx context.Context, actor memory.Actor, op memory.Operation, item memory.Item) error {
	if actor.PrincipalID == "" || actor.TenantID != item.TenantID {
		return memory.ErrDenied
	}
	tx, id, err := s.begin(ctx, actor.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var yes bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memory_grant WHERE tenant_id=$1 AND principal_id=$2 AND owner_id=$3 AND purpose=$4 AND $5=ANY(operations) AND not_before<=$6 AND expires_at>$6 AND revoked_at IS NULL)`, id, actor.PrincipalID, item.OwnerID, item.Purpose, string(op), s.now().UTC()).Scan(&yes)
	if err != nil {
		return err
	}
	if !yes {
		return memory.ErrDenied
	}
	return nil
}

var _ memory.CopyStore = (*Copy)(nil)
var _ memory.PolicyResolver = (*Store)(nil)
var _ memory.SourceResolver = (*Store)(nil)
var _ memory.DispositionResolver = (*Store)(nil)
var _ memory.Authorizer = (*Store)(nil)

// AuthorizeOperation denies even an empty inventory or export unless the
// principal has a current explicitly reviewed grant for that operation.
func (s *Store) AuthorizeOperation(ctx context.Context, actor memory.Actor, op memory.Operation) error {
	if actor.PrincipalID == "" {
		return memory.ErrDenied
	}
	tx, id, err := s.begin(ctx, actor.TenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var yes bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memory_grant WHERE tenant_id=$1 AND principal_id=$2 AND $3=ANY(operations) AND not_before<=$4 AND expires_at>$4 AND revoked_at IS NULL)`, id, actor.PrincipalID, string(op), s.now().UTC()).Scan(&yes)
	if err != nil {
		return err
	}
	if !yes {
		return memory.ErrDenied
	}
	return nil
}
