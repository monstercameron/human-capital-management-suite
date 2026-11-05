// TestTodo_SMB_SETUP_003 covers SMB-SETUP-003: optional business starter
// templates compile against a finite catalog, apply idempotently with
// override preservation, never weaken tenant mandatory restrictions, execute
// only through the existing versioned workflow runtime, and activate only
// under a distinct valid approval authority.
package configbundle

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config/promotion"
	wfversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func smbStarterCatalog(t *testing.T) StarterCatalog {
	t.Helper()
	workflows := wfversion.NewRegistry()
	for _, v := range []wfversion.CompiledVersion{
		{WorkflowID: "onboard-customer", CompiledPlanDigest: "plan-onboard", SemanticVersion: "1.0.0", Status: wfversion.StatusActive},
		{WorkflowID: "dispatch-job", CompiledPlanDigest: "plan-dispatch", SemanticVersion: "1.0.0", Status: wfversion.StatusActive},
		{WorkflowID: "run-recurrence", CompiledPlanDigest: "plan-recurrence", SemanticVersion: "1.0.0", Status: wfversion.StatusActive},
	} {
		if err := workflows.Put(v); err != nil {
			t.Fatalf("put workflow fixture: %v", err)
		}
	}
	return StarterCatalog{
		Modules:   smbSetupModules(t),
		Workflows: workflows,
		Terminology: map[string]bool{
			"engagement": true, "time-entry": true, "dispatch": true, "recurrence": true,
		},
	}
}

func smbStarterManifests() []StarterProfileManifest {
	return []StarterProfileManifest{
		{
			ProfileID: "professional-services", Version: "1.0.0",
			Modules: []string{"party"}, Workflows: []StarterWorkflowRef{{WorkflowID: "onboard-customer", CompiledPlanDigest: "plan-onboard"}},
			Terminology: []string{"engagement", "time-entry"},
			Roles:       []StarterRole{{Role: "engagement-lead", ApprovalRequired: true}},
			Forms:       []string{"engagement-intake"}, RequiredConfig: []string{"billing-connection"},
			Settings: map[string]string{"approval.discount.threshold": "200", "sod.finance": "enforced"},
		},
		{
			ProfileID: "field-services", Version: "1.0.0",
			Modules: []string{"party", "inventory"}, Workflows: []StarterWorkflowRef{{WorkflowID: "dispatch-job", CompiledPlanDigest: "plan-dispatch"}},
			Terminology: []string{"dispatch"},
			Roles:       []StarterRole{{Role: "dispatcher", ApprovalRequired: true}},
			Forms:       []string{"job-checklist"}, RequiredConfig: []string{"materials-site"},
			Settings: map[string]string{"approval.discount.threshold": "200", "sod.finance": "enforced"},
		},
		{
			ProfileID: "recurring-services", Version: "1.0.0",
			Modules: []string{"party"}, Workflows: []StarterWorkflowRef{{WorkflowID: "run-recurrence", CompiledPlanDigest: "plan-recurrence"}},
			Terminology: []string{"recurrence"},
			Roles:       []StarterRole{{Role: "agreement-owner", ApprovalRequired: true}},
			Forms:       []string{"agreement-form"}, RequiredConfig: []string{"recurrence-calendar"},
			Settings: map[string]string{"approval.discount.threshold": "200", "sod.finance": "enforced"},
		},
		{
			ProfileID: "field-services-broken", Version: "1.0.0",
			Modules: []string{"party", "no-such-module"}, Workflows: []StarterWorkflowRef{{WorkflowID: "ghost-flow", CompiledPlanDigest: "plan-ghost"}},
			Terminology: []string{"dispatch", "no-such-term"},
			Settings:    map[string]string{"approval.discount.threshold": "200"},
		},
	}
}

