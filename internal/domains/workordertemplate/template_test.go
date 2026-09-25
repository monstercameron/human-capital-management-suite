package workordertemplate

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func fixture() Draft {
	return Draft{
		ID: "STAIR_REPAIR", Name: "Stair repair", Phases: []Phase{
			{ID: "DRAFT", AllowedExits: []string{"AUTHORIZATION"}, ActorRoles: []string{"INITIATOR"}},
			{ID: "AUTHORIZATION", AllowedExits: []string{"READY"}, ActorRoles: []string{"SUPERVISOR"}, Gates: []Gate{{ID: "SCOPE_APPROVAL", Kind: "AUTHORIZATION", Required: true}, {ID: "SAFETY_REVIEW", Kind: "SAFETY", Required: true}}},
			{ID: "READY", AllowedExits: []string{"EXECUTION"}, ActorRoles: []string{"SUPERVISOR"}},
			{ID: "EXECUTION", AllowedExits: []string{"INSPECTION"}, ActorRoles: []string{"CREW"}},
			{ID: "INSPECTION", AllowedExits: []string{"ACCEPTED"}, ActorRoles: []string{"INSPECTOR"}, Gates: []Gate{{ID: "RECONCILE", Kind: "RECONCILIATION", Required: true}}},
			{ID: "ACCEPTED", AllowedExits: []string{"CLOSED"}, ActorRoles: []string{"SUPERVISOR"}},
			{ID: "CLOSED", ActorRoles: []string{"SUPERVISOR"}, Gates: []Gate{{ID: "CLOSEOUT", Kind: "CLOSURE", Required: true}}},
		},
		Forms:          []RequestForm{{ID: "BUDGET_FORM", Version: "1", Fields: []FormField{{ID: "amount", Type: FieldDecimal, Required: true}, {ID: "reason", Type: FieldText, Required: true}}}},
		Requests:       []RequestDefinition{{ID: "INITIAL_BUDGET", Kind: RequestBudget, FormRef: "BUDGET_FORM", AllowedPhases: []string{"DRAFT", "AUTHORIZATION"}}},
		Roles:          []RoleGrant{{Role: "INITIATOR", Actions: []string{"create", "request"}}, {Role: "SUPERVISOR", Actions: []string{"advance", "approve"}}, {Role: "CREW", Actions: []string{"log_work"}}, {Role: "INSPECTOR", Actions: []string{"inspect"}}},
		Branches:       []OptionalBranch{{ID: "REWORK", EntryPhase: "EXECUTION", ReturnPhase: "INSPECTION", MaxIterations: 3, Optional: true}},
		ReportPolicies: []ReportPolicy{{ID: "ironridge.work_order.field_report", Version: "2", Kind: ReportDailyField, Required: true}},
		Billing:        &BillingPolicy{ID: "UNIT_BILLING", Version: "1", Mode: "UNIT_PRICE"},
	}
}
func meta() PublishMeta {
	return PublishMeta{Version: "1.0.0", PublishedBy: "principal:owner", ReviewRef: "review:approved"}
}

