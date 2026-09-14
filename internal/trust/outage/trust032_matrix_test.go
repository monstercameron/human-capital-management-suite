package outage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/secrets"
)

// FuzzTodo_TRUST_032 is the TRUST-032 fuzz target.
//
// The invariant under test is one sentence: for any key id, version,
// cache age and reachability whatsoever, sealing then restoring into a
// cell that only trusts backup-kek@v3 either refuses, or admits exactly
// that allowlisted reference — never an untrusted identity, never
// material, and never a panic.
func FuzzTodo_TRUST_032(f *testing.F) {
	f.Add("backup-kek", "v3", int64(time.Hour), false)
	f.Add("other-kek", "v9", int64(-1), true)
	f.Add("", "", int64(0), false)
	f.Fuzz(func(t *testing.T, id, version string, cacheAgeNanos int64, reachable bool) {
		policy := trust032Policy()
		mode, reason := ClassifyKeyOutage(policy, KeyServiceHealth{Reachable: reachable, ReferenceCacheAge: time.Duration(cacheAgeNanos)})
		switch mode {
		case KeyModeLive, KeyModeCachedBounded, KeyModeSealedOnly:
		default:
			t.Fatalf("unlisted outage mode %v", mode)
		}
		if reason == "" {
			t.Fatal("outage mode without a reason token")
		}
		ref := trust032Ref(id, version)
		bundle, err := SealReferences([]secrets.SecretReference{ref})
		if err != nil {
			return
		}
		receipt, err := RestoreIsolated(bundle, IsolatedCell{CellID: "cell", AllowedIDs: []string{"backup-kek@v3"}})
		if err != nil {
			return
		}
		if len(receipt.Restored) != 1 || receipt.Restored[0] != "backup-kek@v3" {
			t.Fatalf("fuzz restore admitted %v", receipt.Restored)
		}
		if receipt.Digest == "" {
			t.Fatal("fuzz restore receipt without a digest")
		}
	})
}

