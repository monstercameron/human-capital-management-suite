// Package agentcontextstore retains the exact bounded source image accepted by
// a persona run. Retained reader labels are provenance, never authentication.
package agentcontextstore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var ErrInvalid = errors.New("agentcontextstore: invalid bounded source context")
var ErrConflict = errors.New("agentcontextstore: context conflict")
var ErrNotFound = errors.New("agentcontextstore: context not found")

type TenantTxRunner interface {
	RunTenantTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
}
type Store struct {
	db         TenantTxRunner
	tenantUUID func(string) uuid.UUID
}

func New(db TenantTxRunner) (*Store, error) {
	return NewWithTenantUUID(db, func(tenant string) uuid.UUID { id, _ := uuid.Parse(tenant); return id })
}

// NewWithTenantUUID retains logical tenant labels in the source image while
// binding every transaction to the canonical tenant UUID supplied by its owner.
func NewWithTenantUUID(db TenantTxRunner, tenantUUID func(string) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, ErrInvalid
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

// Put stores one immutable authenticated capture before its context is admitted.
// A repeat must contain the same complete image, including source attribution.
func (s *Store) Put(ctx context.Context, snapshot chat.ThreadSnapshot) error {
	if s == nil || s.db == nil || s.tenantUUID == nil || ctx == nil {
		return ErrInvalid
	}
	tenant, raw, err := encodeWithTenantUUID(snapshot, s.tenantUUID)
	if err != nil {
		return ErrInvalid
	}
	return s.db.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO persona_thread_context(tenant_id,snapshot_id,digest,context_payload) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, tenant, snapshot.SnapshotID, snapshot.Digest, raw)
		if err != nil {
			return err
		}
		current, err := s.read(ctx, tx, tenant, snapshot.TenantID, snapshot.SnapshotID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, snapshot) {
			return ErrConflict
		}
		return nil
	})
}

// Get reads and validates the original context under the requested tenant RLS.
func (s *Store) Get(ctx context.Context, tenant string, snapshotID string) (chat.ThreadSnapshot, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || ctx == nil || tenant == "" || strings.TrimSpace(tenant) != tenant || strings.TrimSpace(snapshotID) != snapshotID || snapshotID == "" {
		return chat.ThreadSnapshot{}, ErrInvalid
	}
	key := s.tenantUUID(tenant)
	if key == uuid.Nil {
		return chat.ThreadSnapshot{}, ErrInvalid
	}
	var snapshot chat.ThreadSnapshot
	err := s.db.RunTenantTx(ctx, key, func(tx dbport.Tx) error {
		var e error
		snapshot, e = s.read(ctx, tx, key, tenant, snapshotID)
		return e
	})
	return snapshot, err
}

func (s *Store) read(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, tenantRef, id string) (chat.ThreadSnapshot, error) {
	var raw []byte
	var digest string
	err := tx.QueryRow(ctx, `SELECT digest,context_payload FROM persona_thread_context WHERE tenant_id=$1 AND snapshot_id=$2`, tenant, id).Scan(&digest, &raw)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ThreadSnapshot{}, ErrNotFound
	}
	if err != nil {
		return chat.ThreadSnapshot{}, err
	}
	var snapshot chat.ThreadSnapshot
	if len(raw) > 1048576 || json.Unmarshal(raw, &snapshot) != nil {
		return snapshot, ErrInvalid
	}
	key, _, err := encodeWithTenantUUID(snapshot, s.tenantUUID)
	if err != nil || key != tenant || snapshot.TenantID != tenantRef || snapshot.SnapshotID != id || snapshot.Digest != digest {
		return chat.ThreadSnapshot{}, ErrInvalid
	}
	return snapshot, nil
}

func encode(snapshot chat.ThreadSnapshot) (uuid.UUID, []byte, error) {
	return encodeWithTenantUUID(snapshot, func(tenant string) uuid.UUID { id, _ := uuid.Parse(tenant); return id })
}

func encodeWithTenantUUID(snapshot chat.ThreadSnapshot, tenantUUID func(string) uuid.UUID) (uuid.UUID, []byte, error) {
	tenant := tenantUUID(snapshot.TenantID)
	if tenant == uuid.Nil || strings.TrimSpace(snapshot.TenantID) != snapshot.TenantID || snapshot.PrincipalTenantID != snapshot.TenantID || snapshot.PrincipalID == "" || snapshot.Revision == 0 || snapshot.AuthorityRevision == 0 || len(snapshot.Posts) == 0 || len(snapshot.Posts) > chat.MaxThreadSnapshotPosts {
		return uuid.Nil, nil, ErrInvalid
	}
	found := false
	seen := map[string]bool{}
	for _, p := range snapshot.Posts {
		if p.Deleted || seen[p.ID] || p.Sequence == 0 || (p.ID != snapshot.ThreadID && p.ParentID != snapshot.ThreadID) {
			return uuid.Nil, nil, ErrInvalid
		}
		seen[p.ID] = true
		if p.ID == snapshot.InvokingPostID && p.AuthorID == snapshot.PrincipalID && p.AuthorHomeTenantID == snapshot.TenantID {
			found = true
		}
	}
	digest, e := chat.ThreadSnapshotDigest(snapshot)
	if e != nil || !found || digest != snapshot.Digest || snapshot.SnapshotID != "chat-thread-"+digest {
		return uuid.Nil, nil, ErrInvalid
	}
	raw, e := json.Marshal(snapshot)
	if e != nil || len(raw) > 1048576 {
		return uuid.Nil, nil, ErrInvalid
	}
	return tenant, raw, nil
}
