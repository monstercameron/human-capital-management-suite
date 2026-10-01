package timestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// AtomicBatchEntry is the data-layer representation of one prepared punch.
// A nil Session or Observation is a row-level rejection whose receipt still
// commits with the rest of the batch.
type AtomicBatchEntry struct {
	// ExpectedProjectionRevision fences a self-clock snapshot at commit.
	ExpectedProjectionRevision int64
	Sequence                   int64
	Receipt                    ReceiptRow
	Session                    *SessionRow
	Event                      *EventRow
	Observation                *ObservationRow
	SessionIsNew               bool
	ExpectedRevision           int64
}

// AtomicBatchResult contains the immutable receipts and durable cursor from
// one transaction. Existing receipt rows are returned unchanged.
type AtomicBatchResult struct {
	Receipts          []ReceiptRow
	HighestContiguous int64
	Duplicates        map[int64]bool
}

// CommitBatch commits receipt, observation, session, transition-event and
// outbox rows under one tenant transaction. The per-device advisory lock
// serializes cursor recomputation with concurrent submissions.
func (s *Store) CommitBatch(ctx context.Context, tenant, deviceID string, entries []AtomicBatchEntry) (AtomicBatchResult, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(deviceID) == "" || len(entries) == 0 {
		return AtomicBatchResult{}, ErrInvalid
	}
	ordered := append([]AtomicBatchEntry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Sequence < ordered[j].Sequence })
	for i := range ordered {
		if ordered[i].Sequence <= 0 || ordered[i].Receipt.DeviceSequence != ordered[i].Sequence || ordered[i].Receipt.Status == "" {
			return AtomicBatchResult{}, ErrInvalid
		}
		ordered[i].Receipt.TenantID, ordered[i].Receipt.DeviceID = tenant, deviceID
	}
	result := AtomicBatchResult{Receipts: make([]ReceiptRow, 0, len(ordered)), Duplicates: make(map[int64]bool)}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+"|"+deviceID); err != nil {
			return err
		}
		var deviceState string
		deviceErr := tx.QueryRow(ctx, `SELECT state FROM time_device WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, deviceID).Scan(&deviceState)
		if deviceErr != nil && !errors.Is(deviceErr, dbport.ErrNoRows) {
			return deviceErr
		}
		if deviceErr == nil && deviceState != "ACTIVE" {
			for i := range ordered {
				if ordered[i].Observation != nil {
					ordered[i].Receipt.Status = "HELD"
					ordered[i].Receipt.Reason = "DEVICE_REVOKED"
					ordered[i].Receipt.ObservationID = ""
					ordered[i].Observation = nil
					ordered[i].Session = nil
					ordered[i].Event = nil
				}
			}
		}
		if err := lockBatchProjections(ctx, tx, tenant, ordered); err != nil {
			return err
		}
		inserted := false
		for _, entry := range ordered {
			receipt, found, err := readBatchReceipt(ctx, tx, tenant, deviceID, entry.Sequence)
			if err != nil {
				return err
			}
			if found {
				result.Duplicates[entry.Sequence] = true
				if validateReceiptPayload(receipt.Payload) != nil {
					return ErrIdempotencyConflict
				}
				if receipt.Status != entry.Receipt.Status || receipt.Reason != entry.Receipt.Reason || !bytes.Equal(normalizeJSON(receipt.Payload), normalizeJSON(entry.Receipt.Payload)) {
					return ErrIdempotencyConflict
				}
				if entry.Observation != nil {
					if receipt.ObservationID != entry.Observation.ID {
						return ErrIdempotencyConflict
					}
					var priorDigest string
					if err := tx.QueryRow(ctx, `SELECT digest FROM time_observation WHERE tenant_id=$1 AND id=$2`, tenant, receipt.ObservationID).Scan(&priorDigest); err != nil {
						return err
					}
					if priorDigest != entry.Observation.Digest {
						return ErrIdempotencyConflict
					}
				}
				result.Receipts = append(result.Receipts, receipt)
				continue
			}
			if entry.Observation != nil {
				if err := commitBatchObservation(ctx, tx, tenant, *entry.Observation); err != nil {
					return err
				}
			}
			if entry.Session != nil && entry.Event != nil {
				if err := commitBatchSession(ctx, tx, tenant, entry); err != nil {
					return err
				}
			}
			if entry.Receipt.Status == "ACCEPTED" && entry.Observation != nil && entry.Session != nil {
				if err := advanceBatchProjection(ctx, tx, tenant, entry); err != nil {
					return err
				}
			}
			r := entry.Receipt
			payload := r.Payload
			if payload == nil {
				payload = json.RawMessage("{}")
			}
			if !json.Valid(payload) {
				return ErrInvalid
			}
			if err := validateReceiptPayload(payload); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO time_receipt(tenant_id,device_id,device_sequence,status,reason,observation_id,payload) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, tenant, deviceID, r.DeviceSequence, r.Status, r.Reason, nullIfEmpty(r.ObservationID), []byte(payload))
			if err != nil {
				return err
			}
			r.Payload = payload
			body, err := batchReceiptEventPayload(tenant, deviceID, entry)
			if err != nil {
				return err
			}
			if err := appendOutbox(ctx, tx, tenant, "clock.punch."+strings.ToLower(r.Status), 1, body); err != nil {
				return err
			}
			result.Receipts = append(result.Receipts, r)
			inserted = true
		}
		cursor, err := batchCursor(ctx, tx, tenant, deviceID)
		if err != nil {
			return err
		}
		result.HighestContiguous = cursor
		if inserted {
			for _, entry := range ordered {
				if entry.Sequence > cursor {
					payload, err := json.Marshal(struct {
						SubjectID   string `json:"subject_id"`
						SubjectKind string `json:"subject_kind"`
						Kind        string `json:"kind"`
						DetailRef   string `json:"detail_ref"`
					}{deviceID, "clock_device", "SEQUENCE_GAP", deviceID + ":gap:" + strconv.FormatInt(cursor+1, 10) + ":" + strconv.FormatInt(entry.Sequence, 10)})
					if err != nil {
						return err
					}
					if err := appendOutbox(ctx, tx, tenant, "clock.exception.raised", 1, payload); err != nil {
						return err
					}
					break
				}
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO time_receipt_cursor(tenant_id,device_id,highest_contiguous) VALUES($1,$2,$3) ON CONFLICT (tenant_id,device_id) DO UPDATE SET highest_contiguous=$3,updated_at=now()`, tenant, deviceID, cursor)
		return err
	})
	if err != nil {
		return AtomicBatchResult{}, err
	}
	return result, nil
}