func smbStarterRegistry(t *testing.T, at time.Time) (*StarterProfileRegistry, *promotion.Registry) {
	t.Helper()
	approvals := promotion.NewRegistry()
	planner := NewSetupPlanner(smbSetupModules(t), approvals)
	registry := NewStarterProfileRegistry(planner, smbStarterCatalog(t))
	return registry, approvals
}

// errStarterSimulatedCrash stands in for a server kill between the adoption
// record and the response: recorded state survives, the response is lost,
// and replay must converge without a second revision.
var errStarterSimulatedCrash = errors.New("configbundle: simulated starter crash")

var smbStarterPinSeq = 0

func smbStarterPin(t *testing.T, approvals *promotion.Registry, tenant string, at time.Time) BaselinePin {
	t.Helper()
	smbStarterPinSeq++
	approval, _ := smbSetupApproval(t, approvals, fmt.Sprintf("starter-baseline-%d", smbStarterPinSeq), "release", "release-authority", at)
	return BaselinePin{PackageID: approval.PackageID, Digest: approval.Digest, Scope: Scope{TenantID: tenant}, Approver: approval.Approver, ExpiresAt: at.Add(24 * time.Hour)}
}

func TestTodo_SMB_SETUP_003(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	registry, approvals := smbStarterRegistry(t, at)

	compiled, err := registry.Compile(smbStarterManifests())
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) != 4 {
		t.Fatalf("compiled %d profiles, want 4", len(compiled))
	}
	byID := make(map[string]CompiledStarterProfile, len(compiled))
	for _, c := range compiled {
		byID[c.Manifest.ProfileID] = c
	}
	for _, id := range []string{"professional-services", "field-services", "recurring-services"} {
		if byID[id].Status != StarterProfileAvailable || len(byID[id].MissingRefs) != 0 {
			t.Fatalf("profile %s = %+v, want available with no missing refs", id, byID[id])
		}
		if byID[id].ProductionReady {
			t.Fatalf("profile %s claims production Ready from a catalog fixture", id)
		}
	}
	broken := byID["field-services-broken"]
	if broken.Status != StarterProfilePlanned {
		t.Fatalf("broken profile = %+v, want Planned", broken)
	}
	for _, want := range []string{"no-such-module", "ghost-flow", "no-such-term"} {
		if !containsString(broken.MissingRefs, want) {
			t.Fatalf("broken profile missing refs = %v, want %q", broken.MissingRefs, want)
		}
	}
	// Criterion 4: every compiled workflow pin resolves through the existing
	// versioned runtime; the profile carries pins, never an executor.
	for _, c := range compiled[:3] {
		for _, ref := range c.Manifest.Workflows {
			if _, err := wfversion.Resolve(registry.Workflows(), ref.WorkflowID, wfversion.Pin{CompiledPlanDigest: ref.CompiledPlanDigest}); err != nil {
				t.Fatalf("profile %s pin %v does not resolve: %v", c.Manifest.ProfileID, ref, err)
			}
		}
	}

	apply := StarterAdoptionRequest{
		Actor: "ada.admin", AdoptionID: "adopt-1", Scope: Scope{TenantID: "tenant-a"},
		ProfileID: "professional-services", CompiledDigest: byID["professional-services"].Digest,
		Baseline:       smbStarterPin(t, approvals, "tenant-a", at),
		Overrides:      map[string]string{"approval.discount.threshold": "500"},
		IdempotencyKey: "key-1", At: at,
	}
	first, err := registry.Apply(apply)
	if err != nil || first.Revision != 1 || first.Status != StarterAdoptionDraft {
		t.Fatalf("first apply = %+v, err=%v", first, err)
	}
	replay, err := registry.Apply(apply)
	if err != nil || replay.Revision != 1 || replay.Digest != first.Digest {
		t.Fatalf("same-key reapply = %+v, err=%v; want the single revision 1", replay, err)
	}
	if got := registry.HistoryLength(Scope{TenantID: "tenant-a"}, "adopt-1"); got != 1 {
		t.Fatalf("adoption history has %d revisions, want one", got)
	}
	clash := apply
	clash.ProfileID = "field-services"
	clash.CompiledDigest = byID["field-services"].Digest
	if _, err := registry.Apply(clash); !errors.Is(err, ErrStarterIdempotencyConflict) {
		t.Fatalf("reused key on another profile err=%v, want idempotency conflict", err)
	}

	published, err := registry.Publish(Scope{TenantID: "tenant-a"}, "adopt-1", "ada.admin", 1, at.Add(time.Minute))
	if err != nil || published.Status != StarterAdoptionActive || !published.BaselineActive {
		t.Fatalf("publish = %+v, err=%v; want Active with the distinct-approved baseline", published, err)
	}
	if got := published.EffectiveSettings()["approval.discount.threshold"]; got != "500" {
		t.Fatalf("effective threshold = %q, want the preserved tenant override 500", got)
	}

	// Upgrade under a new key creates a draft diff and never silently
	// replaces the live workflow versions or the customized approval rule.
	manifests := smbStarterManifests()
	manifests[0].Version = "1.1.0"
	manifests[0].Settings["approval.discount.threshold"] = "250"
	if _, err := registry.Compile(manifests[:1]); err != nil {
		t.Fatal(err)
	}
	upgrade, err := registry.Upgrade(StarterUpgradeRequest{
		Actor: "ada.admin", AdoptionID: "adopt-1", Scope: Scope{TenantID: "tenant-a"},
		NewDigest:      registry.Compiled("professional-services").Digest,
		IdempotencyKey: "key-2", ExpectedRevision: 2, At: at.Add(2 * time.Minute),
	})
	if err != nil || upgrade.Revision != 3 || upgrade.Status != StarterAdoptionDraft {
		t.Fatalf("upgrade = %+v, err=%v; want a draft revision 3", upgrade, err)
	}
	if got := upgrade.EffectiveSettings()["approval.discount.threshold"]; got != "500" {
		t.Fatalf("upgraded threshold = %q, want the preserved override 500, not 250", got)
	}
	seen := false
	for _, diff := range upgrade.EffectiveDiff {
		if diff.Key == "approval.discount.threshold" && diff.Decision == StarterDiffKeepCurrent && diff.Previous == "500" && diff.Proposed == "250" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("upgrade diff = %+v, want keep-current 500 over proposed 250", upgrade.EffectiveDiff)
	}
}

