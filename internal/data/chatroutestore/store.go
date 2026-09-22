// Package chatroutestore persists core-owned conversation placement metadata.
// It is intentionally separate from chatstore: message storage never queries
// the core route table through a data join.
package chatroutestore

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type Database interface {
	dbport.Conn
	dbport.Beginner
}
type Store struct{ db Database }

func New(db Database) (*Store, error) {
	if db == nil {
		return nil, chatrouting.ErrInvalid
	}
	return &Store{db: db}, nil
}

// Migrate is called by the core migration composition root; migration numbers
// remain owned by that root. All five statements run inside one transaction:
// previously they ran as five independent Execs, so a crash between the
// DROP POLICY and the following CREATE POLICY left the table FORCE-RLS with
// no policy at all (every row hidden, including from its own tenant).
//
// create_idempotency_key is scoped (host_tenant_id,create_idempotency_key)
// rather than globally UNIQUE: a global unique constraint meant tenant B
// reusing a key tenant A had already used hit a raw 23505 on an unrelated
// insert, leaking whether that key existed anywhere in the table.
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS chat_conversation_route (
conversation_id text PRIMARY KEY, host_tenant_id text NOT NULL, shard_id text NOT NULL,
target_shard_id text NOT NULL DEFAULT '', epoch bigint NOT NULL CHECK (epoch > 0),
state text NOT NULL CHECK (state IN ('PENDING','ACTIVE','MOVING')),
placement_policy text NOT NULL DEFAULT '', placement_policy_version bigint NOT NULL DEFAULT 0,
create_idempotency_key text NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
CHECK ((state = 'MOVING' AND target_shard_id <> '') OR (state <> 'MOVING' AND target_shard_id = '')),
UNIQUE (host_tenant_id, create_idempotency_key)
)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE chat_conversation_route ENABLE ROW LEVEL SECURITY`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `ALTER TABLE chat_conversation_route FORCE ROW LEVEL SECURITY`); err != nil {
		return err
	}
	// PostgreSQL has no CREATE POLICY IF NOT EXISTS; DROP+CREATE is the only
	// idempotent form, and running it inside this transaction is what
	// removes the crash window between the two statements.
	if _, err = tx.Exec(ctx, `DROP POLICY IF EXISTS chat_conversation_route_tenant_isolation ON chat_conversation_route`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE POLICY chat_conversation_route_tenant_isolation ON chat_conversation_route USING (host_tenant_id = current_setting('hcmnext.tenant_id', true)) WITH CHECK (host_tenant_id = current_setting('hcmnext.tenant_id', true))`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// isUniqueViolation reports whether err is a PostgreSQL unique_violation
// (23505), so callers can map it to a domain error instead of leaking the
// raw driver error.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func scan(row dbport.Row) (chatrouting.Route, error) {
	var r chatrouting.Route
	err := row.Scan(&r.ConversationID, &r.HostTenantID, &r.ShardID, &r.TargetShardID, &r.Epoch, &r.State, &r.PlacementPolicy, &r.PlacementPolicyVer, &r.CreateIdempotencyKey)
	if errors.Is(err, dbport.ErrNoRows) {
		return chatrouting.Route{}, chatrouting.ErrNotFound
	}
	if err != nil {
		return chatrouting.Route{}, err
	}
	return r, r.Validate()
}

