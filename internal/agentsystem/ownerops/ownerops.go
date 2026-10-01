package ownerops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrDenied  = errors.New("ownerops: access denied")
	ErrInvalid = errors.New("ownerops: invalid request")
)

// Audience is the operational view requested by an authenticated principal.
type Audience string

const (
	AudienceMember   Audience = "MEMBER"
	AudienceOwner    Audience = "OWNER"
	AudienceOperator Audience = "OPERATOR"
)

const (
	CapabilityRead        = "agent.operations.read"
	CapabilityPause       = "agent.installation.pause"
	CapabilityQuarantine  = "agent.operations.quarantine"
	PurposeOwnerDashboard = "agent.owner_dashboard"
	PurposeOperatorOps    = "platform.agent_operations"
)

// Scope is resolved by the caller's authorization layer. Capabilities must be
// current decisions, not values copied from a request payload.
type Scope struct {
	Audience     Audience
	TenantID     string
	SubjectID    string
	Purpose      string
	Capabilities []string
}

// RunRecord is a content-bearing input record. Content-bearing fields exist so
// callers can pass durable records directly; Project deliberately never copies
// them into an operational view.
type RunRecord struct {
	TenantID, OwnerID, UserID                  string
	TaskID, AgentID, Version, InstallationID   string
	ScheduleID, CauseKind, CauseID             string
	State, FailureCode, FailureDetail          string
	Goal, Prompt, SourceContent, ProviderError string
	QueueLag                                   time.Duration
	SpendMicros                                int64
	DenialCodes                                []string
	CitationCount                              int
	EvaluationStatus                           string
	IncidentID, IncidentStatus                 string
	StartedAt, FinishedAt                      time.Time
}

// ScheduleRecord contains only the facts required for schedule health.
type ScheduleRecord struct {
	TenantID, OwnerID, AgentID, ScheduleID string
	State, HealthCode                      string
	NextFire, LastFire                     time.Time
	QueueLag                               time.Duration
}

// Snapshot is supplied by authorized durable owners, never assembled from
// general telemetry or free-form logs.
type Snapshot struct {
	Runs      []RunRecord
	Schedules []ScheduleRecord
}

// Dashboard contains the least operational data needed for its audience.
type Dashboard struct {
	Audience  Audience
	Runs      []RunView
	Schedules []ScheduleView
	Aggregate Aggregate
}

// RunView is intentionally free of task goals, prompt text, source content,
// user IDs, arbitrary errors, and citation bodies.
type RunView struct {
	TaskID, AgentID, Version, InstallationID  string
	ScheduleID, CauseKind, State, FailureCode string
	QueueLag                                  time.Duration
	SpendMicros                               int64
	DenialCodes                               []string
	CitationCount                             int
	EvaluationStatus                          string
	IncidentID, IncidentStatus                string
	StartedAt, FinishedAt                     time.Time
}

// ScheduleView reports timing and bounded status codes without trigger payloads.
type ScheduleView struct {
	AgentID, ScheduleID, State, HealthCode string
	NextFire, LastFire                     time.Time
	QueueLag                               time.Duration
}

// Aggregate is safe for platform operators: it contains no tenant, agent,
// user, installation, schedule, run, or incident identifiers.
type Aggregate struct {
	RunCount, FailedRuns, ActiveRuns, IncidentRuns int
	SpendMicros                                    int64
	QueueLagTotal                                  time.Duration
	ScheduleCount, UnhealthySchedules              int
}

