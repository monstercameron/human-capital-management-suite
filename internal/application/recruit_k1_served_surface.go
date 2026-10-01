package application

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/recruiting"
)

// ServedRecruitingSurface exposes the native ATS contracts through the
// application boundary used by hcmnext serve. The recruiting domain remains
// the semantic owner; this surface only carries its value-owned operations
// into the composed application.
type ServedRecruitingSurface struct {
	Version           func() int
	NewAggregate      func(string) (recruiting.Aggregate, error)
	NewStageLedger    func(*recruiting.Aggregate, []string) (*recruiting.StageLedger, error)
	NewOfferLedger    func() *recruiting.OfferLedger
	ApplyTerminal     func(recruiting.CandidacyRecord, recruiting.TerminalCommand) (recruiting.CandidacyRecord, error)
	CorrectTerminal   func(recruiting.CandidacyRecord, string, string, string, time.Time) (recruiting.CandidacyRecord, error)
	RenderCandidacy   func(recruiting.CandidacyRecord, recruiting.RenderPurpose) recruiting.RenderedCandidacy
	ReconcileExternal func(recruiting.CandidacyRecord, map[string]string, recruiting.ExternalObservation, time.Time) (*recruiting.RepairPlan, error)
}

// NewServedRecruitingSurface returns the ATS capabilities reachable from the
// served application. It creates no process-wide or tenant-wide state.
func NewServedRecruitingSurface() ServedRecruitingSurface {
	return ServedRecruitingSurface{
		Version:           recruiting.Version,
		NewAggregate:      recruiting.NewAggregate,
		NewStageLedger:    recruiting.NewStageLedger,
		NewOfferLedger:    recruiting.NewOfferLedger,
		ApplyTerminal:     recruiting.ApplyTerminal,
		CorrectTerminal:   recruiting.CorrectTerminal,
		RenderCandidacy:   recruiting.Render,
		ReconcileExternal: recruiting.ReconcileExternal,
	}
}

// Recruiting returns the ATS capabilities exposed by a composed application.
// A nil application has no served capabilities.
func (a *App) Recruiting() ServedRecruitingSurface {
	if a == nil {
		return ServedRecruitingSurface{}
	}
	return NewServedRecruitingSurface()
}