// TestTodo_TRUST_032_Golden pins the sealed-bundle and restore-receipt
// digest oracle.
func TestTodo_TRUST_032_Golden(t *testing.T) {
	bundle, err := SealReferences([]secrets.SecretReference{trust032Ref("backup-kek", "v3")})
	if err != nil {
		t.Fatalf("SealReferences: %v", err)
	}
	receipt, err := RestoreIsolated(bundle, IsolatedCell{CellID: "recovery-cell-a", AllowedIDs: []string{"backup-kek@v3"}})
	if err != nil {
		t.Fatalf("RestoreIsolated: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "trust032.golden.txt"))
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
	if bundle.Digest != oracle["bundle_digest"] {
		t.Fatalf("bundle digest mismatch:\n got=%q\nwant=%q", bundle.Digest, oracle["bundle_digest"])
	}
	if receipt.Digest != oracle["receipt_digest"] {
		t.Fatalf("receipt digest mismatch:\n got=%q\nwant=%q", receipt.Digest, oracle["receipt_digest"])
	}
}

// TestTodo_TRUST_032_Fault: malformed policies, duplicate and invalid
// references, tampered bundles and unknown cells fail closed.
func TestTodo_TRUST_032_Fault(t *testing.T) {
	policy := trust032Policy()
	// A malformed policy or health snapshot seals instead of guessing.
	if mode, _ := ClassifyKeyOutage(SealedPolicy{}, KeyServiceHealth{Reachable: true}); mode != KeyModeSealedOnly {
		t.Fatalf("zero policy mode=%v", mode)
	}
	if mode, _ := ClassifyKeyOutage(policy, KeyServiceHealth{ReferenceCacheAge: -time.Second}); mode != KeyModeSealedOnly {
		t.Fatalf("negative cache age mode=%v", mode)
	}
	// Empty and duplicate bundles refuse.
	if _, err := SealReferences(nil); err == nil {
		t.Fatal("empty bundle sealed")
	}
	good := trust032Ref("backup-kek", "v3")
	if _, err := SealReferences([]secrets.SecretReference{good, good}); err == nil {
		t.Fatal("duplicate reference sealed")
	}
	bad := good
	bad.State = "EXPIRED"
	if _, err := SealReferences([]secrets.SecretReference{bad}); err == nil {
		t.Fatal("invalid reference sealed")
	}
	// A tampered bundle refuses on digest mismatch.
	bundle, err := SealReferences([]secrets.SecretReference{good})
	if err != nil {
		t.Fatalf("SealReferences: %v", err)
	}
	tampered := bundle
	tampered.References[0].Version = "v4"
	if _, err := RestoreIsolated(tampered, IsolatedCell{CellID: "recovery-cell-a", AllowedIDs: []string{"backup-kek@v4"}}); err == nil {
		t.Fatal("tampered bundle restored")
	}
	// An unnamed cell restores nothing.
	if _, err := RestoreIsolated(bundle, IsolatedCell{AllowedIDs: []string{"backup-kek@v3"}}); err == nil {
		t.Fatal("unnamed cell restore admitted")
	}
}

// TestTodo_TRUST_032_Security: forbidden secret material has no field
// to travel in; credential-shaped provider paths refuse at seal time.
func TestTodo_TRUST_032_Security(t *testing.T) {
	smuggled := trust032Ref("backup-kek", "v3")
	smuggled.ProviderPath = "keys/backup-kek?password=hunter2"
	if _, err := SealReferences([]secrets.SecretReference{smuggled}); err == nil {
		t.Fatal("credential-shaped provider path sealed")
	}
	controlled := trust032Ref("backup-kek", "v3")
	controlled.ProviderPath = "keys/backup-kek\nAuthorization: hunter2"
	if _, err := SealReferences([]secrets.SecretReference{controlled}); err == nil {
		t.Fatal("control-character provider path sealed")
	}
	// A rotated reference restores only under its new allowlisted version.
	rotated := trust032Ref("backup-kek", "v4")
	bundle, err := SealReferences([]secrets.SecretReference{rotated})
	if err != nil {
		t.Fatalf("SealReferences: %v", err)
	}
	if _, err := RestoreIsolated(bundle, IsolatedCell{CellID: "recovery-cell-a", AllowedIDs: []string{"backup-kek@v3"}}); err == nil {
		t.Fatal("stale-version allowlist admitted rotated reference")
	}
	if _, err := RestoreIsolated(bundle, IsolatedCell{CellID: "recovery-cell-a", AllowedIDs: []string{"backup-kek@v4"}}); err != nil {
		t.Fatalf("rotated restore: %v", err)
	}
}

// TestTodo_TRUST_032_Recovery: a pre-outage sealed bundle restores into
// an isolated cell after the cache expires, receipt-verified.
func TestTodo_TRUST_032_Recovery(t *testing.T) {
	policy := trust032Policy()
	bundle, err := SealReferences([]secrets.SecretReference{trust032Ref("backup-kek", "v3"), trust032Ref("db-cred", "v1")})
	if err != nil {
		t.Fatalf("SealReferences: %v", err)
	}
	// The outage ages past the cache window: sealed-only.
	if mode, _ := ClassifyKeyOutage(policy, KeyServiceHealth{ReferenceCacheAge: 2 * time.Hour}); mode != KeyModeSealedOnly {
		t.Fatalf("aged outage mode=%v", mode)
	}
	cell := IsolatedCell{CellID: "recovery-cell-a", AllowedIDs: []string{"backup-kek@v3", "db-cred@v1"}}
	first, err := RestoreIsolated(bundle, cell)
	if err != nil {
		t.Fatalf("first restore: %v", err)
	}
	second, err := RestoreIsolated(bundle, cell)
	if err != nil {
		t.Fatalf("second restore: %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("restore receipt drift:\n got=%q\nwant=%q", second.Digest, first.Digest)
	}
	if len(first.Restored) != 2 || first.Restored[0] != "backup-kek@v3" || first.Restored[1] != "db-cred@v1" {
		t.Fatalf("receipt: %+v", first)
	}
}

// TestTodo_TRUST_032_Mutation: staleness and identity edges resolve on
// the documented side.
func TestTodo_TRUST_032_Mutation(t *testing.T) {
	policy := trust032Policy()
	// Exactly at the bound the cache still serves: expiry is strictly past.
	if mode, _ := ClassifyKeyOutage(policy, KeyServiceHealth{ReferenceCacheAge: time.Hour}); mode != KeyModeCachedBounded {
		t.Fatalf("boundary cache mode=%v", mode)
	}
	// One nanosecond past seals.
	if mode, reason := ClassifyKeyOutage(policy, KeyServiceHealth{ReferenceCacheAge: time.Hour + time.Nanosecond}); mode != KeyModeSealedOnly || reason != ReasonKeyCacheWindowExpired {
		t.Fatalf("past-boundary mode=%v reason=%q", mode, reason)
	}
	// Zero-age cache on an unreachable service still serves bounded.
	if mode, _ := ClassifyKeyOutage(policy, KeyServiceHealth{ReferenceCacheAge: 0}); mode != KeyModeCachedBounded {
		t.Fatalf("zero-age cache mode=%v", mode)
	}
	// Padded and empty identities never seal.
	for _, id := range []string{"", " backup-kek", "backup-kek "} {
		if _, err := SealReferences([]secrets.SecretReference{trust032Ref(id, "v3")}); err == nil {
			t.Fatalf("identity %q sealed", id)
		} else if !strings.Contains(err.Error(), "invalid secret reference") {
			t.Fatalf("identity %q wrong error: %v", id, err)
		}
	}
}