func TestTodo_SMB_SETUP_003_Security(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	registry, approvals := smbStarterRegistry(t, at)
	if _, err := registry.Compile(smbStarterManifests()); err != nil {
		t.Fatal(err)
	}
	digest := registry.Compiled("professional-services").Digest
	registry.SetTenantMandatory("tenant-a", map[string]string{"sod.finance": "enforced"})
	registry.SetAliases(map[string]string{"trennung.finanzen": "sod.finance"})

	apply := StarterAdoptionRequest{
		Actor: "ada.admin", AdoptionID: "adopt-1", Scope: Scope{TenantID: "tenant-a"},
		ProfileID: "professional-services", CompiledDigest: digest,
		Baseline:       smbStarterPin(t, approvals, "tenant-a", at),
		IdempotencyKey: "key-1", At: at,
	}
	if _, err := registry.Apply(apply); err != nil {
		t.Fatal(err)
	}
	if _, visible := registry.View(Scope{TenantID: "tenant-b"}, "adopt-1", "ada.admin"); visible {
		t.Fatal("cross-tenant adoption view is visible, want not-found")
	}
	if _, visible := registry.View(Scope{TenantID: "tenant-a"}, "adopt-1", "mallory"); visible {
		t.Fatal("non-admin adoption view is visible, want not-found")
	}
	forged := apply
	forged.AdoptionID = "adopt-2"
	forged.Scope = Scope{TenantID: "tenant-b"}
	forged.IdempotencyKey = "key-2"
	if _, err := registry.Apply(forged); !errors.Is(err, ErrStarterNotFound) {
		t.Fatalf("cross-tenant baseline reuse err=%v, want neutral not-found", err)
	}

	// The profile proposes relaxing a tenant mandatory restriction: publish
	// fails with the specific policy finding and the adoption is Blocked.
	relaxed := smbStarterManifests()
	relaxed[0].Settings["sod.finance"] = "relaxed"
	if _, err := registry.Compile(relaxed[:1]); err != nil {
		t.Fatal(err)
	}
	relaxedApply := StarterAdoptionRequest{
		Actor: "ada.admin", AdoptionID: "adopt-relax", Scope: Scope{TenantID: "tenant-a"},
		ProfileID: "professional-services", CompiledDigest: registry.Compiled("professional-services").Digest,
		Baseline:       smbStarterPin(t, approvals, "tenant-a", at),
		IdempotencyKey: "key-relax", At: at,
	}
	if _, err := registry.Apply(relaxedApply); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(Scope{TenantID: "tenant-a"}, "adopt-relax", "ada.admin", 1, at.Add(time.Minute)); !errors.Is(err, ErrStarterPolicy) {
		t.Fatalf("mandatory-removal publish err=%v, want a specific policy finding", err)
	} else if !strings.Contains(err.Error(), "sod.finance") {
		t.Fatalf("policy finding %q does not name the restricted setting", err.Error())
	}
	blocked, visible := registry.View(Scope{TenantID: "tenant-a"}, "adopt-relax", "ada.admin")
	if !visible || blocked.Status != StarterAdoptionBlocked || !strings.Contains(blocked.BlockedReason, "sod.finance") {
		t.Fatalf("blocked view = %+v, want Blocked naming sod.finance", blocked)
	}

	// A localized alias for the same canonical setting fails identically;
	// stable identities never change through presentation labels.
	aliased := smbStarterManifests()
	aliased[0].ProfileID = "professional-services-alias"
	aliased[0].Settings = map[string]string{"trennung.finanzen": "relaxed"}
	if _, err := registry.Compile(aliased[:1]); err != nil {
		t.Fatal(err)
	}
	aliasApply := relaxedApply
	aliasApply.AdoptionID = "adopt-alias"
	aliasApply.ProfileID = "professional-services-alias"
	aliasApply.CompiledDigest = registry.Compiled("professional-services-alias").Digest
	aliasApply.IdempotencyKey = "key-alias"
	if _, err := registry.Apply(aliasApply); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(Scope{TenantID: "tenant-a"}, "adopt-alias", "ada.admin", 1, at.Add(2*time.Minute)); !errors.Is(err, ErrStarterPolicy) {
		t.Fatalf("alias mandatory-removal publish err=%v, want a specific policy finding", err)
	} else if !strings.Contains(err.Error(), "sod.finance") {
		t.Fatalf("alias policy finding %q does not name the canonical sod.finance", err.Error())
	}

	// Self-approval and tampered pins are refused; the UI must explain that
	// a distinct approval is required, never weaken the guard.
	selfApproval, signerKey := smbSetupApproval(t, approvals, "starter-self", "release", "release-authority", at)
	_ = selfApproval
	selfPin := BaselinePin{PackageID: "starter-self", Digest: "whatever", Scope: Scope{TenantID: "tenant-a"}, Approver: signerKey, ExpiresAt: at.Add(24 * time.Hour)}
	selfApply := apply
	selfApply.AdoptionID = "adopt-self"
	selfApply.CompiledDigest = digest
	selfApply.Baseline = selfPin
	selfApply.IdempotencyKey = "key-self"
	if _, err := registry.Apply(selfApply); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(Scope{TenantID: "tenant-a"}, "adopt-self", "ada.admin", 1, at.Add(3*time.Minute)); !errors.Is(err, ErrSetupBaselineRefused) {
		t.Fatalf("self-approval publish err=%v, want baseline refusal", err)
	}
	selfView, _ := registry.View(Scope{TenantID: "tenant-a"}, "adopt-self", "ada.admin")
	if selfView.Status != StarterAdoptionBlocked || !strings.Contains(selfView.BlockedReason, "distinct approval") {
		t.Fatalf("self-approval view = %+v, want Blocked demanding distinct approval", selfView)
	}
	tampered := apply
	tampered.AdoptionID = "adopt-tamper"
	tampered.Baseline.Digest = "deadbeef"
	tampered.IdempotencyKey = "key-tamper"
	if _, err := registry.Apply(tampered); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(Scope{TenantID: "tenant-a"}, "adopt-tamper", "ada.admin", 1, at.Add(4*time.Minute)); !errors.Is(err, ErrSetupBaselineRefused) {
		t.Fatalf("tampered-pin publish err=%v, want baseline refusal", err)
	}
	expired := apply
	expired.AdoptionID = "adopt-expired"
	expired.Baseline.ExpiresAt = at.Add(-time.Hour)
	expired.IdempotencyKey = "key-expired"
	if _, err := registry.Apply(expired); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(Scope{TenantID: "tenant-a"}, "adopt-expired", "ada.admin", 1, at.Add(5*time.Minute)); !errors.Is(err, ErrSetupBaselineRefused) {
		t.Fatalf("expired-pin publish err=%v, want baseline refusal", err)
	}
}

