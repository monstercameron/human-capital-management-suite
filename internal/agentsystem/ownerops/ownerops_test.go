package ownerops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTodo_AGENT_041(t *testing.T) {
	snapshot := Snapshot{
		Runs: []RunRecord{
			{TenantID: "tenant-a", OwnerID: "owner-a", UserID: "user-a", TaskID: "task-a", AgentID: "agent-a", Version: "v2", InstallationID: "install-a", State: "FAILED", FailureCode: "PROVIDER_TIMEOUT", FailureDetail: "secret error", Goal: "secret goal", Prompt: "secret prompt", SourceContent: "secret source", ProviderError: "secret provider", SpendMicros: 17, QueueLag: time.Second, DenialCodes: []string{"GRANT_MISSING"}, CitationCount: 2, EvaluationStatus: "PASSED", IncidentID: "incident-a", IncidentStatus: "OPEN"},
			{TenantID: "tenant-b", OwnerID: "owner-b", UserID: "user-b", TaskID: "task-b", State: "RUNNING", SpendMicros: 50},
		},
		Schedules: []ScheduleRecord{{TenantID: "tenant-a", OwnerID: "owner-a", AgentID: "agent-a", ScheduleID: "schedule-a", State: "ACTIVE", HealthCode: "LAGGING", QueueLag: 2 * time.Second}},
	}
	owner := Scope{Audience: AudienceOwner, TenantID: "tenant-a", SubjectID: "owner-a", Purpose: PurposeOwnerDashboard, Capabilities: []string{CapabilityRead}}
	dashboard, err := Project(owner, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Runs) != 1 || dashboard.Runs[0].TaskID != "task-a" || len(dashboard.Schedules) != 1 || dashboard.Schedules[0].ScheduleID != "schedule-a" {
		t.Fatalf("owner scope did not project its records: %+v", dashboard)
	}
	if dashboard.Runs[0].FailureCode != "PROVIDER_TIMEOUT" || dashboard.Runs[0].SpendMicros != 17 || dashboard.Runs[0].IncidentID != "incident-a" {
		t.Fatalf("operational evidence missing: %+v", dashboard.Runs[0])
	}
	encoded, err := json.Marshal(dashboard)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret goal", "secret prompt", "secret source", "secret provider", "secret error", "user-a"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("dashboard leaked %q: %s", secret, encoded)
		}
	}
}