// Project returns rows only for the principal's own tenant and ownership
// scope. Operators receive aggregates across the supplied authorized corpus.
func Project(scope Scope, snapshot Snapshot) (Dashboard, error) {
	if !has(scope.Capabilities, CapabilityRead) || scope.SubjectID == "" {
		return Dashboard{}, ErrDenied
	}
	dashboard := Dashboard{Audience: scope.Audience, Runs: []RunView{}, Schedules: []ScheduleView{}}
	switch scope.Audience {
	case AudienceMember:
		if scope.TenantID == "" || scope.Purpose != PurposeOwnerDashboard {
			return Dashboard{}, ErrDenied
		}
		for _, run := range snapshot.Runs {
			if run.TenantID == scope.TenantID && run.UserID == scope.SubjectID {
				dashboard.Runs = append(dashboard.Runs, viewRun(run))
			}
		}
	case AudienceOwner:
		if scope.TenantID == "" || scope.Purpose != PurposeOwnerDashboard {
			return Dashboard{}, ErrDenied
		}
		for _, run := range snapshot.Runs {
			if run.TenantID == scope.TenantID && run.OwnerID == scope.SubjectID {
				dashboard.Runs = append(dashboard.Runs, viewRun(run))
			}
		}
		for _, schedule := range snapshot.Schedules {
			if schedule.TenantID == scope.TenantID && schedule.OwnerID == scope.SubjectID {
				dashboard.Schedules = append(dashboard.Schedules, viewSchedule(schedule))
			}
		}
	case AudienceOperator:
		if scope.Purpose != PurposeOperatorOps {
			return Dashboard{}, ErrDenied
		}
		for _, run := range snapshot.Runs {
			addRunAggregate(&dashboard.Aggregate, run)
		}
		for _, schedule := range snapshot.Schedules {
			addScheduleAggregate(&dashboard.Aggregate, schedule)
		}
	default:
		return Dashboard{}, ErrDenied
	}
	return dashboard, nil
}

func viewRun(run RunRecord) RunView {
	return RunView{TaskID: run.TaskID, AgentID: run.AgentID, Version: run.Version,
		InstallationID: run.InstallationID, ScheduleID: run.ScheduleID,
		CauseKind: safeCode(run.CauseKind), State: safeCode(run.State),
		FailureCode: safeCode(run.FailureCode), QueueLag: nonnegative(run.QueueLag),
		SpendMicros: nonnegativeInt(run.SpendMicros), DenialCodes: safeCodes(run.DenialCodes),
		CitationCount: max(0, run.CitationCount), EvaluationStatus: safeCode(run.EvaluationStatus),
		IncidentID: run.IncidentID, IncidentStatus: safeCode(run.IncidentStatus),
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt}
}

func viewSchedule(schedule ScheduleRecord) ScheduleView {
	return ScheduleView{AgentID: schedule.AgentID, ScheduleID: schedule.ScheduleID,
		State: safeCode(schedule.State), HealthCode: safeCode(schedule.HealthCode),
		NextFire: schedule.NextFire, LastFire: schedule.LastFire, QueueLag: nonnegative(schedule.QueueLag)}
}

func addRunAggregate(aggregate *Aggregate, run RunRecord) {
	aggregate.RunCount++
	state := safeCode(run.State)
	if state == "FAILED" {
		aggregate.FailedRuns++
	}
	if state == "RUNNING" || state == "WAITING" {
		aggregate.ActiveRuns++
	}
	if run.IncidentID != "" {
		aggregate.IncidentRuns++
	}
	aggregate.SpendMicros = addInt64Saturated(aggregate.SpendMicros, nonnegativeInt(run.SpendMicros))
	aggregate.QueueLagTotal = addDurationSaturated(aggregate.QueueLagTotal, nonnegative(run.QueueLag))
}

func addScheduleAggregate(aggregate *Aggregate, schedule ScheduleRecord) {
	aggregate.ScheduleCount++
	health := safeCode(schedule.HealthCode)
	if health != "" && health != "HEALTHY" {
		aggregate.UnhealthySchedules++
	}
}

func safeCodes(codes []string) []string {
	result := make([]string, 0, len(codes))
	for _, code := range codes {
		if value := safeCode(code); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func safeCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) > 48 {
		return "OTHER"
	}
	for _, r := range value {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return "OTHER"
		}
	}
	return value
}