func (s *Store) Reserve(ctx context.Context, q chatrouting.ReserveRequest) (chatrouting.Route, error) {
	if q.ConversationID == "" || q.HostTenantID == "" || q.ShardID == "" || q.IdempotencyKey == "" {
		return chatrouting.Route{}, chatrouting.ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return chatrouting.Route{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, q.HostTenantID); err != nil {
		return chatrouting.Route{}, err
	}
	r, err := scan(tx.QueryRow(ctx, `INSERT INTO chat_conversation_route (conversation_id,host_tenant_id,shard_id,epoch,state,placement_policy,placement_policy_version,create_idempotency_key) VALUES ($1,$2,$3,1,'PENDING',$4,$5,$6) ON CONFLICT (conversation_id) DO UPDATE SET conversation_id=EXCLUDED.conversation_id WHERE chat_conversation_route.host_tenant_id=EXCLUDED.host_tenant_id AND chat_conversation_route.create_idempotency_key=EXCLUDED.create_idempotency_key RETURNING conversation_id,host_tenant_id,shard_id,target_shard_id,epoch,state,placement_policy,placement_policy_version,create_idempotency_key`, q.ConversationID, q.HostTenantID, q.ShardID, q.PlacementPolicy, q.PlacementPolicyVersion, q.IdempotencyKey))
	if errors.Is(err, chatrouting.ErrNotFound) {
		return chatrouting.Route{}, chatrouting.ErrAlreadyExists
	}
	// A different conversation_id under the same tenant reusing an
	// already-used create_idempotency_key does not hit the ON CONFLICT
	// target above (that target is conversation_id) and instead hits the
	// (host_tenant_id,create_idempotency_key) unique constraint directly;
	// map that raw 23505 to the package's domain error instead of leaking
	// the driver error to callers.
	if isUniqueViolation(err) {
		return chatrouting.Route{}, chatrouting.ErrAlreadyExists
	}
	if err != nil {
		return chatrouting.Route{}, err
	}
	return r, tx.Commit(ctx)
}

func (s *Store) Lookup(ctx context.Context, id, tenant string) (chatrouting.Route, error) {
	if id == "" || tenant == "" {
		return chatrouting.Route{}, chatrouting.ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return chatrouting.Route{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant); err != nil {
		return chatrouting.Route{}, err
	}
	r, err := scan(tx.QueryRow(ctx, `SELECT conversation_id,host_tenant_id,shard_id,target_shard_id,epoch,state,placement_policy,placement_policy_version,create_idempotency_key FROM chat_conversation_route WHERE conversation_id=$1 AND host_tenant_id=$2`, id, tenant))
	if err != nil {
		return chatrouting.Route{}, err
	}
	return r, tx.Commit(ctx)
}

func (s *Store) transition(ctx context.Context, id, tenant string, epoch uint64, set, where string) (chatrouting.Route, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return chatrouting.Route{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant); err != nil {
		return chatrouting.Route{}, err
	}
	r, err := scan(tx.QueryRow(ctx, `UPDATE chat_conversation_route SET `+set+` WHERE conversation_id=$1 AND host_tenant_id=$3 AND epoch=$2 AND `+where+` RETURNING conversation_id,host_tenant_id,shard_id,target_shard_id,epoch,state,placement_policy,placement_policy_version,create_idempotency_key`, id, epoch, tenant))
	if err != nil {
		if errors.Is(err, chatrouting.ErrNotFound) {
			return chatrouting.Route{}, chatrouting.ErrStaleEpoch
		}
		return chatrouting.Route{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return chatrouting.Route{}, err
	}
	return r, nil
}

func (s *Store) Activate(ctx context.Context, id, tenant string, epoch uint64) (chatrouting.Route, error) {
	return s.transition(ctx, id, tenant, epoch, `state='ACTIVE',updated_at=now()`, `(state='PENDING' OR state='ACTIVE')`)
}
func (s *Store) Cutover(ctx context.Context, id, tenant string, epoch uint64) (chatrouting.Route, error) {
	return s.transition(ctx, id, tenant, epoch, `shard_id=target_shard_id,target_shard_id='',state='ACTIVE',epoch=epoch+1,updated_at=now()`, `state='MOVING'`)
}
func (s *Store) AbortMove(ctx context.Context, id, tenant string, epoch uint64) (chatrouting.Route, error) {
	return s.transition(ctx, id, tenant, epoch, `target_shard_id='',state='ACTIVE',epoch=epoch+1,updated_at=now()`, `state='MOVING'`)
}

func (s *Store) BeginMove(ctx context.Context, id, tenant string, epoch uint64, target string) (chatrouting.MovePlan, error) {
	if target == "" {
		return chatrouting.MovePlan{}, chatrouting.ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return chatrouting.MovePlan{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant); err != nil {
		return chatrouting.MovePlan{}, err
	}
	var from string
	var moveEpoch uint64
	err = tx.QueryRow(ctx, `UPDATE chat_conversation_route SET state='MOVING',target_shard_id=$4,epoch=epoch+1,updated_at=now() WHERE conversation_id=$1 AND epoch=$2 AND host_tenant_id=$3 AND state='ACTIVE' AND shard_id<>$4 RETURNING shard_id,epoch`, id, epoch, tenant, target).Scan(&from, &moveEpoch)
	if errors.Is(err, dbport.ErrNoRows) {
		return chatrouting.MovePlan{}, chatrouting.ErrMoveConflict
	}
	if err != nil {
		return chatrouting.MovePlan{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return chatrouting.MovePlan{}, err
	}
	return chatrouting.MovePlan{ConversationID: id, HostTenantID: tenant, FromShard: from, ToShard: target, SourceEpoch: epoch, MoveEpoch: moveEpoch}, nil
}

var _ chatrouting.Directory = (*Store)(nil)