func TestTodo_AGENT_041_Security(t *testing.T) {
	snapshot := Snapshot{Runs: []RunRecord{
		{TenantID: "tenant-a", OwnerID: "owner-a", UserID: "user-a", TaskID: "own"},
		{TenantID: "tenant-b", OwnerID: "owner-b", UserID: "user-a", TaskID: "foreign-tenant"},
		{TenantID: "tenant-a", OwnerID: "owner-b", UserID: "user-b", TaskID: "foreign-owner"},
	}}
	tests := []struct {
		name   string
		scope  Scope
		want   int
		denied bool
	}{
		{name: "member sees own tenant task", scope: Scope{Audience: AudienceMember, TenantID: "tenant-a", SubjectID: "user-a", Purpose: PurposeOwnerDashboard, Capabilities: []string{CapabilityRead}}, want: 1},
		{name: "owner cannot read another tenant", scope: Scope{Audience: AudienceOwner, TenantID: "tenant-b", SubjectID: "owner-a", Purpose: PurposeOwnerDashboard, Capabilities: []string{CapabilityRead}}, want: 0},
		{name: "missing read grant", scope: Scope{Audience: AudienceOwner, TenantID: "tenant-a", SubjectID: "owner-a", Purpose: PurposeOwnerDashboard}, denied: true},
		{name: "operator requires operator purpose", scope: Scope{Audience: AudienceOperator, SubjectID: "ops", Purpose: PurposeOwnerDashboard, Capabilities: []string{CapabilityRead}}, denied: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			view, err := Project(test.scope, snapshot)
			if test.denied {
				if !errors.Is(err, ErrDenied) {
					t.Fatalf("error = %v, want ErrDenied", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Runs) != test.want {
				t.Fatalf("runs = %d, want %d", len(view.Runs), test.want)
			}
		})
	}
	operator := Scope{Audience: AudienceOperator, SubjectID: "ops", Purpose: PurposeOperatorOps, Capabilities: []string{CapabilityRead}}
	view, err := Project(operator, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Runs) != 0 || view.Aggregate.RunCount != 3 {
		t.Fatalf("operator projection = %+v", view)
	}
	encoded, _ := json.Marshal(view)
	for _, id := range []string{"tenant-a", "tenant-b", "own", "owner-a", "user-a"} {
		if strings.Contains(string(encoded), id) {
			t.Fatalf("operator projection leaked %q: %s", id, encoded)
		}
	}
}

func TestTodo_AGENT_041_AggregatesNormalizeAndSaturate(t *testing.T) {
	maxInt64 := int64(^uint64(0) >> 1)
	operator := Scope{Audience: AudienceOperator, SubjectID: "ops", Purpose: PurposeOperatorOps, Capabilities: []string{CapabilityRead}}
	snapshot := Snapshot{
		Runs: []RunRecord{
			{State: " failed ", SpendMicros: maxInt64, QueueLag: time.Duration(maxInt64)},
			{State: "WAITING", SpendMicros: 1, QueueLag: time.Nanosecond},
		},
		Schedules: []ScheduleRecord{
			{HealthCode: " healthy "},
			{HealthCode: " lagging "},
		},
	}
	dashboard, err := Project(operator, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Aggregate.FailedRuns != 1 || dashboard.Aggregate.ActiveRuns != 1 {
		t.Fatalf("aggregate state counts = %+v", dashboard.Aggregate)
	}
	if dashboard.Aggregate.SpendMicros != maxInt64 {
		t.Fatalf("spend overflowed: %d", dashboard.Aggregate.SpendMicros)
	}
	if dashboard.Aggregate.QueueLagTotal != time.Duration(maxInt64) {
		t.Fatalf("queue lag overflowed: %s", dashboard.Aggregate.QueueLagTotal)
	}
	if dashboard.Aggregate.UnhealthySchedules != 1 {
		t.Fatalf("schedule health counts = %+v", dashboard.Aggregate)
	}
}

func TestTodo_AGENT_041_ControlPort(t *testing.T) {
	controller := &stopRecorder{}
	scope := Scope{Audience: AudienceOwner, TenantID: "tenant-a", SubjectID: "owner-a", Purpose: PurposeOwnerDashboard, Capabilities: []string{CapabilityPause}}
	request := StopRequest{Kind: PauseInstallation, TenantID: "tenant-a", OwnerID: "owner-a", InstallationID: "install-a", IncidentID: "incident-a", RequestID: "request-a", ExpectedRevision: 4}
	auditID, err := Stop(context.Background(), scope, request, controller)
	if err != nil {
		t.Fatal(err)
	}
	if auditID != "audit-a" || controller.calls != 1 || controller.command.InstallationID != "install-a" || controller.command.ActorID != "owner-a" {
		t.Fatalf("stop result=%q calls=%d command=%+v", auditID, controller.calls, controller.command)
	}
	request.TenantID = "tenant-b"
	if _, err := Stop(context.Background(), scope, request, controller); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-tenant stop error = %v", err)
	}
	if controller.calls != 1 {
		t.Fatalf("denied stop reached controller: calls=%d", controller.calls)
	}
	operator := Scope{Audience: AudienceOperator, SubjectID: "operator", Purpose: PurposeOperatorOps, Capabilities: []string{CapabilityQuarantine}}
	request = StopRequest{Kind: QuarantineVersion, TenantID: "tenant-a", AgentID: "agent-a", Version: "3", IncidentID: "incident-a", RequestID: "request-b", ExpectedRevision: 8}
	if _, err := Stop(context.Background(), operator, request, controller); err != nil {
		t.Fatal(err)
	}
	if controller.calls != 2 || controller.command.Kind != QuarantineVersion {
		t.Fatalf("operator quarantine not applied: %+v", controller)
	}
	request.TenantID = ""
	if _, err := Stop(context.Background(), operator, request, controller); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unscoped quarantine error = %v", err)
	}
}

type stopRecorder struct {
	calls   int
	command StopCommand
}

func (recorder *stopRecorder) ApplyStop(_ context.Context, command StopCommand) (string, error) {
	recorder.calls++
	recorder.command = command
	return "audit-a", nil
}