type batchProjectionKey struct {
	worker, assignment string
}

func lockBatchProjections(ctx context.Context, tx dbport.Tx, tenant string, entries []AtomicBatchEntry) error {
	keys := make(map[batchProjectionKey]struct{})
	for _, entry := range entries {
		if entry.Receipt.Status != "ACCEPTED" || entry.Observation == nil || entry.Session == nil {
			continue
		}
		keys[batchProjectionKey{worker: entry.Observation.WorkerRef, assignment: entry.Observation.AssignmentRef}] = struct{}{}
	}
	ordered := make([]batchProjectionKey, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].worker == ordered[j].worker {
			return ordered[i].assignment < ordered[j].assignment
		}
		return ordered[i].worker < ordered[j].worker
	})
	for _, key := range ordered {
		var revision int64
		err := tx.QueryRow(ctx, `SELECT revision FROM time_self_clock_projection WHERE tenant_id=$1 AND tenant_id=current_setting('hcmnext.tenant_id',true) AND worker_ref=$2 AND assignment_ref=$3 FOR UPDATE`, tenant, key.worker, key.assignment).Scan(&revision)
		if errors.Is(err, dbport.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func advanceBatchProjection(ctx context.Context, tx dbport.Tx, tenant string, entry AtomicBatchEntry) error {
	obs, session := entry.Observation, entry.Session
	projection, err := LoadSelfProjection(ctx, tx, tenant, obs.WorkerRef, obs.AssignmentRef)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if entry.ExpectedProjectionRevision > 0 && entry.ExpectedProjectionRevision != projection.Revision {
		return ErrRevisionConflict
	}
	_, err = BumpSelfProjection(ctx, tx, tenant, obs.WorkerRef, obs.AssignmentRef, projection.Revision, punchProjectionStatus(obs.EventType, session.Status), obs.OccurredAt)
	return err
}

func readBatchReceipt(ctx context.Context, tx dbport.Tx, tenant, device string, seq int64) (ReceiptRow, bool, error) {
	row := tx.QueryRow(ctx, `SELECT `+receiptColumns+` FROM time_receipt WHERE tenant_id=$1 AND device_id=$2 AND device_sequence=$3`, tenant, device, seq)
	r, err := scanReceiptRow(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return ReceiptRow{}, false, nil
	}
	return r, err == nil, err
}

func commitBatchObservation(ctx context.Context, tx dbport.Tx, tenant string, obs ObservationRow) error {
	var id, digest string
	err := tx.QueryRow(ctx, `SELECT id,digest FROM time_observation WHERE tenant_id=$1 AND source=$2 AND idempotency_key=$3`, tenant, obs.Source, obs.IdempotencyKey).Scan(&id, &digest)
	if err == nil {
		if digest != obs.Digest || id != obs.ID {
			return ErrIdempotencyConflict
		}
		return nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	payload := obs.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	if !json.Valid(payload) {
		return ErrInvalid
	}
	_, err = tx.Exec(ctx, `INSERT INTO time_observation(tenant_id,id,worker_ref,assignment_ref,device_ref,source,event_type,project_ref,timezone,occurred_at,received_at,idempotency_key,digest,corrects_id,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb)`, tenant, obs.ID, obs.WorkerRef, obs.AssignmentRef, obs.DeviceRef, obs.Source, obs.EventType, obs.ProjectRef, obs.Timezone, obs.OccurredAt, obs.ReceivedAt, obs.IdempotencyKey, obs.Digest, nullIfEmpty(obs.CorrectsID), []byte(payload))
	if err != nil {
		return err
	}
	return nil
}

func validateReceiptPayload(payload []byte) error {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(payload, &value); err != nil || len(value) != 1 {
		return ErrInvalid
	}
	digest, ok := value["input_digest"]
	if !ok {
		return ErrInvalid
	}
	var text string
	if err := json.Unmarshal(digest, &text); err != nil || !strings.HasPrefix(text, "sha256:") || len(text) != len("sha256:")+64 {
		return ErrInvalid
	}
	return nil
}

func batchReceiptEventPayload(tenant, device string, entry AtomicBatchEntry) ([]byte, error) {
	p := struct {
		TenantID       string    `json:"tenant_id"`
		DeviceID       string    `json:"device_id"`
		DeviceSequence int64     `json:"device_sequence"`
		ReceiptID      string    `json:"receipt_id"`
		ObservationID  string    `json:"observation_id,omitempty"`
		Status         string    `json:"status"`
		Reason         string    `json:"reason,omitempty"`
		AssignmentRef  string    `json:"assignment_ref,omitempty"`
		EventType      string    `json:"event_type,omitempty"`
		OccurredAt     time.Time `json:"occurred_at,omitempty"`
		OpenedAt       time.Time `json:"opened_at,omitempty"`
		ClosedAt       time.Time `json:"closed_at,omitempty"`
		WorkedSeconds  int64     `json:"worked_seconds"`
		SourceRef      string    `json:"source_ref,omitempty"`
		SessionID      string    `json:"session_id,omitempty"`
		WorkerID       string    `json:"worker_id,omitempty"`
		InputDigest    string    `json:"input_digest"`
	}{TenantID: tenant, DeviceID: device, DeviceSequence: entry.Sequence, ReceiptID: device + ":" + strconv.FormatInt(entry.Sequence, 10), ObservationID: entry.Receipt.ObservationID, Status: entry.Receipt.Status, Reason: entry.Receipt.Reason}
	if len(entry.Receipt.Payload) > 0 {
		var digest struct {
			InputDigest string `json:"input_digest"`
		}
		if err := json.Unmarshal(entry.Receipt.Payload, &digest); err != nil {
			return nil, ErrInvalid
		}
		p.InputDigest = digest.InputDigest
	}
	if entry.Observation != nil {
		p.AssignmentRef = entry.Observation.AssignmentRef
		p.EventType = entry.Observation.EventType
		p.OccurredAt = entry.Observation.OccurredAt
		p.SourceRef = entry.Observation.Source
		p.WorkerID = entry.Observation.WorkerRef
	}
	if entry.Session != nil {
		p.SessionID = entry.Session.ID
		if entry.Event != nil && strings.EqualFold(entry.Event.Kind, "OPENED") {
			p.OpenedAt = entry.Session.OpenedAt
		}
		if !entry.Session.ClosedAt.IsZero() {
			p.ClosedAt = entry.Session.ClosedAt
		}
		if entry.Event != nil && strings.EqualFold(entry.Event.Kind, "CLOSED") {
			var state struct {
				Segments []struct{ Start, End time.Time } `json:"segments"`
			}
			if json.Unmarshal(entry.Session.Payload, &state) == nil {
				for _, segment := range state.Segments {
					if !segment.End.IsZero() && segment.End.After(segment.Start) {
						p.WorkedSeconds += int64(segment.End.Sub(segment.Start) / time.Second)
					}
				}
			}
		}
	}
	return json.Marshal(p)
}

func commitBatchSession(ctx context.Context, tx dbport.Tx, tenant string, entry AtomicBatchEntry) error {
	s, e := *entry.Session, *entry.Event
	payload := s.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	eventPayload := e.Payload
	if eventPayload == nil {
		eventPayload = json.RawMessage("{}")
	}
	if entry.SessionIsNew {
		_, err := tx.Exec(ctx, `INSERT INTO time_session(tenant_id,id,worker_ref,assignment_ref,status,source,project_ref,revision,opened_at,closed_at,payload) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$10::jsonb)`, tenant, s.ID, s.WorkerRef, s.AssignmentRef, s.Status, s.Source, s.ProjectRef, s.OpenedAt, nullableClosed(s.ClosedAt), []byte(payload))
		if err != nil {
			return err
		}
		e.Sequence, e.Revision = 1, 1
		if _, err = tx.Exec(ctx, `INSERT INTO time_session_event(tenant_id,id,session_id,sequence,revision,kind,actor_ref,idempotency_key,digest,payload) VALUES($1,gen_random_uuid()::text,$2,1,1,$3,$4,$5,$6,$7::jsonb)`, tenant, s.ID, e.Kind, e.ActorRef, e.IdempotencyKey, e.Digest, []byte(eventPayload)); err != nil {
			return err
		}
		deviceRef := s.Source
		if entry.Observation != nil && entry.Observation.DeviceRef != "" {
			deviceRef = entry.Observation.DeviceRef
		}
		outboxPayload, err := batchReceiptEventPayload(tenant, deviceRef, entry)
		if err != nil {
			return err
		}
		return appendOutbox(ctx, tx, tenant, "clock.session.opened", 1, outboxPayload)
	}
	var current int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM time_session WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, s.ID).Scan(&current); err != nil {
		return err
	}
	var prior string
	err := tx.QueryRow(ctx, `SELECT digest FROM time_session_event WHERE tenant_id=$1 AND session_id=$2 AND idempotency_key=$3`, tenant, s.ID, e.IdempotencyKey).Scan(&prior)
	if err == nil {
		if prior != e.Digest {
			return ErrIdempotencyConflict
		}
		return nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	if current != entry.ExpectedRevision {
		return ErrRevisionConflict
	}
	e.Sequence, e.Revision = current+1, current+1
	if _, err := tx.Exec(ctx, `INSERT INTO time_session_event(tenant_id,id,session_id,sequence,revision,kind,actor_ref,idempotency_key,digest,payload) VALUES($1,gen_random_uuid()::text,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, tenant, s.ID, e.Sequence, e.Revision, e.Kind, e.ActorRef, e.IdempotencyKey, e.Digest, []byte(eventPayload)); err != nil {
		return err
	}
	deviceRef := entry.Session.Source
	if entry.Observation != nil && entry.Observation.DeviceRef != "" {
		deviceRef = entry.Observation.DeviceRef
	}
	outboxPayload, err := batchReceiptEventPayload(tenant, deviceRef, entry)
	if err != nil {
		return err
	}
	if err := appendOutbox(ctx, tx, tenant, "clock.session."+strings.ToLower(e.Kind), 1, outboxPayload); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE time_session SET status=$1,project_ref=$2,revision=$3,closed_at=$4,payload=$5::jsonb,updated_at=now() WHERE tenant_id=$6 AND id=$7 AND revision=$8`, s.Status, s.ProjectRef, e.Revision, nullableClosed(s.ClosedAt), []byte(payload), tenant, s.ID, current)
	return err
}

func nullableClosed(t interface{ IsZero() bool }) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func batchCursor(ctx context.Context, tx dbport.Tx, tenant, device string) (int64, error) {
	var cursor int64
	err := tx.QueryRow(ctx, `SELECT highest_contiguous FROM time_receipt_cursor WHERE tenant_id=$1 AND device_id=$2`, tenant, device).Scan(&cursor)
	if errors.Is(err, dbport.ErrNoRows) {
		cursor = 0
	} else if err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT device_sequence FROM time_receipt WHERE tenant_id=$1 AND device_id=$2 AND device_sequence>$3 ORDER BY device_sequence`, tenant, device, cursor)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			return 0, err
		}
		if seq != cursor+1 {
			break
		}
		cursor = seq
	}
	return cursor, rows.Err()
}

func normalizeJSON(payload json.RawMessage) []byte {
	if len(payload) == 0 {
		return []byte("{}")
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return payload
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return payload
	}
	return canonical
}
