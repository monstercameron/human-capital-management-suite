package chatstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3815. A post that asks its readers to acknowledge it, and each
// person's acknowledgement. Both are written once and kept as evidence: the
// tables refuse updates, and nothing here deletes. A read is never an
// acknowledgement; only RecordAck writes one. The caller decides who may see
// what: the store only scopes by tenant.

// AckBatch bounds one status read.
const AckBatch = 100

// AckListLimit bounds the acknowledgements of one post read at once.
const AckListLimit = 5000

// AckRequirement marks a post as needing acknowledgement.
type AckRequirement struct {
	TenantID, ConversationID, PostID string
	RequiredByID                     string
	PostRevision                     uint64
	RequiredAt                       time.Time
}

// Ack is one person's acknowledgement of a post.
type Ack struct {
	TenantID, ConversationID, PostID string
	SubjectID                        string
	PostRevision                     uint64
	AcknowledgedAt                   time.Time
}

// RequireAck marks a post. It reports false, and changes nothing, when the
// post already requires acknowledgement.
func (s *Store) RequireAck(ctx context.Context, r AckRequirement) (bool, error) {
	if r.TenantID == "" || r.ConversationID == "" || r.PostID == "" || strings.TrimSpace(r.RequiredByID) == "" {
		return false, chat.ErrInvalidArgument
	}
	if r.RequiredAt.IsZero() {
		r.RequiredAt = time.Now().UTC()
	}
	created := false
	err := s.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO chat_ack_requirement(tenant_id,conversation_id,post_id,required_by_id,post_revision,required_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (tenant_id,conversation_id,post_id) DO NOTHING`,
			r.TenantID, r.ConversationID, r.PostID, r.RequiredByID, int64(r.PostRevision), r.RequiredAt)
		if err != nil {
			return err
		}
		created = n == 1
		return nil
	})
	return created, err
}

// AckRequirements returns the requirement of each given post that has one.
func (s *Store) AckRequirements(ctx context.Context, tenant, conversation string, posts []string) (map[string]AckRequirement, error) {
	out := map[string]AckRequirement{}
	if len(posts) == 0 {
		return out, nil
	}
	if len(posts) > AckBatch {
		return nil, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT post_id,required_by_id,post_revision,required_at FROM chat_ack_requirement WHERE tenant_id=$1 AND conversation_id=$2 AND post_id = ANY($3)`, tenant, conversation, posts)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r := AckRequirement{TenantID: tenant, ConversationID: conversation}
			var rev int64
			if err := rows.Scan(&r.PostID, &r.RequiredByID, &rev, &r.RequiredAt); err != nil {
				return err
			}
			r.PostRevision = uint64(rev)
			out[r.PostID] = r
		}
		return rows.Err()
	})
	return out, err
}

// RecordAck writes one person's acknowledgement. It reports false, and changes
// nothing, when that person already acknowledged the post, and chat.ErrNotFound
// when the post does not require acknowledgement.
func (s *Store) RecordAck(ctx context.Context, a Ack) (bool, error) {
	if a.TenantID == "" || a.ConversationID == "" || a.PostID == "" || strings.TrimSpace(a.SubjectID) == "" {
		return false, chat.ErrInvalidArgument
	}
	if a.AcknowledgedAt.IsZero() {
		a.AcknowledgedAt = time.Now().UTC()
	}
	created := false
	err := s.RunTenantTx(ctx, a.TenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT 1 FROM chat_ack_requirement WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3`, a.TenantID, a.ConversationID, a.PostID)
		if err != nil {
			return err
		}
		has := rows.Next()
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if !has {
			return chat.ErrNotFound
		}
		n, err := tx.Exec(ctx, `INSERT INTO chat_ack_record(tenant_id,conversation_id,post_id,subject_id,post_revision,acknowledged_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (tenant_id,conversation_id,post_id,subject_id) DO NOTHING`,
			a.TenantID, a.ConversationID, a.PostID, a.SubjectID, int64(a.PostRevision), a.AcknowledgedAt)
		if err != nil {
			return err
		}
		created = n == 1
		return nil
	})
	return created, err
}

// AckCounts says how many people acknowledged each of the given posts.
func (s *Store) AckCounts(ctx context.Context, tenant, conversation string, posts []string) (map[string]int, error) {
	out := map[string]int{}
	if len(posts) == 0 {
		return out, nil
	}
	if len(posts) > AckBatch {
		return nil, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT post_id,count(*) FROM chat_ack_record WHERE tenant_id=$1 AND conversation_id=$2 AND post_id = ANY($3) GROUP BY post_id`, tenant, conversation, posts)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var n int64
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			out[id] = int(n)
		}
		return rows.Err()
	})
	return out, err
}

// MyAcks says when one person acknowledged each of the given posts.
func (s *Store) MyAcks(ctx context.Context, tenant, conversation string, posts []string, subject string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	if len(posts) == 0 {
		return out, nil
	}
	if len(posts) > AckBatch {
		return nil, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT post_id,acknowledged_at FROM chat_ack_record WHERE tenant_id=$1 AND conversation_id=$2 AND subject_id=$3 AND post_id = ANY($4)`, tenant, conversation, subject, posts)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var at time.Time
			if err := rows.Scan(&id, &at); err != nil {
				return err
			}
			out[id] = at
		}
		return rows.Err()
	})
	return out, err
}

// ListAcks returns every acknowledgement of one post, oldest first.
func (s *Store) ListAcks(ctx context.Context, tenant, conversation, post string) ([]Ack, error) {
	var out []Ack
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT subject_id,post_revision,acknowledged_at FROM chat_ack_record WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3 ORDER BY acknowledged_at, subject_id LIMIT $4`, tenant, conversation, post, AckListLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a := Ack{TenantID: tenant, ConversationID: conversation, PostID: post}
			var rev int64
			if err := rows.Scan(&a.SubjectID, &rev, &a.AcknowledgedAt); err != nil {
				return err
			}
			a.PostRevision = uint64(rev)
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}
