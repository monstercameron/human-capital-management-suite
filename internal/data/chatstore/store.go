// Package chatstore owns employee conversation persistence. It has no import
// path into core/workflow storage and accepts a chat DSN at its composition root.
package chatstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

var (
	ErrCoreDatabase        = errors.New("chat database must be isolated from core database")
	ErrIdempotencyConflict = errors.New("chat idempotency key fingerprint conflict")
	ErrNotMember           = errors.New("chat member is not authorized")
	// ErrNoRouteLease is returned when a write targets a conversation the route
	// authority has placed on a shard and the caller carries no write lease.
	// Placement is read from the durable conversation row, so omitting the lease
	// cannot turn the fence off.
	ErrNoRouteLease = errors.New("chat write requires a route lease")
)

// routeStateActive is the only conversation route state that accepts writes.
const routeStateActive = "ACTIVE"

type Config struct {
	DSN, CoreDSN       string
	MaxConns, MinConns int32
}
type Store struct{ pool *pgxadapter.Pool }

// Begin exposes the chat-owned transaction port to adjacent chat adapters;
// callers still use dbport and cannot reach pgx types.
func (s *Store) Begin(ctx context.Context) (dbport.Tx, error) { return s.pool.Begin(ctx) }

// RunTx exposes the chat pool's narrow transaction port to other chat-owned
// repositories such as policy and grant authority. It never exposes the
// underlying core database pool.
func (s *Store) RunTx(ctx context.Context, fn func(dbport.Tx) error) error {
	if s == nil || s.pool == nil || fn == nil {
		return errors.New("chat store: transaction unavailable")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RunTenantTx applies the chat database RLS tenant setting before invoking a
// transaction callback.
func (s *Store) RunTenantTx(ctx context.Context, tenantID string, fn func(dbport.Tx) error) error {
	return s.RunTx(ctx, func(tx dbport.Tx) error {
		if err := tenant(ctx, tx, tenantID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// FenceWrites advances the local route epoch and marks the conversation as
// moving. SendPost locks this same row before checking the epoch, so an old
// lease cannot commit after the fence. The move coordinator supplies a tenant
// scope carried by the move plan. An unmatched fence is a stale move, never a
// successful no-op.
func (s *Store) FenceWrites(ctx context.Context, plan chatrouting.MovePlan) error {
	if plan.HostTenantID == "" || plan.ConversationID == "" || plan.SourceEpoch == 0 || plan.MoveEpoch != plan.SourceEpoch+1 {
		return chatrouting.ErrInvalid
	}
	return s.RunTenantTx(ctx, plan.HostTenantID, func(tx dbport.Tx) error {
		rows, err := tx.Exec(ctx, `UPDATE chat_conversation SET route_epoch=$1,route_state='MOVING' WHERE tenant_id=$2 AND id=$3 AND route_epoch=$4 AND route_state='ACTIVE'`, plan.MoveEpoch, plan.HostTenantID, plan.ConversationID, plan.SourceEpoch)
		if err != nil {
			return err
		}
		if rows != 1 {
			return chatrouting.ErrStaleEpoch
		}
		return nil
	})
}

// AbortWrites restores the source shard after a directory abort. The caller
// supplies the epoch returned by the successful directory CAS; this method
// cannot reopen a row that was cut over or advanced by another move.
func (s *Store) AbortWrites(ctx context.Context, plan chatrouting.MovePlan, abortedEpoch uint64) error {
	if plan.HostTenantID == "" || plan.ConversationID == "" || plan.SourceEpoch == 0 || plan.MoveEpoch != plan.SourceEpoch+1 || abortedEpoch != plan.MoveEpoch+1 {
		return chatrouting.ErrInvalid
	}
	return s.RunTenantTx(ctx, plan.HostTenantID, func(tx dbport.Tx) error {
		rows, err := tx.Exec(ctx, `UPDATE chat_conversation SET route_epoch=$1,route_state='ACTIVE' WHERE tenant_id=$2 AND id=$3 AND ((route_epoch=$4 AND route_state='MOVING') OR (route_epoch=$5 AND route_state='ACTIVE'))`, abortedEpoch, plan.HostTenantID, plan.ConversationID, plan.MoveEpoch, plan.SourceEpoch)
		if err != nil {
			return err
		}
		if rows != 1 {
			return chatrouting.ErrStaleEpoch
		}
		return nil
	})
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("chat DSN is required")
	}
	if cfg.CoreDSN != "" && sameDatabase(cfg.DSN, cfg.CoreDSN) {
		return nil, ErrCoreDatabase
	}
	// pgxadapter owns the driver boundary and supplies a bounded pool with
	// session hygiene. Its configured default is deliberately small for chat
	// handlers; callers can enforce a tighter admission budget above it.
	// MaxConns/MinConns are applied through the DSN because pgxpool resolves
	// pool sizing from the connection string, which is the only sizing seam
	// pgxadapter.NewPool exposes.
	dsn, err := withPoolSize(cfg.DSN, cfg.MaxConns, cfg.MinConns)
	if err != nil {
		return nil, err
	}
	p, err := pgxadapter.NewPool(ctx, dsn, nil)
	if err != nil {
		return nil, err
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{pool: p}, nil
}
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
func (s *Store) PoolStats() pgxadapter.Saturation { return s.pool.Stats() }

// withPoolSize applies the configured chat pool bounds to the chat DSN. pgxpool
// resolves pool sizing from the connection string, in both the URL and the
// libpq keyword spelling, so this is where Config.MaxConns/MinConns take
// effect. A bound already spelled in the DSN wins, because that spelling is the
// operator's own and this function must not silently contradict it.
func withPoolSize(dsn string, maxConns, minConns int32) (string, error) {
	if maxConns <= 0 && minConns <= 0 {
		return dsn, nil
	}
	if maxConns > 0 && minConns > maxConns {
		return "", errors.New("chat pool MinConns must not exceed MaxConns")
	}
	out := strings.TrimSpace(dsn)
	url := strings.HasPrefix(out, "postgres://") || strings.HasPrefix(out, "postgresql://")
	add := func(key string, value int32) {
		if value <= 0 || strings.Contains(out, key+"=") {
			return
		}
		if !url {
			out += " " + key + "=" + strconv.FormatInt(int64(value), 10)
			return
		}
		separator := "?"
		if strings.Contains(out, "?") {
			separator = "&"
		}
		out += separator + key + "=" + strconv.FormatInt(int64(value), 10)
	}
	add("pool_max_conns", maxConns)
	add("pool_min_conns", minConns)
	return out, nil
}

// sameDatabase compares logical database identity across both DSN spellings.
// pgconn.ParseConfig understands the URL form and the libpq keyword form, so a
// composition that hands chat a keyword DSN and core a URL for the same database
// is still rejected. Credentials are expected to differ as an additional
// defense; a shared database is forbidden even when callers supplied separate
// users or reordered query parameters.
func sameDatabase(a, b string) bool {
	ca, ea := pgconn.ParseConfig(a)
	cb, eb := pgconn.ParseConfig(b)
	if ea != nil || eb != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return ca.Port == cb.Port && strings.EqualFold(ca.Host, cb.Host) && ca.Database == cb.Database
}
func tenant(ctx context.Context, tx dbport.Tx, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("tenant is required")
	}
	_, err := tx.Exec(ctx, "SELECT set_config('hcmnext.tenant_id',$1,true)", id)
	return err
}

type Conversation struct {
	ID, TenantID, Kind, Name, Description, OwnerID, Lifecycle string
	// RouteShard is the placement the route authority recorded for this
	// conversation. A non-empty shard makes the route fence mandatory for every
	// later write; see routeFence.
	RouteShard       string
	SettingsRevision int64
	CreatedAt        time.Time
}
type Membership struct{ ConversationID, MemberID, HomeTenantID, Role, State string }
type Post struct {
	ID, TenantID, ConversationID, AuthorID, Body string
	Sequence, Revision                           int64
	Tombstoned                                   bool
	CreatedAt, UpdatedAt                         time.Time
	AuthorHomeTenantID                           string
	ParentID                                     string
	References, SourceAttribution                []byte
}
type SendRequest struct {
	TenantID, ConversationID, AuthorID, ClientKey, Fingerprint, Body string
	RouteEpoch                                                       uint64
	ShardID, HomeTenantID, ParentID                                  string
	References, SourceAttribution                                    []byte
	// CreatedAt overrides the row's own clock. It exists for one caller: the
	// demo seeder, which has to lay down two weeks of history in one run and
	// cannot do that if every post is stamped now(). A live send leaves it zero
	// and the column default stands, so no request path can choose its own
	// timestamp by accident.
	CreatedAt time.Time
}
type OutboxEvent struct {
	ID                               int64
	TenantID, AggregateID, EventType string
	Payload                          []byte
	CreatedAt                        time.Time
}

type Revision struct {
	PostID     string
	Revision   int64
	AuthorID   string
	Body       string
	Tombstoned bool
	CreatedAt  time.Time
}

// RevisePostCAS applies an immutable revision only when the caller still has
// the expected revision and the post belongs to the requested conversation.
// The post row lock makes concurrent edits deterministic.
func (s *Store) RevisePostCAS(ctx context.Context, tenantID, conversationID, postID, authorID, body string, expected int64, tombstone bool) (Revision, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Revision{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, tenantID); err != nil {
		return Revision{}, err
	}
	var p Revision
	if err = tx.QueryRow(ctx, `SELECT id,revision+1 FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 AND revision=$4 FOR UPDATE`, tenantID, conversationID, postID, expected).Scan(&p.PostID, &p.Revision); err != nil {
		return Revision{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body,tombstoned) VALUES($1,$2,$3,$4,$5,$6)`, tenantID, postID, p.Revision, authorID, body, tombstone); err != nil {
		return Revision{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE chat_post SET body=$1,revision=$2,tombstoned=$3,updated_at=now() WHERE tenant_id=$4 AND conversation_id=$5 AND id=$6 AND revision=$7`, body, p.Revision, tombstone, tenantID, conversationID, postID, expected); err != nil {
		return Revision{}, err
	}
	p.AuthorID, p.Body, p.Tombstoned, p.CreatedAt = authorID, body, tombstone, time.Now().UTC()
	return p, tx.Commit(ctx)
}

func (s *Store) SetPreference(ctx context.Context, tenantID, memberID, conversationID, marker string, value []byte) error {
	return s.execTenant(ctx, tenantID, `INSERT INTO chat_preference(tenant_id,member_id,conversation_id,marker,value,revision) VALUES($1,$2,$3,$4,$5,1) ON CONFLICT (tenant_id,member_id,conversation_id,marker) DO UPDATE SET value=EXCLUDED.value,revision=chat_preference.revision+1,updated_at=now()`, tenantID, memberID, conversationID, marker, value)
}
func (s *Store) SetReaction(ctx context.Context, tenantID, postID, memberID, emoji string, present bool) error {
	if present {
		return s.execTenant(ctx, tenantID, `INSERT INTO chat_reaction(tenant_id,post_id,member_id,emoji) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, tenantID, postID, memberID, emoji)
	}
	return s.execTenant(ctx, tenantID, `DELETE FROM chat_reaction WHERE tenant_id=$1 AND post_id=$2 AND member_id=$3 AND emoji=$4`, tenantID, postID, memberID, emoji)
}
func (s *Store) SetPin(ctx context.Context, tenantID, conversationID, postID, memberID string, pinned bool) error {
	if pinned {
		return s.execTenant(ctx, tenantID, `INSERT INTO chat_pin(tenant_id,conversation_id,post_id,member_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, tenantID, conversationID, postID, memberID)
	}
	return s.execTenant(ctx, tenantID, `DELETE FROM chat_pin WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 AND member_id=$4`, tenantID, conversationID, postID, memberID)
}
func (s *Store) SetCursor(ctx context.Context, tenantID, memberID, conversationID string, sequence int64) error {
	return s.execTenant(ctx, tenantID, `INSERT INTO chat_cursor(tenant_id,member_id,conversation_id,last_sequence) VALUES($1,$2,$3,$4) ON CONFLICT (tenant_id,member_id,conversation_id) DO UPDATE SET last_sequence=GREATEST(chat_cursor.last_sequence,EXCLUDED.last_sequence),updated_at=now()`, tenantID, memberID, conversationID, sequence)
}

func (s *Store) AddMember(ctx context.Context, tenantID string, m Membership) error {
	return s.execTenant(ctx, tenantID, `INSERT INTO chat_membership(tenant_id,conversation_id,member_id,role,state) VALUES($1,$2,$3,$4,$5) ON CONFLICT (tenant_id,conversation_id,member_id) DO UPDATE SET role=EXCLUDED.role,state=EXCLUDED.state`, tenantID, m.ConversationID, m.MemberID, m.Role, m.State)
}
func (s *Store) sendPostRaw(ctx context.Context, r SendRequest) (Post, error) {
	var out Post
	if len(r.References) == 0 {
		r.References = []byte("[]")
	}
	if r.Fingerprint == "" {
		r.Fingerprint = fingerprintCanonical(r.AuthorID, r.Body, r.ParentID, r.References, r.SourceAttribution)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, r.TenantID); err != nil {
		return out, err
	}
	var member string
	if r.HomeTenantID == "" {
		r.HomeTenantID = r.TenantID
	}
	if err = tx.QueryRow(ctx, `SELECT member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' FOR UPDATE`, r.TenantID, r.ConversationID, r.HomeTenantID, r.AuthorID).Scan(&member); err != nil {
		return out, ErrNotMember
	}
	// The fence locks only this conversation row, which also gives each
	// conversation a stable sequence under concurrent writers without a
	// process-wide mutex.
	if err = routeFence(ctx, tx, r.TenantID, r.ConversationID, r.RouteEpoch, r.ShardID); err != nil {
		return out, err
	}
	if r.ClientKey != "" {
		var fp, postID string
		var seq int64
		e := tx.QueryRow(ctx, `SELECT fingerprint,post_id,sequence FROM chat_idempotency WHERE tenant_id=$1 AND conversation_id=$2 AND client_key=$3`, r.TenantID, r.ConversationID, r.ClientKey).Scan(&fp, &postID, &seq)
		if e == nil {
			if fp != r.Fingerprint {
				return out, ErrIdempotencyConflict
			}
			return s.loadPost(ctx, tx, r.TenantID, postID)
		}
		if !errors.Is(e, dbport.ErrNoRows) {
			return out, e
		}
	}
	// The per-conversation post counter lives on the row this transaction holds.
	// Deriving it from max(sequence) over surviving posts reused numbers after a
	// retention purge; the column only ever moves forward.
	var eventSequence, policyRevision, postSequence int64
	if err = tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1,post_sequence=post_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING event_sequence,settings_revision,post_sequence`, r.TenantID, r.ConversationID).Scan(&eventSequence, &policyRevision, &postSequence); err != nil {
		return out, err
	}
	r.ClientKey = strings.TrimSpace(r.ClientKey)
	// One statement, two clocks: COALESCE keeps the column default for every
	// live send and honours an explicit historical stamp for the seeder.
	err = tx.QueryRow(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,parent_id,references_json,source_attribution,client_key,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$11,$6,$7,$8,$9,$10,COALESCE($12,now()),COALESCE($12,now())) RETURNING id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,revision,tombstoned,created_at,updated_at,parent_id,references_json,source_attribution`, uuid.NewString(), r.TenantID, r.ConversationID, r.AuthorID, r.HomeTenantID, r.Body, r.ParentID, r.References, r.SourceAttribution, r.ClientKey, postSequence, createdAtArg(r.CreatedAt)).Scan(&out.ID, &out.TenantID, &out.ConversationID, &out.AuthorID, &out.AuthorHomeTenantID, &out.Sequence, &out.Body, &out.Revision, &out.Tombstoned, &out.CreatedAt, &out.UpdatedAt, &out.ParentID, &out.References, &out.SourceAttribution)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body) VALUES($1,$2,$3,$4,$5)`, r.TenantID, out.ID, out.Revision, r.AuthorID, r.Body)
	if err != nil {
		return out, err
	}
	value, err := chatPost(out)
	if err != nil {
		return out, err
	}
	kind := "POST"
	if len(r.SourceAttribution) > 0 && string(r.SourceAttribution) != "null" {
		kind = "DERIVED_COPY"
	}
	if err = writeOutbox(ctx, tx, outboxWrite{
		TenantID: r.TenantID, ConversationID: r.ConversationID, AggregateID: out.ID, EventType: "post.created",
		ActorHomeTenantID: r.HomeTenantID, ActorID: r.AuthorID, TargetID: out.ID,
		RecordID: "post:" + out.ID, RecordKind: kind, SourceID: out.ID,
		Revision: uint64(out.Revision), PolicyRevision: policyRevision, EventSequence: eventSequence,
		CorrelationID: r.ClientKey, Value: value,
	}); err != nil {
		return out, err
	}
	if r.ClientKey != "" {
		_, err = tx.Exec(ctx, `INSERT INTO chat_idempotency(tenant_id,conversation_id,client_key,fingerprint,post_id,sequence) VALUES($1,$2,$3,$4,$5,$6)`, r.TenantID, r.ConversationID, r.ClientKey, r.Fingerprint, out.ID, out.Sequence)
		if err != nil {
			return out, err
		}
	}
	return out, tx.Commit(ctx)
}

// createdAtArg renders an optional historical timestamp as a nullable
// parameter, so the column default decides whenever the caller did not.
func createdAtArg(at time.Time) any {
	if at.IsZero() {
		return nil
	}
	return at.UTC()
}

func (s *Store) loadPost(ctx context.Context, tx dbport.Tx, tenantID, id string) (Post, error) {
	var p Post
	err := tx.QueryRow(ctx, `SELECT id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,revision,tombstoned,created_at,updated_at,parent_id,references_json,source_attribution FROM chat_post WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&p.ID, &p.TenantID, &p.ConversationID, &p.AuthorID, &p.AuthorHomeTenantID, &p.Sequence, &p.Body, &p.Revision, &p.Tombstoned, &p.CreatedAt, &p.UpdatedAt, &p.ParentID, &p.References, &p.SourceAttribution)
	return p, err
}
func fingerprint(body string) string { return fingerprintCanonical("", body, "", nil, nil) }
func fingerprintCanonical(author, body, parent string, refs, source []byte) string {
	h := sha256.New()
	h.Write([]byte(author))
	h.Write([]byte{0})
	h.Write([]byte(body))
	h.Write([]byte{0})
	h.Write([]byte(parent))
	h.Write([]byte{0})
	h.Write(refs)
	h.Write([]byte{0})
	h.Write(source)
	return hex.EncodeToString(h.Sum(nil))
}

// PendingOutbox returns unpublished events from the beginning of the tenant's
// outbox. It is the compatibility entry point for consumers that keep no cursor;
// a consumer that can resume should call PendingOutboxAfter with its own cursor
// so catch-up does not rescan the drained prefix on every poll.
func (s *Store) PendingOutbox(ctx context.Context, tenantID string, limit int32) ([]OutboxEvent, error) {
	return s.PendingOutboxAfter(ctx, tenantID, 0, limit)
}

// PendingOutboxAfter returns unpublished events with an id greater than afterID.
func (s *Store) PendingOutboxAfter(ctx context.Context, tenantID string, afterID int64, limit int32) ([]OutboxEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT o.id,o.tenant_id,o.aggregate_id,o.event_type,o.payload,o.created_at FROM chat_outbox o LEFT JOIN chat_outbox_receipt r ON r.tenant_id=o.tenant_id AND r.outbox_id=o.id WHERE o.tenant_id=$1 AND o.id>$3 AND r.outbox_id IS NULL ORDER BY o.id LIMIT $2`, tenantID, limit, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxEvent
	for rows.Next() {
		var e OutboxEvent
		if err := rows.Scan(&e.ID, &e.TenantID, &e.AggregateID, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
func (s *Store) MarkOutboxPublished(ctx context.Context, tenantID string, id int64) error {
	return s.execTenant(ctx, tenantID, `INSERT INTO chat_outbox_receipt(tenant_id,outbox_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, tenantID, id)
}

// OutboxCursor reports how far a named consumer has drained the tenant outbox.
// An unregistered consumer starts at 0.
func (s *Store) OutboxCursor(ctx context.Context, tenantID, consumer string) (int64, error) {
	if strings.TrimSpace(consumer) == "" {
		return 0, errors.New("chat outbox cursor requires a consumer name")
	}
	var id int64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		e := tx.QueryRow(ctx, `SELECT last_outbox_id FROM chat_outbox_cursor WHERE tenant_id=$1 AND consumer=$2`, tenantID, consumer).Scan(&id)
		if errors.Is(e, dbport.ErrNoRows) {
			return nil
		}
		return e
	})
	return id, err
}

// AdvanceOutboxCursor records a consumer's drain position. It never moves
// backwards, so a replaying consumer cannot make retention forget that later
// events are still owed to a slower one.
func (s *Store) AdvanceOutboxCursor(ctx context.Context, tenantID, consumer string, id int64) error {
	if strings.TrimSpace(consumer) == "" {
		return errors.New("chat outbox cursor requires a consumer name")
	}
	return s.execTenant(ctx, tenantID, `INSERT INTO chat_outbox_cursor(tenant_id,consumer,last_outbox_id) VALUES($1,$2,$3) ON CONFLICT (tenant_id,consumer) DO UPDATE SET last_outbox_id=GREATEST(chat_outbox_cursor.last_outbox_id,EXCLUDED.last_outbox_id),updated_at=now()`, tenantID, consumer, id)
}

// PruneOutbox deletes at most limit published outbox events older than before.
// A row is eligible only when it carries a publish receipt and sits at or below
// the slowest registered consumer cursor, so retention can never drop an event
// a consumer has not seen. Migration 00008 narrowed the append-only trigger to
// BEFORE UPDATE for exactly this job; rows stay immutable while they exist.
func (s *Store) PruneOutbox(ctx context.Context, tenantID string, before time.Time, limit int32) (int64, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	var deleted int64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		n, e := tx.Exec(ctx, `DELETE FROM chat_outbox o WHERE o.tenant_id=$1 AND o.id IN (
            SELECT x.id FROM chat_outbox x JOIN chat_outbox_receipt r ON r.tenant_id=x.tenant_id AND r.outbox_id=x.id
            WHERE x.tenant_id=$1 AND x.created_at<$2
              AND x.id<=COALESCE((SELECT min(c.last_outbox_id) FROM chat_outbox_cursor c WHERE c.tenant_id=$1),x.id)
            ORDER BY x.id LIMIT $3)`, tenantID, before, limit)
		if e != nil {
			return e
		}
		deleted = n
		return nil
	})
	return deleted, err
}
func (s *Store) execTenant(ctx context.Context, tid, sql string, args ...any) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = tenant(ctx, tx, tid); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, sql, args...); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// Outbox payload keys.
//
// Every event chatstore writes to chat_outbox uses this one envelope and this
// one key spelling. The package previously wrote snake_case keys from the post
// path and PascalCase keys from the adapter path, which meant the watch reader
// could only filter one of the two. Adjacent chat-owned repositories
// (chatauthority, chatrecordstore, chatappstore) should adopt these constants
// when they emit chat_outbox rows so a single consumer contract covers the
// whole chat database.
//
// Value carries the event-specific body: a chat.Post for post.created,
// post.edited and post.deleted, a chat.Conversation, chat.Membership,
// chat.Reaction or chat.Pin for the corresponding events, and null for
// pin.removed.
const (
	OutboxKeyConversationID    = "ConversationID"
	OutboxKeyActorID           = "ActorID"
	OutboxKeyActorHomeTenantID = "ActorHomeTenantID"
	OutboxKeyTargetID          = "TargetID"
	OutboxKeyRevision          = "Revision"
	OutboxKeyPolicyRevision    = "PolicyRevision"
	OutboxKeyEventSequence     = "EventSequence"
	OutboxKeySchemaVersion     = "SchemaVersion"
	OutboxKeyCorrelationID     = "CorrelationID"
	OutboxKeyValue             = "Value"
)

