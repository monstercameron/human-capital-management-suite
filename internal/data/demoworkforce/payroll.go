package demoworkforce

// The HarborCare demo tenant's recent payroll history.
//
// migrations/00046 defines payroll_run, payroll_frozen_population and
// payroll_population_amendment, and internal/data/payrollstore persists them,
// but before this file no non-test code ever called that store: the package
// was reachable from nothing a serving binary links (REV-027-01). The demo
// seed is that caller. It writes through payrollstore's own transactional
// entry points (SaveRunTx, SavePopulationTx, AppendAmendmentTx) inside the
// seed's transaction, so the store's revision-chain checks, digests and
// refusals are the ones that decide what lands.
//
// The history is derived from the workforce plan, never asserted:
//
//   - a run exists for each of the last completed monthly pay periods of the
//     salaried pay group, and walks the real lifecycle DRAFT -> CALCULATED ->
//     RELEASED -> SETTLED, one immutable revision per state;
//   - the frozen population of a run is exactly the workers whose pay basis
//     puts them in that pay group and whose hire date precedes the period, so
//     somebody who had not started yet is simply absent;
//   - a worker hired inside the period is never a full-period member. The
//     schema records membership, not proration, so the honest record of a
//     mid-period hire is the explicit LATE_ENTRY amendment the run's
//     late-entry policy calls for, which is what this seed writes.
//
// The per-worker rows the schema supports are the frozen population's
// members: worker, employment and pay group, resolved from the same
// journey_worker row every other projection is built from.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/payrollstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DemoPayGroupRef names the one pay group the demo tenant runs: United
// States salaried employees, paid monthly.
const DemoPayGroupRef = "harborcare-us-salaried"

// DemoPayBasis is the compensation basis that places a worker in
// [DemoPayGroupRef]. A worker paid on any other basis is not in this pay
// group and therefore not in its runs.
const DemoPayBasis = "ANNUAL_SALARY"

// PayPeriodSpec is one closed monthly pay period, half-open [Start, End).
type PayPeriodSpec struct {
	ID    string
	Start time.Time
	End   time.Time
}

// DemoPayPeriods are the last three completed monthly periods before the
// demo's recorded instant. They are fixed dates, not derived from a clock, so
// the seeded history is the same on every machine and every replay.
var DemoPayPeriods = []PayPeriodSpec{
	{
		ID:    "2026-06",
		Start: time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
	},
	{
		ID:    "2026-07",
		Start: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
	},
	{
		ID:    "2026-08",
		Start: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
	},
}

// PlannedPayrollRun is one pay period's complete recorded run.
type PlannedPayrollRun struct {
	Period PayPeriodSpec
	// Revisions is the immutable lifecycle chain, revision one first.
	Revisions []payroll.PayrollRun
	// Population is the membership frozen against revision one.
	Population payroll.FrozenPopulation
	// LateEntries are the mid-period hires the run admits explicitly rather
	// than as full-period members.
	LateEntries []payroll.PopulationAmendment
}

// PayrollPlan is the demo tenant's whole recorded payroll history.
type PayrollPlan struct {
	Runs []PlannedPayrollRun
}

// PayrollSummary counts what one [SeedPayroll] call wrote.
type PayrollSummary struct {
	Runs         int
	RunRevisions int
	Populations  int
	Members      int
	LateEntries  int
	SkippedRuns  int
}

// payrollDigest is [demoDigest] in the form payroll's own tables store: the
// content_digest column holds bare hex, and payrollstore reads the column
// straight back into the domain value for every digest except the canonical
// one, so a prefixed digest would not survive the round trip.
func payrollDigest(kind, key string) string {
	return storedDigest(demoDigest(kind, key))
}

// PayrollRunID is the deterministic run id of one pay period.
func PayrollRunID(period PayPeriodSpec) string {
	return "harborcare-payroll-" + period.ID
}

