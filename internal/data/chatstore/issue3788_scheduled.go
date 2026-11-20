package chatstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3788: storage of messages their author chose to send later. Every
// statement runs in the host workspace's tenant transaction, and every
// statement an author makes names the author, so one person's row is never
// read, changed or removed through another's request. A cancelled or sent row
// is deleted, text and all.

const issue3788Columns = `id,tenant_id,home_tenant_id,author_id,org_scope_id,conversation_id,body,send_at,zone,state,refusal,attempts,revision,created_at`

type issue3788Row interface{ Scan(...any) error }

func issue3788Scan(r issue3788Row) (chatrecipient.ScheduledPost, error) {
	var (
		p        chatrecipient.ScheduledPost
		revision int64
	)
	err := r.Scan(&p.ID, &p.HostTenantID, &p.HomeTenantID, &p.AuthorID, &p.OrgScopeID, &p.ConversationID, &p.Body, &p.SendAt, &p.Zone, &p.State, &p.Refusal, &p.Attempts, &revision, &p.CreatedAt)
	p.Revision = uint64(revision)
	p.SendAt = p.SendAt.UTC()
	return p, err
}

// InsertScheduled keeps a new pending message unless its author already has
// limit of them. The count and the insert are one transaction.
func (r *RecipientStateStore) InsertScheduled(ctx context.Context, post chatrecipient.ScheduledPost, limit int) (chatrecipient.ScheduledPost, error) {
	if post.HostTenantID == "" || post.HomeTenantID == "" || post.AuthorID == "" || post.ConversationID == "" || strings.TrimSpace(post.Body) == "" {
		return chatrecipient.ScheduledPost{}, chat.ErrInvalidArgument
	}
	post.ID = uuid.NewString()
	var out chatrecipient.ScheduledPost
	err := r.store.RunTenantTx(ctx, post.HostTenantID, func(tx dbport.Tx) error {
		var held int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_scheduled_post WHERE tenant_id=$1 AND home_tenant_id=$2 AND author_id=$3`, post.HostTenantID, post.HomeTenantID, post.AuthorID).Scan(&held); err != nil {
			return err
		}
		if limit > 0 && held >= limit {
			return chatrecipient.ErrScheduledLimit
		}
		row := tx.QueryRow(ctx, `INSERT INTO chat_scheduled_post(tenant_id,id,home_tenant_id,author_id,org_scope_id,conversation_id,body,send_at,zone,state)
            VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending') RETURNING `+issue3788Columns,
			post.HostTenantID, post.ID, post.HomeTenantID, post.AuthorID, post.OrgScopeID, post.ConversationID, post.Body, post.SendAt, post.Zone)
		var err error
		out, err = issue3788Scan(row)
		return err
	})
	return out, err
}

// ListScheduled returns one author's rows, soonest first.
func (r *RecipientStateStore) ListScheduled(ctx context.Context, host, home, author string) ([]chatrecipient.ScheduledPost, error) {
	if host == "" || home == "" || author == "" {
		return nil, chat.ErrInvalidArgument
	}
	out := []chatrecipient.ScheduledPost{}
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+issue3788Columns+` FROM chat_scheduled_post WHERE tenant_id=$1 AND home_tenant_id=$2 AND author_id=$3 ORDER BY send_at,id LIMIT 200`, host, home, author)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := issue3788Scan(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// UpdateScheduled changes the text and time of one row of the author. It is a
// compare-and-set on the revision: a row that changed since the author read it,
// or that is being sent, is a conflict. Editing a refused row makes it pending
// again and clears the reason.
func (r *RecipientStateStore) UpdateScheduled(ctx context.Context, host, home, author, id, body string, sendAt time.Time, zone string, expected uint64) (chatrecipient.ScheduledPost, error) {
	var out chatrecipient.ScheduledPost
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `UPDATE chat_scheduled_post SET body=$5,send_at=$6,zone=$7,state='pending',refusal='',attempts=0,claim_until=NULL,revision=revision+1,updated_at=now()
            WHERE tenant_id=$1 AND home_tenant_id=$2 AND author_id=$3 AND id=$4 AND revision=$8 AND state IN ('pending','failed') RETURNING `+issue3788Columns,
			host, home, author, id, body, sendAt, zone, int64(expected))
		var err error
		out, err = issue3788Scan(row)
		if errors.Is(err, dbport.ErrNoRows) {
			return r.issue3788Why(ctx, tx, host, home, author, id)
		}
		return err
	})
	return out, err
}

