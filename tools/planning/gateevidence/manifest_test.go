package gateevidence

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// repoRoot is the path from this package's directory to the repository
// root: tools/planning/gateevidence is three levels below root.
const repoRoot = "../../.."

func mustLoadP1AManifest(t *testing.T) P1AManifest {
	t.Helper()
	m, err := LoadP1AManifest(repoRoot + "/definitions/planning/gates/p1a-manifest.yaml")
	if err != nil {
		t.Fatalf("LoadP1AManifest: %v", err)
	}
	if violations := m.Validate(); len(violations) != 0 {
		t.Fatalf("p1a-manifest.yaml fails Validate: %v", violations)
	}
	return *m
}

func mustLoadP1BTemplate(t *testing.T) P1BTemplate {
	t.Helper()
	tpl, err := LoadP1BTemplate(repoRoot + "/definitions/planning/gates/p1b-template.yaml")
	if err != nil {
		t.Fatalf("LoadP1BTemplate: %v", err)
	}
	if violations := tpl.Validate(); len(violations) != 0 {
		t.Fatalf("p1b-template.yaml fails Validate: %v", violations)
	}
	return *tpl
}

// TestP1AManifestAndP1BTemplateAreOrderedDisjointAndBounded pins the
// structural half of NEXT-002 inside this package: canonical order,
// disjoint effect scope, the hard effect ceiling and a non-activatable P1B
// template. The named PRIMARY (selection completeness included) lives in
// ./selectionbind, which can import the selecting packages this package
// cannot.
func TestP1AManifestAndP1BTemplateAreOrderedDisjointAndBounded(t *testing.T) {
	p1a := mustLoadP1AManifest(t)
	p1b := mustLoadP1BTemplate(t)

	t.Run("ordered: P1A intents follow next-steps.md's exact eight-intent sequence", func(t *testing.T) {
		wantOrder := []string{
			"hcmnext.people.explain_worker_state/v1",
			"hcmnext.people.promote_worker/v1",
			"hcmnext.rewards.simulate_compensation/v1",
			"hcmnext.rewards.evaluate_pay_band_position/v1",
			"hcmnext.intelligence.explain_transaction/v1",
			"hcmnext.operations.detect_drift/v1",
			"hcmnext.operations.create_repair_plan/v1",
			"hcmnext.operations.simulate_repair/v1",
		}
		if len(p1a.Intents) != len(wantOrder) {
			t.Fatalf("got %d intents, want %d", len(p1a.Intents), len(wantOrder))
		}
		for i, intent := range p1a.Intents {
			if intent.Order != i+1 {
				t.Errorf("intent %s: order = %d, want %d", intent.ID, intent.Order, i+1)
			}
			if intent.ID != wantOrder[i] {
				t.Errorf("intent[%d] = %s, want %s", i, intent.ID, wantOrder[i])
			}
		}
	})

	t.Run("ordered: P1B contracts follow next-steps.md's exact six-contract sequence", func(t *testing.T) {
		wantOrder := []string{
			"promote_worker",
			"change_base_pay",
			"reserve_compensation_budget",
			"release_compensation_budget",
			"approve_proposal",
			"reject_proposal",
		}
		if len(p1b.Contracts) != len(wantOrder) {
			t.Fatalf("got %d contracts, want %d", len(p1b.Contracts), len(wantOrder))
		}
		for i, c := range p1b.Contracts {
			if c.Order != i+1 {
				t.Errorf("contract %s: order = %d, want %d", c.ID, c.Order, i+1)
			}
			if c.ID != wantOrder[i] {
				t.Errorf("contract[%d] = %s, want %s", i, c.ID, wantOrder[i])
			}
		}
	})

	t.Run("disjoint: no (id, mode) tuple is granted by both documents", func(t *testing.T) {
		type tuple struct{ id, mode string }
		p1aGrants := make(map[tuple]bool)
		for _, intent := range p1a.Intents {
			base := strings.TrimSuffix(intent.ID, "/v1")
			base = base[strings.LastIndex(base, ".")+1:]
			if len(intent.Modes) == 0 {
				p1aGrants[tuple{base, ""}] = true
				continue
			}
			for _, mode := range intent.Modes {
				p1aGrants[tuple{base, mode}] = true
			}
		}
		for _, c := range p1b.Contracts {
			key := tuple{c.ID, c.Mode}
			if p1aGrants[key] {
				t.Errorf("P1B contract %s (mode %q) is also granted by the P1A manifest - not disjoint", c.ID, c.Mode)
			}
		}
		if v := CheckDisjoint(p1a, p1b); len(v) != 0 {
			t.Errorf("CheckDisjoint(live P1A, live P1B) = %v, want none", v)
		}
		// promote_worker is the one id both documents name; confirm the
		// P1A grant set for it is exactly {DRAFT, PREFLIGHT, SIMULATE},
		// which is disjoint from P1B's EXECUTE by construction above, but
		// assert it explicitly so a future edit that adds EXECUTE to P1A
		// fails loudly here.
		for mode := range map[string]bool{"DRAFT": true, "PREFLIGHT": true, "SIMULATE": true} {
			if !p1aGrants[tuple{"promote_worker", mode}] {
				t.Errorf("expected P1A to grant promote_worker/%s", mode)
			}
		}
		if p1aGrants[tuple{"promote_worker", "EXECUTE"}] {
			t.Error("P1A manifest must never grant promote_worker/EXECUTE")
		}
	})

	t.Run("bounded: P1A declares the exact zero-effect ceiling and every capability is READ_ONLY", func(t *testing.T) {
		wantCeiling := []string{
			"zero worker, employment, assignment, organization, position, compensation or budget mutations",
			"zero reservations, WorkItems and timers",
			"zero committed external effects, provider writes and MessageIntents",
		}
		if len(p1a.EffectCeiling) != len(wantCeiling) {
			t.Fatalf("got %d effect_ceiling entries, want %d", len(p1a.EffectCeiling), len(wantCeiling))
		}
		for i, want := range wantCeiling {
			if p1a.EffectCeiling[i] != want {
				t.Errorf("effect_ceiling[%d] = %q, want %q", i, p1a.EffectCeiling[i], want)
			}
		}
		for _, c := range p1a.Capabilities {
			if c.EffectClass != "READ_ONLY" {
				t.Errorf("capability %s has effect_class %q, want READ_ONLY", c.ID, c.EffectClass)
			}
		}
		p1aIDs := make(map[string]bool, len(p1a.Capabilities))
		for _, c := range p1a.Capabilities {
			p1aIDs[c.ID] = true
		}
		for _, c := range p1b.Contracts {
			if p1aIDs[c.ID] {
				t.Errorf("P1B contract %s must not appear in the P1A capability list", c.ID)
			}
		}
	})

	t.Run("selection complete: every P1A intent is explicitly INCLUDED", func(t *testing.T) {
		for _, intent := range p1a.Intents {
			if intent.Disposition != "INCLUDED" {
				t.Errorf("intent %s has disposition %q, want INCLUDED", intent.ID, intent.Disposition)
			}
		}
	})

	t.Run("selection complete: the P1B template cannot self-activate", func(t *testing.T) {
		if !p1b.RequiresGateADecision {
			t.Error("p1b-template.yaml must require a Gate A decision")
		}
		if p1b.CanActivate() {
			t.Error("a checked-in P1B template must never be activatable")
		}
		for _, c := range p1b.Contracts {
			if c.ActivationStatus != ActivationBlocked {
				t.Errorf("contract %s has activation_status %q, want %s", c.ID, c.ActivationStatus, ActivationBlocked)
			}
		}
	})
}

