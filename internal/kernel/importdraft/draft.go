package importdraft

// DraftWrite is a write which a simulated BusinessIntent would request.
type DraftWrite struct {
	Property         string
	Current          string
	CurrentPresent   bool
	Proposed         string
	AuthorityRef     string
	ResourceKey      string
	BaselineRevision string
}

// SimulationConflict is an already observed conflict from the read-only
// snapshot. The simulator classifies it; it never resolves or overwrites it.
type SimulationConflict struct {
	RowID          string
	Property       string
	ExistingIntent string
	EvidenceRef    string
}

// BusinessIntentDraft is the ordered, per-row proposal input produced by the
// simulator. Invalid rows are retained with an error for auditability.
type BusinessIntentDraft struct {
	Ordinal         int
	RowID           string
	IntentType      string
	SubjectKind     string
	SubjectID       string
	AuthorityDomain string
	AuthorityRefs   []string
	Status          string // CREATE, CHANGE, NO_OP, ERROR, CONFLICT
	Writes          []DraftWrite
	Errors          []ValidationError
	ConflictRefs    []string
	Conflicts       []SimulationConflict
}

// ValidationSeverity is the closed set of severities a [ValidationError] carries.
type ValidationSeverity uint8

// Severities.
const (
	// The zero value is unspecified and never legal.
	_ ValidationSeverity = iota
	// ValidationSeverityError blocks the row from being VALID.
	ValidationSeverityError
	// ValidationSeverityWarning is informational and never blocks a row.
	ValidationSeverityWarning
)

var severityWire = map[ValidationSeverity]string{
	ValidationSeverityError:   "ERROR",
	ValidationSeverityWarning: "WARNING",
}

// String returns the stable wire token, or "SEVERITY_UNSPECIFIED".
func (s ValidationSeverity) String() string {
	if w, ok := severityWire[s]; ok {
		return w
	}
	return "SEVERITY_UNSPECIFIED"
}

// ValidationError is one rule violation, identified stably enough that
// re-running validation over the same batch and mapping always reproduces the
// identical error set: the identity is a digest of the batch, the row, the
// property and the rule, so it never depends on where in the batch the row
// sits, or on which pass found it first.
type ValidationError struct {
	Identity string // sha256 over batch id + row id + property + rule id.
	BatchID  string
	// RowID is empty for a batch-level diagnostic (RuleColumnUnmapped).
	RowID    string
	Property string
	RuleID   string
	Severity ValidationSeverity
	Message  string
}
