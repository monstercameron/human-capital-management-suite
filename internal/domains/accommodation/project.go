package accommodation

import (
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// CaseView is what one viewer may receive: the operational case always, and
// medical detail only where field policy grants it. MedicalRedacted tells the
// renderer the case holds more than this viewer may see, so the UI explains
// the gap instead of implying there is none.
type CaseView struct {
	CaseID             string
	WorkerID           string
	Status             string
	Limitations        []string
	Dialogue           []DialogueEntry
	Options            []Option
	Decision           *Decision
	Reviews            []Review
	Diagnosis          string
	MedicalEvidenceRef string
	MedicalRedacted    bool
}

// ProjectCase renders the case for viewer stating purpose. The run wall
// decides first: an outsider, a revoked participant, or a wrong purpose gets
// the wall's single non-disclosing denial, which carries no case content.
// Field policy then decides the medical detail: a viewer the run admits but
// medicine does not receives the accommodation without the diagnosis.
func ProjectCase(c Case, viewer *trust.Principal, purpose string) (CaseView, error) {
	if viewer == nil {
		return CaseView{}, runtime.ErrRunNotDisclosed
	}
	if err := c.Access.Authorize(viewer.Subject(), purpose, runtime.SurfaceInspector); err != nil {
		return CaseView{}, err
	}
	if err := c.Verify(); err != nil {
		return CaseView{}, err
	}
	view := CaseView{
		CaseID:      c.ID,
		WorkerID:    c.Request.WorkerID,
		Status:      c.Request.Status,
		Limitations: append([]string(nil), c.Request.Limitations...),
		Options:     append([]Option(nil), c.Options...),
		Reviews:     append([]Review(nil), c.Reviews...),
	}
	if c.Decision != nil {
		dec := *c.Decision
		view.Decision = &dec
	}
	decision, err := authz.ResolveFields(viewer, purpose, []authz.FieldID{authz.FieldMedicalAccomodation}, nil)
	if err != nil {
		return CaseView{}, err
	}
	if decision.Rulings[authz.FieldMedicalAccomodation].Effect != authz.EffectAllow {
		view.MedicalRedacted = true
		for _, e := range c.Dialogue {
			if e.Classification != EntryMedical {
				view.Dialogue = append(view.Dialogue, e)
			}
		}
		return view, nil
	}
	view.Diagnosis = c.Request.Diagnosis
	view.MedicalEvidenceRef = c.Request.MedicalEvidenceRef
	view.Dialogue = append([]DialogueEntry(nil), c.Dialogue...)
	return view, nil
}
