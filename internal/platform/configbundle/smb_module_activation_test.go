// TestTodo_SMB_CORE_001 covers SMB-CORE-001: optional business modules
// register through existing configuration, capability and page boundaries.
// Compile rejects dependency cycles, undeclared capabilities, duplicate
// semantic ownership and pages without permission mappings; activation is
// revision compare-and-swap with schema-readiness gates; suspension blocks
// new writes while preserving authorized history and observed-result
// recovery; retirement retains referenced records.
package configbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func smbCoreDigest(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func smbCoreModuleFixture() ([]BusinessModuleManifest, map[string]bool) {
	manifests := []BusinessModuleManifest{
		{
			ModuleID: "party", Version: "1.0.0", Owner: "party",
			Capabilities: []string{"hcmnext.party.read@1"},
			Pages:        []ModulePage{{Route: "/workspace/app/party", Permission: "hcmnext.party.read"}},
		},
		{
			ModuleID: "inventory", Version: "1.0.0", Owner: "stock",
			Dependencies: []string{"party"},
			Capabilities: []string{"hcmnext.stock.read@1"},
			Pages:        []ModulePage{{Route: "/workspace/app/stock", Permission: "hcmnext.stock.read"}},
		},
	}
	known := map[string]bool{
		"hcmnext.party.read@1": true,
		"hcmnext.stock.read@1": true,
	}
	return manifests, known
}

func TestTodo_SMB_CORE_001(t *testing.T) {
	manifests, known := smbCoreModuleFixture()

	cyclic := []BusinessModuleManifest{
		{ModuleID: "alpha", Version: "1.0.0", Owner: "alpha", Dependencies: []string{"beta"}},
		{ModuleID: "beta", Version: "1.0.0", Owner: "beta", Dependencies: []string{"alpha"}},
	}
	if _, err := CompileBusinessModules(cyclic, known); !errors.Is(err, ErrModuleCycle) {
		t.Fatalf("dependency cycle err=%v, want module cycle refusal", err)
	} else if !strings.Contains(err.Error(), "alpha -> beta -> alpha") {
		t.Fatalf("cycle error %q lacks deterministic path", err.Error())
	}

	unknownCap := []BusinessModuleManifest{
		{ModuleID: "party", Version: "1.0.0", Owner: "party", Capabilities: []string{"hcmnext.unknown.write@9"}},
	}
	if _, err := CompileBusinessModules(unknownCap, known); !errors.Is(err, ErrModuleUnknownCapability) {
		t.Fatalf("unknown capability err=%v, want unknown-capability refusal", err)
	}

	dupeOwner := []BusinessModuleManifest{
		{ModuleID: "party", Version: "1.0.0", Owner: "party"},
		{ModuleID: "party-v2", Version: "1.0.0", Owner: "party"},
	}
	if _, err := CompileBusinessModules(dupeOwner, known); !errors.Is(err, ErrModuleDuplicateOwner) {
		t.Fatalf("duplicate owner err=%v, want duplicate-owner refusal", err)
	}

	noPermission := []BusinessModuleManifest{
		{ModuleID: "party", Version: "1.0.0", Owner: "party", Pages: []ModulePage{{Route: "/workspace/app/party"}}},
	}
	if _, err := CompileBusinessModules(noPermission, known); !errors.Is(err, ErrModulePagePermission) {
		t.Fatalf("page without permission err=%v, want page-permission refusal", err)
	}

	workflowOwner := []BusinessModuleManifest{
		{ModuleID: "sneaky", Version: "1.0.0", Owner: "workflow"},
	}
	if _, err := CompileBusinessModules(workflowOwner, known); !errors.Is(err, ErrModuleReservedOwner) {
		t.Fatalf("workflow-state owner err=%v, want reserved-owner refusal", err)
	}

	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatalf("valid module set refused: %v", err)
	}
	scope := Scope{TenantID: "tenant-a"}
	ready := map[string]bool{"party": true, "inventory": true}
	partyRev, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", BundleDigest: smbCoreDigest("party-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil || partyRev.Revision != 1 || partyRev.State != ModuleStateEnabled {
		t.Fatalf("party activation = %+v, err=%v", partyRev, err)
	}
	invRev, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "inventory", BundleDigest: smbCoreDigest("inventory-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil || invRev.Revision != 1 || invRev.Predecessor != 0 {
		t.Fatalf("inventory activation = %+v, err=%v", invRev, err)
	}
	if err := registry.AuthorizeWrite(scope, "inventory", invRev.Revision); err != nil {
		t.Fatalf("enabled module write refused: %v", err)
	}

	suspended, err := registry.Suspend(scope, "inventory", "operator hold", invRev.Revision)
	if err != nil || suspended.State != ModuleStateSuspended || suspended.Revision != 2 {
		t.Fatalf("suspend = %+v, err=%v", suspended, err)
	}
	if err := registry.AuthorizeWrite(scope, "inventory", suspended.Revision); !errors.Is(err, ErrModuleWritesBlocked) {
		t.Fatalf("suspended module write err=%v, want writes-blocked refusal", err)
	}
	if err := registry.AuthorizeRead(scope, "inventory"); err != nil {
		t.Fatalf("suspended module history read refused: %v", err)
	}

	retired, err := registry.Retire(scope, "inventory", "replaced by v2", suspended.Revision)
	if err != nil || retired.State != ModuleStateRetired {
		t.Fatalf("retire = %+v, err=%v", retired, err)
	}
	history := registry.History(scope, "inventory")
	if len(history) != 3 || history[0].State != ModuleStateEnabled || history[2].State != ModuleStateRetired {
		t.Fatalf("retirement dropped referenced records: %+v", history)
	}
	if err := registry.AuthorizeRead(scope, "inventory"); err != nil {
		t.Fatalf("retired module history read refused: %v", err)
	}

	view := registry.ResolveView(scope, "inventory", ready)
	if view.EffectiveStatus != string(ModuleStateRetired) || view.HistoricalReadPolicy != ModuleHistoryPreserved {
		t.Fatalf("retired module view = %+v, want retired status with preserved history", view)
	}
}