// TestTamperedP1AManifestFailsSignatureVerification is NEXT-002's "test
// that a tampered manifest fails verification". It proves the real,
// checked-in manifest verifies, then proves that flipping any single
// signed field invalidates the signature.
func TestTamperedP1AManifestFailsSignatureVerification(t *testing.T) {
	original := mustLoadP1AManifest(t)

	ok, err := VerifyManifestSignature(original)
	if err != nil {
		t.Fatalf("VerifyManifestSignature(original): %v", err)
	}
	if !ok {
		t.Fatal("the checked-in p1a-manifest.yaml must verify against its own signature")
	}

	tamperCases := []struct {
		name   string
		tamper func(*P1AManifest)
	}{
		{"effect ceiling text edited", func(m *P1AManifest) { m.EffectCeiling[0] = "quietly permits worker mutations" }},
		{"an intent silently added", func(m *P1AManifest) {
			m.Intents = append(m.Intents, Intent{Order: 9, ID: "hcmnext.people.terminate_worker/v1", Disposition: "INCLUDED"})
		}},
		{"a migration checksum edited", func(m *P1AManifest) {
			m.Migrations.Files[0].Checksum = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		}},
		{"freshness window loosened", func(m *P1AManifest) { m.FreshnessWindowDays = 3650 }},
		{"forbidden import prefix removed", func(m *P1AManifest) { m.ForbiddenImportPrefixes = nil }},
	}

	for _, tc := range tamperCases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := original
			// Deep-copy the slices this case mutates so other subtests
			// (and the shared `original`) are unaffected.
			tampered.EffectCeiling = append([]string(nil), original.EffectCeiling...)
			tampered.Intents = append([]Intent(nil), original.Intents...)
			tampered.Migrations.Files = append([]MigrationFile(nil), original.Migrations.Files...)
			tampered.ForbiddenImportPrefixes = append([]string(nil), original.ForbiddenImportPrefixes...)

			tc.tamper(&tampered)

			ok, err := VerifyManifestSignature(tampered)
			if err != nil {
				t.Fatalf("VerifyManifestSignature(tampered): %v", err)
			}
			if ok {
				t.Fatalf("tampered manifest (%s) must not verify against the original signature", tc.name)
			}
		})
	}
}

