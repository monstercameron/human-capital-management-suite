// Package chatrecordstore persists chat governance through the independent
// chatstore pool. It never receives the core database handle.
package chatrecordstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type Store struct{ Chat *chatstore.Store }

func New(chat *chatstore.Store) *Store { return &Store{Chat: chat} }
func (s *Store) tx(ctx context.Context, fn func(dbport.Tx) error) error {
	if s == nil || s.Chat == nil {
		return fmt.Errorf("chatrecordstore: nil chat store")
	}
	return s.Chat.RunTx(ctx, fn)
}
func (s *Store) txTenant(ctx context.Context, tenant string, fn func(dbport.Tx) error) error {
	if s == nil || s.Chat == nil {
		return fmt.Errorf("chatrecordstore: nil chat store")
	}
	return s.Chat.RunTenantTx(ctx, tenant, fn)
}

func (s *Store) Append(ctx context.Context, r chatrecords.Record, e chatrecords.AuditEvent, o chatrecords.OutboxEvent) error {
	_, err := s.AppendEvent(ctx, r, e, o)
	return err
}

// AppendEvent returns the event identity assigned under the tenant audit lock.
func (s *Store) AppendEvent(ctx context.Context, r chatrecords.Record, e chatrecords.AuditEvent, o chatrecords.OutboxEvent) (chatrecords.AuditEvent, error) {
	_ = o
	if r.HoldIDs == nil {
		r.HoldIDs = []string{}
	}
	hb, _ := json.Marshal(r.HoldIDs)
	err := s.txTenant(ctx, r.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:"+r.TenantID); err != nil {
			return err
		}
		var sequence uint64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM chat_audit_event WHERE tenant_id=$1`, r.TenantID).Scan(&sequence); err != nil {
			return err
		}
		e.Sequence = sequence
		e.EventID = fmt.Sprintf("%s-%d", e.TargetID, sequence)
		e.Digest = chatrecords.DigestEvent(e)
		_, err := tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at,hold_ids,disposition,derived_from) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(tenant_id,record_id) DO UPDATE SET revision=GREATEST(chat_record_inventory.revision,EXCLUDED.revision),disposition=EXCLUDED.disposition`, r.TenantID, r.RecordID, r.ConversationID, string(r.Kind), r.SourceID, r.Revision, r.CreatedAt, hb, r.Disposition, r.DerivedFrom)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, e.TenantID, e.EventID, e.Sequence, e.ActorID, e.Action, e.TargetType, e.TargetID, e.PriorRevision, e.Reason, e.PolicyEvidence, e.At, e.Digest)
		if err != nil {
			return err
		}
		// The payload carries conversation_id/event_sequence/schema_version/
		// event_type so a watcher on chat_outbox can find this event by
		// conversation without needing the audit table; previously the
		// payload held only audit_digest, so records.audit events had no
		// conversation identifier at all.
		//
		// TODO(chatrecordstore): internal/data/chatstore's own outbox watch
		// path (contracts_adapter.go Watch/watchPage) currently filters on
		// payload->>'ConversationID' (PascalCase), not this snake_case key.
		// Switch to chatstore's typed outbox key constants / shared writer
		// once that lane exports them so records.audit events are actually
		// matched by that watcher.
		payload, _ := json.Marshal(map[string]any{
			"audit_digest":    e.Digest,
			"conversation_id": r.ConversationID,
			"event_sequence":  e.Sequence,
			"schema_version":  1,
			"event_type":      "records.audit",
		})
		_, err = tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'records.audit',$3)`, r.TenantID, e.EventID, payload)
		return err
	})
	return e, err
}

// defaultPageLimit and maxPageLimit clamp the *Page methods below: a
// non-positive limit falls back to the default and anything above the
// ceiling is truncated.
const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

func clampPageLimit(limit int) int {
	if limit <= 0 {
		return defaultPageLimit
	}
	if limit > maxPageLimit {
		return maxPageLimit
	}
	return limit
}

// ListPage returns chat_record_inventory rows in record_id order, keyset
// paginated after the given record_id. List (below) is unbounded because it
// implements chatrecords.Repository, whose exported signature is depended on
// by internal/application/chat_audit.go and internal/collaboration/chatrecords
// (both outside this package's scope) and cannot be changed here; ListPage is
// the paginated entry point new callers should use.
func (s *Store) ListPage(ctx context.Context, t, after string, limit int) ([]chatrecords.Record, error) {
	limit = clampPageLimit(limit)
	var out []chatrecords.Record
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT record_id,conversation_id,kind,source_id,revision,created_at,hold_ids,disposition,derived_from FROM chat_record_inventory WHERE tenant_id=$1 AND record_id>$2 ORDER BY record_id LIMIT $3`, t, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var r chatrecords.Record
			var k string
			var h []byte
			if e = rows.Scan(&r.RecordID, &r.ConversationID, &k, &r.SourceID, &r.Revision, &r.CreatedAt, &h, &r.Disposition, &r.DerivedFrom); e != nil {
				return e
			}
			r.TenantID = t
			r.Kind = chatrecords.Kind(k)
			_ = json.Unmarshal(h, &r.HoldIDs)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) List(ctx context.Context, t string) ([]chatrecords.Record, error) {
	var out []chatrecords.Record
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT record_id,conversation_id,kind,source_id,revision,created_at,hold_ids,disposition,derived_from FROM chat_record_inventory WHERE tenant_id=$1 ORDER BY record_id`, t)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var r chatrecords.Record
			var k string
			var h []byte
			if e = rows.Scan(&r.RecordID, &r.ConversationID, &k, &r.SourceID, &r.Revision, &r.CreatedAt, &h, &r.Disposition, &r.DerivedFrom); e != nil {
				return e
			}
			r.TenantID = t
			r.Kind = chatrecords.Kind(k)
			_ = json.Unmarshal(h, &r.HoldIDs)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// EventsPage returns chat_audit_event rows in sequence order, keyset
// paginated after the given sequence. See ListPage for why the unbounded
// Events method below cannot be changed from this package.
func (s *Store) EventsPage(ctx context.Context, t string, after uint64, limit int) ([]chatrecords.AuditEvent, error) {
	limit = clampPageLimit(limit)
	var out []chatrecords.AuditEvent
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest FROM chat_audit_event WHERE tenant_id=$1 AND sequence>$2 ORDER BY sequence LIMIT $3`, t, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x chatrecords.AuditEvent
			if e = rows.Scan(&x.EventID, &x.Sequence, &x.ActorID, &x.Action, &x.TargetType, &x.TargetID, &x.PriorRevision, &x.Reason, &x.PolicyEvidence, &x.At, &x.Digest); e != nil {
				return e
			}
			x.TenantID = t
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) Events(ctx context.Context, t string) ([]chatrecords.AuditEvent, error) {
	var out []chatrecords.AuditEvent
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest FROM chat_audit_event WHERE tenant_id=$1 ORDER BY sequence`, t)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x chatrecords.AuditEvent
			if e = rows.Scan(&x.EventID, &x.Sequence, &x.ActorID, &x.Action, &x.TargetType, &x.TargetID, &x.PriorRevision, &x.Reason, &x.PolicyEvidence, &x.At, &x.Digest); e != nil {
				return e
			}
			x.TenantID = t
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, err
}

// PutHold is idempotent on (tenant_id,hold_id): a retried or duplicated
// placement request no longer surfaces a raw unique-constraint error to the
// caller.
func (s *Store) PutHold(ctx context.Context, h chatrecords.Hold) error {
	return s.txTenant(ctx, h.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_record_hold(tenant_id,hold_id,matter_ref,reason,placed_by,placed_at,released_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (tenant_id,hold_id) DO NOTHING`, h.TenantID, h.HoldID, h.MatterRef, h.Reason, h.PlacedBy, h.PlacedAt, h.ReleasedAt)
		return e
	})
}

