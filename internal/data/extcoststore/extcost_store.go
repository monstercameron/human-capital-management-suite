// Package extcoststore makes external-call accounting durable in PostgreSQL.
package extcoststore

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/extcost"
	"math"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"
)

// Migrations is an independent store migration set.
//
//go:embed migrations/*.sql
var Migrations embed.FS

type Backend interface {
	dbport.Conn
	dbport.Beginner
}
type Store struct {
	db Backend
	mu sync.Mutex
}

var _ extcost.Journal = (*Store)(nil)

func New(db Backend) (*Store, error) {
	if db == nil {
		return nil, extcost.ErrInvalid
	}
	return &Store{db: db}, nil
}
func (s *Store) transaction(ctx context.Context, tenant string, write bool, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil || ctx == nil || strings.TrimSpace(tenant) == "" {
		return extcost.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant); err != nil {
		return err
	}
	if write {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "extcost:"+tenant); err != nil {
			return err
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func reservation(ctx context.Context, tx dbport.Tx, tenant, key string) (extcost.Reservation, string, error) {
	var raw []byte
	var state string
	err := tx.QueryRow(ctx, `SELECT reservation,state FROM extcost_reservation WHERE tenant_id=$1 AND attempt_key=$2`, tenant, key).Scan(&raw, &state)
	var r extcost.Reservation
	if err == nil {
		err = json.Unmarshal(raw, &r)
	}
	r.State = state
	return r, state, err
}
func applies(b extcost.Budget, c extcost.Call) bool {
	for _, s := range extcost.Scopes(c) {
		if s.Kind == b.Scope.Kind && s.ID == b.Scope.ID {
			return true
		}
	}
	return false
}
func warning(b extcost.BudgetStatus) bool {
	used := new(big.Int).Add(big.NewInt(b.SpentMicros), big.NewInt(b.ReservedMicros))
	used.Mul(used, big.NewInt(10000))
	limit := new(big.Int).Mul(big.NewInt(b.Budget.LimitMicros), big.NewInt(b.Budget.WarningBasisPoints))
	return used.Cmp(limit) >= 0
}
func add(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, extcost.ErrOverflow
	}
	return a + b, nil
}
func allLines(ctx context.Context, tx dbport.Tx, tenant string) ([]extcost.Line, error) {
	rows, err := tx.Query(ctx, `SELECT line FROM extcost_usage WHERE tenant_id=$1 ORDER BY occurred_at DESC,attempt_key`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []extcost.Line
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var l extcost.Line
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func pending(ctx context.Context, tx dbport.Tx, tenant string) ([]extcost.Reservation, error) {
	rows, err := tx.Query(ctx, `SELECT reservation,state FROM extcost_reservation WHERE tenant_id=$1 AND state IN ('reserved','sent') ORDER BY attempt_key`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []extcost.Reservation
	for rows.Next() {
		var raw []byte
		var state string
		if err := rows.Scan(&raw, &state); err != nil {
			return nil, err
		}
		var r extcost.Reservation
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		r.State = state
		out = append(out, r)
	}
	return out, rows.Err()
}
func scopeClauses(scope extcost.Scope) (string, string, error) {
	switch scope.Kind {
	case "tenant":
		return "tenant_id=$3", "tenant_id=$3", nil
	case "feature":
		return "feature=$3", "reservation #>> '{Call,feature}'=$3", nil
	case "agent":
		return "agent=$3", "reservation #>> '{Call,attribution,agent}'=$3", nil
	case "workflow":
		return "workflow=$3", "reservation #>> '{Call,attribution,workflow_definition}'=$3", nil
	case "run":
		return "(line #>> '{call,attribution,agent_run}'=$3 OR line #>> '{call,attribution,workflow_run}'=$3)", "(reservation #>> '{Call,attribution,agent_run}'=$3 OR reservation #>> '{Call,attribution,workflow_run}'=$3)", nil
	case "step":
		return "(line #>> '{call,attribution,agent_run}') || ':' || (line #>> '{call,attribution,step}')=$3", "(reservation #>> '{Call,attribution,agent_run}') || ':' || (reservation #>> '{Call,attribution,step}')=$3", nil
	case "node":
		return "(line #>> '{call,attribution,workflow_run}') || ':' || (line #>> '{call,attribution,node}')=$3", "(reservation #>> '{Call,attribution,workflow_run}') || ':' || (reservation #>> '{Call,attribution,node}')=$3", nil
	}
	return "", "", extcost.ErrInvalid
}
func readBudgets(ctx context.Context, tx dbport.Tx, tenant string, at time.Time) ([]extcost.BudgetStatus, error) {
	rows, err := tx.Query(ctx, `SELECT kind,scope_id,period,currency,limit_micros,warning_basis_points FROM extcost_budget WHERE tenant_id=$1 ORDER BY kind,scope_id,period,currency`, tenant)
	if err != nil {
		return nil, err
	}
	var out []extcost.BudgetStatus
	for rows.Next() {
		var b extcost.BudgetStatus
		b.Budget.Tenant = tenant
		if err := rows.Scan(&b.Budget.Scope.Kind, &b.Budget.Scope.ID, &b.Budget.Scope.Period, &b.Budget.Currency, &b.Budget.LimitMicros, &b.Budget.WarningBasisPoints); err != nil {
			rows.Close()
			return nil, err
		}
		b.Start = extcost.PeriodStart(at, b.Budget.Scope.Period)
		out = append(out, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		b := &out[i]
		until := b.Start.AddDate(0, 0, 1)
		if b.Budget.Scope.Period == "month" {
			until = b.Start.AddDate(0, 1, 0)
		}
		if b.Budget.Scope.Period == "run" {
			until = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		usage, open, err := scopeClauses(b.Budget.Scope)
		if err != nil {
			return nil, err
		}
		query := `SELECT
  (SELECT COALESCE(SUM(cost_micros),0)::bigint FROM extcost_usage WHERE tenant_id=$1 AND currency=$2 AND ` + usage + ` AND budget_at>=$4 AND budget_at<$5),
  (SELECT COALESCE(SUM(maximum_micros),0)::bigint FROM extcost_reservation WHERE tenant_id=$1 AND currency=$2 AND ` + open + ` AND state IN ('reserved','sent') AND (reservation->>'At')::timestamptz>=$4 AND (reservation->>'At')::timestamptz<$5)`
		if err := tx.QueryRow(ctx, query, tenant, b.Budget.Currency, b.Budget.Scope.ID, b.Start, until).Scan(&b.SpentMicros, &b.ReservedMicros); err != nil {
			return nil, err
		}
		b.Warning = warning(*b)
	}
	return out, nil
}

func (s *Store) Reserve(ctx context.Context, want extcost.Reservation) (extcost.Reservation, error) {
	if !extcost.ValidCall(want.Call) || want.MaximumMicros < 0 || want.Fingerprint == "" || want.At.IsZero() || want.ScheduleVersion == "" || len(want.Currency) != 3 {
		return extcost.Reservation{}, extcost.ErrInvalid
	}
	result := want
	result.State = "reserved"
	err := s.transaction(ctx, want.Call.Tenant, true, func(tx dbport.Tx) error {
		prior, state, err := reservation(ctx, tx, want.Call.Tenant, want.Call.Key)
		if err == nil {
			if prior.Fingerprint != want.Fingerprint {
				return extcost.ErrConflict
			}
			if state == "settled" {
				var raw []byte
				if err := tx.QueryRow(ctx, `SELECT line FROM extcost_usage WHERE tenant_id=$1 AND attempt_key=$2`, want.Call.Tenant, want.Call.Key).Scan(&raw); err != nil {
					return err
				}
				var line extcost.Line
				if err := json.Unmarshal(raw, &line); err != nil {
					return err
				}
				result = prior
				result.Replay = &line
				return nil
			}
			if state != "released" {
				return extcost.ErrPending
			}
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		var frozen bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM extcost_usage u WHERE u.tenant_id=$1 AND u.currency=$2 AND u.line->>'finding'='reserved_maximum_exceeded' AND u.budget_at>=$3 AND NOT EXISTS(SELECT 1 FROM extcost_budget_audit a WHERE a.tenant_id=u.tenant_id AND a.occurred_at>u.occurred_at))`, want.Call.Tenant, want.Currency, extcost.PeriodStart(want.At, "month")).Scan(&frozen); err != nil {
			return err
		}
		if frozen {
			return extcost.ErrOverrun
		}
		budgets, err := readBudgets(ctx, tx, want.Call.Tenant, want.At)
		if err != nil {
			return err
		}
		tenant, feature := false, false
		for _, b := range budgets {
			if !applies(b.Budget, want.Call) {
				continue
			}
			if b.Budget.Currency != want.Currency {
				return extcost.ErrBudgetMissing
			}
			if b.Budget.Scope.Kind == "tenant" && b.Budget.Scope.Period == "month" {
				tenant = true
			}
			if b.Budget.Scope.Kind == "feature" && b.Budget.Scope.Period == "month" {
				feature = true
			}
			if want.MaximumMicros > b.Budget.LimitMicros || b.SpentMicros > b.Budget.LimitMicros-want.MaximumMicros || b.ReservedMicros > b.Budget.LimitMicros-want.MaximumMicros-b.SpentMicros {
				return extcost.ErrBudget
			}
			b.ReservedMicros += want.MaximumMicros
			b.Warning = warning(b)
			result.Budgets = append(result.Budgets, b)
		}
		if want.MaximumMicros > 0 && (!tenant || !feature) {
			return extcost.ErrBudgetMissing
		}
		raw, _ := json.Marshal(result)
		_, err = tx.Exec(ctx, `INSERT INTO extcost_reservation (tenant_id,attempt_key,fingerprint,maximum_micros,currency,state,reservation) VALUES ($1,$2,$3,$4,$5,'reserved',$6::jsonb) ON CONFLICT (tenant_id,attempt_key) DO UPDATE SET state='reserved',reservation=EXCLUDED.reservation`, want.Call.Tenant, want.Call.Key, want.Fingerprint, want.MaximumMicros, want.Currency, raw)
		return err
	})
	return result, err
}
func (s *Store) MarkSent(ctx context.Context, r extcost.Reservation) error {
	return s.transition(ctx, r, "reserved", "sent")
}
func (s *Store) Release(ctx context.Context, r extcost.Reservation) error {
	return s.transition(ctx, r, "reserved", "released")
}
func (s *Store) transition(ctx context.Context, r extcost.Reservation, from, to string) error {
	return s.transaction(ctx, r.Call.Tenant, true, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE extcost_reservation SET state=$4 WHERE tenant_id=$1 AND attempt_key=$2 AND fingerprint=$3 AND state=$5`, r.Call.Tenant, r.Call.Key, r.Fingerprint, to, from)
		if err != nil {
			return err
		}
		if n != 1 {
			return extcost.ErrPending
		}
		return nil
	})
}
func insertLine(ctx context.Context, tx dbport.Tx, l extcost.Line) error {
	raw, _ := json.Marshal(l)
	c := l.Call
	_, err := tx.Exec(ctx, `INSERT INTO extcost_usage (tenant_id,attempt_key,cause_id,feature,provider,operation,actor,agent,workflow,currency,cost_micros,occurred_at,budget_at,line) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb)`, c.Tenant, c.Key, c.Cause, c.Feature, c.Provider, c.Operation, c.Attribution.Actor, c.Attribution.Agent, c.Attribution.WorkflowDefinition, l.Currency, l.CostMicros, l.At, l.BudgetAt, raw)
	return err
}
func (s *Store) Settle(ctx context.Context, r extcost.Reservation, l extcost.Line) error {
	if !extcost.ValidCall(l.Call) || !extcost.ValidUnits(l.Measurement.Units) || l.CostMicros < 0 || l.CorrectionOf != "" || l.At.IsZero() {
		return extcost.ErrInvalid
	}
	overrun := false
	err := s.transaction(ctx, r.Call.Tenant, true, func(tx dbport.Tx) error {
		stored, state, err := reservation(ctx, tx, r.Call.Tenant, r.Call.Key)
		if err != nil {
			return err
		}
		a, _ := json.Marshal(l.Call)
		b, _ := json.Marshal(stored.Call)
		if stored.Fingerprint != r.Fingerprint || string(a) != string(b) || l.Currency != stored.Currency || l.ScheduleVersion != stored.ScheduleVersion || l.ScheduleDigest != stored.ScheduleDigest || !l.BudgetAt.Equal(stored.At) {
			return extcost.ErrConflict
		}
		if state == "settled" {
			var raw []byte
			if err := tx.QueryRow(ctx, `SELECT line FROM extcost_usage WHERE tenant_id=$1 AND attempt_key=$2`, r.Call.Tenant, r.Call.Key).Scan(&raw); err != nil {
				return err
			}
			var prior extcost.Line
			if err := json.Unmarshal(raw, &prior); err != nil {
				return err
			}
			prior.At = l.At
			old, _ := json.Marshal(prior)
			now, _ := json.Marshal(l)
			if string(old) != string(now) {
				return extcost.ErrConflict
			}
			return nil
		}
		if state != "sent" {
			return extcost.ErrPending
		}
		overrun = l.CostMicros > stored.MaximumMicros
		if err := insertLine(ctx, tx, l); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE extcost_reservation SET state='settled' WHERE tenant_id=$1 AND attempt_key=$2`, r.Call.Tenant, r.Call.Key)
		return err
	})
	if err == nil && overrun {
		return extcost.ErrOverrun
	}
	return err
}
func (s *Store) ChangeBudget(ctx context.Context, c extcost.BudgetChange) error {
	b := c.Budget
	known := false
	for _, kind := range []string{"tenant", "feature", "agent", "workflow", "run", "node", "step"} {
		if b.Scope.Kind == kind {
			known = true
		}
	}
	if !known || b.Tenant == "" || b.Scope.ID == "" || (b.Scope.Kind == "tenant" && b.Scope.ID != b.Tenant) || (b.Scope.Period != "month" && b.Scope.Period != "day" && b.Scope.Period != "run") || len(b.Currency) != 3 || b.LimitMicros < 0 || b.WarningBasisPoints < 1 || b.WarningBasisPoints > 10000 || c.Actor == "" || strings.TrimSpace(c.Reason) == "" || c.At.IsZero() {
		return extcost.ErrInvalid
	}
	return s.transaction(ctx, b.Tenant, true, func(tx dbport.Tx) error {
		statuses, err := readBudgets(ctx, tx, b.Tenant, c.At)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			if status.Budget.Scope == b.Scope && status.Budget.Currency == b.Currency && (status.SpentMicros > b.LimitMicros || status.ReservedMicros > b.LimitMicros-status.SpentMicros) {
				return extcost.ErrBudget
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO extcost_budget (tenant_id,kind,scope_id,period,currency,limit_micros,warning_basis_points) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (tenant_id,kind,scope_id,period,currency) DO UPDATE SET limit_micros=EXCLUDED.limit_micros,warning_basis_points=EXCLUDED.warning_basis_points`, b.Tenant, b.Scope.Kind, b.Scope.ID, b.Scope.Period, b.Currency, b.LimitMicros, b.WarningBasisPoints); err != nil {
			return err
		}
		raw, _ := json.Marshal(c)
		_, err = tx.Exec(ctx, `INSERT INTO extcost_budget_audit (tenant_id,actor,reason,occurred_at,change) VALUES ($1,$2,$3,$4,$5::jsonb)`, b.Tenant, c.Actor, c.Reason, c.At, raw)
		return err
	})
}
func (s *Store) Pending(ctx context.Context, tenant string) ([]extcost.Reservation, error) {
	var out []extcost.Reservation
	err := s.transaction(ctx, tenant, false, func(tx dbport.Tx) error { var err error; out, err = pending(ctx, tx, tenant); return err })
	return out, err
}
func (s *Store) SaveSchedule(ctx context.Context, tenant string, schedule extcost.Schedule) error {
	if _, err := extcost.NewCatalog([]extcost.Schedule{schedule}); err != nil {
		return err
	}
	return s.transaction(ctx, tenant, true, func(tx dbport.Tx) error {
		raw, _ := json.Marshal(schedule)
		digest := extcost.ScheduleDigest(schedule)
		var old string
		err := tx.QueryRow(ctx, `SELECT digest FROM extcost_price_schedule WHERE tenant_id=$1 AND provider=$2 AND operation=$3 AND model=$4 AND model_version=$5 AND version=$6`, tenant, schedule.Provider, schedule.Operation, schedule.Model, schedule.ModelVersion, schedule.Version).Scan(&old)
		if err == nil {
			if old != digest {
				return extcost.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO extcost_price_schedule (tenant_id,provider,operation,model,model_version,version,digest,effective_from,schedule) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)`, tenant, schedule.Provider, schedule.Operation, schedule.Model, schedule.ModelVersion, schedule.Version, digest, schedule.EffectiveFrom, raw)
		return err
	})
}
func (s *Store) LoadCatalog(ctx context.Context, tenant string) (*extcost.Catalog, error) {
	var schedules []extcost.Schedule
	err := s.transaction(ctx, tenant, false, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT schedule FROM extcost_price_schedule WHERE tenant_id=$1 ORDER BY effective_from,version`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var schedule extcost.Schedule
			if err := json.Unmarshal(raw, &schedule); err != nil {
				return err
			}
			schedules = append(schedules, schedule)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return extcost.NewCatalog(schedules)
}
func (s *Store) Reconcile(ctx context.Context, r extcost.Reconciliation, correction *extcost.Line) error {
	if r.Tenant == "" || r.ReportID == "" || r.Day.IsZero() || r.At.IsZero() {
		return extcost.ErrInvalid
	}
	return s.transaction(ctx, r.Tenant, true, func(tx dbport.Tx) error {
		var prior []byte
		err := tx.QueryRow(ctx, `SELECT report FROM extcost_reconciliation WHERE tenant_id=$1 AND report_id=$2`, r.Tenant, r.ReportID).Scan(&prior)
		if err == nil {
			var old extcost.Reconciliation
			if err := json.Unmarshal(prior, &old); err != nil {
				return err
			}
			old.At = r.At
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(r)
			if string(a) != string(b) {
				return extcost.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		var measured int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(cost_micros),0)::bigint FROM extcost_usage WHERE tenant_id=$1 AND provider=$2 AND operation=$3 AND currency=$4 AND occurred_at>=$5 AND occurred_at<$6`, r.Tenant, r.Provider, r.Operation, r.Currency, r.Day, r.Day.AddDate(0, 0, 1)).Scan(&measured); err != nil {
			return err
		}
		if measured != r.LedgerMicros {
			return extcost.ErrConflict
		}
		if correction != nil {
			if correction.Call.Tenant != r.Tenant || correction.Currency != r.Currency || correction.CostMicros != r.DifferenceMicros || correction.CorrectionOf == "" || !extcost.ValidCall(correction.Call) {
				return extcost.ErrInvalid
			}
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM extcost_usage WHERE tenant_id=$1 AND attempt_key=$2 AND provider=$3 AND operation=$4 AND currency=$5)`, r.Tenant, correction.CorrectionOf, r.Provider, r.Operation, r.Currency).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return extcost.ErrInvalid
			}
			if err := insertLine(ctx, tx, *correction); err != nil {
				return err
			}
		}
		raw, _ := json.Marshal(r)
		_, err = tx.Exec(ctx, `INSERT INTO extcost_reconciliation (tenant_id,report_id,occurred_at,report) VALUES ($1,$2,$3,$4::jsonb)`, r.Tenant, r.ReportID, r.At, raw)
		return err
	})
}
func matches(l extcost.Line, f extcost.Filter) bool {
	c := l.Call
	for _, pair := range [][2]string{{f.Provider, c.Provider}, {f.Operation, c.Operation}, {f.Feature, c.Feature}, {f.Purpose, c.Purpose}, {f.Agent, c.Attribution.Agent}, {f.Workflow, c.Attribution.WorkflowDefinition}, {f.Person, c.Attribution.Actor}, {f.Cause, c.Cause}, {f.Key, c.Key}} {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
	}
	if !f.From.IsZero() && l.At.Before(f.From) || !f.Until.IsZero() && !l.At.Before(f.Until) {
		return false
	}
	return f.Search == "" || strings.Contains(strings.ToLower(strings.Join([]string{c.Feature, c.Purpose, c.Provider, c.Operation, c.Attribution.Actor, c.Attribution.Agent, c.Attribution.WorkflowDefinition, c.Cause}, " ")), strings.ToLower(f.Search))
}
func (s *Store) Read(ctx context.Context, tenant string, f extcost.Filter) (extcost.Report, error) {
	var out extcost.Report
	if !f.From.IsZero() && !f.Until.IsZero() && !f.Until.After(f.From) || f.Limit < 0 || f.Limit > 10000 {
		return out, extcost.ErrInvalid
	}
	if f.Limit == 0 {
		f.Limit = 500
	}
	err := s.transaction(ctx, tenant, false, func(tx dbport.Tx) error {
		lines, err := allLines(ctx, tx, tenant)
		if err != nil {
			return err
		}
		for _, l := range lines {
			if matches(l, f) {
				out.Lines = append(out.Lines, l)
			}
		}
		out.Groups, err = Groups(out.Lines)
		if err != nil {
			return err
		}
		out.Statistics, err = Statistics(out.Lines)
		if err != nil {
			return err
		}
		open, err := pending(ctx, tx, tenant)
		if err != nil {
			return err
		}
		for _, r := range open {
			if matches(extcost.Line{Call: r.Call, At: r.At}, f) {
				out.Pending = append(out.Pending, r)
			}
		}
		if len(out.Lines) > f.Limit {
			out.Truncated = true
			out.Lines = out.Lines[:f.Limit]
		}
		at := f.From
		if at.IsZero() {
			at = time.Now().UTC()
		}
		out.Budgets, err = readBudgets(ctx, tx, tenant, at)
		if err != nil {
			return err
		}
		if f.Feature != "" || f.Agent != "" || f.Workflow != "" || f.Person != "" {
			var visible []extcost.BudgetStatus
			for _, b := range out.Budgets {
				if b.Budget.Scope.Kind == "feature" && b.Budget.Scope.ID == f.Feature || b.Budget.Scope.Kind == "agent" && b.Budget.Scope.ID == f.Agent || b.Budget.Scope.Kind == "workflow" && b.Budget.Scope.ID == f.Workflow {
					visible = append(visible, b)
				}
			}
			out.Budgets = visible
		}
		scoped := f.Feature != "" || f.Agent != "" || f.Workflow != "" || f.Person != ""
		out.ReconciledAt = map[string]time.Time{}
		rows, err := tx.Query(ctx, `SELECT report FROM extcost_reconciliation WHERE tenant_id=$1 ORDER BY occurred_at DESC`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var r extcost.Reconciliation
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			for _, l := range out.Lines {
				if l.Call.Provider == r.Provider && l.Call.Operation == r.Operation && l.Currency == r.Currency && r.Day.Equal(extcost.PeriodStart(l.At, "day")) && r.At.After(out.ReconciledAt[l.Call.Key]) {
					out.ReconciledAt[l.Call.Key] = r.At
				}
			}
			// A scoped reader gets the visible call's status, never provider-wide totals.
			if !scoped && (f.Provider == "" || f.Provider == r.Provider) && (f.Operation == "" || f.Operation == r.Operation) {
				out.Reconciliations = append(out.Reconciliations, r)
			}
		}
		return rows.Err()
	})
	return out, err
}
func Groups(lines []extcost.Line) ([]extcost.Group, error) {
	groups := map[string]extcost.Group{}
	for _, l := range lines {
		c := l.Call
		for _, item := range [][2]string{{"total", ""}, {"day", l.At.UTC().Format("2006-01-02")}, {"feature", c.Feature}, {"provider", c.Provider}, {"purpose", c.Purpose}, {"agent", c.Attribution.Agent}, {"workflow", c.Attribution.WorkflowDefinition}, {"person", c.Attribution.Actor}, {"cause", c.Cause}, {"agent_run", c.Attribution.AgentRun}, {"workflow_run", c.Attribution.WorkflowRun}, {"step", c.Attribution.AgentRun + ":" + c.Attribution.Step}, {"node", c.Attribution.WorkflowRun + ":" + c.Attribution.Node}} {
			if item[0] != "total" && (item[1] == "" || item[1] == ":" || strings.HasSuffix(item[1], ":")) {
				continue
			}
			key := strings.Join([]string{item[0], item[1], l.Currency}, "\x00")
			g := groups[key]
			g.Kind, g.Name, g.Currency = item[0], item[1], l.Currency
			var err error
			g.CostMicros, err = add(g.CostMicros, l.CostMicros)
			if err != nil {
				return nil, err
			}
			if l.CorrectionOf == "" {
				g.Calls++
			}
			if l.Measurement.Estimated {
				g.Estimated++
			}
			groups[key] = g
		}
	}
	out := make([]extcost.Group, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Currency != out[j].Currency {
			return out[i].Currency < out[j].Currency
		}
		if out[i].CostMicros != out[j].CostMicros {
			return out[i].CostMicros > out[j].CostMicros
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Statistics counts all attempts under a workflow run/node before computing
// nearest-rank median and p95, preventing retry inflation of run counts.
func Statistics(lines []extcost.Line) ([]extcost.Statistic, error) {
	perRun := map[[4]string]int64{}
	for _, l := range lines {
		a := l.Call.Attribution
		if a.WorkflowDefinition == "" || a.WorkflowRun == "" {
			continue
		}
		nodes := []string{""}
		if a.Node != "" {
			nodes = append(nodes, a.Node)
		}
		for _, node := range nodes {
			key := [4]string{a.WorkflowDefinition, node, l.Currency, a.WorkflowRun}
			n, err := add(perRun[key], l.CostMicros)
			if err != nil {
				return nil, err
			}
			perRun[key] = n
		}
	}
	samples := map[[3]string][]int64{}
	for k, n := range perRun {
		key := [3]string{k[0], k[1], k[2]}
		samples[key] = append(samples[key], n)
	}
	var out []extcost.Statistic
	for key, numbers := range samples {
		sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
		count := len(numbers)
		out = append(out, extcost.Statistic{Workflow: key[0], Node: key[1], Currency: key[2], Runs: int64(count), MedianMicros: numbers[(count-1)/2], P95Micros: numbers[(95*count+99)/100-1]})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Workflow+"\x00"+a.Node+"\x00"+a.Currency < b.Workflow+"\x00"+b.Node+"\x00"+b.Currency
	})
	return out, nil
}