// TestP1AManifestMigrationChecksumsMatchEmbeddedMigrationsFS cross-checks
// the manifest's hand-authored migration list against migrations.Files(),
// the same embedded-checksum function migrate/DB-006 use, so a checksum
// here can never silently drift from the real file bytes (the same drift-
// enforcement pattern TOOL-010 applies to generated code).
func TestP1AManifestMigrationChecksumsMatchEmbeddedMigrationsFS(t *testing.T) {
	p1a := mustLoadP1AManifest(t)

	live, err := migrations.Files()
	if err != nil {
		t.Fatalf("migrations.Files: %v", err)
	}
	liveByVersion := make(map[int64]migrations.File, len(live))
	for _, f := range live {
		liveByVersion[f.Version] = f
	}

	if len(p1a.Migrations.Files) != len(live) {
		t.Fatalf("manifest declares %d migration files, embedded FS has %d", len(p1a.Migrations.Files), len(live))
	}

	for _, declared := range p1a.Migrations.Files {
		liveFile, ok := liveByVersion[declared.Version]
		if !ok {
			t.Errorf("manifest declares migration version %d, which does not exist in the embedded FS", declared.Version)
			continue
		}
		if liveFile.Name != declared.Name {
			t.Errorf("migration %d: manifest name %q, embedded FS name %q", declared.Version, declared.Name, liveFile.Name)
		}
		wantChecksum := "sha256:" + liveFile.Checksum
		if declared.Checksum != wantChecksum {
			t.Errorf("migration %d (%s): manifest checksum %q, embedded FS checksum %q", declared.Version, declared.Name, declared.Checksum, wantChecksum)
		}
	}

	// The one recorded gap (version 9) must actually be absent from the
	// embedded FS - otherwise the manifest is hiding a real migration.
	for _, gap := range p1a.Migrations.Gaps {
		if _, exists := liveByVersion[gap.Version]; exists {
			t.Errorf("manifest records version %d as a gap, but it exists in the embedded FS", gap.Version)
		}
	}
}

func TestP1AManifestValidateRejectsIncompleteManifests(t *testing.T) {
	valid := mustLoadP1AManifest(t)

	cases := []struct {
		name    string
		mutate  func(*P1AManifest)
		wantHit string
	}{
		{"no signature", func(m *P1AManifest) { m.Signature = nil }, "signature"},
		{"no intents", func(m *P1AManifest) { m.Intents = nil }, "intents"},
		{"no effect ceiling", func(m *P1AManifest) { m.EffectCeiling = nil }, "effect_ceiling"},
		{"non-read-only capability", func(m *P1AManifest) { m.Capabilities[0].EffectClass = "READ_WRITE" }, "capabilities"},
		{"no evidence", func(m *P1AManifest) { m.Evidence = nil }, "evidence"},
		{"blended release", func(m *P1AManifest) { m.Release = "P1A+P1B" }, "release"},
		{"EXECUTE granted before Gate A", func(m *P1AManifest) { m.Intents[1].Modes = []string{"DRAFT", "EXECUTE"} }, "intents"},
		{"deferred intent included", func(m *P1AManifest) { m.Intents[0].Disposition = "DEFERRED" }, "intents"},
		{"ceiling text weakened", func(m *P1AManifest) { m.EffectCeiling[2] = "zero committed external effects" }, "effect_ceiling"},
		{"outbox effect dropped", func(m *P1AManifest) { m.ForbiddenEffects = m.ForbiddenEffects[:5] }, "forbidden_effects"},
		{"selection omitted", func(m *P1AManifest) { m.SelectionBindings = m.SelectionBindings[:6] }, "selection_bindings"},
		{"selection substituted", func(m *P1AManifest) { m.SelectionBindings[2].TodoID = "SELECT-009" }, "selection_bindings[2].todo_id"},
		{"selection path escapes root", func(m *P1AManifest) { m.SelectionBindings[0].Path = "../outside.yaml" }, "selection_bindings[0].path"},
		{"selection digest kind unknown", func(m *P1AManifest) { m.SelectionBindings[0].DigestKind = "MD5" }, "selection_bindings[0].digest_kind"},
		{"selection digest missing", func(m *P1AManifest) { m.SelectionBindings[0].Digest = "" }, "selection_bindings[0].digest"},
		{"selection digest uppercase", func(m *P1AManifest) {
			m.SelectionBindings[0].Digest = strings.ToUpper(m.SelectionBindings[0].Digest)
		}, "selection_bindings[0].digest"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := valid
			m.Intents = append([]Intent(nil), valid.Intents...)
			for i := range m.Intents {
				m.Intents[i].Modes = append([]string(nil), valid.Intents[i].Modes...)
			}
			m.Capabilities = append([]Capability(nil), valid.Capabilities...)
			m.EffectCeiling = append([]string(nil), valid.EffectCeiling...)
			m.ForbiddenEffects = append([]string(nil), valid.ForbiddenEffects...)
			m.SelectionBindings = append([]SelectionBinding(nil), valid.SelectionBindings...)
			tc.mutate(&m)
			violations := m.Validate()
			if len(violations) == 0 {
				t.Fatalf("expected a violation for %q, got none", tc.name)
			}
			found := false
			for _, v := range violations {
				if strings.Contains(v.Field, tc.wantHit) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a violation touching field %q, got %v", tc.wantHit, violations)
			}
		})
	}
}
