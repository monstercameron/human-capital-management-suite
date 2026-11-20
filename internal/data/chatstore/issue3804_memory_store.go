package chatstore

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var _ chat.MemoryStore = (*Store)(nil)

// Issue 3804. What the agents of a channel remember. The source message is
// read from chat_post in the same statement as the item, so a deleted or
// edited message retires its memory at the instant it changes, with no job in
// between that could be late.

const memorySourceSQL = `CASE WHEN p.id IS NULL OR p.tombstoned THEN 'REMOVED' WHEN p.revision <> m.source_revision THEN 'EDITED' ELSE 'LIVE' END`

const memoryColumns = `m.id,m.tenant_id,m.conversation_id,m.memory_text,m.source_post_id,m.source_revision,m.source_sequence,m.proposer_home_tenant_id,m.proposer_id,m.audience,m.classification,m.ttl_days,m.status,m.created_at,m.approved_by_home_tenant_id,m.approved_by_id,m.approved_at,m.expires_at,m.forgotten_at,` + memorySourceSQL

const memoryFrom = ` FROM chat_channel_memory m LEFT JOIN chat_post p ON p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id AND p.id=m.source_post_id`

func memoryScan(row interface{ Scan(...any) error }) (chat.ChannelMemory, error) {
	var m chat.ChannelMemory
	var status, source string
	var revision, sequence int64
	var approved, expires, forgotten *time.Time
	err := row.Scan(&m.ID, &m.TenantID, &m.ConversationID, &m.Text, &m.SourcePostID, &revision, &sequence, &m.ProposerHomeTenantID, &m.ProposerID, &m.Audience, &m.Classification,
		&m.TTLDays, &status, &m.CreatedAt, &m.ApprovedByHomeTenantID, &m.ApprovedByID, &approved, &expires, &forgotten, &source)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ChannelMemory{}, chat.ErrNotFound
	}
	if err != nil {
		return chat.ChannelMemory{}, err
	}
	m.SourceRevision, m.SourceSequence = uint64(revision), uint64(sequence)
	m.Status, m.Source = chat.MemoryStatus(status), chat.MemorySource(source)
	if approved != nil {
		m.ApprovedAt = *approved
	}
	if expires != nil {
		m.ExpiresAt = *expires
	}
	if forgotten != nil {
		m.ForgottenAt = *forgotten
	}
	return m, nil
}

func getMemoryTx(ctx context.Context, tx dbport.Tx, tenant, conversation, id string, lock bool) (chat.ChannelMemory, error) {
	query := `SELECT ` + memoryColumns + memoryFrom + ` WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.id=$3`
	if lock {
		query += ` FOR UPDATE OF m`
	}
	return memoryScan(tx.QueryRow(ctx, query, tenant, conversation, id))
}

