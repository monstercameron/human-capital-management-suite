package inbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PageQuery is a bounded recipient feed. Empty ReadState means both states;
// Archived selects the archive (false selects the active inbox). Nil Pinned
// means both. Dates are [CreatedFrom, CreatedBefore), in UTC. Order is immutable
// creation time then record ID, both descending, not mutable read/pin state.
type PageQuery struct {
	Limit                      int
	ReadState                  string
	Archived                   bool
	Pinned                     *bool
	CreatedFrom, CreatedBefore time.Time
	After                      *Position
}

// Position is a storage seek key, not an authorization token or public wire
// cursor. A transport must authenticate its recipient and bind/sign its cursor.
// State changes between pages may change membership; this is not a snapshot.
type Position struct {
	CreatedAt time.Time
	RecordID  uuid.UUID
}

type Page struct {
	Records []Record
	Next    *Position
}

func pagePredicate(tenant uuid.UUID, subject string, q PageQuery) (string, []any, int, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if tenant == uuid.Nil || strings.TrimSpace(subject) == "" || q.Limit < 1 || q.Limit > 200 {
		return "", nil, 0, invalid("page", "tenant, recipient and page size 1..200 required")
	}
	if q.ReadState != "" && q.ReadState != Read && q.ReadState != Unread {
		return "", nil, 0, invalid("read_state", "expected READ or UNREAD")
	}
	if !q.CreatedFrom.IsZero() && !q.CreatedBefore.IsZero() && !q.CreatedFrom.Before(q.CreatedBefore) {
		return "", nil, 0, invalid("created_at", "start must precede exclusive end")
	}
	if q.After != nil && (q.After.CreatedAt.IsZero() || q.After.RecordID == uuid.Nil) {
		return "", nil, 0, invalid("after", "complete seek position required")
	}
	args := []any{tenant, subject, q.Archived}
	where := "i.tenant_id=$1 AND i.subject_ref=$2 AND i.archived=$3"
	add := func(expr string, value any) {
		args = append(args, value)
		where += fmt.Sprintf(" AND "+expr, len(args))
	}
	if q.ReadState != "" {
		add("i.read_state=$%d", q.ReadState)
	}
	if q.Pinned != nil {
		add("i.pinned=$%d", *q.Pinned)
	}
	if !q.CreatedFrom.IsZero() {
		add("i.created_at >= $%d", q.CreatedFrom.UTC())
	}
	if !q.CreatedBefore.IsZero() {
		add("i.created_at < $%d", q.CreatedBefore.UTC())
	}
	if q.After != nil {
		args = append(args, q.After.CreatedAt.UTC(), q.After.RecordID)
		where += fmt.Sprintf(" AND (i.created_at,i.inbox_record_id)<($%d,$%d)", len(args)-1, len(args))
	}
	return where, args, q.Limit, nil
}

// ListPage seeks using a composite index, reads at most Limit+1 rows and does
// not COUNT the population. Tenant RLS and explicit subject predicates apply
// on every page, including when a caller supplies an arbitrary seek position.
func (s Store) ListPage(ctx context.Context, ex Executor, tenant uuid.UUID, subject string, q PageQuery) (Page, error) {
	where, args, limit, err := pagePredicate(tenant, subject, q)
	if err != nil {
		return Page{}, err
	}
	args = append(args, limit+1)
	rows, err := ex.Query(ctx, `SELECT i.inbox_record_id,i.recipient_message_id,i.read_state,i.archived,i.pinned,i.state_changed_at,i.version,i.created_at
		FROM inbox_record i WHERE `+where+fmt.Sprintf(" ORDER BY i.created_at DESC,i.inbox_record_id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return Page{}, fmt.Errorf("inbox: list page: %w", err)
	}
	defer rows.Close()
	out := Page{Records: make([]Record, 0, limit)}
	for rows.Next() {
		var r Record
		var version int64
		r.TenantID, r.SubjectRef = tenant, subject
		if err := rows.Scan(&r.InboxRecordID, &r.RecipientMessageID, &r.ReadState, &r.Archived, &r.Pinned, &r.StateChangedAt, &version, &r.CreatedAt); err != nil {
			return Page{}, err
		}
		r.Version = uint64(version)
		r.CreatedAt, r.StateChangedAt = r.CreatedAt.UTC(), r.StateChangedAt.UTC()
		if len(out.Records) == limit {
			last := out.Records[limit-1]
			out.Next = &Position{CreatedAt: last.CreatedAt, RecordID: last.InboxRecordID}
			break
		}
		out.Records = append(out.Records, r)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	return out, nil
}
