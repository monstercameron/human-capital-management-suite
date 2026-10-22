package chatstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ErrChatlangReport means a report resolution is malformed or names no report.
var ErrChatlangReport = errors.New("chatstore: translation report is invalid")

// ChatlangPairStat is what was measured for one language pair over a period:
// counts and durations only, never text.
type ChatlangPairStat struct {
	Source        string `json:"source"`
	Target        string `json:"target"`
	Translations  int64  `json:"translations"`
	Failures      int64  `json:"failures"`
	LowConfidence int64  `json:"low_confidence"`
	// LatencyMsTotal is the sum over every attempt (a translation or a failure), so
	// the mean is LatencyMsTotal over Translations plus Failures.
	LatencyMsTotal int64 `json:"latency_ms_total"`
	LatencyMsMax   int64 `json:"latency_ms_max"`
	// Reports is how many "this translation is wrong" reports readers made about
	// the pair in the period.
	Reports int64 `json:"reports"`
}

// RecordChatlangPairAttempt adds one translation attempt to the day's row for
// its language pair. A failed attempt counts as a failure, a successful one as a
// translation, and a successful one the engine was unsure of also as low
// confidence.
func (s *Store) RecordChatlangPairAttempt(ctx context.Context, tenant, source, target string, failed, lowConfidence bool, latency time.Duration, at time.Time) error {
	source, target = chatrender.Language(source), chatrender.Language(target)
	if strings.TrimSpace(tenant) == "" || source == "" || target == "" || latency < 0 {
		return chatrender.ErrInvalid
	}
	ms := latency.Milliseconds()
	var translations, failures, low int64
	switch {
	case failed:
		failures = 1
	case lowConfidence:
		translations, low = 1, 1
	default:
		translations = 1
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chatlang_pair_stat (tenant_id,day,source_language,target_language,translations,failures,low_confidence,latency_ms_total,latency_ms_max)
			VALUES ($1,$2::date,$3,$4,$5,$6,$7,$8,$8)
			ON CONFLICT (tenant_id,day,source_language,target_language) DO UPDATE SET
			 translations=chatlang_pair_stat.translations+EXCLUDED.translations,
			 failures=chatlang_pair_stat.failures+EXCLUDED.failures,
			 low_confidence=chatlang_pair_stat.low_confidence+EXCLUDED.low_confidence,
			 latency_ms_total=chatlang_pair_stat.latency_ms_total+EXCLUDED.latency_ms_total,
			 latency_ms_max=GREATEST(chatlang_pair_stat.latency_ms_max,EXCLUDED.latency_ms_max)`,
			tenant, at.UTC().Format("2006-01-02"), source, target, translations, failures, low, ms)
		return err
	})
}

// ChatlangPairStats sums each language pair's rows from the given day on and
// adds the reports made about the pair in the same period, busiest pair first.
func (s *Store) ChatlangPairStats(ctx context.Context, tenant string, since time.Time) ([]ChatlangPairStat, error) {
	if strings.TrimSpace(tenant) == "" {
		return nil, chatrender.ErrInvalid
	}
	byPair := map[[2]string]*ChatlangPairStat{}
	var order [][2]string
	get := func(source, target string) *ChatlangPairStat {
		key := [2]string{source, target}
		if row, ok := byPair[key]; ok {
			return row
		}
		row := &ChatlangPairStat{Source: source, Target: target}
		byPair[key] = row
		order = append(order, key)
		return row
	}
	day := since.UTC().Format("2006-01-02")
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT source_language,target_language,sum(translations),sum(failures),sum(low_confidence),sum(latency_ms_total),max(latency_ms_max) FROM chatlang_pair_stat WHERE tenant_id=$1 AND day>=$2::date GROUP BY source_language,target_language`, tenant, day)
		if err != nil {
			return err
		}
		for rows.Next() {
			var source, target string
			var row ChatlangPairStat
			if err := rows.Scan(&source, &target, &row.Translations, &row.Failures, &row.LowConfidence, &row.LatencyMsTotal, &row.LatencyMsMax); err != nil {
				rows.Close()
				return err
			}
			*get(source, target) = ChatlangPairStat{Source: source, Target: target, Translations: row.Translations, Failures: row.Failures, LowConfidence: row.LowConfidence, LatencyMsTotal: row.LatencyMsTotal, LatencyMsMax: row.LatencyMsMax}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		reports, err := tx.Query(ctx, `SELECT COALESCE(rr.rendering->>'source_language',''),r.language,count(*) FROM chatrender_report r JOIN chatrender_rendering rr ON rr.tenant_id=r.tenant_id AND rr.post_id=r.post_id AND rr.revision=r.revision AND rr.tone=r.tone AND rr.language=r.language WHERE r.tenant_id=$1 AND r.created_at>=$2::date GROUP BY 1,2`, tenant, day)
		if err != nil {
			return err
		}
		defer reports.Close()
		for reports.Next() {
			var source, target string
			var n int64
			if err := reports.Scan(&source, &target, &n); err != nil {
				return err
			}
			get(chatrender.Language(source), target).Reports = n
		}
		return reports.Err()
	})
	if err != nil {
		return nil, err
	}
	out := make([]ChatlangPairStat, 0, len(order))
	for _, key := range order {
		out = append(out, *byPair[key])
	}
	sortChatlangPairs(out)
	return out, nil
}