// OutboxSchemaVersion is the version stamped into OutboxKeySchemaVersion.
const OutboxSchemaVersion = 1

// OutboxEnvelope is the decoded form of a chat_outbox payload written by this
// package. Its field names are the wire names; see the OutboxKey constants.
type OutboxEnvelope struct {
	ConversationID    string          `json:"ConversationID"`
	ActorID           string          `json:"ActorID"`
	ActorHomeTenantID string          `json:"ActorHomeTenantID"`
	TargetID          string          `json:"TargetID"`
	Revision          uint64          `json:"Revision"`
	PolicyRevision    int64           `json:"PolicyRevision"`
	EventSequence     int64           `json:"EventSequence"`
	SchemaVersion     int             `json:"SchemaVersion"`
	CorrelationID     string          `json:"CorrelationID"`
	Value             json.RawMessage `json:"Value"`
}

// outboxWrite is the input to the single outbox producer.
type outboxWrite struct {
	TenantID, ConversationID, AggregateID, EventType string
	ActorHomeTenantID, ActorID, TargetID             string
	RecordID, RecordKind, SourceID                   string
	Revision, AuditRevision                          uint64
	PolicyRevision, EventSequence                    int64
	CorrelationID                                    string
	Value                                            any
}

// writeOutbox is the only place in this package that builds an outbox payload.
// Producers hand it typed values; it owns the envelope, the key spelling and the
// governance projection so the two can never drift apart.
func writeOutbox(ctx context.Context, tx dbport.Tx, w outboxWrite) error {
	value, err := json.Marshal(w.Value)
	if err != nil {
		return err
	}
	if w.CorrelationID == "" {
		w.CorrelationID = uuid.NewString()
	}
	payload, err := json.Marshal(OutboxEnvelope{
		ConversationID: w.ConversationID, ActorID: w.ActorID, ActorHomeTenantID: w.ActorHomeTenantID,
		TargetID: w.TargetID, Revision: w.Revision, PolicyRevision: w.PolicyRevision,
		EventSequence: w.EventSequence, SchemaVersion: OutboxSchemaVersion,
		CorrelationID: w.CorrelationID, Value: value,
	})
	if err != nil {
		return err
	}
	auditRevision := w.AuditRevision
	if auditRevision == 0 {
		auditRevision = w.Revision
	}
	aggregate := w.AggregateID
	if aggregate == "" {
		aggregate = w.ConversationID
	}
	return recordChatEvent(ctx, tx, w.TenantID, w.ConversationID, aggregate, w.EventType, w.ActorHomeTenantID, w.ActorID, w.TargetID, w.RecordID, w.RecordKind, w.SourceID, auditRevision, w.PolicyRevision, payload)
}

