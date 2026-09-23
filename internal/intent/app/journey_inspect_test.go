package app

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// journeyInstanceAt returns a durable instance row in the given runtime state.
func journeyInstanceAt(status runtime.InstanceStatus) *runtime.Instance {
	return &runtime.Instance{
		InstanceID:    uuid.MustParse("11111111-2222-4333-8444-555555555555"),
		WorkflowID:    prototype.ApprovalWorkflowID,
		RuntimeStatus: status,
	}
}

// journeyApprovalItemAt returns the routed approval WorkItem at one status.
func journeyApprovalItemAt(status workitem.Status) workitem.WorkItem {
	return workitem.WorkItem{
		WorkItemID: uuid.MustParse("99999999-8888-4777-8666-555555555555"),
		Kind:       workitem.KindApproval,
		NodeID:     prototype.NodeApproval,
		Status:     status,
	}
}

func journeyNodeRow(nodeID string, status runtime.NodeStatus) runtime.NodeExecution {
	return runtime.NodeExecution{NodeID: nodeID, Attempt: 1, Status: status, StepType: workflow.StepEnd}
}

func TestRecordedJourneyInspectionStillRequiresCurrentSubjectAndPayAuthority(t *testing.T) {
	at := time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)
	subject := values.EntityRef{Tenant: "acme-corp", Kind: "worker", Id: "22222222-2222-4222-8222-222222222222"}
	if _, err := authorizeHistoricalJourneyRead(nil, authz.PurposeCompensationReview, subject, values.NewInstant(at), nil); err == nil {
		t.Fatal("recorded journey was readable without a verified principal")
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "acme-corp", Subject: "viewer-1", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "acme", Roles: []string{string(authz.RoleManager)},
		Purposes: []string{authz.PurposeCompensationReview}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-viewer-1",
		IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest-viewer-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorizeHistoricalJourneyRead(principal, authz.PurposeCompensationReview, subject, values.NewInstant(at), nil); err == nil {
		t.Fatal("unassigned manager could read recorded compensation")
	}
	admin, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "acme-corp", Subject: "comp-admin-1", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "acme", Roles: []string{string(authz.RoleCompAdmin)},
		Purposes: []string{authz.PurposeCompensationReview}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-comp-admin-1",
		IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest-comp-admin-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := authorizeHistoricalJourneyRead(admin, authz.PurposeCompensationReview, subject, values.NewInstant(at), nil)
	if err != nil {
		t.Fatalf("authorized compensation administrator cannot inspect durable history: %v", err)
	}
	if !historicalPayAllowed(result) {
		t.Fatal("compensation administrator inspects with masked pay")
	}
}

func TestTodo_RBAC_RT_005_HistoricalInspectionAdmitsOversight(t *testing.T) {
	at := time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)
	subject := values.EntityRef{Tenant: "acme-corp", Kind: "worker", Id: "22222222-2222-4222-8222-222222222222"}
	newPrincipal := func(subjectID string, roles []string, purpose string) *trust.Principal {
		principal, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant: "acme-corp", Subject: subjectID, SubjectKind: trust.SubjectKindHuman,
			OrganizationScopeID: "acme", Roles: roles,
			Purposes: []string{purpose}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance: trust.AssuranceHigh, SessionRef: "session-" + subjectID,
			IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest-" + subjectID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return principal
	}
	// The legacy administrator token evaluates under its mapped template, so
	// the tenant administrator the directory and the transport already admit
	// is no longer refused the inspection that carries diagnostics.
	admin := newPrincipal("hcm-admin-1", []string{"hcm_admin"}, authz.PurposeCompensationReview)
	adminResult, err := authorizeHistoricalJourneyRead(admin, authz.PurposeCompensationReview, subject, values.NewInstant(at), nil)
	if err != nil {
		t.Fatalf("hcm_admin cannot inspect durable history: %v", err)
	}
	if !historicalPayAllowed(adminResult) {
		t.Fatal("hcm_admin inspects with masked pay")
	}
	// The auditor's compensation grant is redacted-only: the inspection is
	// admitted (the subject is administratively disclosable) but raw pay is
	// never granted, so the caller must mask the amounts.
	auditor := newPrincipal("auditor-1", []string{string(authz.RoleAuditor)}, authz.PurposeAuditReview)
	auditorResult, err := authorizeHistoricalJourneyRead(auditor, authz.PurposeAuditReview, subject, values.NewInstant(at), nil)
	if err != nil {
		t.Fatalf("auditor cannot inspect durable history: %v", err)
	}
	if historicalPayAllowed(auditorResult) {
		t.Fatal("auditor inspects with raw pay")
	}
}

