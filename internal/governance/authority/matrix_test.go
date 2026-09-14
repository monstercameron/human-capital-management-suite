package authority

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustBind(t *testing.T, a Amendment) Amendment {
	t.Helper()
	bound, err := Bind(a)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	return bound
}

// TestTodo_NEXT_006_Property: digest algebra — order-insensitive inputs,
// content-sensitive outputs, stable encoding.
func TestTodo_NEXT_006_Property(t *testing.T) {
	base := redTransferred()
	a := mustBind(t, base)
	shuffled := redTransferred()
	shuffled.Fields = []string{"rewards.base_pay", "people.manager"}
	shuffled.Operations = []string{"change_base_pay/EXECUTE", "promote_worker/EXECUTE"}
	b := mustBind(t, shuffled)
	if a.Digest != b.Digest {
		t.Fatalf("reordered bindings changed digest: %q vs %q", a.Digest, b.Digest)
	}
	other := redTransferred()
	other.AmendmentID = "amd-transferred-2"
	c := mustBind(t, other)
	if a.Digest == c.Digest {
		t.Fatal("distinct amendments share a digest")
	}
	if got := string(a.Canonical()); !strings.Contains(got, "TRANSFERRED_AUTHORITY") || !strings.Contains(got, "harborcare-demo") {
		t.Fatalf("canonical bytes omit material content: %q", got)
	}
	// Sorted encoding: people.manager precedes rewards.base_pay.
	if strings.Index(string(a.Canonical()), "people.manager") > strings.Index(string(a.Canonical()), "rewards.base_pay") {
		t.Fatal("canonical fields are not sorted")
	}
}

// TestTodo_NEXT_006_Golden: exact digest and canonical bytes pinned in
// testdata/amendment.golden.txt (digest line plus canonical hex).
func TestTodo_NEXT_006_Golden(t *testing.T) {
	a := mustBind(t, redTransferred())
	raw, err := os.ReadFile(filepath.Join("testdata", "amendment.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	oracle := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("malformed golden line: %q", line)
		}
		oracle[key] = value
	}
	if a.Digest != oracle["digest"] {
		t.Fatalf("digest mismatch:\n got=%q\nwant=%q", a.Digest, oracle["digest"])
	}
	if got := hex.EncodeToString(a.Canonical()); got != oracle["canonical_hex"] {
		t.Fatalf("canonical bytes mismatch:\n got=%q\nwant=%q", got, oracle["canonical_hex"])
	}
}

// TestTodo_NEXT_006_Fault: every degradation fails closed with a typed error.
func TestTodo_NEXT_006_Fault(t *testing.T) {
	a := mustBind(t, redTransferred())
	cases := map[string]struct {
		mutate func(*Amendment)
		at     time.Time
	}{
		"expired":   {func(x *Amendment) { x.Until = redAt.Add(-time.Second) }, redAt},
		"not_yet":   {func(x *Amendment) {}, redFrom.Add(-time.Second)},
		"revoked":   {func(x *Amendment) { x.Revoked = true }, redAt},
		"tampered":  {func(x *Amendment) { x.Fields = append(x.Fields, "payroll.net_pay") }, redAt},
		"zero_time": {func(x *Amendment) {}, time.Time{}},
	}
	for name, c := range cases {
		c.mutate(&a)
		if name == "tampered" {
			// Re-digest after in-place mutation would hide tampering; the
			// carried digest must no longer verify.
			if err := a.VerifyDigest(); err == nil {
				t.Fatalf("%s: tampered amendment still verifies", name)
			}
			a = mustBind(t, redTransferred())
			continue
		}
		if err := a.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", c.at, "harborcare-demo", "grant-partner-signed-1"); err == nil {
			t.Fatalf("%s: degraded amendment admitted a write", name)
		}
	}
	// Expiry instant itself is already dead (exclusive end).
	edge := mustBind(t, redTransferred())
	if err := edge.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redTill, "harborcare-demo", "grant-partner-signed-1"); err == nil {
		t.Fatal("write at the expiry instant admitted, want denial")
	}
}