func TestPublishPinsImmutableSnapshotAndCanonicalDigest(t *testing.T) {
	d := fixture()
	published, err := Publish(d, meta(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = published.Verify(); err != nil {
		t.Fatalf("verify published template: %v", err)
	}
	pin := published.Pin()
	if pin.TemplateID != d.ID || pin.Version != "1.0.0" || pin.Digest == "" {
		t.Fatalf("bad pin: %+v", pin)
	}
	snapshot := published.Snapshot()
	snapshot.Phases[0].AllowedExits[0] = "CORRUPTED"
	if err = published.Verify(); err != nil {
		t.Fatalf("snapshot mutation reached published value: %v", err)
	}
	again, err := Publish(fixture(), meta(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest() != published.Digest() {
		t.Fatalf("same template yielded different digests: %s != %s", again.Digest(), published.Digest())
	}
}

func TestPublishedPersistenceRoundTripAndTamperDetection(t *testing.T) {
	published, err := Publish(fixture(), meta(), nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(published)
	if err != nil {
		t.Fatalf("marshal published template: %v", err)
	}
	restored, err := RestorePublished(encoded, published.Digest())
	if err != nil {
		t.Fatalf("restore published template: %v", err)
	}
	if err = restored.Verify(); err != nil || restored.Pin() != published.Pin() {
		t.Fatalf("restored publication differs: pin=%+v err=%v", restored.Pin(), err)
	}
	if _, err = RestorePublished(encoded, "sha256:wrong"); !errors.Is(err, ErrMutated) {
		t.Fatalf("wrong expected digest accepted: %v", err)
	}
	tampered := strings.Replace(string(encoded), "Stair repair", "Unsafe edit", 1)
	if _, err = RestorePublished(json.RawMessage(tampered), published.Digest()); !errors.Is(err, ErrMutated) {
		t.Fatalf("tampered persisted content accepted: %v", err)
	}
	if _, err = RestorePublished(json.RawMessage(strings.TrimSuffix(string(encoded), "}")+`,"extra":true}`), published.Digest()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown persisted field accepted: %v", err)
	}
}

func TestPublishedPhaseAndRequestHelpers(t *testing.T) {
	published, err := Publish(fixture(), meta(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !published.AllowsTransition("DRAFT", "AUTHORIZATION") || published.AllowsTransition("DRAFT", "EXECUTION") {
		t.Fatal("phase transition helper did not enforce configured exits")
	}
	request, ok := published.Request("INITIAL_BUDGET")
	if !ok || request.Kind != RequestBudget || !published.AllowsRequest("INITIAL_BUDGET", "DRAFT") || published.AllowsRequest("INITIAL_BUDGET", "EXECUTION") {
		t.Fatalf("request phase lookup mismatch: request=%+v found=%v", request, ok)
	}
	form, ok := published.Form("BUDGET_FORM")
	if !ok || len(form.Fields) != 2 {
		t.Fatalf("form lookup mismatch: form=%+v found=%v", form, ok)
	}
	form.Fields[0].ID = "MUTATED"
	if again, _ := published.Form("BUDGET_FORM"); again.Fields[0].ID == "MUTATED" {
		t.Fatal("form helper exposed published template memory")
	}
}

func TestPublishRejectsUnsafeMissingGateAndUntypedField(t *testing.T) {
	d := fixture()
	d.Phases[1].Gates = nil
	if _, err := Publish(d, meta(), nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing required gate accepted: %v", err)
	}
	d = fixture()
	d.Forms[0].Fields[0].Type = "SCRIPT"
	if _, err := Publish(d, meta(), nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("untyped form field accepted: %v", err)
	}
}

func TestReportPolicyRequiresSupportedKindIndependentOfID(t *testing.T) {
	d := fixture()
	published, err := Publish(d, meta(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := published.Snapshot().ReportPolicies[0]; got.ID != "ironridge.work_order.field_report" || got.Kind != ReportDailyField {
		t.Fatalf("customer policy identity or kind was not retained: %+v", got)
	}
	for _, kind := range []ReportKind{"", "billing", "SCRIPT"} {
		bad := fixture()
		bad.ReportPolicies[0].Kind = kind
		if _, err := Publish(bad, meta(), nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("unsupported report kind %q accepted: %v", kind, err)
		}
	}
}

func TestOverlayCannotOmitGovernanceOrReachablePhase(t *testing.T) {
	d := fixture()
	_, err := PublishWithOverlay(d, Overlay{Operations: []PhaseOverlay{{Operation: OverlayOmit, TargetID: "AUTHORIZATION", Reason: "customer asked"}}}, meta(), nil)
	if !errors.Is(err, ErrUnsafeOverlay) {
		t.Fatalf("authorization omission accepted: %v", err)
	}
	d.Phases[3].AllowedExits = append(d.Phases[3].AllowedExits, "OPTIONAL_PHASE")
	d.Phases = append(d.Phases, Phase{ID: "OPTIONAL_PHASE", AllowedExits: []string{"INSPECTION"}, ActorRoles: []string{"SUPERVISOR"}, Optional: true})
	_, err = PublishWithOverlay(d, Overlay{Operations: []PhaseOverlay{{Operation: OverlayOmit, TargetID: "OPTIONAL_PHASE", Reason: "not needed for this job"}}}, meta(), nil)
	if !errors.Is(err, ErrUnsafeOverlay) {
		t.Fatalf("reachable phase omission accepted: %v", err)
	}
}

func TestOverlayAddsOptionalPhaseAndRecordsProvenance(t *testing.T) {
	p, err := PublishWithOverlay(fixture(), Overlay{Operations: []PhaseOverlay{{Operation: OverlayAdd, Phase: Phase{ID: "WEATHER_REVIEW", AllowedExits: []string{"READY"}, ActorRoles: []string{"SUPERVISOR"}}, Reason: "weather review for outdoor work"}}}, meta(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.OverlayDigest() == "" {
		t.Fatal("overlay provenance digest missing")
	}
	found := false
	for _, phase := range p.Snapshot().Phases {
		if phase.ID == "WEATHER_REVIEW" && phase.Optional {
			found = true
		}
	}
	if !found {
		t.Fatal("new optional phase missing")
	}
}

func TestPreviewMigrationClassifiesPinnedRunImpact(t *testing.T) {
	old, err := Publish(fixture(), meta(), nil)
	if err != nil {
		t.Fatal(err)
	}
	nextDraft := fixture()
	for i := range nextDraft.Phases {
		if nextDraft.Phases[i].ID == "INSPECTION" {
			nextDraft.Phases[i].ActorRoles = append(nextDraft.Phases[i].ActorRoles, "SUPERVISOR")
		}
	}
	nextMeta := meta()
	nextMeta.Version = "1.1.0"
	next, err := Publish(nextDraft, nextMeta, nil)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewMigration(old, next, "EXECUTION", nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Class != MigrationTransformable {
		t.Fatalf("expected transformable, got %+v", preview)
	}
	preview, err = PreviewMigration(old, next, "MISSING_PHASE", nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Class != MigrationImpossible {
		t.Fatalf("missing active phase migration not impossible: %+v", preview)
	}
}
