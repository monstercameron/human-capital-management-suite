package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrWritingStyleLimit means the person has used every writing-style operation
// the day allows.
var ErrWritingStyleLimit = errors.New("chatstore: writing style daily limit reached")

// ErrWritingStyleInvalid means a setting or usage line is malformed.
var ErrWritingStyleInvalid = errors.New("chatstore: writing style record is invalid")

// WritingStyleSetting is a workspace's saved choice about the message-box
// writing-style controls. Styles is the registry document as the service
// validated it; this package neither reads nor widens it.
type WritingStyleSetting struct {
	Found    bool
	Enabled  bool
	Styles   json.RawMessage
	Revision int64
}

// LoadWritingStyleSetting returns the workspace's saved setting; Found is false
// when the workspace has never saved one.
func (s *Store) LoadWritingStyleSetting(ctx context.Context, tenantID string) (WritingStyleSetting, error) {
	var out WritingStyleSetting
	if strings.TrimSpace(tenantID) == "" {
		return out, ErrWritingStyleInvalid
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT enabled, styles, revision FROM chattone_workspace_setting WHERE tenant_id=$1`, tenantID).Scan(&out.Enabled, &raw, &out.Revision)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out.Found, out.Styles = true, json.RawMessage(raw)
		return nil
	})
	return out, err
}

// SaveWritingStyleSetting writes the workspace's choice and returns its new
// revision. The database moves the revision forward on every change.
func (s *Store) SaveWritingStyleSetting(ctx context.Context, tenantID, updatedBy string, enabled bool, styles json.RawMessage) (int64, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(updatedBy) == "" || !json.Valid(styles) {
		return 0, ErrWritingStyleInvalid
	}
	var revision int64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO chattone_workspace_setting (tenant_id, enabled, styles, updated_by) VALUES ($1,$2,$3::jsonb,$4)
			ON CONFLICT (tenant_id) DO UPDATE SET enabled=EXCLUDED.enabled, styles=EXCLUDED.styles, updated_by=EXCLUDED.updated_by
			RETURNING revision`, tenantID, enabled, string(styles), updatedBy).Scan(&revision)
	})
	return revision, err
}

func writingStyleDay(at time.Time) string { return at.UTC().Format("2006-01-02") }

// ReserveWritingStyleUse counts one operation against the person's day and
// refuses with ErrWritingStyleLimit when the day's limit is already reached.
// The count and the line are written under one lock, so concurrent presses
// cannot both take the last place.
func (s *Store) ReserveWritingStyleUse(ctx context.Context, tenantID, personID string, at time.Time, limit int) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(personID) == "" || limit <= 0 {
		return ErrWritingStyleLimit
	}
	day := writingStyleDay(at)
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || chr(31) || $2 || chr(31) || $3, 32))`, tenantID, personID, day); err != nil {
			return err
		}
		var used int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chattone_usage WHERE tenant_id=$1 AND person_id=$2 AND day=$3::date AND operation='reserve'`, tenantID, personID, day).Scan(&used); err != nil {
			return err
		}
		if used >= limit {
			return ErrWritingStyleLimit
		}
		_, err := tx.Exec(ctx, `INSERT INTO chattone_usage (tenant_id, person_id, day, operation, occurred_at) VALUES ($1,$2,$3::date,'reserve',$4)`, tenantID, personID, day, at.UTC())
		return err
	})
}

// RecordWritingStyleUse appends the outcome of one operation. It carries
// identifiers and an outcome only, never text.
func (s *Store) RecordWritingStyleUse(ctx context.Context, tenantID, personID string, at time.Time, operation string, attempt int, succeeded bool) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(personID) == "" || (operation != "rewrite" && operation != "meaning") || attempt < 0 || attempt > 2 {
		return ErrWritingStyleInvalid
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chattone_usage (tenant_id, person_id, day, operation, attempt, succeeded, occurred_at) VALUES ($1,$2,$3::date,$4,$5,$6,$7)`,
			tenantID, personID, writingStyleDay(at), operation, attempt, succeeded, at.UTC())
		return err
	})
}

// WritingStyleUsageToday reports how many places the person has reserved today.
func (s *Store) WritingStyleUsageToday(ctx context.Context, tenantID, personID string, at time.Time) (int, error) {
	var used int
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chattone_usage WHERE tenant_id=$1 AND person_id=$2 AND day=$3::date AND operation='reserve'`, tenantID, personID, writingStyleDay(at)).Scan(&used)
	})
	return used, err
}
