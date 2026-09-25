package workorderstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
)

var (
	ErrNotFound            = errors.New("work order not found")
	ErrRevisionConflict    = errors.New("work order revision conflict")
	ErrIdempotencyConflict = errors.New("work order idempotency key conflict")
	ErrInvalid             = errors.New("invalid work order record")
	ErrCoreCredential      = errors.New("work order database must use a distinct core credential")
)

type Config struct {
	DSN, CoreDSN, Schema string
	MaxConns, MinConns   int32
}
type Store struct{ pool *pgxadapter.Pool }

const SchemaName = "hcmnext_workorder"

func New(ctx context.Context, cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, errors.New("work order DSN is required")
	}
	if !validSchema(cfg.Schema) {
		return nil, errors.New("work order schema must be a PostgreSQL identifier")
	}
	if cfg.CoreDSN != "" && sameDatabase(cfg.DSN, cfg.CoreDSN) && sameCredential(cfg.DSN, cfg.CoreDSN) {
		return nil, ErrCoreCredential
	}
	dsn, err := withPoolSize(cfg.DSN, cfg.MaxConns, cfg.MinConns)
	if err != nil {
		return nil, err
	}
	dsn = strings.TrimSpace(dsn)
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("search_path", cfg.Schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		if _, err := pgconn.ParseConfig(dsn); err != nil {
			return nil, err
		}
		dsn += " search_path=" + cfg.Schema
	}
	p, err := pgxadapter.NewPool(ctx, dsn, nil)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{pool: p}, nil
}
func validSchema(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	for i, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func withPoolSize(dsn string, maxConns, minConns int32) (string, error) {
	if maxConns <= 0 && minConns <= 0 {
		return dsn, nil
	}
	if maxConns > 0 && minConns > maxConns {
		return "", errors.New("work order pool MinConns must not exceed MaxConns")
	}
	out := strings.TrimSpace(dsn)
	isURL := strings.HasPrefix(out, "postgres://") || strings.HasPrefix(out, "postgresql://")
	add := func(key string, value int32) {
		if value <= 0 || strings.Contains(out, key+"=") {
			return
		}
		v := strconv.FormatInt(int64(value), 10)
		if !isURL {
			out += " " + key + "=" + v
			return
		}
		sep := "?"
		if strings.Contains(out, "?") {
			sep = "&"
		}
		out += sep + key + "=" + v
	}
	add("pool_max_conns", maxConns)
	add("pool_min_conns", minConns)
	return out, nil
}

func sameDatabase(a, b string) bool {
	ca, ea := pgconn.ParseConfig(a)
	cb, eb := pgconn.ParseConfig(b)
	if ea != nil || eb != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return ca.Port == cb.Port && strings.EqualFold(ca.Host, cb.Host) && ca.Database == cb.Database
}
func sameCredential(a, b string) bool {
	ca, ea := pgconn.ParseConfig(a)
	cb, eb := pgconn.ParseConfig(b)
	if ea != nil || eb != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return ca.User == cb.User && ca.Password == cb.Password
}
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) RunTenantTx(ctx context.Context, tenant string, fn func(dbport.Tx) error) error {
	if s == nil || s.pool == nil || fn == nil || strings.TrimSpace(tenant) == "" {
		return ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT set_config('hcmnext.tenant_id',$1,true)", tenant); err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type Record struct {
	ID, TenantID, WorkOrderID, Kind, ActorID, IdempotencyKey string
	Sequence, Revision                                       int64
	Payload                                                  json.RawMessage
	CreatedAt                                                time.Time
}

func (s *Store) History(ctx context.Context, tenant, id string, after int64, limit int) ([]Record, error) {
	if tenant == "" || id == "" || after < 0 || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	if _, err := s.Get(ctx, tenant, id); err != nil {
		return nil, err
	}
	out := make([]Record, 0)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,work_order_id,sequence,revision,kind,actor_id,idempotency_key,payload,created_at FROM work_order_record WHERE tenant_id=$1 AND work_order_id=$2 AND sequence>$3 ORDER BY sequence LIMIT $4`, tenant, id, after, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r Record
			var b []byte
			if err := rows.Scan(&r.ID, &r.TenantID, &r.WorkOrderID, &r.Sequence, &r.Revision, &r.Kind, &r.ActorID, &r.IdempotencyKey, &b, &r.CreatedAt); err != nil {
				return err
			}
			r.Payload = append(json.RawMessage(nil), b...)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// Create stores the initial domain snapshot verbatim, including its creation journal entry.
func (s *Store) Create(ctx context.Context, tenant string, snapshot workorder.Snapshot, actor, key string) error {
	if tenant == "" || snapshot.TenantID != tenant || actor == "" || key == "" || snapshot.Revision != 1 || len(snapshot.Journal) == 0 {
		return ErrInvalid
	}
	if _, err := workorder.Restore(snapshot); err != nil {
		return err
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if snapshot.Journal[0].Revision != 1 || snapshot.Journal[0].IdempotencyKey != key || snapshot.Journal[0].ActorID != actor || snapshot.Journal[0].Type == "" || snapshot.Journal[0].CommandDigest == "" {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		// Serialize first-write attempts for the same aggregate/key before looking
		// for the idempotency receipt, including when no aggregate row exists yet.
		lockKey, err := json.Marshal([]string{tenant, snapshot.ID, actor, key})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(lockKey)); err != nil {
			return err
		}
		var priorDigest string
		var priorResult []byte
		err = tx.QueryRow(ctx, `SELECT command_digest,result_snapshot FROM work_order_idempotency WHERE tenant_id=$1 AND work_order_id=$2 AND actor_id=$3 AND client_key=$4`, tenant, snapshot.ID, actor, key).Scan(&priorDigest, &priorResult)
		if err == nil {
			if priorDigest != snapshot.Journal[0].CommandDigest {
				return ErrIdempotencyConflict
			}
			var prior workorder.Snapshot
			if err := json.Unmarshal(priorResult, &prior); err != nil {
				return err
			}
			if prior.ID != snapshot.ID || prior.TenantID != tenant || prior.Revision != snapshot.Revision {
				return ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO work_order(tenant_id,id,project_id,status,phase_id,revision,template_id,template_version,payload,created_by) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8::jsonb,$9)`, snapshot.TenantID, snapshot.ID, snapshot.ProjectID, string(snapshot.Phase), string(snapshot.Phase), snapshot.TemplateID, snapshot.TemplateVersion, b, actor)
		if err != nil {
			return err
		}
		event := snapshot.Journal[0]
		eventPayload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO work_order_record(tenant_id,id,work_order_id,sequence,revision,kind,actor_id,idempotency_key,payload) VALUES($1,$2,$3,1,1,$4,$5,$6,$7::jsonb)`, tenant, uuid.NewString(), snapshot.ID, event.Type, actor, key, eventPayload)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO work_order_outbox(tenant_id,id,work_order_id,sequence,event_type,schema_version,payload) VALUES($1,$2,$3,1,$4,1,$5::jsonb)`, tenant, uuid.NewString(), snapshot.ID, "work_order."+strings.ToLower(event.Type), eventPayload)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO work_order_idempotency(tenant_id,work_order_id,actor_id,client_key,command_digest,result_snapshot) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, tenant, snapshot.ID, actor, key, event.CommandDigest, b)
		return err
	})
}

