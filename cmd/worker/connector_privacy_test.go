package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	privacydispatch "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/dispatch"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

// connectorPrivacySnapshot seals one signed policy snapshot for the
// connector test scope: the fixture destination, the PAYROLL_SYNC purpose,
// confidential classification.
func connectorPrivacySnapshot(t *testing.T, kind privacydispatch.SnapshotKind, destination string, decision dlp.Decision) privacydispatch.PolicySnapshot {
	t.Helper()
	sealed, err := privacydispatch.SealSnapshot(privacydispatch.PolicySnapshot{
		Kind: kind, Version: "policy.v1", Processor: "processor.payroll",
		Destination: destination, Region: "us-east-1", Classification: dlp.ClassConfidential,
		Purpose: "PAYROLL_SYNC", TransferAssessment: "transfer.v1",
		DLPDecision: decision,
	}, "sig-"+string(kind))
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func connectorPrivacyPolicy(t *testing.T, destination string, decision dlp.Decision) *dlp.Policy {
	t.Helper()
	egress, err := outbound.NewPolicy(outbound.Destination{
		Name: destination, TrustBundleRef: "bundle:payroll:v1",
		Purposes: []string{"PAYROLL_SYNC"}, DataClasses: []string{string(dlp.ClassConfidential)},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := dlp.NewPolicy(egress, dlp.Clearance{
		Destination: destination, DataClass: dlp.ClassConfidential, Decision: decision,
	})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

// connectorPrivacyAuthorityFor builds the real snapshot authority for the
// live-path tests: genuinely sealed snapshots bound at construction plus
// the real DLP policy evaluator. Revoking the current DLP snapshot models
// a live policy denial.
func connectorPrivacyAuthorityFor(t *testing.T, destination string, revoked bool) operation.StaticPrivacyAuthority {
	t.Helper()
	current := privacydispatch.SnapshotSet{
		Processor: connectorPrivacySnapshot(t, privacydispatch.SnapshotProcessor, destination, dlp.Allow),
		Transfer:  connectorPrivacySnapshot(t, privacydispatch.SnapshotTransfer, destination, dlp.Allow),
		DLP:       connectorPrivacySnapshot(t, privacydispatch.SnapshotDLP, destination, dlp.Allow),
	}
	binding, err := privacydispatch.Bind(current)
	if err != nil {
		t.Fatal(err)
	}
	if revoked {
		current.DLP.Revoked = true
		current.DLP.Digest = privacydispatch.DigestSnapshot(current.DLP)
	}
	authority, err := operation.NewStaticPrivacyAuthority(operation.PrivacyGrant{
		Binding: binding, Current: current, Policy: connectorPrivacyPolicy(t, destination, dlp.Allow),
		Principal: "worker-1", DataClasses: []dlp.DataClass{dlp.ClassConfidential},
	})
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func connectorPrivacyEvents(t *testing.T, journal connectorJournal, tenant string, id uuid.UUID) []operation.JournalEvent {
	t.Helper()
	mem, ok := journal.(*operation.MemoryJournal)
	if !ok {
		t.Fatalf("journal is %T, want *operation.MemoryJournal", journal)
	}
	entries, err := mem.Journal(context.Background(), tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func connectorPrivacyHasEvent(entries []operation.JournalEvent, name string) bool {
	for _, entry := range entries {
		if entry.Event == name {
			return true
		}
	}
	return false
}

// TestTodo_REV_016_01_LivePath proves the reviewer finding on REV-016-01 is
// closed: the worker's live connector dispatch chain invokes the DLP egress
// gate before any provider call. An allow-configured authority dispatches;
// a revoked snapshot denies with EGRESS_BLOCKED, durable evidence and zero
// provider calls; an unconfigured authority fails closed.
func TestTodo_REV_016_01_LivePath(t *testing.T) {
	t.Run("allow_dispatches", func(t *testing.T) {
		j := connectorJournalFor(t)
		id := uuid.New()
		op := planQueuedConnectorOperation(t, j, id, "connector.example")
		manager := newConnectorMachineManager(t, connectorTestNow)
		credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
		writer := &recordingCredentialWriter{}
		role := newConnectorRole(j, ampleLedger(), credSource, writer, manager, "worker-1", connectorPrivacyAuthorityFor(t, "connector.example", false))

		result, err := role.dispatchOperation(context.Background(), op, connectorTestNow)
		if err != nil {
			t.Fatalf("live allow dispatch: %v", err)
		}
		if !result.ProviderCall || writer.calls() != 1 {
			t.Fatalf("provider calls = %d, want exactly 1", writer.calls())
		}
		if !connectorPrivacyHasEvent(connectorPrivacyEvents(t, j, "tenant-connector", id), "PRIVACY_BOUND") {
			t.Fatal("PRIVACY_BOUND evidence missing on the live path")
		}
	})

	t.Run("deny_blocks_with_evidence", func(t *testing.T) {
		j := connectorJournalFor(t)
		id := uuid.New()
		op := planQueuedConnectorOperation(t, j, id, "connector.example")
		manager := newConnectorMachineManager(t, connectorTestNow)
		credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
		writer := &recordingCredentialWriter{}
		role := newConnectorRole(j, ampleLedger(), credSource, writer, manager, "worker-1", connectorPrivacyAuthorityFor(t, "connector.example", true))

		_, err := role.dispatchOperation(context.Background(), op, connectorTestNow)
		if !errors.Is(err, privacydispatch.ErrEGRESSBlocked) {
			t.Fatalf("live deny err = %v, want EGRESS_BLOCKED", err)
		}
		if writer.calls() != 0 {
			t.Fatalf("denied live dispatch made %d provider calls, want zero", writer.calls())
		}
		got, err := j.Get(context.Background(), "tenant-connector", id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != operation.StateLeased || len(got.Attempts) != 0 {
			t.Fatalf("denied operation = %+v, want LEASED with zero attempts", got)
		}
		if !connectorPrivacyHasEvent(connectorPrivacyEvents(t, j, "tenant-connector", id), "EGRESS_BLOCKED") {
			t.Fatal("EGRESS_BLOCKED evidence missing on the live path")
		}
	})

	t.Run("unconfigured_fails_closed", func(t *testing.T) {
		j := connectorJournalFor(t)
		id := uuid.New()
		op := planQueuedConnectorOperation(t, j, id, "connector.example")
		manager := newConnectorMachineManager(t, connectorTestNow)
		credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
		writer := &recordingCredentialWriter{}
		role := newConnectorRole(j, ampleLedger(), credSource, writer, manager, "worker-1", unconfiguredPrivacyAuthority{})

		_, err := role.dispatchOperation(context.Background(), op, connectorTestNow)
		if err == nil {
			t.Fatal("unconfigured privacy authority dispatched")
		}
		if writer.calls() != 0 {
			t.Fatalf("unconfigured dispatch made %d provider calls, want zero", writer.calls())
		}
	})
}

// TestTodo_REV_016_01_DurableAdmission proves the durable journal wrapper
// admits through the same gate and keeps its durable rows tracking the
// delegate on both outcomes.
func TestTodo_REV_016_01_DurableAdmission(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := rev013Tenant(t, db, "rev016-durable")
	pool := schemaScopedPool(t, db)
	journal := newDurableConnectorJournal(pool, nil)

	allowID := uuid.New()
	rev013PlanQueued(t, journal, allowID, tenant, "connector.example", []byte(`{"worker":"w-1"}`))
	claim, err := journal.Lease(ctx, operation.LeaseRequest{
		TenantID: tenant.String(), OperationID: allowID, WorkerID: "worker-1",
		At: time.Now().UTC(), Duration: time.Minute,
		Revalidate: func(current operation.Operation) operation.Revalidation {
			return operation.Revalidation{OperationID: current.OperationID, Confirmed: true, PlanDigest: "plan-v1", CurrentPlanDigest: "plan-v1"}
		},
	})
	if err != nil {
		t.Fatalf("durable lease: %v", err)
	}
	authority := connectorPrivacyAuthorityFor(t, "connector.example", false)
	grant, err := authority.ResolvePrivacyGrant(ctx, operation.Operation{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.AdmitPrivacyDispatch(ctx, claim, grant); err != nil {
		t.Fatalf("durable admit (allow): %v", err)
	}
	events, err := journal.Journal(ctx, tenant.String(), allowID)
	if err != nil {
		t.Fatal(err)
	}
	if !connectorPrivacyHasEvent(events, "PRIVACY_BOUND") {
		t.Fatal("PRIVACY_BOUND missing after durable admission")
	}

	denyID := uuid.New()
	denyReq := rev013PlanRequest(denyID, tenant, "connector.example", []byte(`{"worker":"w-1"}`))
	// The unknown-connector quota admits one live lease per connection, so
	// the deny operation plans on its own connection: sharing the allow
	// operation's connection would block this claim while that lease is live.
	denyReq.ConnectionID = "payroll-connection-deny"
	if _, err := journal.Plan(ctx, denyReq); err != nil {
		t.Fatalf("durable Plan (deny): %v", err)
	}
	denyOp, err := journal.Queue(ctx, tenant.String(), denyID)
	if err != nil {
		t.Fatalf("durable Queue (deny): %v", err)
	}
	if denyOp.State != operation.StateQueued {
		t.Fatalf("deny operation state after Queue = %s, want QUEUED", denyOp.State)
	}
	denyClaim, err := journal.Lease(ctx, operation.LeaseRequest{
		TenantID: tenant.String(), OperationID: denyID, WorkerID: "worker-1",
		At: time.Now().UTC(), Duration: time.Minute,
		Revalidate: func(current operation.Operation) operation.Revalidation {
			return operation.Revalidation{OperationID: current.OperationID, Confirmed: true, PlanDigest: "plan-v1", CurrentPlanDigest: "plan-v1"}
		},
	})
	if err != nil {
		t.Fatalf("durable lease (deny): %v", err)
	}
	denied := connectorPrivacyAuthorityFor(t, "connector.example", true)
	deniedGrant, err := denied.ResolvePrivacyGrant(ctx, operation.Operation{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.AdmitPrivacyDispatch(ctx, denyClaim, deniedGrant); !errors.Is(err, privacydispatch.ErrEGRESSBlocked) {
		t.Fatalf("durable admit (deny) err = %v, want EGRESS_BLOCKED", err)
	}
	denyEvents, err := journal.Journal(ctx, tenant.String(), denyID)
	if err != nil {
		t.Fatal(err)
	}
	if !connectorPrivacyHasEvent(denyEvents, "EGRESS_BLOCKED") {
		t.Fatal("EGRESS_BLOCKED missing after durable denial")
	}
}