func TestApprovalReviewRelationshipsScopeOnlyTheCurrentManagerOwner(t *testing.T) {
	at := time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "acme-corp", Subject: "manager-1", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "acme", Roles: []string{string(authz.RoleManager)},
		Purposes: []string{authz.PurposeCompensationReview}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-manager-1",
		IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest-manager-1",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	subject := values.EntityRef{
		Tenant: "acme-corp",
		Kind:   "worker",
		Id:     "22222222-2222-4222-8222-222222222222",
	}
	item := journeyApprovalItemAt(workitem.StatusAssigned)
	item.NodeID = promotionexec.NodeApproveManager
	item.RecordedAt = at
	item.Assignment.ChosenOwner = principal.Subject()
	assignedAt := values.NewInstant(at.Add(-30 * time.Second))
	item.Assignment.Resolution.ResolvedAt = assignedAt
	caseEffectiveAt := values.NewInstant(at.Add(-10 * time.Minute))

	relationships, err := approvalReviewRelationships(
		principal, subject, caseEffectiveAt, []workitem.WorkItem{item},
	)
	if err != nil {
		t.Fatalf("approvalReviewRelationships: %v", err)
	}
	if len(relationships) != 1 || relationships[0].Kind != authz.RelationshipAssignedPopulation || relationships[0].Subject != subject {
		t.Fatalf("relationship = %+v, want one assigned-population fact for the proposal worker", relationships)
	}
	if err := relationships[0].Validate(); err != nil {
		t.Fatalf("relationship is not recordable: %v", err)
	}
	if start, ok := relationships[0].Effective.StartInstant(); !ok || start != caseEffectiveAt {
		t.Fatalf("relationship starts at %v, %v; want journey creation %v", start, ok, caseEffectiveAt)
	}
	if _, err := authorizeRead(principal, authz.PurposeCompensationReview, authorizationRequest{
		Subject:       subject,
		EvaluatedAt:   caseEffectiveAt,
		Gate:          []authz.FieldID{authz.FieldBaseSalary},
		Relationships: relationships,
	}); err != nil {
		t.Fatalf("the assigned manager cannot review the routed proposal: %v", err)
	}

	item.Assignment.ChosenOwner = "another-manager"
	if got, err := approvalReviewRelationships(principal, subject, caseEffectiveAt, []workitem.WorkItem{item}); err != nil || len(got) != 0 {
		t.Fatalf("another owner's relationship = %+v, %v; want none", got, err)
	}
	item.Assignment.ChosenOwner = principal.Subject()
	item.Status = workitem.StatusCompleted
	if got, err := approvalReviewRelationships(principal, subject, caseEffectiveAt, []workitem.WorkItem{item}); err != nil || len(got) != 0 {
		t.Fatalf("unattributed completed approval relationship = %+v, %v; want none", got, err)
	}
	item.CompletedBy = principal.Subject()
	if got, err := approvalReviewRelationships(principal, subject, caseEffectiveAt, []workitem.WorkItem{item}); err != nil || len(got) != 1 {
		t.Fatalf("recorded reviewer relationship = %+v, %v; want one", got, err)
	}
	if got, err := approvalReviewRelationships(principal, subject, values.Instant{}, []workitem.WorkItem{item}); err == nil || len(got) != 0 {
		t.Fatalf("matched reviewer with unset case time = %+v, %v; want a refusal", got, err)
	}
	item.Status = workitem.StatusAssigned
	item.CompletedBy = ""
	item.NodeID = promotionexec.NodeApproveFinance
	if got, err := approvalReviewRelationships(principal, subject, caseEffectiveAt, []workitem.WorkItem{item}); err != nil || len(got) != 0 {
		t.Fatalf("finance assignment relationship = %+v, %v; want none", got, err)
	}
}