// HoldsPage returns chat_record_hold rows in hold_id order, keyset paginated
// after the given hold_id. See ListPage for why the unbounded Holds method
// below cannot be changed from this package.
func (s *Store) HoldsPage(ctx context.Context, t, after string, limit int) ([]chatrecords.Hold, error) {
	limit = clampPageLimit(limit)
	var out []chatrecords.Hold
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT hold_id,matter_ref,reason,placed_by,placed_at,released_at FROM chat_record_hold WHERE tenant_id=$1 AND hold_id>$2 ORDER BY hold_id LIMIT $3`, t, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var h chatrecords.Hold
			if e = rows.Scan(&h.HoldID, &h.MatterRef, &h.Reason, &h.PlacedBy, &h.PlacedAt, &h.ReleasedAt); e != nil {
				return e
			}
			h.TenantID = t
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) Holds(ctx context.Context, t string) ([]chatrecords.Hold, error) {
	var out []chatrecords.Hold
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT hold_id,matter_ref,reason,placed_by,placed_at,released_at FROM chat_record_hold WHERE tenant_id=$1`, t)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var h chatrecords.Hold
			if e = rows.Scan(&h.HoldID, &h.MatterRef, &h.Reason, &h.PlacedBy, &h.PlacedAt, &h.ReleasedAt); e != nil {
				return e
			}
			h.TenantID = t
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) PutExport(ctx context.Context, e chatrecords.Export) error {
	ids, _ := json.Marshal(e.RecordIDs)
	return s.txTenant(ctx, e.TenantID, func(tx dbport.Tx) error {
		_, x := tx.Exec(ctx, `INSERT INTO chat_record_export(tenant_id,export_id,record_ids,digest,created_at) VALUES($1,$2,$3,$4,$5)`, e.TenantID, e.ExportID, ids, e.Digest, e.CreatedAt)
		return x
	})
}
func (s *Store) PutReport(ctx context.Context, p chatrecords.Report) error {
	return s.txTenant(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_moderation_report(tenant_id,report_id,conversation_id,target_id,reporter_id,reason,created_at,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.TenantID, p.ReportID, p.ConversationID, p.TargetID, p.ReporterID, p.Reason, p.CreatedAt, p.State)
		return e
	})
}

