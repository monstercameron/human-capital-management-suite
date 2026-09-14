package operations

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// OPS009SchemaVersion identifies the continuity drill evidence shape.
	OPS009SchemaVersion = 1

	StatusContinuityReady    = "READY"
	StatusContinuityRejected = "OPS_009_REJECTED"
)

// DrillKind names the continuity scenario a drill rehearses.
type DrillKind string

// Continuity drill kinds.
const (
	DrillOutage          DrillKind = "OUTAGE"
	DrillExit            DrillKind = "EXIT"
	DrillMaintenance     DrillKind = "MAINTENANCE"
	DrillEmergencyChange DrillKind = "EMERGENCY_CHANGE"
)

// Valid reports whether k is a declared drill kind.
func (k DrillKind) Valid() bool {
	switch k {
	case DrillOutage, DrillExit, DrillMaintenance, DrillEmergencyChange:
		return true
	}
	return false
}

// ContinuityPlan is one vendor's continuity posture: who owns it, what the
// fallback is, and how long the evidence stays trustworthy.
type ContinuityPlan struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Vendor      string    `json:"vendor"`
	Owner       string    `json:"owner"`
	BackupOwner string    `json:"backup_owner"`
	Fallback    string    `json:"fallback"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ContinuityDrill is one observed drill against a plan. Every protection
// the GREEN contract names is an explicit boolean: an unrecorded step is
// a missing step, never a passed one.
type ContinuityDrill struct {
	ID        string    `json:"id"`
	PlanID    string    `json:"plan_id"`
	Kind      DrillKind `json:"kind"`
	StartedAt time.Time `json:"started_at"`

	NewWorkStoppedAt   time.Time `json:"new_work_stopped_at"`
	InFlightPreserved  bool      `json:"in_flight_preserved"`
	InFlightReconciled bool      `json:"in_flight_reconciled"`
	CredentialsRevoked bool      `json:"credentials_revoked"`
	FallbackActivated  bool      `json:"fallback_activated"`
	DrainedAt          time.Time `json:"drained_at"`
	RolledBackAt       time.Time `json:"rolled_back_at"`
	ReopenedAt         time.Time `json:"reopened_at"`

	ExceptionID     string    `json:"exception_id"`
	ExceptionExpiry time.Time `json:"exception_expiry"`

	EvidenceRef string `json:"evidence_ref"`
}

// ContinuityDiagnostic names the exact failed continuity target.
type ContinuityDiagnostic struct {
	Code    string `json:"code"`
	DrillID string `json:"drill_id"`
	PlanID  string `json:"plan_id"`
	Field   string `json:"field"`
	State   string `json:"state"`
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

func (d ContinuityDiagnostic) String() string {
	return fmt.Sprintf("%s drill=%s plan=%s field=%s state=%s version=%s: %s",
		d.Code, d.DrillID, d.PlanID, d.Field, d.State, d.Version, d.Reason)
}

// ContinuityResult is the pure OPS-009 readiness result.
type ContinuityResult struct {
	Status      string                 `json:"status"`
	Plans       int                    `json:"plans"`
	Drills      int                    `json:"drills"`
	Diagnostics []ContinuityDiagnostic `json:"diagnostics"`
	Effects     ZeroEffects            `json:"effects"`
}

// Ready reports whether every plan and drill met its continuity target.
func (r ContinuityResult) Ready() bool { return r.Status == StatusContinuityReady }

// EvaluateContinuity judges vendor continuity, maintenance and emergency
// change drills at now. It never persists, stops work, revokes
// credentials or calls a vendor, so rejected evidence has explicit zero
// effects.
func EvaluateContinuity(plans []ContinuityPlan, drills []ContinuityDrill, now time.Time) ContinuityResult {
	result := ContinuityResult{Status: StatusContinuityRejected, Plans: len(plans), Drills: len(drills), Effects: ZeroEffects{}}
	byID := make(map[string]ContinuityPlan, len(plans))
	for _, plan := range plans {
		result.Diagnostics = append(result.Diagnostics, validateContinuityPlan(plan, now)...)
		if _, dup := byID[plan.ID]; dup {
			result.Diagnostics = append(result.Diagnostics, ContinuityDiagnostic{Code: StatusContinuityRejected, PlanID: plan.ID, Field: "id", State: "DUPLICATE", Version: plan.Version, Reason: "continuity plan id is registered more than once"})
		}
		byID[plan.ID] = plan
	}
	if len(plans) == 0 {
		result.Diagnostics = append(result.Diagnostics, ContinuityDiagnostic{Code: StatusContinuityRejected, Field: "plans", State: "MISSING", Reason: "at least one vendor continuity plan is required"})
	}
	seen := map[string]bool{}
	for _, drill := range drills {
		if seen[drill.ID] {
			result.Diagnostics = append(result.Diagnostics, ContinuityDiagnostic{Code: StatusContinuityRejected, DrillID: drill.ID, PlanID: drill.PlanID, Field: "id", State: "DUPLICATE", Reason: "drill id is registered more than once"})
		}
		seen[drill.ID] = true
		plan, exists := byID[drill.PlanID]
		if !exists {
			result.Diagnostics = append(result.Diagnostics, ContinuityDiagnostic{Code: StatusContinuityRejected, DrillID: drill.ID, PlanID: drill.PlanID, Field: "plan_id", State: "UNKNOWN", Reason: "drill has no continuity plan"})
			continue
		}
		validateDrill(&result, plan, drill, now)
	}
	if len(drills) == 0 {
		result.Diagnostics = append(result.Diagnostics, ContinuityDiagnostic{Code: StatusContinuityRejected, Field: "drills", State: "MISSING", Reason: "at least one continuity drill is required"})
	}
	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		if result.Diagnostics[i].DrillID != result.Diagnostics[j].DrillID {
			return result.Diagnostics[i].DrillID < result.Diagnostics[j].DrillID
		}
		if result.Diagnostics[i].PlanID != result.Diagnostics[j].PlanID {
			return result.Diagnostics[i].PlanID < result.Diagnostics[j].PlanID
		}
		return result.Diagnostics[i].Field < result.Diagnostics[j].Field
	})
	if len(result.Diagnostics) == 0 {
		result.Status = StatusContinuityReady
	}
	return result
}

func validateContinuityPlan(plan ContinuityPlan, now time.Time) []ContinuityDiagnostic {
	var out []ContinuityDiagnostic
	add := func(field, state, reason string) {
		out = append(out, ContinuityDiagnostic{Code: StatusContinuityRejected, PlanID: plan.ID, Field: field, State: state, Version: plan.Version, Reason: reason})
	}
	if strings.TrimSpace(plan.ID) == "" {
		add("id", "MISSING", "plan id is required")
	}
	if strings.TrimSpace(plan.Version) == "" {
		add("version", "MISSING", "plan version is required")
	}
	if strings.TrimSpace(plan.Vendor) == "" {
		add("vendor", "MISSING", "vendor is required")
	}
	if strings.TrimSpace(plan.Owner) == "" || strings.TrimSpace(plan.BackupOwner) == "" {
		add("owner", "MISSING", "owner and backup owner are required")
	}
	if plan.Owner != "" && plan.Owner == plan.BackupOwner {
		add("backup_owner", "INVALID", "backup owner must be independent")
	}
	if strings.TrimSpace(plan.Fallback) == "" {
		add("fallback", "MISSING", "fallback path is required")
	}
	if plan.ExpiresAt.IsZero() {
		add("expires_at", "MISSING", "plan expiry is required")
	} else if !plan.ExpiresAt.After(now) {
		add("expires_at", "EXPIRED", "continuity evidence has expired")
	}
	return out
}

func validateDrill(result *ContinuityResult, plan ContinuityPlan, drill ContinuityDrill, now time.Time) {
	add := func(field, state, reason string) {
		result.Diagnostics = append(result.Diagnostics, ContinuityDiagnostic{Code: StatusContinuityRejected, DrillID: drill.ID, PlanID: plan.ID, Field: field, State: state, Version: plan.Version, Reason: reason})
	}
	if strings.TrimSpace(drill.ID) == "" || strings.TrimSpace(drill.EvidenceRef) == "" {
		add("identity", "MISSING", "drill id and evidence reference are required")
	}
	if !drill.Kind.Valid() {
		add("kind", "UNKNOWN", "drill kind is not declared")
	}
	if drill.StartedAt.IsZero() || drill.StartedAt.After(now) {
		add("started_at", "INVALID", "drill start must be recorded and past")
	}
	if drill.NewWorkStoppedAt.IsZero() || drill.NewWorkStoppedAt.Before(drill.StartedAt) {
		add("new_work_stopped_at", "INVALID", "new work must stop after the drill starts")
	}
	if !drill.InFlightPreserved {
		add("in_flight_preserved", "MISSING", "in-flight effects must be preserved")
	}
	if !drill.InFlightReconciled {
		add("in_flight_reconciled", "MISSING", "in-flight effects must reconcile")
	}
	if !drill.CredentialsRevoked {
		add("credentials_revoked", "MISSING", "vendor credentials must be revoked")
	}
	if !drill.FallbackActivated {
		add("fallback_activated", "MISSING", "fallback path must activate")
	}
	if drill.DrainedAt.IsZero() || drill.DrainedAt.Before(drill.NewWorkStoppedAt) {
		add("drained_at", "INVALID", "drain must follow the work stop")
	}
	if drill.RolledBackAt.IsZero() || drill.RolledBackAt.Before(drill.DrainedAt) {
		add("rolled_back_at", "INVALID", "rollback must follow the drain")
	}
	if drill.ReopenedAt.IsZero() || drill.ReopenedAt.Before(drill.RolledBackAt) {
		add("reopened_at", "INVALID", "reopen must follow the rollback")
	}
	if drill.ExceptionID != "" && drill.ExceptionExpiry.IsZero() {
		add("exception_expiry", "MISSING", "a drill exception must expire")
	}
	if drill.ExceptionID != "" && !drill.ExceptionExpiry.IsZero() && !drill.ExceptionExpiry.After(now) {
		add("exception_expiry", "EXPIRED", "drill exception has expired")
	}
}
