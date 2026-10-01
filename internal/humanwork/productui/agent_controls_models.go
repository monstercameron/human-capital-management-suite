package productui

// AgentControlsSnapshot contains only the owner's authorized projections.
// Action lists are affordances; each owner command rechecks authority.
type AgentControlsSnapshot struct {
	Available bool                   `json:"available"`
	CanDraft  bool                   `json:"can_draft"`
	CanExport bool                   `json:"can_export"`
	Schedules []AgentControlSchedule `json:"schedules"`
	Runs      []AgentControlRun      `json:"runs"`
	Memory    []AgentControlMemory   `json:"memory"`
}

type AgentControlSchedule struct {
	ID             string   `json:"id"`
	Revision       uint64   `json:"revision"`
	State          string   `json:"state"`
	Version        string   `json:"version"`
	Installation   string   `json:"installation"`
	Zone           string   `json:"zone"`
	Recurrence     string   `json:"recurrence"`
	DST            string   `json:"dst"`
	Calendar       string   `json:"calendar"`
	Destination    string   `json:"destination"`
	Misfire        string   `json:"misfire"`
	Overlap        string   `json:"overlap"`
	Budget         string   `json:"budget"`
	Occurrences    []string `json:"occurrences"`
	OccurrenceKeys []string `json:"occurrence_keys"`
	Actions        []string `json:"actions"`
}

type AgentControlRun struct {
	ID           string   `json:"id"`
	Revision     uint64   `json:"revision"`
	Version      string   `json:"version"`
	Installation string   `json:"installation"`
	State        string   `json:"state"`
	Cause        string   `json:"cause"`
	QueueLag     string   `json:"queue_lag"`
	Spend        string   `json:"spend"`
	Failure      string   `json:"failure"`
	Denials      []string `json:"denials"`
	Citations    []string `json:"citations"`
	Evals        []string `json:"evals"`
	Incident     string   `json:"incident"`
	Actions      []string `json:"actions"`
}

type AgentControlMemory struct {
	ID       string   `json:"id"`
	Revision uint64   `json:"revision"`
	Source   string   `json:"source"`
	Audience string   `json:"audience"`
	Purpose  string   `json:"purpose"`
	Class    string   `json:"class"`
	Expires  string   `json:"expires"`
	Held     bool     `json:"held"`
	Actions  []string `json:"actions"`
}

type AgentControlsCommand struct {
	Kind             string `json:"kind"`
	ID               string `json:"id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	Action           string `json:"action"`
	IdempotencyKey   string `json:"idempotency_key"`
	Reason           string `json:"reason"`
	Occurrence       string `json:"occurrence,omitempty"`
	IncidentID       string `json:"incident_id,omitempty"`
}

// AgentScheduleDraft is a bounded scheduling owner input, never a grant.
type AgentScheduleDraft struct {
	ID               string `json:"id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	Version          string `json:"version"`
	Installation     string `json:"installation"`
	Recurrence       string `json:"recurrence"`
	Zone             string `json:"zone"`
	Calendar         string `json:"calendar"`
	Destination      string `json:"destination"`
	Budget           string `json:"budget"`
	Misfire          string `json:"misfire"`
	Overlap          string `json:"overlap"`
	DST              string `json:"dst"`
}
