// Package chatappstore is the durable adapter for collaboration app state.
// It owns only chat-app tables and speaks the database-neutral dbport.
package chatappstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var ErrConflict = errors.New("chatappstore: revision conflict")

type Store struct{ DB dbport.Beginner }

func New(db dbport.Beginner) *Store           { return &Store{DB: db} }
func NewChatStore(db *chatstore.Store) *Store { return New(db) }

type tenantContextKey struct{}

func WithTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenant)
}
func tenantFrom(ctx context.Context) string { v, _ := ctx.Value(tenantContextKey{}).(string); return v }
func setTenant(ctx context.Context, tx dbport.Execer, tenant string) error {
	if tenant == "" {
		return errors.New("chatappstore: tenant required")
	}
	_, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant)
	return err
}

func (s *Store) Get(ctx context.Context, id string) (chatapps.Installation, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return chatapps.Installation{}, err
	}
	defer tx.Rollback(ctx)
	tenant := tenantFrom(ctx)
	if err := setTenant(ctx, tx, tenant); err != nil {
		return chatapps.Installation{}, err
	}
	var v chatapps.Installation
	var raw []byte
	var status string
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at FROM chat_app_installation WHERE id=$1 AND tenant_id=$2`, id, tenant).Scan(&v.ID, &v.Tenant, &v.Conversation, &v.AppID, &v.Version, &raw, &v.GrantedScopes, &status, &v.Approver, &v.Revision, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return chatapps.Installation{}, chatapps.ErrNotFound
	}
	if err != nil {
		return chatapps.Installation{}, err
	}
	if err = json.Unmarshal(raw, &v.Manifest); err != nil {
		return chatapps.Installation{}, err
	}
	v.Status = chatapps.Status(status)
	return v, nil
}

func (s *Store) Put(ctx context.Context, v chatapps.Installation) error {
	return s.put(ctx, v, nil)
}

// PutAudited commits the CAS mutation and immutable audit projection together.
func (s *Store) PutAudited(ctx context.Context, v chatapps.Installation, actor chatapps.Actor, home, action string, policyRevision uint64) error {
	if actor.Tenant != v.Tenant || actor.Conversation != v.Conversation || actor.Principal == "" || home == "" || policyRevision == 0 ||
		(action != "chat.app.install" && action != "chat.app.status") {
		return chatapps.ErrDenied
	}
	return s.put(ctx, v, func(tx dbport.Tx) error {
		return chatstore.RecordAppEvent(ctx, tx, v.Tenant, v.Conversation, v.ID, action, home, actor.Principal, v.Revision, int64(policyRevision))
	})
}

func (s *Store) put(ctx context.Context, v chatapps.Installation, audit func(dbport.Tx) error) error {
	raw, err := json.Marshal(v.Manifest)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := setTenant(ctx, tx, v.Tenant); err != nil {
		return err
	}
	if v.Revision <= 0 {
		return chatapps.ErrInvalid
	}
	n, err := tx.Exec(ctx, `INSERT INTO chat_app_installation(id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(id) DO UPDATE SET version=EXCLUDED.version,manifest=EXCLUDED.manifest,granted_scopes=EXCLUDED.granted_scopes,status=EXCLUDED.status,approver=EXCLUDED.approver,revision=EXCLUDED.revision,updated_at=EXCLUDED.updated_at WHERE chat_app_installation.revision=$13 AND chat_app_installation.tenant_id=EXCLUDED.tenant_id AND chat_app_installation.conversation_id=EXCLUDED.conversation_id AND chat_app_installation.app_id=EXCLUDED.app_id`, v.ID, v.Tenant, v.Conversation, v.AppID, v.Version, raw, v.GrantedScopes, string(v.Status), v.Approver, v.Revision, v.CreatedAt, v.UpdatedAt, v.Revision-1)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	if audit != nil {
		if err := audit(tx); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ByConversation(ctx context.Context, tenant, conversation string) ([]chatapps.Installation, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := setTenant(ctx, tx, tenant); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,tenant_id,conversation_id,app_id,version,manifest,granted_scopes,status,approver,revision,created_at,updated_at FROM chat_app_installation WHERE tenant_id=$1 AND conversation_id=$2 ORDER BY id`, tenant, conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chatapps.Installation{}
	for rows.Next() {
		var v chatapps.Installation
		var raw []byte
		var st string
		if err := rows.Scan(&v.ID, &v.Tenant, &v.Conversation, &v.AppID, &v.Version, &raw, &v.GrantedScopes, &st, &v.Approver, &v.Revision, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &v.Manifest); err != nil {
			return nil, err
		}
		v.Status = chatapps.Status(st)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) Seen(ctx context.Context, id string) (bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	tenant := tenantFrom(ctx)
	if err := setTenant(ctx, tx, tenant); err != nil {
		return false, err
	}
	var x string
	err = tx.QueryRow(ctx, `SELECT event_id FROM chat_app_event_seen WHERE tenant_id=$1 AND event_id=$2`, tenant, id).Scan(&x)
	if errors.Is(err, dbport.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
func (s *Store) MarkSeen(ctx context.Context, id string, at time.Time) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tenant := tenantFrom(ctx)
	if err := setTenant(ctx, tx, tenant); err != nil {
		return err
	}
	n, err := tx.Exec(ctx, `INSERT INTO chat_app_event_seen(tenant_id,event_id,seen_at) VALUES($1,$2,$3) ON CONFLICT(tenant_id,event_id) DO NOTHING`, tenant, id, at)
	if err != nil {
		return err
	}
	if n == 0 {
		return chatapps.ErrReplay
	}
	return tx.Commit(ctx)
}
func (s *Store) AppendEvent(ctx context.Context, e chatapps.Event) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := setTenant(ctx, tx, e.Tenant); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_app_event(id,tenant_id,conversation_id,installation_id,event_type,sequence,payload,occurred_at,expires_at,signature) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO NOTHING`, e.ID, e.Tenant, e.Conversation, e.InstallationID, e.Type, e.Sequence, []byte(e.Payload), e.At, e.ExpiresAt, e.Signature)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) AppendOnce(ctx context.Context, e chatapps.Event, seenAt time.Time) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := setTenant(ctx, tx, e.Tenant); err != nil {
		return err
	}
	n, err := tx.Exec(ctx, `INSERT INTO chat_app_event_seen(tenant_id,event_id,seen_at) VALUES($1,$2,$3) ON CONFLICT(tenant_id,event_id) DO NOTHING`, e.Tenant, e.ID, seenAt)
	if err != nil {
		return err
	}
	if n == 0 {
		return chatapps.ErrReplay
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_app_event(id,tenant_id,conversation_id,installation_id,event_type,sequence,payload,occurred_at,expires_at,signature) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, e.ID, e.Tenant, e.Conversation, e.InstallationID, e.Type, e.Sequence, []byte(e.Payload), e.At, e.ExpiresAt, e.Signature)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// defaultEventsLimit and maxEventsLimit clamp the caller-supplied page size:
// a non-positive limit falls back to the default and anything above the
// ceiling is truncated, so a negative value can no longer raise the
// effective LIMIT and a zero value can no longer starve the page.
const (
	defaultEventsLimit = 50
	maxEventsLimit     = 200
)

func (s *Store) Events(ctx context.Context, conversation string, after uint64, limit int) ([]chatapps.Event, error) {
	if limit <= 0 {
		limit = defaultEventsLimit
	} else if limit > maxEventsLimit {
		limit = maxEventsLimit
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	tenant := tenantFrom(ctx)
	if err := setTenant(ctx, tx, tenant); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,tenant_id,conversation_id,installation_id,event_type,sequence,payload,occurred_at,expires_at,signature FROM chat_app_event WHERE tenant_id=$1 AND conversation_id=$2 AND sequence>$3 ORDER BY sequence LIMIT $4`, tenant, conversation, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chatapps.Event{}
	for rows.Next() {
		var e chatapps.Event
		if err := rows.Scan(&e.ID, &e.Tenant, &e.Conversation, &e.InstallationID, &e.Type, &e.Sequence, &e.Payload, &e.At, &e.ExpiresAt, &e.Signature); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
