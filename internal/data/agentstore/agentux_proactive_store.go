package agentstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrAnnouncementInvalid  = errors.New("agentstore: invalid announcement")
	ErrAnnouncementNotFound = errors.New("agentstore: announcement not found")
	ErrAnnouncementRevision = errors.New("agentstore: announcement revision conflict")
)

const (
	AnnouncementActive  = "ACTIVE"
	AnnouncementPaused  = "PAUSED"
	AnnouncementDeleted = "DELETED"
	AnnouncementPosted  = "POSTED"
	AnnouncementRefused = "REFUSED"
	AnnouncementFailed  = "FAILED"
)

// Announcement is the tenant-scoped owner definition. SchedulerID points to
// the native schedule substrate; this record never calculates occurrences.
type Announcement struct {
	TenantID        uuid.UUID
	TenantKey       string
	ID              string
	InstallationID  string
	PersonaID       string
	ConversationID  string
	Instruction     string
	Documents       []agentdocref.Reference
	Cadence         string
	Weekdays        []int16
	MonthDay        int16
	LocalTime       time.Time
	Zone            string
	State           string
	OwnerID         string
	SchedulerID     string
	Revision        uint64
	NextRunAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastOccurrence  string
	LastResult      string
	LastReason      string
	LastMessageID   string
	LastAttemptedAt *time.Time
}

type AnnouncementOccurrence struct {
	TenantID, AnnouncementID, OccurrenceID string
	Result, Reason, MessageID              string
	AttemptedAt                            time.Time
}

type announcementTenantRunner interface {
	RunTenantTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
}

type AnnouncementStore struct{ runner announcementTenantRunner }

func (s *AnnouncementStore) CheckCommand(ctx context.Context, tenant uuid.UUID, owner, key, id, digest string) (bool, error) {
	if !s.validCommand(ctx, tenant, owner, key, id, digest) {
		return false, ErrAnnouncementInvalid
	}
	var storedID, storedDigest string
	err := s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT announcement_id,request_digest FROM agent_announcement_command WHERE tenant_id=$1 AND owner_id=$2 AND idempotency_key=$3`, tenant, owner, key).Scan(&storedID, &storedDigest)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if storedID != id || storedDigest != digest {
		return false, ErrAnnouncementRevision
	}
	return true, nil
}

func (s *AnnouncementStore) RecordCommand(ctx context.Context, tenant uuid.UUID, owner, key, id, digest string) error {
	if !s.validCommand(ctx, tenant, owner, key, id, digest) {
		return ErrAnnouncementInvalid
	}
	return s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO agent_announcement_command(tenant_id,owner_id,idempotency_key,announcement_id,request_digest) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, tenant, owner, key, id, digest)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrAnnouncementRevision
		}
		return nil
	})
}

func (s *AnnouncementStore) validCommand(ctx context.Context, tenant uuid.UUID, owner, key, id, digest string) bool {
	return s != nil && s.runner != nil && ctx != nil && tenant != uuid.Nil && cleanAnnouncementValue(owner, 256) && cleanAnnouncementValue(key, 128) && len(key) >= 8 && cleanAnnouncementValue(id, 128) && cleanAnnouncementValue(digest, 128) && strings.HasPrefix(digest, "sha256:") && len(digest) == 71
}

func (s *AnnouncementStore) GetOccurrence(ctx context.Context, tenant uuid.UUID, id, key string) (AnnouncementOccurrence, bool, error) {
	if s == nil || ctx == nil || tenant == uuid.Nil || !cleanAnnouncementValue(id, 128) || !cleanAnnouncementValue(key, 512) {
		return AnnouncementOccurrence{}, false, ErrAnnouncementInvalid
	}
	out := AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: key}
	err := s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT result,reason,message_id,attempted_at FROM agent_announcement_occurrence WHERE tenant_id=$1 AND announcement_id=$2 AND occurrence_id=$3`, tenant, id, key).Scan(&out.Result, &out.Reason, &out.MessageID, &out.AttemptedAt)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return AnnouncementOccurrence{}, false, nil
	}
	return out, err == nil, err
}

// WithAnnouncementFence serializes definition controls with occurrences. It
// uses the bounded fence pool, leaving the request pool free for enclosed reads.
func (s *AnnouncementStore) WithAnnouncementFence(ctx context.Context, tenant uuid.UUID, id string, fn func() error) error {
	return s.withAnnouncementLocks(ctx, tenant, id, "", fn)
}

func (s *AnnouncementStore) WithAnnouncementCommandFence(ctx context.Context, tenant uuid.UUID, id, owner, key string, fn func() error) error {
	if !cleanAnnouncementValue(owner, 256) || !cleanAnnouncementValue(key, 128) || len(key) < 8 {
		return ErrAnnouncementInvalid
	}
	return s.withAnnouncementLocks(ctx, tenant, id, owner+":"+key, fn)
}