func TestWorkflowHistoryClosedTimeUsesAuthoritativeChronology(t *testing.T) {
	intentTime := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	runtimeTime := intentTime.Add(2 * time.Hour)
	ledgerTime := runtimeTime.Add(3 * time.Minute)
	summary := workspace.JourneySummary{UpdatedAt: intentTime}
	instance := journeyInstanceAt(runtime.InstanceCompleted)
	instance.CompletedAt = &runtimeTime

	applyDurableJourneyTime(&summary, journeyRecord{instance: instance})
	if !summary.UpdatedAt.Equal(runtimeTime) {
		t.Fatalf("runtime-backed closed time = %s, want %s", summary.UpdatedAt, runtimeTime)
	}
	applyDurableJourneyTime(&summary, journeyRecord{
		instance: instance,
		ledger:   &workspace.JourneyLedgerEvent{RecordedAt: ledgerTime},
	})
	if !summary.UpdatedAt.Equal(ledgerTime) {
		t.Fatalf("ledger-backed closed time = %s, want %s", summary.UpdatedAt, ledgerTime)
	}
}

func TestTodo_PROMOUX_011_DurableTimestamp(t *testing.T) {
	intentAt := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	startedAt := intentAt.Add(time.Minute)
	nodeAt := intentAt.Add(2 * time.Minute)
	assignedAt := intentAt.Add(3 * time.Minute)
	decidedAt := intentAt.Add(4 * time.Minute)
	completedAt := intentAt.Add(5 * time.Minute)
	ledgerAt := intentAt.Add(6 * time.Minute)
	instance := journeyInstanceAt(runtime.InstanceRunning)
	instance.StartedAt = &startedAt
	record := journeyRecord{
		instance: instance,
		nodes:    []runtime.NodeExecution{{CompletedAt: &nodeAt}},
		transitions: []workspace.JourneyTransition{
			{To: "ASSIGNED", At: assignedAt},
			{To: "COMPLETED", At: decidedAt},
		},
	}
	summary := workspace.JourneySummary{UpdatedAt: intentAt}
	applyDurableJourneyTime(&summary, record)
	if !summary.UpdatedAt.Equal(decidedAt) {
		t.Fatalf("in-flight approval updated at %s, want decision at %s", summary.UpdatedAt, decidedAt)
	}

	instance.CompletedAt = &completedAt
	record.ledger = &workspace.JourneyLedgerEvent{RecordedAt: ledgerAt}
	applyDurableJourneyTime(&summary, record)
	if !summary.UpdatedAt.Equal(ledgerAt) {
		t.Fatalf("recorded promotion updated at %s, want ledger at %s", summary.UpdatedAt, ledgerAt)
	}

	newerIntentAt := ledgerAt.Add(time.Minute)
	summary.UpdatedAt = newerIntentAt
	applyDurableJourneyTime(&summary, record)
	if !summary.UpdatedAt.Equal(newerIntentAt) {
		t.Fatalf("durable projection regressed from %s to %s", newerIntentAt, summary.UpdatedAt)
	}
}

