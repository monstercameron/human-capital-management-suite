package app

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/leave"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/workreview"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

func servedLeaveProcess(t *testing.T) leave.ProcessRequest {
	t.Helper()
	start, err := values.ParseLocalDate("2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2026-10-08")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "business", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	process, err := leave.Bind(leave.RequestLeave{
		WorkerID: "worker:abc-123", LeaveType: "medical", Interval: interval,
		Mode: leave.ModeContinuous, Reason: "planned absence", ExpectedWorkerRevision: "rev:worker:v1",
		ClientRequestID: "client-1234", EvidenceRefs: []string{"evidence:request-1"},
	}, leave.TrustedContext{TenantID: values.TenantId("tenant-a"), OrganizationScopeID: "org:1", PrincipalID: "principal:1"})
	if err != nil {
		t.Fatal(err)
	}
	return process
}

func servedLeaveGateway(t *testing.T) (*capability.Registry, *MemoryEvidenceSink, *capability.Gateway, *domainHandlers) {
	t.Helper()
	handlers := &domainHandlers{}
	registry, err := newCapabilityRegistry(handlers)
	if err != nil {
		t.Fatal(err)
	}
	sink := NewMemoryEvidenceSink()
	return registry, sink, capability.NewGateway(registry, sink), handlers
}

func servedLeaveAuthorization(scope string) capability.Authorization {
	return capability.Authorization{Decision: capability.Allow, Scopes: []string{scope}, Tenant: "tenant-a", SubjectRef: "worker:abc-123"}
}

const servedLeaveScope = "scope:workforce.leave.request"

// TestTodo_LEAVE_016_Served proves the shipped application registry routes
// the Leave request capability to the real idempotent anchor, preserving the
// zero-workforce/effect boundary and replay evidence.
func TestTodo_LEAVE_016_Served(t *testing.T) {
	registry, sink, gateway, handlers := servedLeaveGateway(t)
	process := servedLeaveProcess(t)
	key := capability.Key{ID: LeaveRequestCapabilityID, Version: 1}
	if _, ok := registry.Lookup(key); !ok {
		t.Fatal("served Leave request capability is not published")
	}
	first, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability: key, Payload: process, Authorization: servedLeaveAuthorization(servedLeaveScope),
	})
	if err != nil {
		t.Fatalf("first Leave request invocation: %v", err)
	}
	created, ok := first.Response.(LeaveRequestCapabilityResult)
	if !ok || !created.Created {
		t.Fatalf("first response=%T %+v, want created anchor", first.Response, first.Response)
	}
	if err := created.Record.Verify(); err != nil {
		t.Fatalf("served anchor does not verify: %v", err)
	}
	counts := handlers.leaveAnchors.CountByCategory()
	if counts[leave.AnchorWriteIntent] != 1 || counts[leave.AnchorWriteRequest] != 1 || counts[leave.AnchorWriteChronology] != 1 {
		t.Fatalf("expected the three logical anchor writes, got %v", counts)
	}

	second, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability: key, Payload: process, Authorization: servedLeaveAuthorization(servedLeaveScope),
	})
	if err != nil {
		t.Fatalf("replay Leave request invocation: %v", err)
	}
	replayed, ok := second.Response.(LeaveRequestCapabilityResult)
	if !ok || replayed.Created || replayed.Record.IntentID != created.Record.IntentID || replayed.Record.Digest != created.Record.Digest {
		t.Fatalf("replay response=%+v, want the original anchor without creation", second.Response)
	}
	if sink.Len() != 2 {
		t.Fatalf("evidence records=%d, want one decision per invocation", sink.Len())
	}
}