func TestTodo_SMB_CORE_001_Integration(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}

	missingParty := map[string]bool{"inventory": true}
	if _, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "inventory", BundleDigest: smbCoreDigest("inventory-bundle"), Scope: scope, SchemaReady: missingParty,
	}); !errors.Is(err, ErrModuleDependencyNotReady) {
		t.Fatalf("missing party schema err=%v, want dependency-not-ready refusal", err)
	}
	if revs := registry.History(scope, "inventory"); len(revs) != 0 {
		t.Fatalf("failed activation left %d revisions, want no active pointer", len(revs))
	}

	if _, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", BundleDigest: smbCoreDigest("party-bundle"), Scope: scope, SchemaReady: map[string]bool{"party": true},
	}); err != nil {
		t.Fatal(err)
	}
	ready := map[string]bool{"party": true, "inventory": true}
	first, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "inventory", BundleDigest: smbCoreDigest("inventory-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "inventory", BundleDigest: smbCoreDigest("inventory-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil || replay.Revision != first.Revision {
		t.Fatalf("activation replay = %+v, err=%v; want the same single revision", replay, err)
	}
	if revs := registry.History(scope, "inventory"); len(revs) != 1 {
		t.Fatalf("replay left %d revisions, want exactly one", len(revs))
	}

	if _, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", ExpectedPredecessor: 7, BundleDigest: smbCoreDigest("party-bundle-v2"),
		Scope: scope, SchemaReady: map[string]bool{"party": true},
	}); !errors.Is(err, ErrModuleRevisionConflict) {
		t.Fatalf("stale predecessor err=%v, want revision-conflict refusal", err)
	}
	if err := registry.AuthorizeWrite(scope, "party", 99); !errors.Is(err, ErrModuleRevisionConflict) {
		t.Fatalf("stale dispatch revision err=%v, want revision-conflict refusal", err)
	}
}