// leaseFence reads the route lease a routed caller carries. A caller with no
// lease yields a zero epoch and empty shard, which routeFence rejects for any
// conversation the route authority has placed.
func leaseFence(ctx context.Context) (uint64, string) {
	if lease, ok := chatrouting.WriteLeaseFromContext(ctx); ok {
		return lease.Route.Epoch, lease.Route.ShardID
	}
	return 0, ""
}

// routeFence locks the conversation row and enforces the route fence. Every
// write path in this package calls it before touching conversation state.
//
// The checks are not opt-in. The route state assertion is unconditional, so a
// conversation the move coordinator has fenced (route_state='MOVING') rejects
// every write, leased or not. Placement is read from the durable row rather than
// from the request, so a caller cannot disable the shard and epoch comparison by
// omitting its lease: once the route authority has recorded a shard for the
// conversation, a write with no lease fails with ErrNoRouteLease and a write
// with a stale lease fails with chatrouting.ErrStaleEpoch.
//
// A conversation created without a lease carries no shard and has no entry in
// the core route directory, so it is unreachable through the routed service; it
// is fenced on route state only. Requiring a lease on every write of every kind
// additionally needs internal/collaboration/chatroutingadapter to resolve a
// route for EditPost, DeletePost, PutReaction, RemoveReaction, PutPin,
// RemovePin, PutMembership, RemoveMembership and UpdateConversation, which today
// it does only for CreateConversation, SendPost, SendPostWithReferences and
// ForwardPost.
func routeFence(ctx context.Context, tx dbport.Tx, tenantID, conversationID string, epoch uint64, shard string) error {
	var routeEpoch int64
	var routeState, routeShard string
	if err := tx.QueryRow(ctx, `SELECT route_epoch,route_state,route_shard FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&routeEpoch, &routeState, &routeShard); err != nil {
		return err
	}
	if routeState != routeStateActive {
		return chatrouting.ErrStaleEpoch
	}
	if epoch == 0 && shard == "" {
		if routeShard != "" {
			return ErrNoRouteLease
		}
		return nil
	}
	if shard == "" || routeShard == "" || routeShard != shard || uint64(routeEpoch) != epoch {
		return chatrouting.ErrStaleEpoch
	}
	return nil
}

// fenceContextWrite applies routeFence using the lease on the context. It is the
// entry point for the adapter write paths that take their fence from context
// rather than from a SendRequest.
func fenceContextWrite(ctx context.Context, tx dbport.Tx, tenantID, conversationID string) error {
	epoch, shard := leaseFence(ctx)
	return routeFence(ctx, tx, tenantID, conversationID, epoch, shard)
}