// TestTodo_LEAVE_014_Served proves determination rendering and delivery are
// reachable through the same shipped capability registry and gateway.
func TestTodo_LEAVE_014_Served(t *testing.T) {
	registry, _, gateway, _ := servedLeaveGateway(t)
	key := capability.Key{ID: LeaveRequestCapabilityID, Version: 1}
	if _, ok := registry.Lookup(key); !ok {
		t.Fatal("served Leave determination capability is not published")
	}
	input := leave.DeterminationInput{
		ProposalDigest: "sha256:proposal", LegalRelease: "fmla-2026.1",
		Programs: []leave.ProgramResult{{ProgramID: "fmla", Authority: leave.AuthorityStatutory, Release: "fmla-2026.1", Result: leave.ProgramEligible}},
		Interval: "2026-10-01/2026-10-08", PaidHours: 64, Rights: []string{"job-protection"}, Obligations: []string{"recertify"},
		Explanation: "FMLA covers eight continuous days", TemplateID: "determination-letter", TemplateVersion: "v4", TemplateApproved: true,
		Locale: "en-US", Recipient: "worker:abc-123", BlockingNoticeID: "notice:leave-1",
	}
	assessment, err := delivery.AssessRequirement(delivery.NoticeRequirement{
		ID: "notice:leave-1", Recipient: "worker:abc-123", RecipientVerified: true, RecipientProof: "proof:recipient",
		ContentDigest: "sha256:determination", ContentVersion: "v4", Timestamp: 1700000000,
		JurisdictionRule: "fmla/v1", AckProof: "ack:worker", SignatureDigest: "sha256:ceremony",
	}, map[string]bool{"fmla/v1": true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability:    key,
		Payload:       LeaveDeterminationInvocation{Input: input, Assessment: assessment, ProviderAccepted: true, Acknowledged: true},
		Authorization: servedLeaveAuthorization(servedLeaveScope),
	})
	if err != nil {
		t.Fatalf("served determination invocation: %v", err)
	}
	determination, ok := result.Response.(leave.Determination)
	if !ok || determination.DeliveryState != leave.DeterminationAcknowledged || !determination.LeaveStartAllowed {
		t.Fatalf("determination=%T %+v, want acknowledged and start-allowed", result.Response, result.Response)
	}
	if err := determination.Verify(input); err != nil {
		t.Fatalf("served determination seal: %v", err)
	}
}

// TestTodo_LEAVE_017_Served proves the served adapter preserves the restricted
// request-more-information and exactly-once governed-evidence resume loop.
func TestTodo_LEAVE_017_Served(t *testing.T) {
	_, _, gateway, _ := servedLeaveGateway(t)
	key := capability.Key{ID: LeaveRequestCapabilityID, Version: 1}
	loop, err := leave.OpenLoop("loop:served", "task:served", "leave-administrator", "policy:review:v1")
	if err != nil {
		t.Fatal(err)
	}
	finding := workreview.Finding{TaskID: "task:served", Verdict: workreview.ReviewMoreInfo, RequirementID: "req:certification", Reason: "evidence-partial"}
	requestedResult, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability: key, Payload: LeaveEvidenceReviewInvocation{Loop: loop, Finding: finding},
		Authorization: servedLeaveAuthorization(servedLeaveScope),
	})
	if err != nil {
		t.Fatalf("served request-more-information invocation: %v", err)
	}
	requested, ok := requestedResult.Response.(leave.ReviewLoop)
	if !ok || requested.State != leave.LoopMoreInfo || requested.Message.MessageID == "" || requested.SignalID == "" {
		t.Fatalf("requested=%T %+v, want one message and signal", requestedResult.Response, requestedResult.Response)
	}
	resumedResult, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability:    key,
		Payload:       LeaveEvidenceReviewInvocation{Loop: requested, Finding: finding, Resume: &leave.EvidenceRef{Ref: "evidence:served", Quarantined: true, Classified: true, AuthorityCurrent: true}},
		Authorization: servedLeaveAuthorization(servedLeaveScope),
	})
	if err != nil {
		t.Fatalf("served evidence resume invocation: %v", err)
	}
	resumed, ok := resumedResult.Response.(leave.ReviewLoop)
	if !ok || resumed.State != leave.LoopResumed || resumed.ResumeCount != 1 {
		t.Fatalf("resumed=%T %+v, want one governed resume", resumedResult.Response, resumedResult.Response)
	}
	if err := resumed.Verify(); err != nil {
		t.Fatalf("served review-loop seal: %v", err)
	}
}

// TestTodo_LEAVE_015_Served proves the exploratory trace capability is
// published with an exact version and rejects an untyped payload before any
// trace stage can be entered.
func TestTodo_LEAVE_015_Served(t *testing.T) {
	registry, _, gateway, _ := servedLeaveGateway(t)
	key := capability.Key{ID: LeaveRequestCapabilityID, Version: 1}
	record, ok := registry.Lookup(key)
	if !ok || record.Definition.TestRef != "conformance:LEAVE-016/served/v1" {
		t.Fatalf("served trace record=%+v, found=%v", record, ok)
	}
	_, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability: key, Payload: leave.TraceInput{}, Authorization: servedLeaveAuthorization(servedLeaveScope),
	})
	if err == nil || !strings.Contains(err.Error(), "trace intake") {
		t.Fatalf("invalid trace error=%v, want trace dispatch and fail-closed intake", err)
	}
}
