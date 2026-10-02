package agentstore

import (
	"context"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrSupportInboxInvalid  = errors.New("agentstore: invalid support inbox receipt")
	ErrSupportInboxNotFound = errors.New("agentstore: support inbox receipt not found")
	ErrSupportInboxReplay   = errors.New("agentstore: support inbox receipt replay conflict")
)

// SupportInboxMessage is an immutable receipt. Subject and body live only in
// the governed object named by ContentRef. The sender is always unverified.
type SupportInboxMessage struct {
	TenantID                                uuid.UUID
	MessageID, ContentRef, ContentDigest    string
	ClaimedSenderName, ClaimedSenderAddress string
	ReceivedAt                              time.Time
}

type SupportInboxStore struct{ runner announcementTenantRunner }

func NewSupportInboxStore(runner announcementTenantRunner) (*SupportInboxStore, error) {
	if runner == nil {
		return nil, ErrSupportInboxInvalid
	}
	return &SupportInboxStore{runner: runner}, nil
}

func agentuxDemoClean(value string, max int) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsAny(value, "\r\n\x00")
}

func validSupportInboxMessage(r SupportInboxMessage) bool {
	digest, err := hex.DecodeString(strings.TrimPrefix(r.ContentDigest, "sha256:"))
	return r.TenantID != uuid.Nil && agentuxDemoClean(r.MessageID, 128) && agentuxDemoClean(r.ContentRef, 512) && strings.HasPrefix(r.ContentDigest, "sha256:") && r.ContentDigest == strings.ToLower(r.ContentDigest) && err == nil && len(digest) == 32 && agentuxDemoClean(r.ClaimedSenderName, 200) && agentuxDemoClean(r.ClaimedSenderAddress, 320) && !r.ReceivedAt.IsZero()
}

// Receive verifies replay bytes, rather than treating any collision as success.
func (s *SupportInboxStore) Receive(ctx context.Context, r SupportInboxMessage) (bool, error) {
	if s == nil || s.runner == nil || ctx == nil || !validSupportInboxMessage(r) {
		return false, ErrSupportInboxInvalid
	}
	r.ReceivedAt = r.ReceivedAt.UTC().Truncate(time.Microsecond)
	inserted := false
	err := s.runner.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO support_inbox_message(tenant_id,message_id,content_ref,content_digest,claimed_sender_name,claimed_sender_address,received_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, r.TenantID, r.MessageID, r.ContentRef, r.ContentDigest, r.ClaimedSenderName, r.ClaimedSenderAddress, r.ReceivedAt)
		if err != nil {
			return err
		}
		if n == 1 {
			inserted = true
			return nil
		}
		got, err := agentuxDemoReadInbox(ctx, tx, r.TenantID, r.MessageID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(got, r) {
			return ErrSupportInboxReplay
		}
		return nil
	})
	return inserted && err == nil, err
}

func (s *SupportInboxStore) Get(ctx context.Context, tenant uuid.UUID, id string) (SupportInboxMessage, error) {
	if s == nil || s.runner == nil || ctx == nil || tenant == uuid.Nil || !agentuxDemoClean(id, 128) {
		return SupportInboxMessage{}, ErrSupportInboxInvalid
	}
	var result SupportInboxMessage
	err := s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		result, err = agentuxDemoReadInbox(ctx, tx, tenant, id)
		return err
	})
	return result, err
}

func agentuxDemoReadInbox(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, id string) (SupportInboxMessage, error) {
	r := SupportInboxMessage{TenantID: tenant, MessageID: id}
	err := tx.QueryRow(ctx, `SELECT content_ref,content_digest,claimed_sender_name,claimed_sender_address,received_at FROM support_inbox_message WHERE tenant_id=$1 AND message_id=$2`, tenant, id).Scan(&r.ContentRef, &r.ContentDigest, &r.ClaimedSenderName, &r.ClaimedSenderAddress, &r.ReceivedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return SupportInboxMessage{}, ErrSupportInboxNotFound
	}
	r.ReceivedAt = r.ReceivedAt.UTC()
	return r, err
}

type BirthdayPreference struct {
	Share    bool
	Revision uint64
}

// BirthdayPreference reads only a worker's opt-out choice. Default sharing is
// supplied by the server's demo-pack policy; production callers pass false.
func (s *SupportInboxStore) BirthdayPreference(ctx context.Context, tenant uuid.UUID, worker string, demoDefault bool) (BirthdayPreference, error) {
	if s == nil || s.runner == nil || ctx == nil || tenant == uuid.Nil || !agentuxDemoClean(worker, 256) {
		return BirthdayPreference{}, ErrSupportInboxInvalid
	}
	r := BirthdayPreference{Share: demoDefault}
	err := s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT share_birthday,revision FROM agent_profile_birthday_preference WHERE tenant_id=$1 AND worker_key=$2`, tenant, worker).Scan(&r.Share, &r.Revision)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		return err
	})
	return r, err
}

// SaveBirthdayPreference is a storage port. Its application caller must bind
// worker to the authenticated subject before calling it.
func (s *SupportInboxStore) SaveBirthdayPreference(ctx context.Context, tenant uuid.UUID, worker string, p BirthdayPreference, now time.Time) (BirthdayPreference, error) {
	if s == nil || s.runner == nil || ctx == nil || tenant == uuid.Nil || !agentuxDemoClean(worker, 256) || now.IsZero() {
		return BirthdayPreference{}, ErrSupportInboxInvalid
	}
	err := s.agentuxDemoFenceTx(ctx, tenant, func(tx dbport.Tx) error {
		var n int64
		var err error
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "birthday-preferences:"+tenant.String()); err != nil {
			return err
		}
		if p.Revision == 0 {
			n, err = tx.Exec(ctx, `INSERT INTO agent_profile_birthday_preference(tenant_id,worker_key,share_birthday,revision,updated_at) VALUES($1,$2,$3,1,$4) ON CONFLICT DO NOTHING`, tenant, worker, p.Share, now)
		} else {
			n, err = tx.Exec(ctx, `UPDATE agent_profile_birthday_preference SET share_birthday=$3,revision=revision+1,updated_at=$4 WHERE tenant_id=$1 AND worker_key=$2 AND revision=$5`, tenant, worker, p.Share, now, p.Revision)
		}
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrSupportInboxReplay
		}
		return nil
	})
	if err == nil {
		p.Revision++
	}
	return p, err
}

func (s *SupportInboxStore) WithBirthdayPreferenceFence(ctx context.Context, tenant uuid.UUID, fn func() error) error {
	if s == nil || s.runner == nil || ctx == nil || tenant == uuid.Nil || fn == nil {
		return ErrSupportInboxInvalid
	}
	return s.agentuxDemoFenceTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "birthday-preferences:"+tenant.String()); err != nil {
			return err
		}
		return fn()
	})
}
