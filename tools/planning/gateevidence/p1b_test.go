package gateevidence

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
)

func testKey(seed byte) (ed25519.PrivateKey, string) {
	priv := ed25519.NewKeyFromSeed([]byte(strings.Repeat(string(rune('a'+seed)), ed25519.SeedSize)))
	return priv, hex.EncodeToString(priv.Public().(ed25519.PublicKey))
}

func signTemplate(t *testing.T, tpl P1BTemplate, priv ed25519.PrivateKey, pub string) P1BTemplate {
	t.Helper()
	digest, err := tpl.CanonicalDigest()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := SignDigest(priv, digest)
	if err != nil {
		t.Fatal(err)
	}
	tpl.Signature = &Signature{Algorithm: "ed25519", PublicKey: pub, Value: sig}
	return tpl
}

func cloneTemplate(tpl P1BTemplate) P1BTemplate {
	tpl.Contracts = append([]Contract(nil), tpl.Contracts...)
	return tpl
}

func TestCheckedInP1BTemplateIsSeparatelySignedAndBoundToTheLiveP1AManifest(t *testing.T) {
	p1a := mustLoadP1AManifest(t)
	p1b := mustLoadP1BTemplate(t)

	ok, err := VerifyP1BTemplateSignature(p1b)
	if err != nil || !ok {
		t.Fatalf("VerifyP1BTemplateSignature = %v, %v; the checked-in template must verify", ok, err)
	}
	p1aDigest, _ := p1a.CanonicalDigest()
	p1bDigest, _ := p1b.CanonicalDigest()
	if p1bDigest == p1aDigest || p1b.Signature.Value == p1a.Signature.Value {
		t.Fatal("P1B must carry its own digest and signature, not P1A's")
	}
	if p1b.P1AManifest.Digest != p1aDigest {
		t.Fatalf("P1B binds P1A digest %s, live P1A digest is %s", p1b.P1AManifest.Digest, p1aDigest)
	}

	for name, tamper := range map[string]func(*P1BTemplate){
		"gate A decision forged":   func(tp *P1BTemplate) { tp.GateADecision = "PROCEED" },
		"contract status unlocked": func(tp *P1BTemplate) { tp.Contracts[1].ActivationStatus = "ACTIVE" },
		"P1A binding repointed":    func(tp *P1BTemplate) { tp.P1AManifest.Digest = strings.Repeat("0", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			tampered := cloneTemplate(p1b)
			tamper(&tampered)
			if ok, err := VerifyP1BTemplateSignature(tampered); err != nil || ok {
				t.Fatalf("tampered template verified: ok=%v err=%v", ok, err)
			}
		})
	}
	if _, err := VerifyP1BTemplateSignature(P1BTemplate{}); err == nil {
		t.Fatal("unsigned template must be an error")
	}
	if _, err := VerifyP1BTemplateSignature(P1BTemplate{Signature: &Signature{Algorithm: "rsa"}}); err == nil {
		t.Fatal("non-ed25519 template signature must be an error")
	}
}

func TestP1BActivationRequiresGateAAndANewAuthorityDigest(t *testing.T) {
	base := mustLoadP1BTemplate(t)
	if blockers := base.ActivationBlockers(); len(blockers) != 2 {
		t.Fatalf("checked-in template blockers = %v, want exactly the Gate A and authority-digest blockers", blockers)
	}
	fresh := strings.Repeat("ab", 32)
	cases := []struct {
		name       string
		decision   string
		authority  string
		requires   bool
		activation bool
		wantHit    string
	}{
		{"no Gate A", "", fresh, true, false, "gate_a_decision"},
		{"Gate A HOLD", "HOLD", fresh, true, false, "gate_a_decision"},
		{"no authority digest", "PROCEED", "", true, false, "new 64-hex"},
		{"malformed authority digest", "PROCEED", "not-hex", true, false, "new 64-hex"},
		{"authority digest reuses P1A identity", "PROCEED", base.P1AManifest.Digest, true, false, "reuses the P1A manifest digest"},
		{"Gate A not required", "PROCEED", fresh, false, false, "requires_gate_a_decision"},
		{"Gate A plus new authority digest", "PROCEED", fresh, true, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tpl := cloneTemplate(base)
			tpl.GateADecision, tpl.AuthorityDigest, tpl.RequiresGateADecision = tc.decision, tc.authority, tc.requires
			if got := tpl.CanActivate(); got != tc.activation {
				t.Fatalf("CanActivate = %v, want %v (blockers %v)", got, tc.activation, tpl.ActivationBlockers())
			}
			if tc.wantHit != "" && !strings.Contains(strings.Join(tpl.ActivationBlockers(), "|"), tc.wantHit) {
				t.Fatalf("blockers %v do not name %q", tpl.ActivationBlockers(), tc.wantHit)
			}
			if tc.activation && !hasViolation(tpl.Validate(), "gate_a_decision/authority_digest") {
				t.Fatal("an activatable template must fail Validate - no checked-in template may be activatable")
			}
		})
	}
}