func (s *AnnouncementStore) withAnnouncementLocks(ctx context.Context, tenant uuid.UUID, id, command string, fn func() error) error {
	if s == nil || ctx == nil || tenant == uuid.Nil || !cleanAnnouncementValue(id, 128) || fn == nil {
		return ErrAnnouncementInvalid
	}
	runner, ok := s.runner.(interface {
		RunTenantFenceTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
	})
	if !ok {
		return ErrAnnouncementInvalid
	}
	return runner.RunTenantFenceTx(ctx, tenant, func(tx dbport.Tx) error {
		if command != "" {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-announcement-command:"+tenant.String()+":"+command); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-announcement:"+tenant.String()+":"+id); err != nil {
			return err
		}
		return fn()
	})
}

func NewAnnouncementStore(runner announcementTenantRunner) (*AnnouncementStore, error) {
	if runner == nil {
		return nil, ErrAnnouncementInvalid
	}
	return &AnnouncementStore{runner: runner}, nil
}

func validateAnnouncement(a Announcement) error {
	if a.TenantID == uuid.Nil || !cleanAnnouncementValue(a.TenantKey, 256) || !cleanAnnouncementValue(a.ID, 128) || !cleanAnnouncementValue(a.InstallationID, 256) ||
		!cleanAnnouncementValue(a.PersonaID, 256) || !cleanAnnouncementValue(a.ConversationID, 256) ||
		strings.TrimSpace(a.Instruction) == "" || !utf8.ValidString(a.Instruction) || len([]rune(a.Instruction)) > 1000 ||
		!cleanAnnouncementValue(a.OwnerID, 256) || !cleanAnnouncementValue(a.SchedulerID, 256) || a.Revision == 0 ||
		a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return ErrAnnouncementInvalid
	}
	if err := agentdocref.Validate(a.Documents, agentdocref.MaxRequestReferences); err != nil || len(a.Documents) == 0 {
		return errors.Join(ErrAnnouncementInvalid, err)
	}
	if !slices.Contains([]string{"NOW", "ONCE", "DAILY", "WEEKLY", "MONTHLY"}, a.Cadence) ||
		!slices.Contains([]string{AnnouncementActive, AnnouncementPaused, AnnouncementDeleted}, a.State) || strings.TrimSpace(a.Zone) == "" {
		return ErrAnnouncementInvalid
	}
	if _, err := time.LoadLocation(a.Zone); err != nil {
		return errors.Join(ErrAnnouncementInvalid, err)
	}
	if a.Cadence == "WEEKLY" {
		if len(a.Weekdays) == 0 || len(a.Weekdays) > 7 {
			return ErrAnnouncementInvalid
		}
		seen := map[int16]bool{}
		for _, day := range a.Weekdays {
			if day < 0 || day > 6 || seen[day] {
				return ErrAnnouncementInvalid
			}
			seen[day] = true
		}
	} else if len(a.Weekdays) != 0 {
		return ErrAnnouncementInvalid
	}
	if a.Cadence == "MONTHLY" && (a.MonthDay < 1 || a.MonthDay > 31) || a.Cadence != "MONTHLY" && a.MonthDay != 0 {
		return ErrAnnouncementInvalid
	}
	return nil
}

func cleanAnnouncementValue(value string, limit int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= limit
}

func (s *AnnouncementStore) Create(ctx context.Context, a Announcement) error {
	if s == nil || s.runner == nil || ctx == nil || validateAnnouncement(a) != nil || a.Revision != 1 {
		return ErrAnnouncementInvalid
	}
	documents, _ := json.Marshal(a.Documents)
	return s.runner.RunTenantTx(ctx, a.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO agent_announcement
			(tenant_id,announcement_id,installation_id,persona_id,conversation_id,instruction,document_references,cadence,weekdays,month_day,local_time,zone,state,owner_id,scheduler_id,revision,next_run_at,created_at,updated_at,tenant_key)
			VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12,$13,$14,$15,1,$16,$17,$17,$18) ON CONFLICT DO NOTHING`,
			a.TenantID, a.ID, a.InstallationID, a.PersonaID, a.ConversationID, a.Instruction, string(documents), a.Cadence, append([]int16{}, a.Weekdays...), a.MonthDay, a.LocalTime, a.Zone, a.State, a.OwnerID, a.SchedulerID, a.NextRunAt, a.CreatedAt.UTC(), a.TenantKey)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrAnnouncementRevision
		}
		return nil
	})
}

func (s *AnnouncementStore) Save(ctx context.Context, a Announcement, expected uint64) error {
	if s == nil || ctx == nil || validateAnnouncement(a) != nil || a.Revision != expected+1 || expected == 0 {
		return ErrAnnouncementInvalid
	}
	documents, _ := json.Marshal(a.Documents)
	return s.runner.RunTenantTx(ctx, a.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE agent_announcement SET installation_id=$3,persona_id=$4,conversation_id=$5,instruction=$6,document_references=$7::jsonb,cadence=$8,weekdays=$9,month_day=$10,local_time=$11,zone=$12,state=$13,revision=$16,next_run_at=$17,updated_at=$18 WHERE tenant_id=$1 AND announcement_id=$2 AND revision=$19 AND owner_id=$14 AND scheduler_id=$15 AND tenant_key=$20`,
			a.TenantID, a.ID, a.InstallationID, a.PersonaID, a.ConversationID, a.Instruction, string(documents), a.Cadence, append([]int16{}, a.Weekdays...), a.MonthDay, a.LocalTime, a.Zone, a.State, a.OwnerID, a.SchedulerID, a.Revision, a.NextRunAt, a.UpdatedAt.UTC(), expected, a.TenantKey)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrAnnouncementRevision
		}
		return nil
	})
}