func sortChatlangPairs(rows []ChatlangPairStat) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			a, b := rows[j-1], rows[j]
			ca, cb := a.Translations+a.Failures, b.Translations+b.Failures
			if ca > cb || (ca == cb && a.Source+a.Target <= b.Source+b.Target) {
				break
			}
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
}

// ChatlangReport is one reader's "this translation is wrong", with what an
// administrator did about it. It carries the pair, the reader's suggestion and
// the message's identifiers; the message text is not part of it.
type ChatlangReport struct {
	ID           string    `json:"id"`
	Conversation string    `json:"conversation"`
	Message      string    `json:"message"`
	Revision     uint64    `json:"revision"`
	Source       string    `json:"source"`
	Target       string    `json:"target"`
	Reporter     string    `json:"reporter"`
	Suggestion   string    `json:"suggestion"`
	At           time.Time `json:"at"`
	// State is "open", "resolved" or "dismissed".
	State      string    `json:"state"`
	Note       string    `json:"note,omitempty"`
	ResolvedBy string    `json:"resolved_by,omitempty"`
	ResolvedAt time.Time `json:"resolved_at,omitempty"`
}

// ChatlangReports lists the workspace's reports, open ones first and newest
// first within each, at most limit of them.
func (s *Store) ChatlangReports(ctx context.Context, tenant string, limit int) ([]ChatlangReport, error) {
	if strings.TrimSpace(tenant) == "" || limit <= 0 || limit > 500 {
		return nil, ErrChatlangReport
	}
	out := []ChatlangReport{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT r.report_id,p.conversation_id,r.post_id,r.revision,COALESCE(rr.rendering->>'source_language',''),r.language,r.reporter_id,r.reason,r.created_at,COALESCE(x.state,'open'),COALESCE(x.note,''),COALESCE(x.resolved_by,''),x.resolved_at
			FROM chatrender_report r
			JOIN chat_post p ON p.tenant_id=r.tenant_id AND p.id=r.post_id
			LEFT JOIN chatrender_rendering rr ON rr.tenant_id=r.tenant_id AND rr.post_id=r.post_id AND rr.revision=r.revision AND rr.tone=r.tone AND rr.language=r.language
			LEFT JOIN chatlang_report_resolution x ON x.tenant_id=r.tenant_id AND x.report_id=r.report_id
			WHERE r.tenant_id=$1 ORDER BY (x.state IS NULL) DESC, r.created_at DESC LIMIT $2`, tenant, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row ChatlangReport
			var resolvedAt *time.Time
			if err := rows.Scan(&row.ID, &row.Conversation, &row.Message, &row.Revision, &row.Source, &row.Target, &row.Reporter, &row.Suggestion, &row.At, &row.State, &row.Note, &row.ResolvedBy, &resolvedAt); err != nil {
				return err
			}
			if resolvedAt != nil {
				row.ResolvedAt = *resolvedAt
			}
			row.Source = chatrender.Language(row.Source)
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}

// ResolveChatlangReport records that an administrator resolved or dismissed a
// report. It names the report it closes and fails when there is no such report.
func (s *Store) ResolveChatlangReport(ctx context.Context, tenant, reportID, state, note, by string) error {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(reportID) == "" || strings.TrimSpace(by) == "" || (state != "resolved" && state != "dismissed") || len(note) > 1000 {
		return ErrChatlangReport
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO chatlang_report_resolution (tenant_id,report_id,state,note,resolved_by)
			SELECT $1,$2,$3,$4,$5 WHERE EXISTS (SELECT 1 FROM chatrender_report WHERE tenant_id=$1 AND report_id=$2)
			ON CONFLICT (tenant_id,report_id) DO UPDATE SET state=EXCLUDED.state,note=EXCLUDED.note,resolved_by=EXCLUDED.resolved_by,resolved_at=now()`, tenant, reportID, state, note, by)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrChatlangReport
		}
		return nil
	})
}

// SaveRewordExemption records that the writer sent one revision of a message
// "as written". It is the writer's alone to make; the caller checks that.
func (s *Store) SaveRewordExemption(ctx context.Context, tenant, post string, revision uint64, person string) error {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(post) == "" || revision == 0 || strings.TrimSpace(person) == "" {
		return chatrender.ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chattone_reword_exemption (tenant_id,post_id,revision,person_id) VALUES ($1,$2,$3,$4) ON CONFLICT (tenant_id,post_id,revision) DO NOTHING`, tenant, post, revision, person)
		return err
	})
}

// RewordExempt reports whether the writer sent this revision "as written".
func (s *Store) RewordExempt(ctx context.Context, tenant, post string, revision uint64) (bool, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(post) == "" {
		return false, chatrender.ErrInvalid
	}
	var found bool
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chattone_reword_exemption WHERE tenant_id=$1 AND post_id=$2 AND revision=$3)`, tenant, post, revision).Scan(&found)
	})
	return found, err
}

// ChatlangGlossaryEditors names, for each target language that has terms, the
// person who added the newest one. A term that is kept as written belongs to no
// language and is not listed.
func (s *Store) ChatlangGlossaryEditors(ctx context.Context, tenant string) (map[string]string, error) {
	if strings.TrimSpace(tenant) == "" {
		return nil, chatrender.ErrInvalid
	}
	out := map[string]string{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (target_language) target_language,created_by FROM chatlang_glossary WHERE tenant_id=$1 AND target_language<>'' ORDER BY target_language,created_at DESC,term_id DESC`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var language, person string
			if err := rows.Scan(&language, &person); err != nil {
				return err
			}
			out[language] = person
		}
		return rows.Err()
	})
	return out, err
}
