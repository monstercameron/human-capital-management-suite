package selectionbind

import (
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
)

func attackerKey() (ed25519.PrivateKey, string) {
	priv := ed25519.NewKeyFromSeed([]byte(strings.Repeat("x", ed25519.SeedSize)))
	return priv, hex.EncodeToString(priv.Public().(ed25519.PublicKey))
}

// TestTodo_NEXT_002_Security attacks the selection binding from every side a
// forger could use, always starting from the COMPLETE tree so the attack is
// the only thing that can make the verdict fail: editing a signed binding,
// re-signing the manifest or a bound artifact with an untrusted key,
// escaping the root through a binding path, downgrading a digest kind,
// smuggling EXECUTE into P1A, and forging P1B activation.
func TestTodo_NEXT_002_Security(t *testing.T) {
	attackPriv, attackPub := attackerKey()

	t.Run("a binding digest edited without re-signing breaks the manifest signature", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		f.manifest = cloneManifest(f.manifest)
		f.manifest.SelectionBindings[2].Digest = strings.Repeat("0", 64)
		r := evaluate(t, f)
		if r.Status != StatusIncomplete || !strings.Contains(strings.Join(r.ManifestReasons, "|"), "tampered") {
			t.Fatalf("tampered binding accepted: %s %v", r.Status, r.ManifestReasons)
		}
	})

	t.Run("a manifest re-signed by an untrusted key is refused", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		f.manifest = signManifest(t, f.manifest, attackPriv, attackPub)
		r := evaluate(t, f)
		if r.Status != StatusIncomplete || !strings.Contains(strings.Join(r.ManifestReasons, "|"), "untrusted key") {
			t.Fatalf("attacker-signed manifest accepted: %s %v", r.Status, r.ManifestReasons)
		}
	})

	t.Run("a bound selection re-signed by an untrusted key is refused even when correctly re-bound", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		p := mustLoad(t, filepath.Join(f.root, filepath.FromSlash(providerPath)), pilotprovider.LoadTopology)
		signed, err := pilotprovider.SignTopology(*p, attackPriv, attackPub, keyFixture)
		if err != nil {
			t.Fatal(err)
		}
		resignAndRebind(t, &f, "SELECT-002", providerPath, signed)
		r := evaluate(t, f)
		if r.Status != StatusIncomplete || !strings.Contains(strings.Join(bindingByTodo(r, "SELECT-002").Reasons, "|"), "untrusted key") {
			t.Fatalf("attacker-signed provider selection accepted: %s\n%s", r.Status, joinedReasons(r))
		}
	})

	t.Run("a binding path cannot escape the evaluation root", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		escape := gateevidence.SelectionBinding{TodoID: "SELECT-002", Path: "../" + providerPath, DigestKind: gateevidence.DigestKindCanonicalJSON, Digest: f.manifest.SelectionBindings[2].Digest}
		if _, err := LiveDigest(f.root, escape); err == nil {
			t.Fatal("LiveDigest resolved a path outside the root")
		}
		f.manifest = cloneManifest(f.manifest)
		f.manifest.SelectionBindings[2] = escape
		f.manifest = signManifest(t, f.manifest, f.priv, f.pub)
		r := evaluate(t, f)
		if r.Status != StatusIncomplete || !strings.Contains(joinedReasons(r), "not a clean repository-relative path") {
			t.Fatalf("root-escaping binding accepted: %s\n%s", r.Status, joinedReasons(r))
		}
	})

	t.Run("a canonical binding cannot be downgraded to raw file bytes", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		downgraded := f.manifest.SelectionBindings[0]
		downgraded.DigestKind = gateevidence.DigestKindFileSHA256
		if _, err := LiveDigest(f.root, downgraded); err == nil || !strings.Contains(err.Error(), "must bind with digest kind CANONICAL_JSON") {
			t.Fatalf("digest-kind downgrade accepted: %v", err)
		}
		if _, err := LiveDigest(f.root, gateevidence.SelectionBinding{TodoID: "SELECT-999", Path: providerPath, DigestKind: gateevidence.DigestKindCanonicalJSON}); err == nil {
			t.Fatal("an unknown todo was bindable")
		}
	})

	t.Run("a validly signed P1A granting EXECUTE is refused", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		f.manifest = cloneManifest(f.manifest)
		f.manifest.Intents[1].Modes = []string{"DRAFT", "PREFLIGHT", "SIMULATE", "EXECUTE"}
		f.manifest = signManifest(t, f.manifest, f.priv, f.pub)
		r := evaluate(t, f)
		if r.Status != StatusIncomplete || !strings.Contains(strings.Join(r.ManifestReasons, "|"), "grants EXECUTE") {
			t.Fatalf("EXECUTE-granting P1A accepted: %s %v", r.Status, r.ManifestReasons)
		}
	})

	t.Run("a validly signed P1A dropping the outbox effect ceiling is refused", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		f.manifest = cloneManifest(f.manifest)
		f.manifest.ForbiddenEffects = []string{"domain_mutation", "reservation", "work_item", "timer", "message", "provider_write"}
		f.manifest = signManifest(t, f.manifest, f.priv, f.pub)
		if r := evaluate(t, f); !strings.Contains(strings.Join(r.ManifestReasons, "|"), "forbidden_effects") {
			t.Fatalf("outbox-permitting P1A accepted: %v", r.ManifestReasons)
		}
	})

	t.Run("P1B activation cannot be forged", func(t *testing.T) {
		live := mustLoadLiveTemplate(t)
		priv, pub := signingKey(t)
		resign := func(tpl gateevidence.P1BTemplate, k ed25519.PrivateKey, p string) gateevidence.P1BTemplate {
			tpl.Contracts = append([]gateevidence.Contract(nil), tpl.Contracts...)
			d, err := tpl.CanonicalDigest()
			if err != nil {
				t.Fatal(err)
			}
			sig, err := gateevidence.SignDigest(k, d)
			if err != nil {
				t.Fatal(err)
			}
			tpl.Signature = &gateevidence.Signature{Algorithm: "ed25519", PublicKey: p, Value: sig}
			return tpl
		}
		forged := live
		forged.GateADecision = "PROCEED"
		if reasons := strings.Join(EvaluateP1B(forged, repoRoot, pub), "|"); !strings.Contains(reasons, "tampered") {
			t.Fatalf("unsigned forged Gate A accepted: %s", reasons)
		}
		reused := live
		reused.GateADecision, reused.AuthorityDigest = "PROCEED", live.P1AManifest.Digest
		reused = resign(reused, priv, pub)
		if reused.CanActivate() {
			t.Fatal("P1A digest re-presented as authority activated P1B")
		}
		activatable := live
		activatable.GateADecision, activatable.AuthorityDigest = "PROCEED", strings.Repeat("cd", 32)
		if reasons := strings.Join(EvaluateP1B(resign(activatable, priv, pub), repoRoot, pub), "|"); !strings.Contains(reasons, "activatable") {
			t.Fatalf("a checked-in-shaped activatable template passed: %s", reasons)
		}
		if reasons := strings.Join(EvaluateP1B(resign(live, attackPriv, attackPub), repoRoot, pub), "|"); !strings.Contains(reasons, "untrusted key") {
			t.Fatalf("attacker-signed P1B accepted: %s", reasons)
		}
		escaped := live
		escaped.P1AManifest.Path = "../p1a-manifest.yaml"
		if reasons := strings.Join(EvaluateP1B(escaped, repoRoot, pub), "|"); !strings.Contains(reasons, "not a clean repository-relative path") {
			t.Fatalf("root-escaping P1B binding accepted: %s", reasons)
		}
	})
}
