package scopefidelity

import "fmt"

// CodeUndocumentedP1ACompletion flags a ticked completion outside the
// section authorized P1A set that no signed GOV-006 scope-exchange record
// admits.
const CodeUndocumentedP1ACompletion = "UNDOCUMENTED_P1A_COMPLETION"

// Completion is one backlog item's completion state as seen by the audit.
type Completion struct {
	ID           string
	Ticked       bool
	Category     string
	EvidenceDate string
}

// Disposition is the section scope disposition the audit holds completions
// against: the exact ids admitted to P1A and the work categories the
// disposition defers.
type Disposition struct {
	AuthorizedP1A      map[string]bool
	DeferredCategories map[string]bool
}

// ExchangeRecord is one GOV-006 scope-exchange record. Every field is
// required: owner, schedule impact, acceptance evidence, displaced scope,
// approval digest and the changed critical path.
type ExchangeRecord struct {
	ID                  string
	Owner               string
	ScheduleImpact      string
	AcceptanceEvidence  string
	DisplacedScope      string
	ApprovalDigest      string
	ChangedCriticalPath string
}

func (e ExchangeRecord) complete() bool {
	return e.Owner != "" &&
		e.ScheduleImpact != "" &&
		e.AcceptanceEvidence != "" &&
		e.DisplacedScope != "" &&
		e.ApprovalDigest != "" &&
		e.ChangedCriticalPath != ""
}

// AuditDisposition returns one finding per ticked completion outside the
// authorized P1A set that no complete signed exchange record admits.
// Unticked items never flag: an unticked item is honest about being open.
func AuditDisposition(d Disposition, completions []Completion, exchanges []ExchangeRecord) []Finding {
	admitted := map[string]bool{}
	for _, ex := range exchanges {
		if ex.complete() {
			admitted[ex.ID] = true
		}
	}
	var out []Finding
	for _, c := range completions {
		if !c.Ticked || d.AuthorizedP1A[c.ID] || admitted[c.ID] {
			continue
		}
		msg := fmt.Sprintf("ticked P1A completion outside the authorized set (category %s, evidence %s) with no signed GOV-006 scope-exchange record", c.Category, c.EvidenceDate)
		if d.DeferredCategories[c.Category] {
			msg = fmt.Sprintf("ticked P1A completion in disposition-deferred category %q (evidence %s) with no signed GOV-006 scope-exchange record", c.Category, c.EvidenceDate)
		}
		out = append(out, Finding{TodoID: c.ID, Code: CodeUndocumentedP1ACompletion, Message: msg})
	}
	sortFindings(out)
	return out
}