// PlanPayroll derives the demo tenant's payroll history from its workforce.
func PlanPayroll(tenant uuid.UUID) (PayrollPlan, error) {
	employees, err := Plan(tenant)
	if err != nil {
		return PayrollPlan{}, err
	}
	rows := make([]workforce.WorkerRow, 0, len(employees))
	for _, employee := range employees {
		rows = append(rows, employee.Row)
	}
	return PlanPayrollFor(rows)
}

// PlanPayrollFor plans the payroll history of an arbitrary workforce. It is
// the whole of [PlanPayroll]'s logic; the demo plan is just one input.
func PlanPayrollFor(rows []workforce.WorkerRow) (PayrollPlan, error) {
	plan := PayrollPlan{Runs: make([]PlannedPayrollRun, 0, len(DemoPayPeriods))}
	for _, period := range DemoPayPeriods {
		run, err := planPayrollRun(period, rows)
		if err != nil {
			return PayrollPlan{}, err
		}
		plan.Runs = append(plan.Runs, run)
	}
	return plan, nil
}

func planPayrollRun(period PayPeriodSpec, rows []workforce.WorkerRow) (PlannedPayrollRun, error) {
	runID := PayrollRunID(period)
	periodRef := payroll.PeriodRef{
		ID: "harborcare.payroll.period." + period.ID, Version: "1",
		Digest: payrollDigest("payroll-period", period.ID),
	}
	binding := payroll.PopulationBindingRef{
		DefinitionID: "harborcare.payroll.population." + DemoPayGroupRef, RevisionVersion: period.ID,
		Digest: payrollDigest("payroll-population", DemoPayGroupRef+"/"+period.ID),
	}
	draft, err := payroll.NewPayrollRun(runID, DemoPayGroupRef, periodRef, binding,
		payrollDigest("payroll-calculation-input", runID))
	if err != nil {
		return PlannedPayrollRun{}, fmt.Errorf("demoworkforce: payroll run %s: %w", runID, err)
	}
	calculated, err := draft.Transition(payroll.PayrollRunStateCalculated, payrollDigest("payroll-calculation", runID))
	if err != nil {
		return PlannedPayrollRun{}, fmt.Errorf("demoworkforce: calculate %s: %w", runID, err)
	}
	released, err := calculated.Release(payrollDigest("payroll-release", runID))
	if err != nil {
		return PlannedPayrollRun{}, fmt.Errorf("demoworkforce: release %s: %w", runID, err)
	}
	settled, err := released.Settle()
	if err != nil {
		return PlannedPayrollRun{}, fmt.Errorf("demoworkforce: settle %s: %w", runID, err)
	}

	members := make([]payroll.PopulationMember, 0, len(rows))
	lateEntries := make([]payroll.PopulationAmendment, 0)
	for _, row := range rows {
		member, standing, err := payrollMembership(row, period)
		if err != nil {
			return PlannedPayrollRun{}, err
		}
		switch standing {
		case payrollStandingFullPeriod:
			members = append(members, member)
		case payrollStandingMidPeriodHire:
			hire, _ := time.Parse(workforce.DateLayout, row.HireDate)
			amendment, amendErr := payroll.NewPopulationAmendment(payroll.PopulationAmendmentLateEntry, member,
				"hired inside the pay period", values.NewInstant(hire.UTC()))
			if amendErr != nil {
				return PlannedPayrollRun{}, fmt.Errorf("demoworkforce: late entry for %s: %w", row.WorkerKey, amendErr)
			}
			lateEntries = append(lateEntries, amendment)
		}
	}
	if len(members) == 0 {
		return PlannedPayrollRun{}, fmt.Errorf("demoworkforce: payroll run %s has no population", runID)
	}
	// Membership is frozen at the period's close, against revision one: the
	// run is calculated from a population that stopped moving.
	population, err := payroll.FreezePopulation(draft, values.NewInstant(period.End), members, payroll.LateEntryPolicyExplicitAmendment)
	if err != nil {
		return PlannedPayrollRun{}, fmt.Errorf("demoworkforce: freeze %s population: %w", runID, err)
	}
	return PlannedPayrollRun{
		Period:      period,
		Revisions:   []payroll.PayrollRun{draft, calculated, released, settled},
		Population:  population,
		LateEntries: lateEntries,
	}, nil
}