// ReportsPage returns chat_moderation_report rows in report_id order, keyset
// paginated after the given report_id. See ListPage for why the unbounded
// Reports method below cannot be changed from this package.
func (s *Store) ReportsPage(ctx context.Context, t, after string, limit int) ([]chatrecords.Report, error) {
	limit = clampPageLimit(limit)
	var out []chatrecords.Report
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT report_id,conversation_id,target_id,reporter_id,reason,created_at,state FROM chat_moderation_report WHERE tenant_id=$1 AND report_id>$2 ORDER BY report_id LIMIT $3`, t, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var p chatrecords.Report
			if e = rows.Scan(&p.ReportID, &p.ConversationID, &p.TargetID, &p.ReporterID, &p.Reason, &p.CreatedAt, &p.State); e != nil {
				return e
			}
			p.TenantID = t
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) Reports(ctx context.Context, t string) ([]chatrecords.Report, error) {
	var out []chatrecords.Report
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT report_id,conversation_id,target_id,reporter_id,reason,created_at,state FROM chat_moderation_report WHERE tenant_id=$1 ORDER BY created_at,report_id`, t)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var p chatrecords.Report
			if e = rows.Scan(&p.ReportID, &p.ConversationID, &p.TargetID, &p.ReporterID, &p.Reason, &p.CreatedAt, &p.State); e != nil {
				return e
			}
			p.TenantID = t
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) PutCaseAction(ctx context.Context, t string, a chatrecords.CaseAction) error {
	return s.txTenant(ctx, t, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_moderation_action(tenant_id,case_id,action,actor_id,reason,evidence_ref,at_time) VALUES($1,$2,$3,$4,$5,$6,$7)`, t, a.CaseID, a.Action, a.ActorID, a.Reason, a.EvidenceRef, a.At)
		return e
	})
}

// snapshotTableRowCap bounds how many rows of a single durable table Snapshot
// will pull into memory for one tenant. Previously each table was loaded with
// an unbounded jsonb_agg over the whole tenant partition on the send-path
// pool; a tenant with an unexpectedly large table (e.g. years of
// chat_audit_event) could exhaust adapter memory. Snapshot now asks for one
// row more than the cap and fails loudly instead of silently truncating the
// snapshot (a truncated snapshot would restore incomplete data without
// telling anyone).
const snapshotTableRowCap = 5000

func (s *Store) Snapshot(ctx context.Context, t string) (chatrecords.Snapshot, error) {
	r, e := s.List(ctx, t)
	if e != nil {
		return chatrecords.Snapshot{}, e
	}
	ev, e := s.Events(ctx, t)
	if e != nil {
		return chatrecords.Snapshot{}, e
	}
	var raw = map[string][]byte{}
	if err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		for _, table := range durableTables {
			// The inner SELECT caps at cap+1 rows so the outer jsonb_agg
			// (and thus the byte payload actually written into raw[table])
			// never materializes more than cap+1 rows; if it hits cap+1 the
			// snapshot is rejected below rather than silently truncated to
			// cap rows and used anyway.
			rows, e := tx.Query(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(x)), '[]'::jsonb), count(*) FROM (SELECT * FROM `+table+` WHERE tenant_id=$1 LIMIT $2) x`, t, snapshotTableRowCap+1)
			if e != nil {
				return e
			}
			var b []byte
			var n int
			if rows.Next() {
				if e = rows.Scan(&b, &n); e != nil {
					rows.Close()
					return e
				}
			}
			rows.Close()
			if e = rows.Err(); e != nil {
				return e
			}
			if n > snapshotTableRowCap {
				return fmt.Errorf("chatrecordstore: snapshot table %s exceeds row cap %d for tenant %s", table, snapshotTableRowCap, t)
			}
			raw[table] = b
		}
		return nil
	}); err != nil {
		return chatrecords.Snapshot{}, err
	}
	snapOut := chatrecords.Snapshot{TenantID: t, Watermark: uint64(len(ev)), Records: r, Events: ev, RawTables: raw}
	snapOut.Digest = snapshotDigest(snapOut)
	return snapOut, nil
}

