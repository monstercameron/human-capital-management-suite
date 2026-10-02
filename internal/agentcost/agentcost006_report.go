package agentcost

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Kind says what a run's spend bought. Screening and decisions are the quiet
// work an agent does to decide whether to speak; answers are what people ask
// for.
type Kind string

const (
	KindAnswer    Kind = "answer"
	KindScreening Kind = "screening"
	KindDecision  Kind = "decision"
)

// Run is one finished run's cost. Answered is true when the run answered a
// question; MessagesRead counts the messages an ambient agent read.
type Run struct {
	TenantID       string
	AgentID        string
	ConversationID string
	RunID          string
	At             time.Time
	Kind           Kind
	SpendMicros    int64
	Answered       bool
	MessagesRead   int64
}

// Ledger keeps finished runs for the cost report. The durable form is a
// table the migration in the lane report describes; the report logic does not
// care where the rows live.
type Ledger struct {
	mu    sync.Mutex
	runs  []Run
	store Store
}

// WithStore makes the ledger keep its runs in store and read the report from
// it. It must be called before the ledger is used.
func (l *Ledger) WithStore(store Store) *Ledger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.store = store
	return l
}

// Add records a finished run in memory.
func (l *Ledger) Add(run Run) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runs = append(l.runs, run)
}

// Append records a finished run, in the store when the ledger has one. The
// store is written first so a run that was not stored is not reported.
func (l *Ledger) Append(run Run) error {
	l.mu.Lock()
	store := l.store
	l.mu.Unlock()
	if store != nil {
		if err := store.AppendRun(run); err != nil {
			return err
		}
		return nil
	}
	l.Add(run)
	return nil
}

// DayCost is one day's spend.
type DayCost struct {
	Day         string
	SpendMicros int64
	Runs        int64
}

// AgentDay is one agent's spend on one day.
type AgentDay struct {
	AgentID     string
	Day         string
	SpendMicros int64
}

// RunLine is one run in the recent-runs table.
type RunLine struct {
	AgentID     string
	RunID       string
	At          time.Time
	Kind        Kind
	SpendMicros int64
}

// Report is what Agent operations shows an owner.
type Report struct {
	RecentRuns  []RunLine
	PerAgentDay []AgentDay
	// Trend is the last thirty days, oldest first, zero-filled.
	Trend []DayCost
	// PerAnsweredQuestionMicros is answer spend divided by answered questions;
	// zero when nothing was answered.
	PerAnsweredQuestionMicros int64
	// QuietSharePercent is the share of spend on screening and decisions rather
	// than answers, as a whole percent.
	QuietSharePercent int
	// PerHundredMessagesMicros is the ambient agents' cost per hundred
	// messages they read; zero when none were read.
	PerHundredMessagesMicros int64
	// ForecastMonthMicros is the month so far plus the daily average so far
	// carried to the month's end.
	ForecastMonthMicros int64
	MonthToDateMicros   int64
}

// Report builds the report for a viewer. It counts only runs of agents the
// viewer owns in their own tenant, so a figure never carries another owner's
// or another tenant's spend.
func (l *Ledger) Report(owners Owners, tenant, viewer string, now time.Time, zone *time.Location) Report {
	report, _ := l.ReportFor(owners, tenant, viewer, now, zone)
	return report
}

// ReportFor is Report that also says when the store could not be read, so a
// page can offer Try again instead of drawing zeros.
func (l *Ledger) ReportFor(owners Owners, tenant, viewer string, now time.Time, zone *time.Location) (Report, error) {
	if zone == nil {
		zone = time.UTC
	}
	l.mu.Lock()
	store := l.store
	runs := append([]Run(nil), l.runs...)
	l.mu.Unlock()
	if store != nil {
		// The report reaches back thirty days and the month's start, whichever is
		// earlier, so the trend and the forecast see the same runs.
		since := now.In(zone).AddDate(0, 0, -62)
		stored, err := store.RunsSince(tenant, since)
		if err != nil {
			return Report{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		runs = stored
	}
	var mine []Run
	owned := map[string]bool{}
	for _, run := range runs {
		if run.TenantID != tenant {
			continue
		}
		ok, known := owned[run.AgentID]
		if !known {
			ok = owners.IsOwner(tenant, run.AgentID, viewer)
			owned[run.AgentID] = ok
		}
		if ok {
			mine = append(mine, run)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].At.Before(mine[j].At) })

	local := now.In(zone)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	dayOf := func(at time.Time) string { return at.In(zone).Format("2006-01-02") }

	var report Report
	trendIndex := map[string]int{}
	for offset := 29; offset >= 0; offset-- {
		day := today.AddDate(0, 0, -offset).Format("2006-01-02")
		trendIndex[day] = len(report.Trend)
		report.Trend = append(report.Trend, DayCost{Day: day})
	}
	perAgentDay := map[[2]string]int64{}
	var answerSpend, answered, quietSpend, ambientSpend, ambientMessages int64
	monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, zone)
	for _, run := range mine {
		day := dayOf(run.At)
		if index, ok := trendIndex[day]; ok {
			report.Trend[index].SpendMicros += run.SpendMicros
			report.Trend[index].Runs++
		}
		perAgentDay[[2]string{run.AgentID, day}] += run.SpendMicros
		switch run.Kind {
		case KindAnswer:
			answerSpend += run.SpendMicros
			if run.Answered {
				answered++
			}
		default:
			quietSpend += run.SpendMicros
		}
		if run.MessagesRead > 0 {
			ambientSpend += run.SpendMicros
			ambientMessages += run.MessagesRead
		}
		if !run.At.Before(monthStart) && run.At.Before(today.AddDate(0, 0, 1)) {
			report.MonthToDateMicros += run.SpendMicros
		}
	}
	for key, micros := range perAgentDay {
		report.PerAgentDay = append(report.PerAgentDay, AgentDay{AgentID: key[0], Day: key[1], SpendMicros: micros})
	}
	sort.Slice(report.PerAgentDay, func(i, j int) bool {
		if report.PerAgentDay[i].Day != report.PerAgentDay[j].Day {
			return report.PerAgentDay[i].Day > report.PerAgentDay[j].Day
		}
		return report.PerAgentDay[i].AgentID < report.PerAgentDay[j].AgentID
	})
	for index := len(mine) - 1; index >= 0 && len(report.RecentRuns) < 20; index-- {
		run := mine[index]
		report.RecentRuns = append(report.RecentRuns, RunLine{AgentID: run.AgentID, RunID: run.RunID, At: run.At, Kind: run.Kind, SpendMicros: run.SpendMicros})
	}
	if answered > 0 {
		report.PerAnsweredQuestionMicros = answerSpend / answered
	}
	if total := answerSpend + quietSpend; total > 0 {
		report.QuietSharePercent = int((quietSpend*100 + total/2) / total)
	}
	if ambientMessages > 0 {
		report.PerHundredMessagesMicros = ambientSpend * 100 / ambientMessages
	}
	daysElapsed := int64(local.Day())
	daysInMonth := int64(monthStart.AddDate(0, 1, -1).Day())
	report.ForecastMonthMicros = report.MonthToDateMicros * daysInMonth / daysElapsed
	return report, nil
}
