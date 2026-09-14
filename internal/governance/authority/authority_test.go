// Package authority binds exactly one P1B authority topology per tenant.
//
// NEXT-006 RED: this test names the contract before the implementation
// exists. It must fail (missing symbols) until authority.go lands.
package authority

import (
	"strings"
	"testing"
	"time"
)

var (
	redAt   = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	redFrom = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	redTill = time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
)

func redExternal() Amendment {
	return Amendment{
		AmendmentID: "amd-external-1",
		Tenant:      "harborcare-demo",
		Topology:    TopologyExternalAuthority,
		Fields:      []string{"people.manager", "rewards.base_pay"},
		Operations:  []string{"promote_worker/EXECUTE", "change_base_pay/EXECUTE"},
		From:        redFrom,
		Until:       redTill,
	}
}

func redTransferred() Amendment {
	return Amendment{
		AmendmentID: "amd-transferred-1",
		Tenant:      "harborcare-demo",
		Topology:    TopologyTransferredAuthority,
		Fields:      []string{"people.manager", "rewards.base_pay"},
		Operations:  []string{"promote_worker/EXECUTE", "change_base_pay/EXECUTE"},
		From:        redFrom,
		Until:       redTill,
		GrantRef:    "grant-partner-signed-1",
	}
}

// TestP1BAuthorityAmendmentSelectsOneTopologyAndEmitsOnlyTruthfulFacts is the
// NEXT-006 PRIMARY contract: one immutable topology per amendment, exact
// field/operation binding, truthful fact emission, fail-closed degradation.
func TestP1BAuthorityAmendmentSelectsOneTopologyAndEmitsOnlyTruthfulFacts(t *testing.T) {
	ext, err := Bind(redExternal())
	if err != nil {
		t.Fatalf("Bind(external): %v", err)
	}
	if ext.Topology != TopologyExternalAuthority {
		t.Fatalf("topology=%q, want EXTERNAL_AUTHORITY", ext.Topology)
	}
	// RED: externally mastered state must never be emitted as a local fact.
	kind, err := ext.Classify("people.manager")
	if err != nil {
		t.Fatalf("Classify(bound): %v", err)
	}
	if kind != KindExternalObservation {
		t.Fatalf("kind=%q, want EXTERNAL_OBSERVATION", kind)
	}
	// RED: an external amendment never admits a local write.
	if err := ext.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redAt, "harborcare-demo", ""); err == nil {
		t.Fatal("external AdmitLocalWrite succeeded, want denial")
	}
	// RED: transferred writes before an explicit partner grant are denied.
	ungranted := redTransferred()
	ungranted.GrantRef = ""
	if _, err := Bind(ungranted); err == nil {
		t.Fatal("Bind(transferred without grant) succeeded, want rejection")
	}
	tr, err := Bind(redTransferred())
	if err != nil {
		t.Fatalf("Bind(transferred): %v", err)
	}
	kind, err = tr.Classify("people.manager")
	if err != nil {
		t.Fatalf("Classify(bound): %v", err)
	}
	if kind != KindLocalAuthoritative {
		t.Fatalf("kind=%q, want LOCAL_AUTHORITATIVE", kind)
	}
	if err := tr.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redAt, "harborcare-demo", "grant-partner-signed-1"); err != nil {
		t.Fatalf("AdmitLocalWrite(granted): %v", err)
	}
	// GREEN: unbound fields fail closed as topology mismatches.
	if _, err := tr.Classify("payroll.net_pay"); err == nil {
		t.Fatal("Classify(unbound) succeeded, want TOPOLOGY_MISMATCH")
	}
	// GREEN: expiry and revocation fail closed.
	expired := redTransferred()
	expired.Until = redFrom.Add(24 * time.Hour)
	bound, err := Bind(expired)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := bound.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redAt, "harborcare-demo", "grant-partner-signed-1"); err == nil {
		t.Fatal("AdmitLocalWrite(expired) succeeded, want denial")
	}
	revoked := redTransferred()
	revoked.Revoked = true
	bound, err = Bind(revoked)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := bound.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redAt, "harborcare-demo", "grant-partner-signed-1"); err == nil {
		t.Fatal("AdmitLocalWrite(revoked) succeeded, want denial")
	}
	// GREEN: digest is stable and self-verifying.
	if bound.Digest == "" {
		t.Fatal("Digest is empty")
	}
	if err := bound.VerifyDigest(); err != nil {
		t.Fatalf("VerifyDigest: %v", err)
	}
	if !strings.HasPrefix(bound.Digest, "sha256:") {
		t.Fatalf("digest=%q, want sha256: prefix", bound.Digest)
	}
}