// bigserialTables lists the durable tables whose primary key is a bigserial
// `id` column (see internal/data/chatstore/migrations/00001_chat.sql and
// 00002_chat_records.sql, which this package cannot edit). Restore inserts
// these ids explicitly from the snapshot, which leaves each table's sequence
// unchanged at whatever it was before restore (often 1 on a fresh database);
// the first ordinary insert after restore then collides with a snapshot row
// that already occupies that id, e.g. INSERT INTO chat_outbox without an
// explicit id. Restore resets every touched sequence to max(id) so the next
// nextval() continues after the highest restored id.
var bigserialTables = []string{"chat_post_revision", "chat_outbox", "chat_moderation_action"}

func (s *Store) Restore(ctx context.Context, snap chatrecords.Snapshot) error {
	if snap.TenantID == "" || snap.Digest == "" || snapshotDigest(snap) != snap.Digest {
		return fmt.Errorf("chatrecordstore: snapshot digest mismatch")
	}
	return s.txTenant(ctx, snap.TenantID, func(tx dbport.Tx) error {
		touched := make(map[string]bool, len(durableTables))
		for _, table := range durableTables {
			b := snap.RawTables[table]
			if len(b) == 0 {
				continue
			}
			if _, e := tx.Exec(ctx, `INSERT INTO `+table+` SELECT * FROM jsonb_populate_recordset(NULL::`+table+`, $1) ON CONFLICT DO NOTHING`, b); e != nil {
				return fmt.Errorf("restore %s: %w", table, e)
			}
			touched[table] = true
		}
		for _, table := range bigserialTables {
			if !touched[table] {
				continue
			}
			if _, e := tx.Exec(ctx, `SELECT setval(pg_get_serial_sequence('`+table+`','id'), COALESCE((SELECT max(id) FROM `+table+`),1), (SELECT max(id) FROM `+table+`) IS NOT NULL)`); e != nil {
				return fmt.Errorf("restore %s: reset sequence: %w", table, e)
			}
		}
		return nil
	})
}
func (s *Store) Reconcile(ctx context.Context, t string) (chatrecords.ReconcileResult, error) {
	r, e := s.List(ctx, t)
	if e != nil {
		return chatrecords.ReconcileResult{}, e
	}
	ev, e := s.Events(ctx, t)
	if e != nil {
		return chatrecords.ReconcileResult{}, e
	}
	result := chatrecords.ReconcileResult{Records: len(r), Events: len(ev)}
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE a.event_id IS NULL) FROM chat_outbox o LEFT JOIN chat_audit_event a ON a.tenant_id=o.tenant_id AND a.event_id='chat-outbox:'||o.id::text WHERE o.tenant_id=$1 AND o.event_type <> 'records.audit'`, t).Scan(&result.Outbox, &result.MissingOutbox)
	})
	if err != nil {
		return chatrecords.ReconcileResult{}, err
	}
	result.Ready = result.MissingOutbox == 0
	return result, nil
}

var _ chatrecords.Repository = (*Store)(nil)

var durableTables = []string{
	"chat_conversation", "chat_membership", "chat_conversation_idempotency",
	"chat_post", "chat_post_revision", "chat_idempotency", "chat_outbox", "chat_outbox_receipt",
	"chat_preference", "chat_reaction", "chat_pin", "chat_cursor",
	"chat_channel_policy", "chat_share_grant",
	"chat_channel_todo", "chat_channel_todo_revision",
	"chat_channel_widget", "chat_channel_widget_revision",
	"chat_channel_poll", "chat_channel_poll_vote", "chat_channel_poll_revision",
	"chat_app_installation", "chat_app_event", "chat_app_event_seen",
	"chat_thread_follow", "chat_personal_sidebar", "chat_quiet_hours",
	"chat_record_inventory", "chat_audit_event", "chat_record_hold", "chat_record_export",
	"chat_retention_policy",
	"chat_moderation_report", "chat_moderation_action",
}

func snapshotDigest(s chatrecords.Snapshot) string {
	s.Digest = ""
	b, _ := json.Marshal(s)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
