package agentpromo

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
)

// TestTodo_AGENT2_024_Conformance pins the journey transcript: the exact
// ordered effects a promotion-preparation journey must produce, and the
// byte-stability of the approval digest across identical runs.
func TestTodo_AGENT2_024_Conformance(t *testing.T) {
	first := newPromoWorld(t, promoTask, promoDirectory(t))
	first.driveFull()

	second := newPromoWorld(t, promoTask, promoDirectory(t))
	second.driveFull()

	if len(first.transcript) != len(second.transcript) {
		t.Fatalf("transcript lengths=%d/%d", len(first.transcript), len(second.transcript))
	}
	for i := range first.transcript {
		if first.transcript[i] != second.transcript[i] {
			t.Fatalf("transcript[%d]=%q vs %q", i, first.transcript[i], second.transcript[i])
		}
	}
	if first.batch.Digest != second.batch.Digest {
		t.Fatalf("batch digest=%s vs %s, want one stable digest", first.batch.Digest, second.batch.Digest)
	}
	if agentapproval.Digest(first.batch) != first.batch.Digest {
		t.Fatal("the pinned digest does not recompute from the pinned batch")
	}

	want := []string{
		"plan:10-steps:confirmed",
		"extract:3-candidates:quarantined",
		"refuse:instruction:quarantined",
		"refuse:non-report:jane-doe",
		"resolve:omar-reyes:direct",
		"resolve:noor-haddad:indirect",
		"band:omar-reyes:evaluated",
		"band:noor-haddad:evaluated",
		"simulate:omar-reyes:executable",
		"simulate:noor-haddad:executable",
		"draft:omar-reyes:READY",
		"draft:noor-haddad:READY",
		"card:4-args:pinned",
		"verify:owner-reread:match",
		"batch:2-items:created",
		"approve:manager:step-up",
		"wake:approval:accepted",
		"bind:T3:exact-card",
		"exchange:agent-for-manager:verified",
		"submit:2-intents:once",
		"park:signal:promotion-batch-1",
		"resume:worker-2:once",
		"workflow:promote-into-management:finance-obligation",
		"decide:hr-adaeze-obaro:approve_finance:APPROVE",
		"wake:signal:accepted",
		"report:owner-observed:2-intents",
	}
	if len(first.transcript) != len(want) {
		t.Fatalf("transcript=%q, want %d lines", first.transcript, len(want))
	}
	for i := range want {
		if first.transcript[i] != want[i] {
			t.Fatalf("transcript[%d]=%q, want %q", i, first.transcript[i], want[i])
		}
	}
}
