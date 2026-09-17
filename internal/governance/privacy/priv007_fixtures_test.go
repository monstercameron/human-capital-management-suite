package privacy

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// --- PRIV-007 shared fixtures ------------------------------------------------
//
// Every PRIV-007 matrix test starts from one fulfillment request: three
// copies held by two processors, each with its own acknowledgement due
// instant. Tests perturb one thing at a time from here.

const (
	fxProcIssuedAt = 1_800_000_000
	fxProcAckAt    = 1_800_005_000
	// fxProcDue1/2 are the acknowledgement due instants. fxProcEarly is
	// before every due instant (retries still allowed); fxProcLate is after
	// every due instant (silence is an escalation, never completion).
	fxProcDue1  = 1_800_010_000
	fxProcDue2  = 1_800_020_000
	fxProcEarly = 1_800_006_000
	fxProcLate  = 1_800_030_000

	fxProcReceiptA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	fxProcReceiptB = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	fxProcReceiptC = "5feceb66ffc86f38d952786c6d696c79c2dbc239dd4e91b46729d73a27fb57e9"
)

// fixtureFulfillmentRequest returns the three-copy request every PRIV-007
// test reconciles: two processors, two actions, distinct due instants.
func fixtureFulfillmentRequest(t *testing.T) FulfillmentRequest {
	t.Helper()
	return FulfillmentRequest{
		RequestID:  "dsr-fulfill-2026-001",
		SubjectRef: "subject:worker-1048",
		Items: []FulfillmentItem{
			{
				ItemID: "copy-backup-a", Processor: "processor-backup",
				Action: ProcessorActionDelete, CopyRef: "backup/snap-42",
				DueAt: mustInstant(t, fxProcDue1),
			},
			{
				ItemID: "copy-analytics-b", Processor: "processor-analytics",
				Action: ProcessorActionDelete, CopyRef: "events/shard-7",
				DueAt: mustInstant(t, fxProcDue2),
			},
			{
				ItemID: "copy-archive-c", Processor: "processor-backup",
				Action: ProcessorActionReturn, CopyRef: "vault/box-9",
				DueAt: mustInstant(t, fxProcDue2),
			},
		},
	}
}

// fixtureFulfilledAcks returns one FULFILLED acknowledgement with a receipt
// for every item of the fixture request.
func fixtureFulfilledAcks(t *testing.T) []ProcessorAck {
	t.Helper()
	at := mustInstant(t, fxProcAckAt)
	return []ProcessorAck{
		{
			ItemID: "copy-backup-a", Processor: "processor-backup",
			Outcome: AckFulfilled, At: at, ReceiptDigest: fxProcReceiptA,
		},
		{
			ItemID: "copy-analytics-b", Processor: "processor-analytics",
			Outcome: AckFulfilled, At: at, ReceiptDigest: fxProcReceiptB,
		},
		{
			ItemID: "copy-archive-c", Processor: "processor-backup",
			Outcome: AckFulfilled, At: at, ReceiptDigest: fxProcReceiptC,
		},
	}
}

// fixtureReconciliation reconciles the fixture request against the fixture
// acks at fxProcEarly, failing the test on any error so matrix tests start
// from a known fully-resolved reconciliation.
func fixtureReconciliation(t *testing.T) Reconciliation {
	t.Helper()
	rec, err := ReconcileFulfillment(
		fixtureFulfillmentRequest(t),
		fixtureFulfilledAcks(t),
		mustInstant(t, fxProcEarly),
	)
	if err != nil {
		t.Fatalf("ReconcileFulfillment (fixture): %v", err)
	}
	return rec
}

// mustInstantAt is a small alias so fault tests read plainly at the call
// site; the shared mustInstant in fixtures_test.go does the work.
func mustInstantAt(t *testing.T, unixSec int64) values.Instant {
	t.Helper()
	return mustInstant(t, unixSec)
}
