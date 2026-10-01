// Package agentauditstore persists the agent audit hash chain in PostgreSQL.
//
// It implements agentaudit.Store: each tenant owns one gap-free, append-only
// chain whose links are sealed and projected by the agentaudit package, so the
// durable and in-memory stores cannot disagree about hashing or redaction.
package agentauditstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the transaction source the store needs.
type DB interface{ dbport.Beginner }

// Store is the PostgreSQL agentaudit.Store.
type Store struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
	clock      func() time.Time
}

var _ agentaudit.Store = (*Store)(nil)

// New builds a Store over db. tenantUUID maps the entry's tenant text to the
// canonical tenant row and must return uuid.Nil for an unknown tenant.
func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: database and canonical tenant mapper are required", agentaudit.ErrInvalidEntry)
	}
	return &Store{db: db, tenantUUID: tenantUUID, clock: func() time.Time { return time.Now().UTC() }}, nil
}

// Append validates and records one event. A byte-identical retry returns the
// stored record with Created=false; changed content under a reused event ID is
// ErrDuplicateConflict. Appends for one tenant are serialised on the tenant row
// so the sequence is gap-free and the chain cannot fork.
func (s *Store) Append(ctx context.Context, entry agentaudit.Entry) (agentaudit.Record, error) {
	if s == nil || s.db == nil {
		return agentaudit.Record{}, fmt.Errorf("%w: nil store", agentaudit.ErrInvalidEntry)
	}
	if err := ctx.Err(); err != nil {
		return agentaudit.Record{}, err
	}
	if err := agentaudit.ValidateEntry(entry); err != nil {
		return agentaudit.Record{}, err
	}
	tenant, err := s.tenant(entry.TenantID)
	if err != nil {
		return agentaudit.Record{}, err
	}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return agentaudit.Record{}, fmt.Errorf("agentauditstore: encode entry: %w", err)
	}
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return agentaudit.Record{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockTenant(ctx, tx, tenant); err != nil {
		return agentaudit.Record{}, err
	}
	prior, err := scanRecord(tx.QueryRow(ctx, selectRecord+` WHERE tenant_id=$1 AND event_id=$2`, tenant, entry.EventID))
	if err == nil {
		if !agentaudit.SameEntry(prior.Entry, entry) {
			return agentaudit.Record{}, fmt.Errorf("%w: %s", agentaudit.ErrDuplicateConflict, entry.EventID)
		}
		prior.Created = false
		return prior, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return agentaudit.Record{}, err
	}
	var sequence int64
	prev := ""
	err = tx.QueryRow(ctx, `SELECT sequence, chain_hash FROM agent_audit_record WHERE tenant_id=$1 ORDER BY sequence DESC LIMIT 1`, tenant).Scan(&sequence, &prev)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return agentaudit.Record{}, fmt.Errorf("agentauditstore: read chain head: %w", err)
	}
	record := agentaudit.SealRecord(entry, uint64(sequence)+1, prev, s.clock().Truncate(time.Microsecond))
	if _, err := tx.Exec(ctx, `INSERT INTO agent_audit_record (tenant_id,sequence,event_id,kind,user_id,task_id,entry,prev_hash,chain_hash,recorded_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)`,
		tenant, int64(record.Sequence), entry.EventID, string(entry.Kind), entry.Actor.UserID, entry.Actor.TaskID, string(entryJSON), record.PrevHash, record.ChainHash, record.RecordedAt); err != nil {
		return agentaudit.Record{}, fmt.Errorf("agentauditstore: insert record: %w", err)
	}
	for i, edge := range entry.Edges {
		if _, err := tx.Exec(ctx, `INSERT INTO agent_audit_edge (tenant_id,sequence,ordinal,kind,from_ref,to_ref) VALUES ($1,$2,$3,$4,$5,$6)`,
			tenant, int64(record.Sequence), i, string(edge.Kind), edge.From, edge.To); err != nil {
			return agentaudit.Record{}, fmt.Errorf("agentauditstore: insert edge: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return agentaudit.Record{}, fmt.Errorf("agentauditstore: commit record: %w", err)
	}
	return record, nil
}

// Query returns the records the viewer may see, redacted per field. USER
// viewers see only their own user's records. Rows are read under the viewer's
// tenant context, so another tenant's chain is never visible.
func (s *Store) Query(ctx context.Context, q agentaudit.Query) ([]agentaudit.View, error) {
	if s == nil || s.db == nil {
		return nil, agentaudit.ErrInvalidEntry
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := agentaudit.ValidateQuery(q); err != nil {
		return nil, err
	}
	tenant, err := s.tenant(q.Viewer.TenantID)
	if err != nil {
		if errors.Is(err, agentaudit.ErrTenantBoundary) {
			return []agentaudit.View{}, nil
		}
		return nil, err
	}
	sqlText := selectRecord + ` WHERE tenant_id=$1`
	args := []any{tenant}
	if q.Viewer.Role == agentaudit.ViewerUser {
		args = append(args, q.Viewer.UserID)
		sqlText += fmt.Sprintf(` AND user_id=$%d`, len(args))
	}
	if q.TaskID != "" {
		args = append(args, q.TaskID)
		sqlText += fmt.Sprintf(` AND task_id=$%d`, len(args))
	}
	if len(q.Kinds) > 0 {
		kinds := make([]string, len(q.Kinds))
		for i, kind := range q.Kinds {
			kinds[i] = string(kind)
		}
		args = append(args, kinds)
		sqlText += fmt.Sprintf(` AND kind = ANY($%d)`, len(args))
	}
	sqlText += ` ORDER BY sequence`
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	records, err := readRecords(ctx, tx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("agentauditstore: commit query: %w", err)
	}
	views := make([]agentaudit.View, 0, len(records))
	for _, record := range records {
		if record.TenantID != q.Viewer.TenantID {
			continue
		}
		views = append(views, agentaudit.ProjectRecord(record, q.Viewer))
	}
	return views, nil
}

// Verify reads the tenant's whole chain in sequence order and recomputes it.
func (s *Store) Verify(ctx context.Context, tenantID string) error {
	if s == nil || s.db == nil || strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenant is required", agentaudit.ErrInvalidEntry)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tenant, err := s.tenant(tenantID)
	if err != nil {
		return err
	}
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, selectRecord+` WHERE tenant_id=$1 ORDER BY sequence`, tenant)
	if err != nil {
		return fmt.Errorf("agentauditstore: read chain: %w", err)
	}
	defer rows.Close()
	var chain []agentaudit.Record
	for rows.Next() {
		record, c, err := scanFull(rows)
		if err != nil {
			return err
		}
		// The indexed columns must agree with the hashed entry, otherwise a
		// lookup would disagree with the evidence.
		if record.TenantID != tenantID || c.eventID != record.EventID || c.kind != string(record.Kind) ||
			c.userID != record.Actor.UserID || c.taskID != record.Actor.TaskID {
			return fmt.Errorf("%w: sequence %d columns disagree with entry", agentaudit.ErrChainTampered, record.Sequence)
		}
		chain = append(chain, record)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("agentauditstore: read chain: %w", err)
	}
	return agentaudit.VerifyChain(chain)
}

// FindByEdge returns the event IDs, in chain order, whose entry carries an edge
// of the given kind between from and to. An empty from or to matches any.
func (s *Store) FindByEdge(ctx context.Context, tenantID string, kind agentaudit.EdgeKind, from, to string) ([]string, error) {
	if s == nil || s.db == nil || strings.TrimSpace(tenantID) == "" || kind == "" {
		return nil, fmt.Errorf("%w: tenant and edge kind are required", agentaudit.ErrInvalidEntry)
	}
	tenant, err := s.tenant(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := s.begin(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT r.event_id FROM agent_audit_edge e JOIN agent_audit_record r USING (tenant_id, sequence)
		WHERE e.tenant_id=$1 AND e.kind=$2 AND ($3='' OR e.from_ref=$3) AND ($4='' OR e.to_ref=$4) ORDER BY r.sequence, e.ordinal`, tenant, string(kind), from, to)
	if err != nil {
		return nil, fmt.Errorf("agentauditstore: find edge: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("agentauditstore: scan edge: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentauditstore: find edge: %w", err)
	}
	return ids, nil
}

const selectRecord = `SELECT sequence,event_id,kind,user_id,task_id,entry,prev_hash,chain_hash,recorded_at FROM agent_audit_record`

func (s *Store) tenant(text string) (uuid.UUID, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(text) != text {
		return uuid.Nil, fmt.Errorf("%w: tenant %q", agentaudit.ErrInvalidEntry, text)
	}
	tenant := s.tenantUUID(values.TenantId(text))
	if tenant == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: unknown tenant %q", agentaudit.ErrTenantBoundary, text)
	}
	return tenant, nil
}

func (s *Store) begin(ctx context.Context, tenant uuid.UUID) (dbport.Tx, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("agentauditstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// lockTenant serialises writers per tenant on the tenant row, the same lock the
// other tenant-versioned stores use.
func lockTenant(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) error {
	var locked uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT tenant_id FROM tenant WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&locked); err != nil {
		return fmt.Errorf("agentauditstore: lock tenant: %w", err)
	}
	return nil
}

func readRecords(ctx context.Context, tx dbport.Tx, sqlText string, args ...any) ([]agentaudit.Record, error) {
	rows, err := tx.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("agentauditstore: query records: %w", err)
	}
	defer rows.Close()
	var records []agentaudit.Record
	for rows.Next() {
		record, _, err := scanFull(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentauditstore: query records: %w", err)
	}
	return records, nil
}

type scanner interface{ Scan(dest ...any) error }

type columns struct{ eventID, kind, userID, taskID string }

func scanRecord(row scanner) (agentaudit.Record, error) {
	record, _, err := scanFull(row)
	return record, err
}

func scanFull(row scanner) (agentaudit.Record, columns, error) {
	var (
		sequence int64
		c        columns
		entry    []byte
		record   agentaudit.Record
	)
	if err := row.Scan(&sequence, &c.eventID, &c.kind, &c.userID, &c.taskID, &entry, &record.PrevHash, &record.ChainHash, &record.RecordedAt); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return agentaudit.Record{}, columns{}, err
		}
		return agentaudit.Record{}, columns{}, fmt.Errorf("agentauditstore: scan record: %w", err)
	}
	if err := json.Unmarshal(entry, &record.Entry); err != nil {
		return agentaudit.Record{}, columns{}, fmt.Errorf("%w: sequence %d entry does not decode", agentaudit.ErrChainTampered, sequence)
	}
	record.Sequence = uint64(sequence)
	record.RecordedAt = record.RecordedAt.UTC()
	return record, c, nil
}