func TestTodo_SMB_SETUP_003_Fault(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	registry, approvals := smbStarterRegistry(t, at)
	if _, err := registry.Compile(smbStarterManifests()); err != nil {
		t.Fatal(err)
	}
	apply := StarterAdoptionRequest{
		Actor: "ada.admin", AdoptionID: "adopt-1", Scope: Scope{TenantID: "tenant-a"},
		ProfileID: "professional-services", CompiledDigest: registry.Compiled("professional-services").Digest,
		Baseline:       smbStarterPin(t, approvals, "tenant-a", at),
		IdempotencyKey: "key-1", At: at,
	}
	registry.AfterRecord = func() error { return errStarterSimulatedCrash }
	if _, err := registry.Apply(apply); !errors.Is(err, ErrStarterResponseLost) {
		t.Fatalf("crashed apply err=%v, want response-lost after the record", err)
	}
	registry.AfterRecord = nil
	replay, err := registry.Apply(apply)
	if err != nil || replay.Revision != 1 {
		t.Fatalf("replay after crash = %+v, err=%v; want the single recorded revision", replay, err)
	}
	if got := registry.HistoryLength(Scope{TenantID: "tenant-a"}, "adopt-1"); got != 1 {
		t.Fatalf("crashed adoption history has %d revisions, want one", got)
	}
	if _, err := registry.Publish(Scope{TenantID: "tenant-a"}, "adopt-1", "ada.admin", 1, at.Add(time.Minute)); err != nil {
		t.Fatalf("publish after crash replay refused: %v", err)
	}
}