// GetSnapshot returns the authoritative persisted aggregate snapshot under tenant RLS.
func (s *Store) Get(ctx context.Context, tenant, id string) (workorder.Snapshot, error) {
	var snapshot workorder.Snapshot
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var payload []byte
		var revision int64
		var phase string
		err := tx.QueryRow(ctx, `SELECT payload,revision,phase_id FROM work_order WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&payload, &revision, &phase)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			return fmt.Errorf("decode work order snapshot: %w", err)
		}
		if snapshot.TenantID != tenant || snapshot.ID != id || snapshot.Revision != uint64(revision) || string(snapshot.Phase) != phase {
			return ErrInvalid
		}
		return nil
	})
	if err != nil {
		return workorder.Snapshot{}, err
	}
	return snapshot, nil
}

// List returns a bounded tenant/project page ordered by stable work-order ID.
// The next cursor is the last returned ID; an empty cursor means the first page.
func (s *Store) List(ctx context.Context, tenant, project, status, cursor string, limit int) ([]workorder.Snapshot, string, error) {
	if tenant == "" || project == "" || limit <= 0 || limit > 200 {
		return nil, "", ErrInvalid
	}
	items := make([]workorder.Snapshot, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT payload FROM work_order WHERE tenant_id=$1 AND project_id=$2 AND ($3='' OR status=$3) AND id>$4 ORDER BY id LIMIT $5`, tenant, project, status, cursor, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var payload []byte
			if err := rows.Scan(&payload); err != nil {
				return err
			}
			var item workorder.Snapshot
			if err := json.Unmarshal(payload, &item); err != nil {
				return fmt.Errorf("decode listed work order: %w", err)
			}
			if item.TenantID != tenant || item.ProjectID != project {
				return ErrInvalid
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, "", err
	}
	if len(items) <= limit {
		return items, "", nil
	}
	items = items[:limit]
	return items, items[len(items)-1].ID, nil
}

// Execute serializes a domain mutation with its journal row, outbox event, and replay result.
// The command JSON is fingerprinted so a repeated key cannot authorize a different command.
func (s *Store) Execute(ctx context.Context, tenant, id, actor, key string, expected uint64, commandDigest string, mutate func(workorder.Snapshot) (workorder.Snapshot, error)) (workorder.Snapshot, error) {
	if tenant == "" || id == "" || actor == "" || key == "" || expected == 0 || commandDigest == "" || mutate == nil {
		return workorder.Snapshot{}, ErrInvalid
	}
	digest := commandDigest
	var result workorder.Snapshot
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var payload []byte
		var revision int64
		var phase string
		err := tx.QueryRow(ctx, `SELECT payload,revision,phase_id FROM work_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&payload, &revision, &phase)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var priorDigest string
		var priorResult []byte
		err = tx.QueryRow(ctx, `SELECT command_digest,result_snapshot FROM work_order_idempotency WHERE tenant_id=$1 AND work_order_id=$2 AND actor_id=$3 AND client_key=$4`, tenant, id, actor, key).Scan(&priorDigest, &priorResult)
		if err == nil {
			if priorDigest != digest {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(priorResult, &result)
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if uint64(revision) != expected {
			return ErrRevisionConflict
		}
		var current workorder.Snapshot
		if err = json.Unmarshal(payload, &current); err != nil {
			return fmt.Errorf("decode work order snapshot: %w", err)
		}
		if current.TenantID != tenant || current.ID != id || current.Revision != expected || string(current.Phase) != phase {
			return ErrInvalid
		}
		result, err = mutate(current)
		if err != nil {
			return err
		}
		if _, err = workorder.Restore(result); err != nil {
			return err
		}
		if result.ID != id || result.TenantID != tenant || result.Revision != expected+1 || len(result.Journal) != len(current.Journal)+1 {
			return ErrInvalid
		}
		event := result.Journal[len(result.Journal)-1]
		if event.Revision != result.Revision || event.IdempotencyKey != key || event.CommandDigest != digest || event.ActorID != actor || event.Type == "" {
			return ErrInvalid
		}
		newPayload, err := json.Marshal(result)
		if err != nil {
			return err
		}
		eventPayload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO work_order_record(tenant_id,id,work_order_id,sequence,revision,kind,actor_id,idempotency_key,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, tenant, uuid.NewString(), id, int64(result.Revision), int64(result.Revision), event.Type, actor, key, eventPayload)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE work_order SET payload=$1::jsonb,status=$2,phase_id=$2,revision=$3,updated_at=now() WHERE tenant_id=$4 AND id=$5 AND revision=$6`, newPayload, string(result.Phase), int64(result.Revision), tenant, id, revision)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO work_order_outbox(tenant_id,id,work_order_id,sequence,event_type,schema_version,payload) VALUES($1,$2,$3,$4,$5,1,$6::jsonb)`, tenant, uuid.NewString(), id, int64(result.Revision), "work_order."+strings.ToLower(event.Type), eventPayload)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO work_order_idempotency(tenant_id,work_order_id,actor_id,client_key,command_digest,result_snapshot) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, tenant, id, actor, key, digest, newPayload)
		return err
	})
	return result, err
}

func (s *Store) PublishTemplate(ctx context.Context, tenant, id, version, digest, author string, payload json.RawMessage) error {
	if tenant == "" || id == "" || version == "" || digest == "" || author == "" || !json.Valid(payload) {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO work_order_template_version(tenant_id,template_id,version,digest,payload,published_by) VALUES($1,$2,$3,$4,$5::jsonb,$6)`, tenant, id, version, digest, []byte(payload), author)
		return err
	})
}