func hasViolation(vs []Violation, field string) bool {
	for _, v := range vs {
		if strings.Contains(v.Field, field) {
			return true
		}
	}
	return false
}

func TestP1BTemplateValidateNamesOnlyTheSixCandidateContracts(t *testing.T) {
	base := mustLoadP1BTemplate(t)
	cases := []struct {
		name    string
		mutate  func(*P1BTemplate)
		wantHit string
	}{
		{"seventh contract", func(tp *P1BTemplate) {
			tp.Contracts = append(tp.Contracts, Contract{Order: 7, ID: "terminate_worker", ActivationStatus: ActivationBlocked})
		}, "contracts"},
		{"swapped order", func(tp *P1BTemplate) { tp.Contracts[0], tp.Contracts[1] = tp.Contracts[1], tp.Contracts[0] }, "contracts[0]"},
		{"promote_worker loses EXECUTE", func(tp *P1BTemplate) { tp.Contracts[0].Mode = "" }, "contracts[0]"},
		{"deferred intent substituted", func(tp *P1BTemplate) { tp.Contracts[5].ID = "change_manager" }, "contracts[5]"},
		{"blended release", func(tp *P1BTemplate) { tp.Release = "P1A" }, "release"},
		{"wrong todo", func(tp *P1BTemplate) { tp.TodoID = "NEXT-003" }, "todo_id"},
		{"unbound P1A path", func(tp *P1BTemplate) { tp.P1AManifest.Path = "p1a.yaml" }, "p1a_manifest.path"},
		{"unbound P1A digest", func(tp *P1BTemplate) { tp.P1AManifest.Digest = "" }, "p1a_manifest.digest"},
		{"unsigned", func(tp *P1BTemplate) { tp.Signature = nil }, "signature"},
		{"incomplete signature", func(tp *P1BTemplate) { tp.Signature = &Signature{Algorithm: "ed25519"} }, "signature"},
		{"no schema", func(tp *P1BTemplate) { tp.SchemaVersion = 0 }, "schema_version"},
		{"no contracts", func(tp *P1BTemplate) { tp.Contracts = nil }, "contracts"},
		{"Gate A optional", func(tp *P1BTemplate) { tp.RequiresGateADecision = false }, "requires_gate_a_decision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tpl := cloneTemplate(base)
			tc.mutate(&tpl)
			if !hasViolation(tpl.Validate(), tc.wantHit) {
				t.Fatalf("Validate() = %v, want a violation on %q", tpl.Validate(), tc.wantHit)
			}
		})
	}
}

func TestCheckDisjointDetectsOverlapAndStaleP1ABinding(t *testing.T) {
	p1a := mustLoadP1AManifest(t)
	p1b := mustLoadP1BTemplate(t)
	priv, pub := testKey(1)

	t.Run("P1A granting EXECUTE overlaps P1B", func(t *testing.T) {
		m := p1a
		m.Intents = append([]Intent(nil), p1a.Intents...)
		m.Intents[1].Modes = []string{"DRAFT", "EXECUTE"}
		if !hasViolation(CheckDisjoint(m, p1b), "contracts") {
			t.Fatal("overlapping promote_worker/EXECUTE not detected")
		}
	})
	t.Run("P1A listing a P1B write contract as a capability", func(t *testing.T) {
		m := p1a
		m.Capabilities = append(append([]Capability(nil), p1a.Capabilities...), Capability{ID: "hcmnext.rewards.change_base_pay", EffectClass: "READ_ONLY"})
		if !hasViolation(CheckDisjoint(m, p1b), "contracts") {
			t.Fatal("change_base_pay capability in P1A not detected")
		}
	})
	t.Run("P1B template re-signed but bound to a different P1A", func(t *testing.T) {
		tpl := cloneTemplate(p1b)
		tpl.P1AManifest.Digest = strings.Repeat("1", 64)
		tpl = signTemplate(t, tpl, priv, pub)
		if !hasViolation(CheckDisjoint(p1a, tpl), "p1a_manifest.digest") {
			t.Fatal("stale P1A binding not detected")
		}
	})
}