// TestTodo_NEXT_006_Security: denials are tenant-opaque and grant-exact.
func TestTodo_NEXT_006_Security(t *testing.T) {
	a := mustBind(t, redTransferred())
	other := redTransferred()
	other.Tenant = "other-tenant"
	other.AmendmentID = "amd-other-1"
	b := mustBind(t, other)
	// A write attempted under the wrong tenant's amendment fails even with
	// a syntactically valid grant, and the error narrates nothing bound.
	err := b.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redAt, "harborcare-demo", "grant-partner-signed-1")
	if err == nil {
		t.Fatal("cross-tenant write admitted")
	}
	if strings.Contains(err.Error(), "harborcare-demo") || strings.Contains(err.Error(), "grant-partner-signed-1") {
		t.Fatalf("denial leaks amendment contents: %v", err)
	}
	for _, grant := range []string{"", " ", "grant-partner-signed-2", "GRANT-PARTNER-SIGNED-1"} {
		if err := a.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redAt, "harborcare-demo", grant); err == nil {
			t.Fatalf("grant %q admitted, want denial", grant)
		}
	}
	// The requesting tenant must match the amendment tenant, and a missing
	// tenant never authorizes.
	if err := a.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redAt, "other-tenant", "grant-partner-signed-1"); err == nil {
		t.Fatal("wrong-tenant write admitted, want denial")
	}
	if err := a.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redAt, "", "grant-partner-signed-1"); err == nil {
		t.Fatal("empty-tenant write admitted, want denial")
	}
	if _, err := a.Classify(""); err == nil {
		t.Fatal("Classify(empty) succeeded, want mismatch")
	}
}

// TestTodo_NEXT_006_Conformance: both topologies honor the same truth
// contract — external never emits local facts, transferred only admits
// bound writes under the grant.
func TestTodo_NEXT_006_Conformance(t *testing.T) {
	ext := mustBind(t, redExternal())
	tr := mustBind(t, redTransferred())
	for _, field := range []string{"people.manager", "rewards.base_pay"} {
		ek, err := ext.Classify(field)
		if err != nil || ek != KindExternalObservation {
			t.Fatalf("external Classify(%q)=%v,%v", field, ek, err)
		}
		tk, err := tr.Classify(field)
		if err != nil || tk != KindLocalAuthoritative {
			t.Fatalf("transferred Classify(%q)=%v,%v", field, tk, err)
		}
		for _, op := range []string{"promote_worker/EXECUTE", "change_base_pay/EXECUTE"} {
			if err := ext.AdmitLocalWrite(field, op, redAt, "harborcare-demo", "grant-partner-signed-1"); err == nil {
				t.Fatalf("external admitted %s/%s", field, op)
			}
			if err := tr.AdmitLocalWrite(field, op, redAt, "harborcare-demo", "grant-partner-signed-1"); err != nil {
				t.Fatalf("transferred denied %s/%s: %v", field, op, err)
			}
		}
	}
	// Unbound operation on a bound field is still a mismatch.
	if err := tr.AdmitLocalWrite("people.manager", "terminate_worker/EXECUTE", redAt, "harborcare-demo", "grant-partner-signed-1"); err == nil {
		t.Fatal("unbound operation admitted, want mismatch")
	}
}

// TestTodo_NEXT_006_Mutation: boundary mutants die — window edges,
// single-octet bindings, grant padding.
func TestTodo_NEXT_006_Mutation(t *testing.T) {
	a := mustBind(t, redTransferred())
	// Start instant is inclusive; one nanosecond before is not.
	if err := a.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redFrom, "harborcare-demo", "grant-partner-signed-1"); err != nil {
		t.Fatalf("write at window start denied: %v", err)
	}
	if err := a.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redFrom.Add(-time.Nanosecond), "harborcare-demo", "grant-partner-signed-1"); err == nil {
		t.Fatal("write before window start admitted")
	}
	// One nanosecond before expiry is alive; the instant is not.
	if err := a.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redTill.Add(-time.Nanosecond), "harborcare-demo", "grant-partner-signed-1"); err != nil {
		t.Fatalf("write before expiry denied: %v", err)
	}
	// Near-miss field and padded grant are not the bound values.
	if _, err := a.Classify("people.manage"); err == nil {
		t.Fatal("near-miss field classified")
	}
	if err := a.AdmitLocalWrite("people.manager", "promote_worker/EXECUTE", redAt, "harborcare-demo", " grant-partner-signed-1"); err == nil {
		t.Fatal("padded grant admitted")
	}
	// Zero topology and dual-topology strings never bind.
	for _, topo := range []Topology{"", "EXTERNAL_AUTHORITY,TRANSFERRED_AUTHORITY", "external_authority"} {
		bad := redTransferred()
		bad.Topology = topo
		if _, err := Bind(bad); err == nil {
			t.Fatalf("topology %q bound, want rejection", topo)
		}
	}
}