func TestTodo_SMB_CORE_001_Security(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	tenantA := Scope{TenantID: "tenant-a"}
	tenantB := Scope{TenantID: "tenant-b"}
	ready := map[string]bool{"party": true, "inventory": true}
	rev, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", BundleDigest: smbCoreDigest("party-bundle"), Scope: tenantA, SchemaReady: ready,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := registry.AuthorizeWrite(tenantB, "party", rev.Revision); !errors.Is(err, ErrModuleNotActive) {
		t.Fatalf("cross-scope write err=%v, want generic not-active refusal", err)
	}
	if err := registry.AuthorizeWrite(tenantB, "no-such-module", 1); !errors.Is(err, ErrModuleNotActive) {
		t.Fatalf("unknown module write err=%v, want the same generic not-active refusal", err)
	}
	if err := registry.AuthorizeRead(tenantB, "party"); !errors.Is(err, ErrModuleNotActive) {
		t.Fatalf("cross-scope history read err=%v, want generic not-active refusal", err)
	}
	if revs := registry.History(tenantB, "party"); len(revs) != 0 {
		t.Fatalf("cross-scope history disclosed %d revisions", len(revs))
	}
	view := registry.ResolveView(tenantB, "party", ready)
	if view.EffectiveStatus != ModuleStatusAvailable || len(view.MissingDependencyReasons) != 0 {
		t.Fatalf("cross-scope view = %+v, want available status with no internals", view)
	}

	if _, err := registry.Suspend(tenantA, "party", "hold", rev.Revision); err != nil {
		t.Fatal(err)
	}
	if err := registry.AuthorizeWrite(tenantA, "party", rev.Revision+1); !errors.Is(err, ErrModuleWritesBlocked) {
		t.Fatalf("suspended write err=%v, want writes-blocked refusal", err)
	}
	if err := registry.AuthorizeWrite(tenantA, "party", rev.Revision); !errors.Is(err, ErrModuleRevisionConflict) {
		t.Fatalf("pre-suspend dispatch err=%v, want revision-conflict refusal", err)
	}
}

func TestTodo_SMB_CORE_001_Fault(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	ready := map[string]bool{"party": true, "inventory": true}
	rev, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "inventory", BundleDigest: smbCoreDigest("inventory-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil {
		t.Fatal(err)
	}

	preparedRevision := rev.Revision
	if err := registry.AuthorizeWrite(scope, "inventory", preparedRevision); err != nil {
		t.Fatalf("prepared dispatch refused before suspension: %v", err)
	}
	if _, err := registry.Suspend(scope, "inventory", "suspend between prepare and dispatch", preparedRevision); err != nil {
		t.Fatal(err)
	}
	if err := registry.AuthorizeWrite(scope, "inventory", preparedRevision); !errors.Is(err, ErrModuleRevisionConflict) {
		t.Fatalf("post-suspend dispatch of prepared revision err=%v, want revision-conflict refusal", err)
	}

	deduped, err := registry.CompleteObservedEffect(scope, "inventory", preparedRevision, "effect-1")
	if err != nil || deduped {
		t.Fatalf("observed-result recovery = deduped %v, err=%v; want first completion", deduped, err)
	}
	deduped, err = registry.CompleteObservedEffect(scope, "inventory", preparedRevision, "effect-1")
	if err != nil || !deduped {
		t.Fatalf("observed-result retry = deduped %v, err=%v; want idempotent recovery", deduped, err)
	}
	if _, err := registry.CompleteObservedEffect(scope, "inventory", 99, "effect-2"); !errors.Is(err, ErrModuleRevisionConflict) {
		t.Fatalf("completion for unknown revision err=%v, want revision-conflict refusal", err)
	}
}