func TestTodo_PROMOUX_011_SimulationChronology(t *testing.T) {
	intentAt := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	approvalAt := intentAt.Add(2 * time.Hour)
	detail := workspace.JourneyDetail{
		Summary: workspace.JourneySummary{
			IntentID: "intent-1", ProposalRevisionID: "revision-1",
			CreatedAt: intentAt.Add(-time.Minute), UpdatedAt: intentAt,
		},
		Transitions: []workspace.JourneyTransition{{WorkItemID: "item-1", To: "ASSIGNED", At: approvalAt}},
	}
	applyJourneyChronology(&detail, journeyRecord{transitions: detail.Transitions})
	if !detail.Summary.UpdatedAt.Equal(approvalAt) {
		t.Fatalf("detail updated at %s, want approval at %s", detail.Summary.UpdatedAt, approvalAt)
	}
	var simulatedAt time.Time
	for _, event := range detail.Timeline {
		if event.Kind == JourneyEventSimulated {
			simulatedAt = event.At
		}
	}
	if !simulatedAt.Equal(intentAt) {
		t.Fatalf("simulation event moved to %s, want original intent time %s", simulatedAt, intentAt)
	}
}

func TestJourneyWorkItemEventTitleNamesTheBusinessReview(t *testing.T) {
	for _, test := range []struct {
		node, status, want string
	}{
		{promotionexec.NodeApproveFinance, "ASSIGNED", "Finance review assigned"},
		{promotionexec.NodeApproveFinance, "COMPLETED", "Finance review completed"},
		{promotionexec.NodeApproveManager, "IN_PROGRESS", "Manager review started"},
		{promotionexec.NodeApproveManager, "CANCELLED", "Manager review cancelled"},
	} {
		if got := journeyWorkItemEventTitle(test.node, test.status); got != test.want {
			t.Errorf("journeyWorkItemEventTitle(%q, %q) = %q, want %q", test.node, test.status, got, test.want)
		}
	}
}

func TestDeriveJourneyStageBeforeExecution(t *testing.T) {
	if got := deriveJourneyStage("revision-1", journeyRecord{}); got != workspace.JourneyStageProposed {
		t.Fatalf("stage with a minted revision and no instance = %s, want PROPOSED", got)
	}
	if got := deriveJourneyStage("", journeyRecord{}); got != workspace.JourneyStageBlocked {
		t.Fatalf("stage with no minted revision = %s, want BLOCKED", got)
	}
}

func TestTodo_PROMO_EXEC_SERVE_StageProjectionCoversEveryExecutablePromotionNode(t *testing.T) {
	for _, nodeID := range promotionexec.NodeOrder() {
		stage, ok := journeyStageForNode(nodeID)
		if !ok || stage == "" {
			t.Errorf("journeyStageForNode(%q) = %q, %t; want a named stage", nodeID, stage, ok)
		}
	}
	// A run parked on a 1.1.0 provider-confirmation wait has committed and is
	// confirming its effects.
	for _, nodeID := range []string{promotionexec.NodeAwaitPayrollConfirmation, promotionexec.NodeAwaitAccessConfirmation} {
		if stage, _ := journeyStageForNode(nodeID); stage != journeyStageObservingEffects {
			t.Errorf("journeyStageForNode(%q) = %q, want %q", nodeID, stage, journeyStageObservingEffects)
		}
	}
	for _, nodeID := range promotionexec.NodeOrderV1_0() {
		if _, ok := journeyStageForNode(nodeID); !ok {
			t.Errorf("frozen 1.0.0 node %q has no stage", nodeID)
		}
	}
}