type payrollStanding int

const (
	// payrollStandingAbsent is a worker this pay group's run does not cover.
	payrollStandingAbsent payrollStanding = iota
	// payrollStandingFullPeriod is a worker employed for the whole period.
	payrollStandingFullPeriod
	// payrollStandingMidPeriodHire is a worker who started inside it.
	payrollStandingMidPeriodHire
)

// payrollMembership reports how one worker stands in one pay period. A worker
// paid on another basis is not in this pay group at all; a worker who had not
// started by the period's close is absent; a worker who started inside the
// period is a late entry, never a full-period member.
func payrollMembership(row workforce.WorkerRow, period PayPeriodSpec) (payroll.PopulationMember, payrollStanding, error) {
	if row.PayBasis != DemoPayBasis {
		return payroll.PopulationMember{}, payrollStandingAbsent, nil
	}
	hire, err := time.Parse(workforce.DateLayout, row.HireDate)
	if err != nil {
		return payroll.PopulationMember{}, payrollStandingAbsent, fmt.Errorf("demoworkforce: %s hire date: %w", row.WorkerKey, err)
	}
	hire = hire.UTC()
	member := payroll.PopulationMember{
		WorkerRef:     row.WorkerID.String(),
		EmploymentRef: row.EmploymentID,
		PayGroupRef:   DemoPayGroupRef,
	}
	switch {
	case !hire.Before(period.End):
		return payroll.PopulationMember{}, payrollStandingAbsent, nil
	case hire.After(period.Start):
		return member, payrollStandingMidPeriodHire, nil
	default:
		return member, payrollStandingFullPeriod, nil
	}
}

// SeedPayroll records the planned payroll history through payrollstore inside
// tx, which the caller owns. A run whose first revision is already stored is
// left alone: these tables are append-only and every revision identity is
// derived from the period, so a replay has nothing to add.
func SeedPayroll(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) (PayrollSummary, error) {
	if tx == nil || tenant == uuid.Nil {
		return PayrollSummary{}, fmt.Errorf("demoworkforce: seed payroll: a transaction and tenant are required")
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return PayrollSummary{}, err
	}
	plan, err := PlanPayroll(tenant)
	if err != nil {
		return PayrollSummary{}, err
	}
	store := payrollstore.Store{}
	var summary PayrollSummary
	for _, run := range plan.Runs {
		runID := run.Revisions[0].RunID
		if _, err := store.LoadRunTx(ctx, tx, tenant, runID, 1); err == nil {
			summary.SkippedRuns++
			continue
		} else if !isPayrollNotFound(err) {
			return PayrollSummary{}, fmt.Errorf("demoworkforce: inspect payroll run %s: %w", runID, err)
		}
		for _, revision := range run.Revisions {
			if err := store.SaveRunTx(ctx, tx, tenant, revision); err != nil {
				return PayrollSummary{}, fmt.Errorf("demoworkforce: save payroll run %s revision %d: %w", runID, revision.Revision, err)
			}
			summary.RunRevisions++
		}
		summary.Runs++
		if err := store.SavePopulationTx(ctx, tx, tenant, run.Population); err != nil {
			return PayrollSummary{}, fmt.Errorf("demoworkforce: save frozen population %s: %w", runID, err)
		}
		summary.Populations++
		summary.Members += len(run.Population.Members)
		for index, amendment := range run.LateEntries {
			if err := store.AppendAmendmentTx(ctx, tx, tenant, runID, uint64(index+1), amendment); err != nil {
				return PayrollSummary{}, fmt.Errorf("demoworkforce: append late entry %d on %s: %w", index+1, runID, err)
			}
			summary.LateEntries++
		}
	}
	return summary, nil
}

// isPayrollNotFound reports whether err is the store's "no such revision"
// refusal rather than a real fault.
func isPayrollNotFound(err error) bool { return errors.Is(err, payroll.ErrNotFound) }