func TestTodo_SMB_CORE_001_Recovery(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	ready := map[string]bool{"party": true, "inventory": true}
	rev, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "inventory", BundleDigest: smbCoreDigest("inventory-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.CompleteObservedEffect(scope, "inventory", rev.Revision, "effect-1"); err != nil {
		t.Fatal(err)
	}

	snapshot, err := registry.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := restored.AuthorizeWrite(scope, "inventory", rev.Revision); err != nil {
		t.Fatalf("restored activation lost its write authority: %v", err)
	}
	replay, err := restored.Activate(ModuleActivationRequest{
		ModuleID: "inventory", BundleDigest: smbCoreDigest("inventory-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil || replay.Revision != rev.Revision {
		t.Fatalf("post-restore replay = %+v, err=%v; want the same single revision", replay, err)
	}
	if revs := restored.History(scope, "inventory"); len(revs) != 1 {
		t.Fatalf("post-restore history has %d revisions, want exactly one", len(revs))
	}
	deduped, err := restored.CompleteObservedEffect(scope, "inventory", rev.Revision, "effect-1")
	if err != nil || !deduped {
		t.Fatalf("post-restore completion retry = deduped %v, err=%v; want no duplicate work", deduped, err)
	}
	if err := restored.Import([]byte("{not-json")); err == nil {
		t.Fatal("corrupt snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_CORE_001_Browser(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	ready := map[string]bool{"party": true, "inventory": true}

	pending := registry.ResolveView(scope, "inventory", map[string]bool{"inventory": true})
	if pending.EffectiveStatus != ModuleStatusDependencyNotReady {
		t.Fatalf("unready module view = %+v, want dependency-not-ready status", pending)
	}
	if len(pending.MissingDependencyReasons) != 1 || !strings.Contains(pending.MissingDependencyReasons[0], "party") {
		t.Fatalf("unready module view reasons = %v, want the missing party schema", pending.MissingDependencyReasons)
	}
	if len(pending.AvailableActions) != 0 {
		t.Fatalf("unready module view offers %v, want no executable actions", pending.AvailableActions)
	}

	rev, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "inventory", BundleDigest: smbCoreDigest("inventory-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil {
		t.Fatal(err)
	}
	enabled := registry.ResolveView(scope, "inventory", ready)
	if enabled.EffectiveStatus != string(ModuleStateEnabled) || enabled.ActiveRevision != rev.Revision {
		t.Fatalf("enabled module view = %+v, want enabled status at the active revision", enabled)
	}
	if len(enabled.AvailableActions) == 0 || enabled.AvailableActions[0] != ModuleActionSuspend {
		t.Fatalf("enabled module view actions = %v, want suspend first", enabled.AvailableActions)
	}

	if _, err := registry.Suspend(scope, "inventory", "operator hold", rev.Revision); err != nil {
		t.Fatal(err)
	}
	suspended := registry.ResolveView(scope, "inventory", ready)
	if suspended.EffectiveStatus != string(ModuleStateSuspended) || suspended.HistoricalReadPolicy != ModuleHistoryPreserved {
		t.Fatalf("suspended module view = %+v, want suspended status with preserved history", suspended)
	}
	if err := registry.AuthorizeWrite(scope, "inventory", suspended.ActiveRevision); !errors.Is(err, ErrModuleWritesBlocked) {
		t.Fatalf("action discovery offered a write the server still refuses: view=%+v", suspended)
	}
}

func TestTodo_SMB_CORE_001_RetiredTerminal(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	ready := map[string]bool{"party": true, "inventory": true}
	rev, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", BundleDigest: smbCoreDigest("party-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil {
		t.Fatal(err)
	}
	retired, err := registry.Retire(scope, "party", "replaced", rev.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", ExpectedPredecessor: retired.Revision,
		BundleDigest: smbCoreDigest("party-bundle"), Scope: scope, SchemaReady: ready,
	}); !errors.Is(err, ErrModuleRetired) {
		t.Fatalf("activate after retire err=%v, want retired-terminal refusal", err)
	}
	view := registry.ResolveView(scope, "party", ready)
	if len(view.AvailableActions) != 0 {
		t.Fatalf("retired view offers %v, want no actions", view.AvailableActions)
	}
	if err := registry.AuthorizeWrite(scope, "party", retired.Revision); !errors.Is(err, ErrModuleWritesBlocked) {
		t.Fatalf("retired write err=%v, want writes-blocked refusal", err)
	}
}

func TestTodo_SMB_CORE_001_ReasonRequired(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	ready := map[string]bool{"party": true, "inventory": true}
	rev, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", BundleDigest: smbCoreDigest("party-bundle"), Scope: scope, SchemaReady: ready,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Suspend(scope, "party", "", rev.Revision); err == nil {
		t.Fatal("suspend without reason succeeded, want refusal")
	}
	if _, err := registry.Suspend(scope, "party", "  ", rev.Revision); err == nil {
		t.Fatal("suspend with blank reason succeeded, want refusal")
	}
	if _, err := registry.Retire(scope, "party", "", rev.Revision); err == nil {
		t.Fatal("retire without reason succeeded, want refusal")
	}
	suspended, err := registry.Suspend(scope, "party", "hold", rev.Revision)
	if err != nil || suspended.Reason != "hold" {
		t.Fatalf("reasoned suspend = %+v, err=%v", suspended, err)
	}
}

func TestTodo_SMB_CORE_001_MalformedDigestRefused(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	ready := map[string]bool{"party": true, "inventory": true}
	for _, digest := range []string{"", "sha256:not-hex", "md5:abc", "sha256:party-bundle"} {
		if _, err := registry.Activate(ModuleActivationRequest{
			ModuleID: "party", BundleDigest: digest, Scope: scope, SchemaReady: ready,
		}); err == nil {
			t.Fatalf("activate with digest %q succeeded, want refusal", digest)
		}
	}
	if revs := registry.History(scope, "party"); len(revs) != 0 {
		t.Fatalf("malformed digests left %d revisions, want none", len(revs))
	}
}

func smbCoreExportMap(t *testing.T, registry *ModuleRegistry) map[string]any {
	t.Helper()
	raw, err := registry.Export()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func smbCoreImportMap(t *testing.T, registry *ModuleRegistry, decoded map[string]any) error {
	t.Helper()
	raw, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	return registry.Import(raw)
}

func TestTodo_SMB_CORE_001_ScopeSwapRefused(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	scopeA := Scope{TenantID: "tenant-a"}
	if _, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", BundleDigest: smbCoreDigest("party-bundle"), Scope: scopeA,
		SchemaReady: map[string]bool{"party": true, "inventory": true},
	}); err != nil {
		t.Fatal(err)
	}
	decoded := smbCoreExportMap(t, registry)
	swappedKey := "tenant-b\x00"
	for _, section := range []string{"active", "history"} {
		entries := decoded[section].(map[string]any)
		var keys []string
		for key := range entries {
			keys = append(keys, key)
		}
		if len(keys) == 0 {
			t.Fatalf("exported snapshot has no %s entries to swap", section)
		}
		for _, key := range keys {
			entries[swappedKey] = entries[key]
			delete(entries, key)
		}
	}
	fresh, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	if err := smbCoreImportMap(t, fresh, decoded); err == nil {
		t.Fatal("scope-swapped snapshot import succeeded, want refusal")
	}
	if err := fresh.AuthorizeWrite(Scope{TenantID: "tenant-b"}, "party", 1); !errors.Is(err, ErrModuleNotActive) {
		t.Fatalf("scope-swapped write err=%v, want generic not-active refusal", err)
	}
}

func TestTodo_SMB_CORE_001_PhantomActiveRefused(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", BundleDigest: smbCoreDigest("party-bundle"), Scope: Scope{TenantID: "tenant-a"},
		SchemaReady: map[string]bool{"party": true, "inventory": true},
	}); err != nil {
		t.Fatal(err)
	}
	decoded := smbCoreExportMap(t, registry)
	history := decoded["history"].(map[string]any)
	for key := range history {
		history[key] = map[string]any{}
	}
	fresh, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	if err := smbCoreImportMap(t, fresh, decoded); err == nil {
		t.Fatal("active-without-history snapshot import succeeded, want refusal")
	}

	registry2, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry2.Activate(ModuleActivationRequest{
		ModuleID: "party", BundleDigest: smbCoreDigest("party-bundle"), Scope: Scope{TenantID: "tenant-a"},
		SchemaReady: map[string]bool{"party": true, "inventory": true},
	}); err != nil {
		t.Fatal(err)
	}
	decoded2 := smbCoreExportMap(t, registry2)
	active := decoded2["active"].(map[string]any)
	for key := range active {
		active[key] = map[string]any{}
	}
	fresh2, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	if err := smbCoreImportMap(t, fresh2, decoded2); err == nil {
		t.Fatal("history-without-active snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_CORE_001_SnapshotRevalidation(t *testing.T) {
	manifests, known := smbCoreModuleFixture()
	registry, err := CompileBusinessModules(manifests, known)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-a"}
	rev, err := registry.Activate(ModuleActivationRequest{
		ModuleID: "party", BundleDigest: smbCoreDigest("party-bundle"), Scope: scope,
		SchemaReady: map[string]bool{"party": true, "inventory": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.CompleteObservedEffect(scope, "party", rev.Revision, "effect-1"); err != nil {
		t.Fatal(err)
	}

	mutate := func(t *testing.T, change func(decoded map[string]any), want string) {
		t.Helper()
		decoded := smbCoreExportMap(t, registry)
		change(decoded)
		fresh, err := CompileBusinessModules(manifests, known)
		if err != nil {
			t.Fatal(err)
		}
		if err := smbCoreImportMap(t, fresh, decoded); err == nil {
			t.Fatalf("%s: tampered snapshot import succeeded, want refusal", want)
		}
	}

	scopeKey := "tenant-a\x00"
	revisionAt := func(decoded map[string]any, section string) map[string]any {
		return decoded[section].(map[string]any)[scopeKey].(map[string]any)["party"].(map[string]any)
	}
	chainFirst := func(decoded map[string]any) map[string]any {
		return decoded["history"].(map[string]any)[scopeKey].(map[string]any)["party"].([]any)[0].(map[string]any)
	}

	mutate(t, func(decoded map[string]any) {
		revisionAt(decoded, "active")["bundle_digest"] = "sha256:zzz"
	}, "malformed digest")
	mutate(t, func(decoded map[string]any) {
		revisionAt(decoded, "active")["state"] = "launched"
	}, "unknown state")
	mutate(t, func(decoded map[string]any) {
		revisionAt(decoded, "active")["reason"] = "sneaky"
	}, "enabled revision with reason")
	mutate(t, func(decoded map[string]any) {
		chainFirst(decoded)["module_id"] = "inventory"
	}, "misfiled module")
	mutate(t, func(decoded map[string]any) {
		chainFirst(decoded)["revision"] = float64(7)
	}, "broken chain")
	mutate(t, func(decoded map[string]any) {
		decoded["manifests"].([]any)[0].(map[string]any)["owner"] = "someone-else"
	}, "manifest drift")
	mutate(t, func(decoded map[string]any) {
		decoded["completed"] = append(decoded["completed"].([]any), "bogus")
	}, "malformed completion key")
	mutate(t, func(decoded map[string]any) {
		decoded["completed"] = append(decoded["completed"].([]any), "tenant-a\x00\x00party\x0099\x00effect-9")
	}, "completion for unknown revision")
}
