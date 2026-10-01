package clockservice

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/records"
)

// TimeRecordClass is the data classification used by the time-record
// retention schedule. The classes are intentionally separate: a punch is not
// a timecard, and raw evidence is not a worker-facing record.
type TimeRecordClass string

const (
	TimeRecordClassPunch                 TimeRecordClass = "PUNCH"
	TimeRecordClassTimecard              TimeRecordClass = "TIMECARD"
	TimeRecordClassPayrollLinkedTimecard TimeRecordClass = "PAYROLL_LINKED_TIMECARD"
	TimeRecordClassAttestation           TimeRecordClass = "ATTESTATION"
	TimeRecordClassPhotoEvidence         TimeRecordClass = "PHOTO_EVIDENCE"
	TimeRecordClassLocationEvidence      TimeRecordClass = "LOCATION_EVIDENCE"
	TimeRecordClassDeviceLog             TimeRecordClass = "DEVICE_LOG"
)

// Valid reports whether c is a declared time-record class.
func (c TimeRecordClass) Valid() bool {
	switch c {
	case TimeRecordClassPunch, TimeRecordClassTimecard, TimeRecordClassPayrollLinkedTimecard,
		TimeRecordClassAttestation, TimeRecordClassPhotoEvidence, TimeRecordClassLocationEvidence,
		TimeRecordClassDeviceLog:
		return true
	default:
		return false
	}
}

// Retention statuses deliberately mirror the existing records simulator while
// keeping clockservice callers independent of its implementation details.
type TimeRecordRetentionStatus string

const (
	TimeRecordRetentionEligible TimeRecordRetentionStatus = "ELIGIBLE"
	TimeRecordRetentionBlocked  TimeRecordRetentionStatus = "BLOCKED_WITH_REASONS"
	TimeRecordRetentionRepair   TimeRecordRetentionStatus = "REPAIR_REQUIRED"
)

// TimeRecordRetentionRule is one jurisdiction-scoped minimum. A jurisdiction
// rule is data supplied by governance; no state or member-state law is
// hard-coded into the clock workflow.
type TimeRecordRetentionRule struct {
	RecordClass  TimeRecordClass `json:"record_class"`
	Jurisdiction string          `json:"jurisdiction"`
	MinimumDays  int             `json:"minimum_days"`
	AuthorityRef string          `json:"authority_ref"`
}

func (r TimeRecordRetentionRule) validate() error {
	if !r.RecordClass.Valid() || strings.TrimSpace(r.Jurisdiction) == "" || r.MinimumDays <= 0 || strings.TrimSpace(r.AuthorityRef) == "" {
		return fmt.Errorf("clockservice: invalid time-record retention rule")
	}
	return nil
}

// TimeRecordRetentionPolicy is an immutable-by-convention, versioned set of
// data rules. Use WithRule to obtain a copied policy with a jurisdictional
// extension or replacement.
type TimeRecordRetentionPolicy struct {
	Version int                       `json:"version"`
	Rules   []TimeRecordRetentionRule `json:"rules"`
}

// DefaultTimeRecordRetentionPolicy returns the federal baseline. The raw
// evidence classes are intentionally shorter than a timecard; a tenant adds a
// longer state or EU member-state rule with WithRule when applicable.
func DefaultTimeRecordRetentionPolicy() TimeRecordRetentionPolicy {
	return TimeRecordRetentionPolicy{
		Version: 1,
		Rules: []TimeRecordRetentionRule{
			{RecordClass: TimeRecordClassPunch, Jurisdiction: "US-FEDERAL", MinimumDays: 730, AuthorityRef: "FLSA-29-CFR-516"},
			{RecordClass: TimeRecordClassTimecard, Jurisdiction: "US-FEDERAL", MinimumDays: 730, AuthorityRef: "FLSA-29-CFR-516"},
			{RecordClass: TimeRecordClassPayrollLinkedTimecard, Jurisdiction: "US-FEDERAL", MinimumDays: 1095, AuthorityRef: "FLSA-29-CFR-516-PAYROLL"},
			{RecordClass: TimeRecordClassAttestation, Jurisdiction: "US-FEDERAL", MinimumDays: 730, AuthorityRef: "FLSA-29-CFR-516"},
			{RecordClass: TimeRecordClassPhotoEvidence, Jurisdiction: "US-FEDERAL", MinimumDays: 30, AuthorityRef: "TIME-EVIDENCE-MINIMIZATION"},
			{RecordClass: TimeRecordClassLocationEvidence, Jurisdiction: "US-FEDERAL", MinimumDays: 90, AuthorityRef: "TIME-EVIDENCE-MINIMIZATION"},
			{RecordClass: TimeRecordClassDeviceLog, Jurisdiction: "US-FEDERAL", MinimumDays: 365, AuthorityRef: "TIME-DEVICE-AUDIT"},
		},
	}
}