func nonnegative(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}
func nonnegativeInt(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func addInt64Saturated(left, right int64) int64 {
	if right > 0 && left > int64(^uint64(0)>>1)-right {
		return int64(^uint64(0) >> 1)
	}
	return left + right
}

func addDurationSaturated(left, right time.Duration) time.Duration {
	max := time.Duration(int64(^uint64(0) >> 1))
	if right > 0 && left > max-right {
		return max
	}
	return left + right
}

func has(capabilities []string, expected string) bool {
	for _, capability := range capabilities {
		if capability == expected {
			return true
		}
	}
	return false
}

// StopKind identifies the limited operational stop this package can request.
type StopKind string

const (
	PauseInstallation StopKind = "PAUSE_INSTALLATION"
	QuarantineVersion StopKind = "QUARANTINE_VERSION"
	PauseTask         StopKind = "PAUSE_TASK"
)

// StopRequest is revisioned and idempotency-keyed. It must identify one tenant
// and one installation or version; wildcard fleet stops are not accepted here.
type StopRequest struct {
	Kind                                                            StopKind
	TenantID, OwnerID, InstallationID, AgentID, Version, IncidentID string
	RequestID                                                       string
	ExpectedRevision                                                uint64
	TaskID                                                          string
	Reason                                                          string
}

// StopCommand is the fully resolved, auditable instruction given to the owner.
type StopCommand struct {
	StopRequest
	ActorID  string
	Purpose  string
	Audience Audience
}

// StopController is implemented by the agent lifecycle owner. Implementations
// must persist audit evidence and fence affected work before returning success.
type StopController interface {
	ApplyStop(context.Context, StopCommand) (string, error)
}

// Stop validates current scope before invoking the durable lifecycle owner.
func Stop(ctx context.Context, scope Scope, request StopRequest, controller StopController) (string, error) {
	command, err := authorizeStop(scope, request)
	if err != nil {
		return "", err
	}
	if controller == nil {
		return "", fmt.Errorf("%w: stop controller is required", ErrInvalid)
	}
	return controller.ApplyStop(ctx, command)
}

func authorizeStop(scope Scope, request StopRequest) (StopCommand, error) {
	if scope.SubjectID == "" || request.TenantID == "" || request.RequestID == "" || request.ExpectedRevision == 0 || request.IncidentID == "" {
		return StopCommand{}, ErrInvalid
	}
	command := StopCommand{StopRequest: request, ActorID: scope.SubjectID, Audience: scope.Audience}
	switch request.Kind {
	case PauseTask:
		if strings.TrimSpace(request.Reason) == "" {
			return StopCommand{}, ErrInvalid
		}
		if scope.TenantID != request.TenantID || request.TaskID == "" || !has(scope.Capabilities, CapabilityPause) || (scope.Audience != AudienceOperator && scope.Audience != AudienceOwner && scope.Audience != AudienceMember) {
			return StopCommand{}, ErrDenied
		}
		if scope.Audience == AudienceOperator {
			if scope.Purpose != PurposeOperatorOps {
				return StopCommand{}, ErrDenied
			}
			command.Purpose = PurposeOperatorOps
		} else {
			if scope.Purpose != PurposeOwnerDashboard {
				return StopCommand{}, ErrDenied
			}
			command.Purpose = PurposeOwnerDashboard
		}
	case PauseInstallation:
		if scope.Audience != AudienceOwner || scope.Purpose != PurposeOwnerDashboard || scope.TenantID != request.TenantID || request.OwnerID != scope.SubjectID || request.InstallationID == "" || !has(scope.Capabilities, CapabilityPause) {
			return StopCommand{}, ErrDenied
		}
		command.Purpose = PurposeOwnerDashboard
	case QuarantineVersion:
		if scope.Audience != AudienceOperator || scope.Purpose != PurposeOperatorOps || request.AgentID == "" || request.Version == "" || !has(scope.Capabilities, CapabilityQuarantine) {
			return StopCommand{}, ErrDenied
		}
		command.Purpose = PurposeOperatorOps
	default:
		return StopCommand{}, ErrInvalid
	}
	return command, nil
}