func TestDeriveJourneyStageFromDurableExecutionState(t *testing.T) {
	cases := []struct {
		name   string
		record journeyRecord
		want   workspace.JourneyStage
	}{
		{
			name: "parked on an assigned approval",
			record: journeyRecord{
				instance: journeyInstanceAt(runtime.InstanceWaiting),
				nodes:    []runtime.NodeExecution{journeyNodeRow(prototype.NodeApproval, runtime.NodeWaiting)},
				items:    []workitem.WorkItem{journeyApprovalItemAt(workitem.StatusAssigned)},
			},
			want: workspace.JourneyStageAwaitingApproval,
		},
		{
			name: "parked on an available approval",
			record: journeyRecord{
				instance: journeyInstanceAt(runtime.InstanceWaiting),
				items:    []workitem.WorkItem{journeyApprovalItemAt(workitem.StatusAvailable)},
			},
			want: workspace.JourneyStageAwaitingApproval,
		},
		{
			name: "approved terminal reached",
			record: journeyRecord{
				instance: journeyInstanceAt(runtime.InstanceCompleted),
				nodes: []runtime.NodeExecution{
					journeyNodeRow(prototype.NodeApproval, runtime.NodeSucceeded),
					journeyNodeRow(prototype.NodeApproved, runtime.NodeSucceeded),
				},
				items: []workitem.WorkItem{journeyApprovalItemAt(workitem.StatusCompleted)},
			},
			want: workspace.JourneyStageCompleted,
		},
		{
			name: "rejected terminal reached",
			record: journeyRecord{
				instance: journeyInstanceAt(runtime.InstanceCompleted),
				nodes: []runtime.NodeExecution{
					journeyNodeRow(prototype.NodeApproval, runtime.NodeSucceeded),
					journeyNodeRow(prototype.NodeRejected, runtime.NodeSucceeded),
				},
				items: []workitem.WorkItem{journeyApprovalItemAt(workitem.StatusCompleted)},
			},
			want: workspace.JourneyStageRejected,
		},
		{
			name: "cancelled terminal reached",
			record: journeyRecord{
				instance: journeyInstanceAt(runtime.InstanceCancelled),
				nodes:    []runtime.NodeExecution{journeyNodeRow(prototype.NodeCancelled, runtime.NodeSucceeded)},
			},
			want: workspace.JourneyStageFailed,
		},
		{
			name: "expired terminal reached",
			record: journeyRecord{
				instance: journeyInstanceAt(runtime.InstanceCancelled),
				nodes:    []runtime.NodeExecution{journeyNodeRow(prototype.NodeExpired, runtime.NodeSucceeded)},
			},
			want: workspace.JourneyStageFailed,
		},
		{
			name: "invalidated terminal reached",
			record: journeyRecord{
				instance: journeyInstanceAt(runtime.InstanceSuperseded),
				nodes:    []runtime.NodeExecution{journeyNodeRow(prototype.NodeInvalidated, runtime.NodeSucceeded)},
			},
			want: workspace.JourneyStageFailed,
		},
		{
			name:   "instance blocked with nothing open",
			record: journeyRecord{instance: journeyInstanceAt(runtime.InstanceBlocked)},
			want:   workspace.JourneyStageFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveJourneyStage("revision-1", tc.record); got != tc.want {
				t.Fatalf("deriveJourneyStage = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestDeriveJourneyStageOnADecidedButUnresumedInstance pins the one window
// the stage vocabulary has no word for: the approval WorkItem is COMPLETED but
// the driver has not been resumed, so no terminal node has run. Decide commits
// the completion and resumes inside one call, so this is not a stage a journey
// rests in; it is still waiting for its approval to be acted on, and that is
// what the engine reports rather than calling a live instance FAILED.
func TestDeriveJourneyStageOnADecidedButUnresumedInstance(t *testing.T) {
	record := journeyRecord{
		instance: journeyInstanceAt(runtime.InstanceWaiting),
		items:    []workitem.WorkItem{journeyApprovalItemAt(workitem.StatusCompleted)},
	}
	if got := deriveJourneyStage("revision-1", record); got != workspace.JourneyStageAwaitingApproval {
		t.Fatalf("deriveJourneyStage = %s, want AWAITING_APPROVAL for a live, unresumed instance", got)
	}
}

func TestRecordedJourneyTimelineKeepsSimulationWithoutResimulating(t *testing.T) {
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	detail := workspace.JourneyDetail{Summary: workspace.JourneySummary{
		IntentID: "recorded-intent", CreatedAt: at, UpdatedAt: at,
		MaterialDigest: "sha256:recorded-proposal",
	}}
	events := journeyTimeline(detail)
	if len(events) != 2 || events[0].Kind != JourneyEventIntentCreated ||
		events[1].Kind != JourneyEventSimulated || events[1].Ref != detail.Summary.MaterialDigest {
		t.Fatalf("recorded timeline = %+v, want proposal followed by pinned simulation evidence", events)
	}
}

func TestOpenApprovalFindsOnlyTheUndecidedApprovalOnTheApprovalNode(t *testing.T) {
	if _, ok := openApproval(nil); ok {
		t.Fatal("no items must mean no open approval")
	}
	if _, ok := openApproval([]workitem.WorkItem{journeyApprovalItemAt(workitem.StatusCompleted)}); ok {
		t.Fatal("a COMPLETED approval is not open")
	}
	item := journeyApprovalItemAt(workitem.StatusAssigned)
	item.Kind = workitem.KindTask
	if _, ok := openApproval([]workitem.WorkItem{item}); ok {
		t.Fatal("a TASK is never the journey's approval")
	}
	other := journeyApprovalItemAt(workitem.StatusAssigned)
	other.NodeID = "some_other_node"
	if _, ok := openApproval([]workitem.WorkItem{other}); ok {
		t.Fatal("an approval on another node is not this journey's approval")
	}
	for _, status := range []workitem.Status{
		workitem.StatusCreated, workitem.StatusRouted, workitem.StatusAssigned,
		workitem.StatusAvailable, workitem.StatusClaimed, workitem.StatusInProgress,
		workitem.StatusReturned, workitem.StatusEscalated,
	} {
		if _, ok := openApproval([]workitem.WorkItem{journeyApprovalItemAt(status)}); !ok {
			t.Errorf("an approval at %s must still read as open", status)
		}
	}
}

func TestReachedNodeReadsTheDurableNodeExecutions(t *testing.T) {
	nodes := []runtime.NodeExecution{journeyNodeRow(prototype.NodeApproved, runtime.NodeSucceeded)}
	if !reachedNode(nodes, prototype.NodeApproved) {
		t.Fatal("reachedNode must see a successful attempt")
	}
	if reachedNode(nodes, prototype.NodeRejected) {
		t.Fatal("reachedNode must not invent an attempt that was never recorded")
	}
}

// TestReachedNodeIgnoresASkippedSibling pins the reason the status check
// exists: a completed instance carries a row for every terminal the frontier
// could have taken, and only the one that ran is SUCCEEDED.
func TestReachedNodeIgnoresASkippedSibling(t *testing.T) {
	nodes := []runtime.NodeExecution{
		journeyNodeRow(prototype.NodeApproved, runtime.NodeSkipped),
		journeyNodeRow(prototype.NodeRejected, runtime.NodeSucceeded),
	}
	if reachedNode(nodes, prototype.NodeApproved) {
		t.Fatal("a SKIPPED terminal was never reached")
	}
	if !reachedNode(nodes, prototype.NodeRejected) {
		t.Fatal("the SUCCEEDED terminal is the one the instance reached")
	}
}

// TestDeriveJourneyStageReadsTheSucceededTerminalOnly is the same rule at the
// stage level: a rejected promotion whose end_approved row is SKIPPED is
// REJECTED, not COMPLETED.
func TestDeriveJourneyStageReadsTheSucceededTerminalOnly(t *testing.T) {
	record := journeyRecord{
		instance: journeyInstanceAt(runtime.InstanceCompleted),
		nodes: []runtime.NodeExecution{
			journeyNodeRow(prototype.NodeApproval, runtime.NodeSucceeded),
			journeyNodeRow(prototype.NodeApproved, runtime.NodeSkipped),
			journeyNodeRow(prototype.NodeCancelled, runtime.NodeSkipped),
			journeyNodeRow(prototype.NodeExpired, runtime.NodeSkipped),
			journeyNodeRow(prototype.NodeInvalidated, runtime.NodeSkipped),
			journeyNodeRow(prototype.NodeRejected, runtime.NodeSucceeded),
		},
	}
	if got := deriveJourneyStage("revision-1", record); got != workspace.JourneyStageRejected {
		t.Fatalf("deriveJourneyStage = %s, want REJECTED", got)
	}
}

func TestJourneyNodesProjectEveryDurableRow(t *testing.T) {
	started := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	completed := started.Add(time.Minute)
	rows := []runtime.NodeExecution{{
		NodeID: prototype.NodeApproval, Attempt: 1, StepType: workflow.StepApproval,
		Status: runtime.NodeSucceeded, TraceID: "trace-1",
		StartedAt: &started, CompletedAt: &completed, RecordedAt: completed,
	}}
	got := journeyNodes(rows)
	if len(got) != 1 {
		t.Fatalf("journeyNodes returned %d rows, want 1", len(got))
	}
	node := got[0]
	if node.NodeID != prototype.NodeApproval || node.Attempt != 1 ||
		node.StepType != string(workflow.StepApproval) || node.Status != string(runtime.NodeSucceeded) ||
		node.TraceID != "trace-1" {
		t.Fatalf("journeyNodes projected %+v", node)
	}
	if node.StartedAt == nil || !node.StartedAt.Equal(started) ||
		node.CompletedAt == nil || !node.CompletedAt.Equal(completed) {
		t.Fatalf("journeyNodes lost the node's own instants: %+v", node)
	}
}

func TestUtcPtrNormalizesWithoutAliasing(t *testing.T) {
	if utcPtr(nil) != nil {
		t.Fatal("utcPtr(nil) must stay nil")
	}
	zone := time.FixedZone("test", 3600)
	at := time.Date(2026, 9, 3, 13, 0, 0, 0, zone)
	got := utcPtr(&at)
	if got == &at {
		t.Fatal("utcPtr must not alias its argument")
	}
	if got.Location() != time.UTC || !got.Equal(at) {
		t.Fatalf("utcPtr = %v, want the same instant in UTC", got)
	}
}

func TestJourneyTimelineIsChronologicalAndCausal(t *testing.T) {
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	instanceStarted := base.Add(2 * time.Minute)
	nodeDone := base.Add(4 * time.Minute)
	ledgerAt := base.Add(5 * time.Minute)
	detail := workspace.JourneyDetail{
		Summary: workspace.JourneySummary{
			IntentID: "intent:1", ProposalRevisionID: "revision-1", MaterialDigest: "sha256:abc",
			CreatedAt: base, UpdatedAt: base.Add(time.Minute),
		},
		Instance: &workspace.JourneyInstance{
			InstanceID: "instance-1", Status: "COMPLETED",
			CreatedAt: instanceStarted, StartedAt: &instanceStarted,
		},
		Nodes: []workspace.JourneyNode{{
			NodeID: prototype.NodeApproved, Status: "SUCCEEDED",
			CompletedAt: &nodeDone, RecordedAt: nodeDone,
		}},
		Transitions: []workspace.JourneyTransition{{
			WorkItemID: "item-1", From: "IN_PROGRESS", To: "COMPLETED",
			Actor: "user-1", Reason: journeyReasonDecided, At: base.Add(3 * time.Minute),
		}},
		Ledger: &workspace.JourneyLedgerEvent{
			StreamKey: "workflow:x:instance-1", SchemaRef: "schema/v2", RecordedAt: ledgerAt,
		},
	}
	timeline := journeyTimeline(detail)
	wantKinds := []string{
		JourneyEventIntentCreated, JourneyEventSimulated, JourneyEventInstanceStarted,
		JourneyEventWorkItem, JourneyEventNode, JourneyEventLedgerRecorded,
	}
	if len(timeline) != len(wantKinds) {
		t.Fatalf("timeline = %d entries, want %d: %+v", len(timeline), len(wantKinds), timeline)
	}
	for i, want := range wantKinds {
		if timeline[i].Kind != want {
			t.Fatalf("timeline[%d].Kind = %s, want %s (full: %+v)", i, timeline[i].Kind, want, timeline)
		}
	}
	for i := 1; i < len(timeline); i++ {
		if timeline[i].At.Before(timeline[i-1].At) {
			t.Fatalf("timeline is not chronological at %d: %v before %v", i, timeline[i].At, timeline[i-1].At)
		}
	}
	if timeline[3].Actor != "user-1" {
		t.Fatalf("the work-item entry lost its actor: %+v", timeline[3])
	}
}

func TestJourneyTimelineBreaksTiesInCausalOrder(t *testing.T) {
	// Every instant is the same pinned clock reading, which is exactly what a
	// deterministic composition produces: the entries must still read in the
	// order the journey actually happened.
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	detail := workspace.JourneyDetail{
		Summary:     workspace.JourneySummary{IntentID: "i", ProposalRevisionID: "r", CreatedAt: at, UpdatedAt: at},
		Instance:    &workspace.JourneyInstance{InstanceID: "n", CreatedAt: at, StartedAt: &at},
		Nodes:       []workspace.JourneyNode{{NodeID: prototype.NodeApproval, RecordedAt: at}},
		Transitions: []workspace.JourneyTransition{{WorkItemID: "w", To: "COMPLETED", At: at}},
		Ledger:      &workspace.JourneyLedgerEvent{StreamKey: "s", RecordedAt: at},
	}
	timeline := journeyTimeline(detail)
	want := []string{
		JourneyEventIntentCreated, JourneyEventSimulated, JourneyEventInstanceStarted,
		JourneyEventNode, JourneyEventWorkItem, JourneyEventLedgerRecorded,
	}
	for i, kind := range want {
		if timeline[i].Kind != kind {
			t.Fatalf("timeline[%d].Kind = %s, want %s", i, timeline[i].Kind, kind)
		}
	}
}

func TestTodo_PROMOUX_011_UnstartedInstanceHasNoStartedEvent(t *testing.T) {
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	timeline := journeyTimeline(workspace.JourneyDetail{
		Summary:  workspace.JourneySummary{IntentID: "i", CreatedAt: at},
		Instance: &workspace.JourneyInstance{InstanceID: "n", Status: "CREATED", CreatedAt: at},
	})
	if len(timeline) != 1 || timeline[0].Kind != JourneyEventIntentCreated {
		t.Fatalf("unstarted instance claimed a start: %+v", timeline)
	}
}

func TestJourneyTimelineOmitsWhatNeverHappened(t *testing.T) {
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	timeline := journeyTimeline(workspace.JourneyDetail{
		Summary: workspace.JourneySummary{IntentID: "i", CreatedAt: at},
	})
	if len(timeline) != 1 || timeline[0].Kind != JourneyEventIntentCreated {
		t.Fatalf("an unsimulated, unexecuted journey has one entry; got %+v", timeline)
	}
}

func TestJourneyWorkItemEventTitleMakesApprovalStagesDistinct(t *testing.T) {
	for _, test := range []struct {
		node, status, want string
	}{
		{promotionexec.NodeApproveFinance, "ASSIGNED", "Finance review assigned"},
		{promotionexec.NodeApproveFinance, "COMPLETED", "Finance review completed"},
		{promotionexec.NodeApproveManager, "IN_PROGRESS", "Manager review started"},
		{promotionexec.NodeApproveManager, "COMPLETED", "Manager review completed"},
	} {
		if got := journeyWorkItemEventTitle(test.node, test.status); got != test.want {
			t.Errorf("journeyWorkItemEventTitle(%q, %q) = %q, want %q", test.node, test.status, got, test.want)
		}
	}
}

func TestBeginTenantRefusesWithoutAnExecutionDatabase(t *testing.T) {
	engine := newJourneyEngine(&IntentService{}, nil, "", nil, nil)
	if _, err := engine.beginTenant(t.Context(), nil); err == nil {
		t.Fatal("a journey engine with no database must refuse to open a read transaction")
	}
}