// Validate checks the schedule as a closed data set. Duplicate class and
// jurisdiction pairs are refused so a disposition cannot choose by order.
func (p TimeRecordRetentionPolicy) Validate() error {
	if p.Version < 1 || len(p.Rules) == 0 {
		return fmt.Errorf("clockservice: retention policy requires a positive version and rules")
	}
	seen := make(map[string]struct{}, len(p.Rules))
	for _, rule := range p.Rules {
		if err := rule.validate(); err != nil {
			return err
		}
		key := string(rule.RecordClass) + "\x00" + strings.TrimSpace(rule.Jurisdiction)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("clockservice: duplicate retention rule %q", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// WithRule returns a copied policy. A non-federal rule cannot weaken the
// federal minimum for the same record class; this prevents a jurisdictional
// override from accidentally shortening an FLSA floor.
func (p TimeRecordRetentionPolicy) WithRule(rule TimeRecordRetentionRule) (TimeRecordRetentionPolicy, error) {
	if err := p.Validate(); err != nil {
		return TimeRecordRetentionPolicy{}, err
	}
	if err := rule.validate(); err != nil {
		return TimeRecordRetentionPolicy{}, err
	}
	for _, prior := range p.Rules {
		if prior.RecordClass == rule.RecordClass && prior.Jurisdiction == "US-FEDERAL" && rule.MinimumDays < prior.MinimumDays {
			return TimeRecordRetentionPolicy{}, fmt.Errorf("clockservice: %s rule cannot weaken federal minimum", rule.RecordClass)
		}
	}
	out := TimeRecordRetentionPolicy{Version: p.Version, Rules: append([]TimeRecordRetentionRule(nil), p.Rules...)}
	for i, prior := range out.Rules {
		if prior.RecordClass == rule.RecordClass && prior.Jurisdiction == rule.Jurisdiction {
			out.Rules[i] = rule
			return out, nil
		}
	}
	out.Rules = append(out.Rules, rule)
	return out, nil
}

// RuleFor resolves an exact jurisdiction and composes it with the federal
// floor. An unregistered jurisdiction fails closed instead of silently using
// a possibly wrong state's or member state's law.
func (p TimeRecordRetentionPolicy) RuleFor(class TimeRecordClass, jurisdiction string) (TimeRecordRetentionRule, error) {
	if err := p.Validate(); err != nil {
		return TimeRecordRetentionRule{}, err
	}
	jurisdiction = strings.TrimSpace(jurisdiction)
	var exact, federal *TimeRecordRetentionRule
	for i := range p.Rules {
		rule := &p.Rules[i]
		if rule.RecordClass != class {
			continue
		}
		if rule.Jurisdiction == jurisdiction {
			exact = rule
		}
		if rule.Jurisdiction == "US-FEDERAL" {
			federal = rule
		}
	}
	if exact == nil {
		return TimeRecordRetentionRule{}, fmt.Errorf("clockservice: no retention rule for %s in %s", class, jurisdiction)
	}
	out := *exact
	if federal != nil && federal.MinimumDays > out.MinimumDays {
		out.MinimumDays = federal.MinimumDays
	}
	refs := []string{out.AuthorityRef}
	if federal != nil && federal.AuthorityRef != out.AuthorityRef {
		refs = append(refs, federal.AuthorityRef)
	}
	sort.Strings(refs)
	out.AuthorityRef = strings.Join(refs, "+")
	return out, nil
}

// TimeRecordDeclaration binds one clock record to its retention class. The
// declaration contains metadata only; payloads and sensitive evidence never
// enter the retention decision.
type TimeRecordDeclaration struct {
	TenantID            string          `json:"tenant_id"`
	RecordID            string          `json:"record_id"`
	WorkerRef           string          `json:"worker_ref"`
	Custodian           string          `json:"custodian"`
	Jurisdiction        string          `json:"jurisdiction"`
	RecordClass         TimeRecordClass `json:"record_class"`
	RecordedAt          time.Time       `json:"recorded_at"`
	CutoffAt            time.Time       `json:"cutoff_at"`
	ArchiveAcknowledged bool            `json:"archive_acknowledged"`
	ActiveLegalHoldRefs []string        `json:"active_legal_hold_refs,omitempty"`
}

func (d TimeRecordDeclaration) validate() error {
	if strings.TrimSpace(d.TenantID) == "" || strings.TrimSpace(d.RecordID) == "" || strings.TrimSpace(d.WorkerRef) == "" ||
		strings.TrimSpace(d.Custodian) == "" || strings.TrimSpace(d.Jurisdiction) == "" || !d.RecordClass.Valid() ||
		d.RecordedAt.IsZero() || d.CutoffAt.IsZero() || d.CutoffAt.Before(d.RecordedAt) || !d.ArchiveAcknowledged {
		return fmt.Errorf("clockservice: invalid time-record declaration")
	}
	for _, hold := range d.ActiveLegalHoldRefs {
		if strings.TrimSpace(hold) == "" {
			return fmt.Errorf("clockservice: legal hold reference is empty")
		}
	}
	return nil
}

// TimeRecordRetentionDecision is the payload-free result of one retention
// evaluation. DispositionAllowed is false for every active legal hold.
type TimeRecordRetentionDecision struct {
	RecordID           string                    `json:"record_id"`
	RecordClass        TimeRecordClass           `json:"record_class"`
	Status             TimeRecordRetentionStatus `json:"status"`
	DispositionAllowed bool                      `json:"disposition_allowed"`
	LegalHoldBlocked   bool                      `json:"legal_hold_blocked"`
	Rule               TimeRecordRetentionRule   `json:"rule"`
	EligibleAt         *time.Time                `json:"eligible_at,omitempty"`
	Blockers           []string                  `json:"blockers,omitempty"`
	Digest             string                    `json:"digest"`
}

// EvaluateTimeRecordRetention delegates cutoff, hold and minimum-period
// evaluation to the existing records disposition simulator.
func EvaluateTimeRecordRetention(policy TimeRecordRetentionPolicy, declaration TimeRecordDeclaration, asOf time.Time) (TimeRecordRetentionDecision, error) {
	if err := declaration.validate(); err != nil {
		return TimeRecordRetentionDecision{}, err
	}
	rule, err := policy.RuleFor(declaration.RecordClass, declaration.Jurisdiction)
	if err != nil {
		return TimeRecordRetentionDecision{}, err
	}
	report, err := records.Simulate(records.SimulationRequest{
		AsOf: asOf,
		Copies: []records.Copy{{
			ID: declaration.RecordID, RecordSeries: string(declaration.RecordClass), Custodian: declaration.Custodian,
			Jurisdiction: declaration.Jurisdiction, CreatedAt: declaration.RecordedAt, CutoffAt: &declaration.CutoffAt,
			ArchiveAcknowledged: declaration.ArchiveAcknowledged, ActiveHoldRefs: append([]string(nil), declaration.ActiveLegalHoldRefs...),
		}},
		Rules: []records.RetentionRule{{RecordSeries: string(declaration.RecordClass), Jurisdiction: declaration.Jurisdiction, MinimumDays: rule.MinimumDays, AuthorityRef: rule.AuthorityRef}},
	})
	if err != nil {
		return TimeRecordRetentionDecision{}, err
	}
	item := report.Copies[0]
	status := TimeRecordRetentionStatus(item.Status)
	decision := TimeRecordRetentionDecision{RecordID: declaration.RecordID, RecordClass: declaration.RecordClass, Status: status, Rule: rule, EligibleAt: item.EligibleAt, Digest: report.Digest}
	for _, blocker := range item.Blockers {
		decision.Blockers = append(decision.Blockers, blocker.Code)
		if blocker.Code == "ACTIVE_LEGAL_HOLD" {
			decision.LegalHoldBlocked = true
		}
	}
	decision.DispositionAllowed = status == TimeRecordRetentionEligible && !decision.LegalHoldBlocked
	return decision, nil
}

// EvaluateRetention is a concise alias used by disposition adapters.
func EvaluateRetention(policy TimeRecordRetentionPolicy, declaration TimeRecordDeclaration, asOf time.Time) (TimeRecordRetentionDecision, error) {
	return EvaluateTimeRecordRetention(policy, declaration, asOf)
}