// InsertMemory stores a proposal once per live source message.
func (s *Store) InsertMemory(ctx context.Context, m chat.ChannelMemory) (chat.ChannelMemory, error) {
	if m.ID == "" || m.TenantID == "" || m.ConversationID == "" || m.SourcePostID == "" || m.SourceRevision == 0 || m.ProposerID == "" || m.ProposerHomeTenantID == "" || m.Text == "" {
		return chat.ChannelMemory{}, chat.ErrInvalidArgument
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if m.TTLDays == 0 {
		m.TTLDays = chat.MemoryDefaultTTLDays
	}
	var stored chat.ChannelMemory
	err := s.RunTenantTx(ctx, m.TenantID, func(tx dbport.Tx) error {
		existing, err := memoryScan(tx.QueryRow(ctx, `SELECT `+memoryColumns+memoryFrom+` WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.source_post_id=$3 AND m.status<>'FORGOTTEN'`, m.TenantID, m.ConversationID, m.SourcePostID))
		if err == nil && existing.Source == chat.MemorySourceLive && existing.SourceRevision == m.SourceRevision {
			stored = existing
			return nil
		}
		if err == nil {
			// The message was changed since that item was made, so the item is
			// retired. Clear it and its words so the new wording starts afresh.
			if _, err = tx.Exec(ctx, `UPDATE chat_channel_memory SET status='FORGOTTEN', memory_text='', forgotten_by_home_tenant_id=$4, forgotten_by_id=$5, forgotten_at=$6 WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 AND status<>'FORGOTTEN'`,
				m.TenantID, m.ConversationID, existing.ID, m.ProposerHomeTenantID, m.ProposerID, m.CreatedAt.UTC()); err != nil {
				return err
			}
		} else if !errors.Is(err, chat.ErrNotFound) {
			return err
		}
		// The source must be a message of this conversation, standing, at the
		// revision the words were taken from.
		var live bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 AND NOT tombstoned AND revision=$4)`, m.TenantID, m.ConversationID, m.SourcePostID, int64(m.SourceRevision)).Scan(&live); err != nil {
			return err
		}
		if !live {
			return chat.ErrNotFound
		}
		if _, err = tx.Exec(ctx, `INSERT INTO chat_channel_memory(tenant_id,id,conversation_id,memory_text,source_post_id,source_revision,source_sequence,proposer_home_tenant_id,proposer_id,audience,classification,ttl_days,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'PROPOSED',$13)`,
			m.TenantID, m.ID, m.ConversationID, m.Text, m.SourcePostID, int64(m.SourceRevision), int64(m.SourceSequence), m.ProposerHomeTenantID, m.ProposerID, m.Audience, m.Classification, m.TTLDays, m.CreatedAt); err != nil {
			return err
		}
		stored, err = getMemoryTx(ctx, tx, m.TenantID, m.ConversationID, m.ID, false)
		return err
	})
	return stored, err
}

// GetMemory reads one item of a conversation.
func (s *Store) GetMemory(ctx context.Context, tenant, conversation, id string) (chat.ChannelMemory, error) {
	var m chat.ChannelMemory
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		m, err = getMemoryTx(ctx, tx, tenant, conversation, id, false)
		return err
	})
	return m, err
}

// ListMemory returns a conversation's items that are not forgotten, newest first.
func (s *Store) ListMemory(ctx context.Context, tenant, conversation string, limit int) ([]chat.ChannelMemory, error) {
	if limit <= 0 || limit > chat.MemoryListLimit {
		limit = chat.MemoryListLimit
	}
	out := []chat.ChannelMemory{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+memoryColumns+memoryFrom+` WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.status<>'FORGOTTEN' ORDER BY m.created_at DESC, m.id DESC LIMIT $3`, tenant, conversation, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := memoryScan(rows)
			if err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

// ApproveMemory puts a proposed item to work for the days it was proposed with.
func (s *Store) ApproveMemory(ctx context.Context, tenant, conversation, id, homeTenant, approver string, at time.Time) (chat.ChannelMemory, error) {
	if tenant == "" || conversation == "" || id == "" || approver == "" || homeTenant == "" {
		return chat.ChannelMemory{}, chat.ErrInvalidArgument
	}
	var out chat.ChannelMemory
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		item, err := getMemoryTx(ctx, tx, tenant, conversation, id, true)
		if err != nil {
			return err
		}
		if item.Status != chat.MemoryProposed || item.Source != chat.MemorySourceLive {
			return chat.ErrConflict
		}
		expires := at.UTC().AddDate(0, 0, item.TTLDays)
		if _, err = tx.Exec(ctx, `UPDATE chat_channel_memory SET status='APPROVED', approved_by_home_tenant_id=$4, approved_by_id=$5, approved_at=$6, expires_at=$7 WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 AND status='PROPOSED'`,
			tenant, conversation, id, homeTenant, approver, at.UTC(), expires); err != nil {
			return err
		}
		out, err = getMemoryTx(ctx, tx, tenant, conversation, id, false)
		return err
	})
	return out, err
}

// ForgetMemory ends an item and erases its words.
func (s *Store) ForgetMemory(ctx context.Context, tenant, conversation, id, homeTenant, actor string, at time.Time) error {
	if tenant == "" || conversation == "" || id == "" || actor == "" || homeTenant == "" {
		return chat.ErrInvalidArgument
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE chat_channel_memory SET status='FORGOTTEN', memory_text='', forgotten_by_home_tenant_id=$4, forgotten_by_id=$5, forgotten_at=$6 WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 AND status<>'FORGOTTEN'`,
			tenant, conversation, id, homeTenant, actor, at.UTC())
		if err != nil {
			return err
		}
		if tag == 0 {
			return chat.ErrNotFound
		}
		return nil
	})
}

// ServingMemory returns the items an agent may read: approved, not expired,
// with the source message standing exactly as proposed.
func (s *Store) ServingMemory(ctx context.Context, tenant, conversation string, at time.Time, limit int) ([]chat.ChannelMemory, error) {
	if limit <= 0 || limit > chat.MemoryServeLimit {
		limit = chat.MemoryServeLimit
	}
	out := []chat.ChannelMemory{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+memoryColumns+memoryFrom+` WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.status='APPROVED' AND m.expires_at>$3 AND p.id IS NOT NULL AND NOT p.tombstoned AND p.revision=m.source_revision ORDER BY m.approved_at DESC, m.id DESC LIMIT $4`,
			tenant, conversation, at.UTC(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := memoryScan(rows)
			if err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}
