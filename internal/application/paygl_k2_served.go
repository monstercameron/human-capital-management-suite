package application

import paygldomain "github.com/monstercameron/human-capital-management-suite/internal/domains/paygl"

// ServedPayGLSurface is the application boundary for the payroll-to-general-
// ledger correction and reconciliation semantics. The domain remains the
// semantic owner; this value only makes its pure operations reachable from a
// composed serving application.
type ServedPayGLSurface struct {
	CorrectJournal   func(paygldomain.Journal, paygldomain.CorrectionRequest) (paygldomain.JournalCorrection, error)
	ReconcilePosting func(paygldomain.Journal, paygldomain.ERPObservation) (paygldomain.PostingReconciliation, error)
}

// NewServedPayGLSurface returns the PAYGL operations exposed by hcmnext serve.
func NewServedPayGLSurface() ServedPayGLSurface {
	return ServedPayGLSurface{
		CorrectJournal:   paygldomain.CorrectJournal,
		ReconcilePosting: paygldomain.ReconcilePosting,
	}
}

// PayGL returns the payroll-to-general-ledger surface exposed by a composed
// application. A nil application has no reachable capabilities.
func (a *App) PayGL() ServedPayGLSurface {
	if a == nil {
		return ServedPayGLSurface{}
	}
	return NewServedPayGLSurface()
}
