package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3920: the weekly quiet-hours schedule and one-off pause live in
// chat_quiet_schedule, one row per person and company, written with a revision
// check like the daily interval beside it.

var _ chatrecipient.ScheduleStore = (*RecipientStateStore)(nil)

type quietDayRow struct {
	Mode  string `json:"mode"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

func quietDaysJSON(w chatrecipient.QuietWeek) (string, error) {
	rows := make([]quietDayRow, 7)
	for i, d := range w.Days {
		rows[i] = quietDayRow{Mode: d.Mode, Start: d.StartMinute, End: d.EndMinute}
		if rows[i].Mode == "" {
			rows[i].Mode = chatrecipient.QuietDayNone
		}
	}
	b, err := json.Marshal(rows)
	return string(b), err
}

func quietDaysFromJSON(raw string) (chatrecipient.QuietWeek, error) {
	var rows []quietDayRow
	if err := json.Unmarshal([]byte(raw), &rows); err != nil || len(rows) != 7 {
		return chatrecipient.QuietWeek{}, errors.New("chatstore: a stored weekly quiet schedule is not seven days")
	}
	var w chatrecipient.QuietWeek
	for i, r := range rows {
		w.Days[i] = chatrecipient.QuietDay{Mode: r.Mode, StartMinute: r.Start, EndMinute: r.End}
	}
	return w, nil
}

// QuietSchedule returns the person's weekly schedule and pause. A person who has
// none gets an inactive week with no quiet days, at revision 1.
func (r *RecipientStateStore) QuietSchedule(ctx context.Context, home, subject string) (chatrecipient.QuietSchedule, error) {
	out := chatrecipient.QuietSchedule{Revision: 1}
	for i := range out.Week.Days {
		out.Week.Days[i] = chatrecipient.QuietDay{Mode: chatrecipient.QuietDayNone}
	}
	err := r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		var days string
		var active bool
		var kind string
		var until *time.Time
		var rev int64
		err := tx.QueryRow(ctx, `SELECT week_active,days::text,pause_kind,pause_until,revision FROM chat_quiet_schedule WHERE tenant_id=$1 AND member_id=$2`, home, subject).
			Scan(&active, &days, &kind, &until, &rev)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		week, err := quietDaysFromJSON(days)
		if err != nil {
			return err
		}
		week.Active = active
		out.Week, out.Revision = week, uint64(rev)
		if kind != "" && until != nil {
			out.Pause = chatrecipient.QuietPause{Kind: kind, Until: until.UTC()}
		}
		return nil
	})
	return out, err
}

// PutQuietSchedule stores the schedule when expected is the revision the caller
// read; any other revision is a conflict.
func (r *RecipientStateStore) PutQuietSchedule(ctx context.Context, home, subject string, s chatrecipient.QuietSchedule, expected uint64) (chatrecipient.QuietSchedule, error) {
	days, err := quietDaysJSON(s.Week)
	if err != nil {
		return chatrecipient.QuietSchedule{}, err
	}
	var until any
	if s.Pause.Kind != "" {
		until = s.Pause.Until.UTC()
	}
	out := s
	err = r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		var rev int64
		err := tx.QueryRow(ctx, `WITH updated AS (
            UPDATE chat_quiet_schedule SET week_active=$3,days=$4::jsonb,pause_kind=$5,pause_until=$6,revision=revision+1,updated_at=now()
            WHERE tenant_id=$1 AND member_id=$2 AND revision=$7 RETURNING revision
        ), inserted AS (
            INSERT INTO chat_quiet_schedule(tenant_id,member_id,week_active,days,pause_kind,pause_until,revision)
            SELECT $1,$2,$3,$4::jsonb,$5,$6,2 WHERE $7=1 AND NOT EXISTS (SELECT 1 FROM updated)
            ON CONFLICT DO NOTHING RETURNING revision
        ) SELECT revision FROM updated UNION ALL SELECT revision FROM inserted`,
			home, subject, s.Week.Active, days, s.Pause.Kind, until, expected).Scan(&rev)
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrConflict
		}
		if err != nil {
			return err
		}
		out.Revision = uint64(rev)
		return nil
	})
	return out, err
}

// quietScheduleInto fills the schedule on a QuietHours read, so every reader of
// the person's quiet hours (delivery, scheduled sends, the digest) sees the
// weekly schedule and the pause. A person with none is unchanged.
func (r *RecipientStateStore) quietScheduleInto(ctx context.Context, home, subject string, q *chatrecipient.QuietHours) error {
	s, err := r.QuietSchedule(ctx, home, subject)
	if err != nil {
		return err
	}
	q.Schedule = s
	return nil
}