func TestTodo_SMB_SETUP_003_Recovery(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	registry, approvals := smbStarterRegistry(t, at)
	if _, err := registry.Compile(smbStarterManifests()); err != nil {
		t.Fatal(err)
	}
	apply := StarterAdoptionRequest{
		Actor: "ada.admin", AdoptionID: "adopt-1", Scope: Scope{TenantID: "tenant-a"},
		ProfileID: "professional-services", CompiledDigest: registry.Compiled("professional-services").Digest,
		Baseline:       smbStarterPin(t, approvals, "tenant-a", at),
		Overrides:      map[string]string{"approval.discount.threshold": "500"},
		IdempotencyKey: "key-1", At: at,
	}
	first, err := registry.Apply(apply)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := registry.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored, _ := smbStarterRegistry(t, at)
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	replay, err := restored.Apply(apply)
	if err != nil || replay.Revision != first.Revision || replay.Digest != first.Digest {
		t.Fatalf("post-restore replay = %+v, err=%v; want the original revision, no duplicate work", replay, err)
	}
	if got := restored.HistoryLength(Scope{TenantID: "tenant-a"}, "adopt-1"); got != 1 {
		t.Fatalf("post-restore history has %d revisions, want one", got)
	}
	view, visible := restored.View(Scope{TenantID: "tenant-a"}, "adopt-1", "ada.admin")
	if !visible || view.Status != StarterAdoptionDraft {
		t.Fatalf("post-restore view = %+v, want the draft adoption", view)
	}
	if err := restored.Import([]byte("{not-json")); err == nil {
		t.Fatal("corrupt snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_SETUP_003_Browser(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	registry, approvals := smbStarterRegistry(t, at)
	if _, err := registry.Compile(smbStarterManifests()); err != nil {
		t.Fatal(err)
	}

	card, ok := registry.ProfileCard("professional-services")
	if !ok || card.Readiness != StarterProfileAvailable || card.DailyWorkflow == "" || len(card.RequiredModules) == 0 || len(card.SetupSteps) == 0 {
		t.Fatalf("profile card = %+v, want an available card with workflow, modules and steps", card)
	}
	if !strings.Contains(card.ReadinessNote, "production") || !containsString(card.Actions, "Apply to draft") {
		t.Fatalf("profile card = %+v, want Apply to draft with a production-readiness caveat", card)
	}
	planned, ok := registry.ProfileCard("field-services-broken")
	if !ok || planned.Readiness != StarterProfilePlanned || len(planned.Reasons) == 0 {
		t.Fatalf("broken profile card = %+v, want Planned with explanations", planned)
	}
	if containsString(planned.Actions, "Apply to draft") {
		t.Fatalf("broken profile card offers Apply to draft: %+v", planned)
	}

	apply := StarterAdoptionRequest{
		Actor: "ada.admin", AdoptionID: "adopt-1", Scope: Scope{TenantID: "tenant-a"},
		ProfileID: "professional-services", CompiledDigest: registry.Compiled("professional-services").Digest,
		Baseline:       smbStarterPin(t, approvals, "tenant-a", at),
		Overrides:      map[string]string{"approval.discount.threshold": "500"},
		Replace:        []string{"approval.discount.threshold"},
		IdempotencyKey: "key-1", At: at,
	}
	if _, err := registry.Apply(apply); err != nil {
		t.Fatal(err)
	}
	draft, visible := registry.View(Scope{TenantID: "tenant-a"}, "adopt-1", "ada.admin")
	if !visible || draft.Status != StarterAdoptionDraft {
		t.Fatalf("draft view = %+v, want the draft adoption", draft)
	}
	if !containsString(draft.Actions, "Publish") || !containsString(draft.Actions, "Discard draft") {
		t.Fatalf("draft view actions = %v, want Publish and Discard draft", draft.Actions)
	}
	kept := false
	for _, diff := range draft.Diff {
		if diff.Decision == StarterDiffKeepCurrent && diff.Previous == "" {
			t.Fatalf("draft diff = %+v, keep-current must show its previous value", draft.Diff)
		}
		if diff.Key == "approval.discount.threshold" && diff.Decision == StarterDiffReplace && diff.Previous == "500" && diff.Proposed == "200" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("draft diff = %+v, want explicit Replace 500 with previous value visible", draft.Diff)
	}

	if _, err := registry.Publish(Scope{TenantID: "tenant-a"}, "adopt-1", "ada.admin", 1, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	active, _ := registry.View(Scope{TenantID: "tenant-a"}, "adopt-1", "ada.admin")
	if active.Status != StarterAdoptionActive || !containsString(active.Actions, "Upgrade") {
		t.Fatalf("active view = %+v, want Active with Upgrade", active)
	}
}