func (s *AnnouncementStore) Get(ctx context.Context, tenant uuid.UUID, id string) (Announcement, error) {
	if s == nil || ctx == nil || tenant == uuid.Nil || !cleanAnnouncementValue(id, 128) {
		return Announcement{}, ErrAnnouncementInvalid
	}
	var out Announcement
	var documents []byte
	var local time.Time
	err := s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT a.tenant_id,a.announcement_id,a.installation_id,a.persona_id,a.conversation_id,a.instruction,a.document_references,a.cadence,a.weekdays,a.month_day,a.local_time,a.zone,a.state,a.owner_id,a.scheduler_id,a.revision,a.next_run_at,a.created_at,a.updated_at,a.tenant_key,
			COALESCE(o.occurrence_id,''),COALESCE(o.result,''),COALESCE(o.reason,''),COALESCE(o.message_id,''),o.attempted_at
			FROM agent_announcement a LEFT JOIN LATERAL (SELECT * FROM agent_announcement_occurrence x WHERE x.tenant_id=a.tenant_id AND x.announcement_id=a.announcement_id ORDER BY x.attempted_at DESC,x.occurrence_id DESC LIMIT 1) o ON true
			WHERE a.tenant_id=$1 AND a.announcement_id=$2`, tenant, id).Scan(&out.TenantID, &out.ID, &out.InstallationID, &out.PersonaID, &out.ConversationID, &out.Instruction, &documents, &out.Cadence, &out.Weekdays, &out.MonthDay, &local, &out.Zone, &out.State, &out.OwnerID, &out.SchedulerID, &out.Revision, &out.NextRunAt, &out.CreatedAt, &out.UpdatedAt, &out.TenantKey, &out.LastOccurrence, &out.LastResult, &out.LastReason, &out.LastMessageID, &out.LastAttemptedAt)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrAnnouncementNotFound
		}
		return err
	})
	if err != nil {
		return Announcement{}, err
	}
	if err := json.Unmarshal(documents, &out.Documents); err != nil {
		return Announcement{}, fmt.Errorf("decode announcement documents: %w", err)
	}
	out.LocalTime = local
	if err := validateAnnouncement(out); err != nil {
		return Announcement{}, err
	}
	return out, nil
}

func (s *AnnouncementStore) ListOwner(ctx context.Context, tenant uuid.UUID, owner string) ([]Announcement, error) {
	if s == nil || ctx == nil || tenant == uuid.Nil || !cleanAnnouncementValue(owner, 256) {
		return nil, ErrAnnouncementInvalid
	}
	var ids []string
	err := s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT announcement_id FROM agent_announcement WHERE tenant_id=$1 AND owner_id=$2 AND state<>'DELETED' ORDER BY created_at,announcement_id`, tenant, owner)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	out := make([]Announcement, 0, len(ids))
	for _, id := range ids {
		record, err := s.Get(ctx, tenant, id)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

// RecordOccurrence is append-only and idempotent. A replay with a different
// result is a conflict; the first public message remains the only message.
func (s *AnnouncementStore) RecordOccurrence(ctx context.Context, occurrence AnnouncementOccurrence) (bool, error) {
	tenant, err := uuid.Parse(occurrence.TenantID)
	if err != nil || s == nil || ctx == nil || !cleanAnnouncementValue(occurrence.AnnouncementID, 128) || !cleanAnnouncementValue(occurrence.OccurrenceID, 512) ||
		!slices.Contains([]string{AnnouncementPosted, AnnouncementRefused, AnnouncementFailed}, occurrence.Result) || occurrence.AttemptedAt.IsZero() || len(occurrence.Reason) > 500 {
		return false, ErrAnnouncementInvalid
	}
	inserted := false
	err = s.runner.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO agent_announcement_occurrence(tenant_id,announcement_id,occurrence_id,result,reason,message_id,attempted_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, tenant, occurrence.AnnouncementID, occurrence.OccurrenceID, occurrence.Result, occurrence.Reason, occurrence.MessageID, occurrence.AttemptedAt.UTC())
		if err != nil {
			return err
		}
		inserted = n == 1
		if inserted {
			return nil
		}
		var result, reason, message string
		if err := tx.QueryRow(ctx, `SELECT result,reason,message_id FROM agent_announcement_occurrence WHERE tenant_id=$1 AND announcement_id=$2 AND occurrence_id=$3`, tenant, occurrence.AnnouncementID, occurrence.OccurrenceID).Scan(&result, &reason, &message); err != nil {
			return err
		}
		if result != occurrence.Result || reason != occurrence.Reason || message != occurrence.MessageID {
			return ErrAnnouncementRevision
		}
		return nil
	})
	return inserted, err
}