// GetPublishedTemplate resolves an immutable published template version.
func (s *Store) GetPublishedTemplate(ctx context.Context, tenant, id, version string) (json.RawMessage, string, error) {
	var payload []byte
	var digest string
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT payload,digest FROM work_order_template_version WHERE tenant_id=$1 AND template_id=$2 AND version=$3`, tenant, id, version).Scan(&payload, &digest)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return nil, "", err
	}
	return append(json.RawMessage(nil), payload...), digest, nil
}

type ArtifactRecord struct {
	ID, TenantID, WorkOrderID, ProjectID, Kind, ActorID, IdempotencyKey, CommandDigest string
	SourceRevision                                                                     uint64
	Payload                                                                            json.RawMessage
	CreatedAt                                                                          time.Time
}

func (s *Store) GetArtifact(ctx context.Context, tenantID, orderID, actorID, idempotencyKey, kind string) (ArtifactRecord, error) {
	if tenantID == "" || orderID == "" || actorID == "" || idempotencyKey == "" || (kind != "REPORT" && kind != "BILLING") {
		return ArtifactRecord{}, ErrInvalid
	}
	var out ArtifactRecord
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var payload []byte
		err := tx.QueryRow(ctx, `SELECT id,tenant_id,work_order_id,project_id,kind,actor_id,idempotency_key,command_digest,source_revision,payload,created_at FROM work_order_artifact WHERE tenant_id=$1 AND work_order_id=$2 AND actor_id=$3 AND idempotency_key=$4 AND kind=$5`, tenantID, orderID, actorID, idempotencyKey, kind).Scan(&out.ID, &out.TenantID, &out.WorkOrderID, &out.ProjectID, &out.Kind, &out.ActorID, &out.IdempotencyKey, &out.CommandDigest, &out.SourceRevision, &payload, &out.CreatedAt)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out.Payload = append(json.RawMessage(nil), payload...)
		return nil
	})
	return out, err
}

// RecordArtifact stores an immutable report or billing artifact tied to the exact
// work-order revision it read. Replay returns the original artifact before checking
// current revision; a reused key with different input is rejected.
func (s *Store) RecordArtifact(ctx context.Context, tenantID, orderID, projectID, actorID, idempotencyKey string, expectedRevision uint64, kind, digest string, payload []byte) (ArtifactRecord, error) {
	if tenantID == "" || orderID == "" || projectID == "" || actorID == "" || idempotencyKey == "" || expectedRevision == 0 || digest == "" || (kind != "REPORT" && kind != "BILLING") || !json.Valid(payload) {
		return ArtifactRecord{}, ErrInvalid
	}
	var out ArtifactRecord
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var currentProject string
		var revision int64
		err := tx.QueryRow(ctx, `SELECT project_id,revision FROM work_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, orderID).Scan(&currentProject, &revision)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if currentProject != projectID {
			return ErrInvalid
		}
		var priorPayload []byte
		err = tx.QueryRow(ctx, `SELECT id,tenant_id,work_order_id,project_id,kind,actor_id,idempotency_key,command_digest,source_revision,payload,created_at FROM work_order_artifact WHERE tenant_id=$1 AND work_order_id=$2 AND kind=$3 AND actor_id=$4 AND idempotency_key=$5`, tenantID, orderID, kind, actorID, idempotencyKey).Scan(&out.ID, &out.TenantID, &out.WorkOrderID, &out.ProjectID, &out.Kind, &out.ActorID, &out.IdempotencyKey, &out.CommandDigest, &out.SourceRevision, &priorPayload, &out.CreatedAt)
		if err == nil {
			if out.CommandDigest != digest {
				return ErrIdempotencyConflict
			}
			out.Payload = append(json.RawMessage(nil), priorPayload...)
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if uint64(revision) != expectedRevision {
			return ErrRevisionConflict
		}
		out = ArtifactRecord{ID: uuid.NewString(), TenantID: tenantID, WorkOrderID: orderID, ProjectID: projectID, Kind: kind, ActorID: actorID, IdempotencyKey: idempotencyKey, CommandDigest: digest, SourceRevision: expectedRevision, Payload: append(json.RawMessage(nil), payload...)}
		_, err = tx.Exec(ctx, `INSERT INTO work_order_artifact(tenant_id,id,work_order_id,project_id,kind,actor_id,idempotency_key,command_digest,source_revision,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)`, tenantID, out.ID, orderID, projectID, kind, actorID, idempotencyKey, digest, int64(expectedRevision), payload)
		return err
	})
	return out, err
}

func (s *Store) String() string {
	if s == nil || s.pool == nil {
		return "workorderstore(closed)"
	}
	return fmt.Sprintf("workorderstore(%p)", s.pool)
}