// issue3788Why tells a missing row from one that cannot be changed now.
func (r *RecipientStateStore) issue3788Why(ctx context.Context, tx dbport.Tx, host, home, author, id string) error {
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM chat_scheduled_post WHERE tenant_id=$1 AND home_tenant_id=$2 AND author_id=$3 AND id=$4`, host, home, author, id).Scan(&state)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ErrNotFound
	}
	if err != nil {
		return err
	}
	return chat.ErrConflict
}

// DeleteScheduled removes one row of the author, text included. A row that is
// being sent right now is a conflict: it can no longer be taken back.
func (r *RecipientStateStore) DeleteScheduled(ctx context.Context, host, home, author, id string) error {
	if host == "" || home == "" || author == "" || id == "" {
		return chat.ErrInvalidArgument
	}
	return r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `DELETE FROM chat_scheduled_post WHERE tenant_id=$1 AND home_tenant_id=$2 AND author_id=$3 AND id=$4 AND state IN ('pending','failed')`, host, home, author, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return r.issue3788Why(ctx, tx, host, home, author, id)
		}
		return nil
	})
}

// DueScheduled lists the rows of the workspace that are due: pending ones whose
// time has come, and ones whose last attempt ran out its claim.
func (r *RecipientStateStore) DueScheduled(ctx context.Context, host string, now time.Time, limit int) ([]chatrecipient.ScheduledPost, error) {
	if host == "" {
		return nil, chat.ErrInvalidArgument
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	out := []chatrecipient.ScheduledPost{}
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+issue3788Columns+` FROM chat_scheduled_post WHERE tenant_id=$1 AND send_at<=$2
            AND (state='pending' OR (state='sending' AND claim_until<=$2)) ORDER BY send_at,id LIMIT $3`, host, now, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := issue3788Scan(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// ClaimScheduled takes one due row for a delivery attempt. Only one worker can
// hold it: the claim is a conditional update, and the row it returns is the one
// that was claimed, so a row edited a moment ago is sent as edited.
func (r *RecipientStateStore) ClaimScheduled(ctx context.Context, host, id string, now time.Time, lease time.Duration) (chatrecipient.ScheduledPost, bool, error) {
	var (
		out     chatrecipient.ScheduledPost
		claimed bool
	)
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `UPDATE chat_scheduled_post SET state='sending',claim_until=$3,attempts=attempts+1,updated_at=now()
            WHERE tenant_id=$1 AND id=$2 AND send_at<=$4 AND (state='pending' OR (state='sending' AND claim_until<=$4)) RETURNING `+issue3788Columns,
			host, id, now.Add(lease), now)
		var err error
		out, err = issue3788Scan(row)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		claimed = err == nil
		return err
	})
	return out, claimed, err
}

// FinishScheduled deletes a row that was sent.
func (r *RecipientStateStore) FinishScheduled(ctx context.Context, host, id string) error {
	return r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chat_scheduled_post WHERE tenant_id=$1 AND id=$2 AND state='sending'`, host, id)
		return err
	})
}

// FailScheduled keeps a refused row for its author with the reason.
func (r *RecipientStateStore) FailScheduled(ctx context.Context, host, id, refusal string) error {
	return r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_scheduled_post SET state='failed',refusal=$3,claim_until=NULL,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND state='sending'`, host, id, refusal)
		return err
	})
}

// ReleaseScheduled puts a row back to pending after an attempt that could not
// finish, or fails it as unavailable once it has been tried maxAttempts times.
func (r *RecipientStateStore) ReleaseScheduled(ctx context.Context, host, id string, maxAttempts int) error {
	return r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_scheduled_post SET
            state=CASE WHEN attempts>=$3 THEN 'failed' ELSE 'pending' END,
            refusal=CASE WHEN attempts>=$3 THEN $4 ELSE refusal END,
            claim_until=NULL,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND state='sending'`, host, id, maxAttempts, chatrecipient.ScheduledRefusedUnavailable)
		return err
	})
}
