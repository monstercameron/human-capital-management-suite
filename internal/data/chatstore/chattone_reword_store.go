package chatstore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrRewordInvalid means a reword setting or a reader's choice is malformed.
var ErrRewordInvalid = errors.New("chatstore: reword record is invalid")

// Reword modes are the administrator's "Reword heated messages".
const (
	RewordModeOff     = "off"
	RewordModeOffered = "offered"
	RewordModeOn      = "on"
)

// RewordSetting is one administrator's choice for a workspace (Channel empty)
// or for one channel, which overrides the workspace.
type RewordSetting struct {
	Tenant, Channel        string
	Mode                   string
	MembersMayViewOriginal bool
	Revision               int64
	UpdatedBy              string
}

func validRewordMode(mode string) bool {
	return mode == RewordModeOff || mode == RewordModeOffered || mode == RewordModeOn
}

// LoadRewordSettings returns every setting the workspace has saved: its own and
// its channel overrides. A workspace with none has rewording off and members
// may view the original.
func (s *Store) LoadRewordSettings(ctx context.Context, tenantID string) ([]RewordSetting, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrRewordInvalid
	}
	var out []RewordSetting
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT channel_id, mode, members_may_view_original, revision, updated_by FROM chattone_reword_setting WHERE tenant_id=$1 ORDER BY channel_id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			row := RewordSetting{Tenant: tenantID}
			if err := rows.Scan(&row.Channel, &row.Mode, &row.MembersMayViewOriginal, &row.Revision, &row.UpdatedBy); err != nil {
				return err
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}

// SaveRewordSetting writes one setting and returns its new revision; the
// database moves the revision forward on every change.
func (s *Store) SaveRewordSetting(ctx context.Context, in RewordSetting) (int64, error) {
	if strings.TrimSpace(in.Tenant) == "" || strings.TrimSpace(in.UpdatedBy) == "" || !validRewordMode(in.Mode) || len(in.Channel) > 200 || strings.TrimSpace(in.Channel) != in.Channel {
		return 0, ErrRewordInvalid
	}
	var revision int64
	err := s.RunTenantTx(ctx, in.Tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO chattone_reword_setting (tenant_id, channel_id, mode, members_may_view_original, updated_by) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (tenant_id, channel_id) DO UPDATE SET mode=EXCLUDED.mode, members_may_view_original=EXCLUDED.members_may_view_original, updated_by=EXCLUDED.updated_by
			RETURNING revision`, in.Tenant, in.Channel, in.Mode, in.MembersMayViewOriginal, in.UpdatedBy).Scan(&revision)
	})
	return revision, err
}

// DeleteRewordOverride returns a channel to the workspace's choice. The
// workspace's own row is never removed this way: an empty channel is refused.
func (s *Store) DeleteRewordOverride(ctx context.Context, tenantID, channelID string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(channelID) == "" {
		return ErrRewordInvalid
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chattone_reword_setting WHERE tenant_id=$1 AND channel_id=$2`, tenantID, channelID)
		return err
	})
}

// ReaderChoice is a person's choice for heated messages: "as-written" or
// "reworded", for every conversation (Channel empty) or one.
type ReaderChoice struct {
	Channel, Tone string
}

func validReaderTone(tone string) bool { return tone == "as-written" || tone == "reworded" }

// LoadReaderChoices returns the person's own choices, general and per channel.
func (s *Store) LoadReaderChoices(ctx context.Context, tenantID, personID string) ([]ReaderChoice, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(personID) == "" {
		return nil, ErrRewordInvalid
	}
	var out []ReaderChoice
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT channel_id, tone FROM chattone_reader_choice WHERE tenant_id=$1 AND person_id=$2 ORDER BY channel_id`, tenantID, personID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row ReaderChoice
			if err := rows.Scan(&row.Channel, &row.Tone); err != nil {
				return err
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}

// SaveReaderChoice records the person's choice for one conversation or, with an
// empty channel, for all of them.
func (s *Store) SaveReaderChoice(ctx context.Context, tenantID, personID, channelID, tone string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(personID) == "" || !validReaderTone(tone) || len(channelID) > 200 || strings.TrimSpace(channelID) != channelID {
		return ErrRewordInvalid
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chattone_reader_choice (tenant_id, person_id, channel_id, tone) VALUES ($1,$2,$3,$4)
			ON CONFLICT (tenant_id, person_id, channel_id) DO UPDATE SET tone=EXCLUDED.tone, updated_at=now()`, tenantID, personID, channelID, tone)
		return err
	})
}

// DeleteReaderChoice returns one conversation to the person's general choice.
func (s *Store) DeleteReaderChoice(ctx context.Context, tenantID, personID, channelID string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(personID) == "" || strings.TrimSpace(channelID) == "" {
		return ErrRewordInvalid
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM chattone_reader_choice WHERE tenant_id=$1 AND person_id=$2 AND channel_id=$3`, tenantID, personID, channelID)
		return err
	})
}
