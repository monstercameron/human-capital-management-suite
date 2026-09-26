package ironridgeseed

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
)

func TestIronridgeRiversideWorkOrderSeedIsPublishedAndResolvable(t *testing.T) {
	pilot := RiversideGymWingPilot()
	if pilot.TenantKey != TenantKey || pilot.ProjectKey != "RIV" || pilot.TemplateID != TemplateID || pilot.TemplateVersion != TemplateVersion {
		t.Fatalf("pilot identity = %+v", pilot)
	}
	if !reflect.DeepEqual(pilot.LinkedTaskKeys, []string{"RIV-35", "RIV-32", "RIV-34"}) {
		t.Fatalf("linked Riverside tasks = %v", pilot.LinkedTaskKeys)
	}
	if !reflect.DeepEqual(pilot.DocumentKeys, []string{"riverside-project-overview", "daily-report-template", "riverside-gym-wing-work-order"}) || !reflect.DeepEqual(pilot.ConversationKeys, []string{"jobsite-riverside", "foremen"}) {
		t.Fatalf("cross-collaboration refs = docs %v, chats %v", pilot.DocumentKeys, pilot.ConversationKeys)
	}

	payload, digest, err := PublishedTemplatePayload()
	if err != nil {
		t.Fatalf("publish Ironridge template: %v", err)
	}
	if !json.Valid(payload) || digest == "" {
		t.Fatalf("published template payload or digest missing: payload=%s digest=%q", payload, digest)
	}
	restored, err := workordertemplate.RestorePublished(payload, digest)
	if err != nil {
		t.Fatalf("restore storage envelope: %v", err)
	}
	if err := restored.Verify(); err != nil {
		t.Fatalf("restored template verification: %v", err)
	}
	if restored.Pin().TemplateID != TemplateID || restored.Pin().Version != TemplateVersion || restored.Pin().Digest != digest {
		t.Fatalf("restored template pin = %+v", restored.Pin())
	}
	phases := map[string]bool{}
	for _, phase := range restored.Snapshot().Phases {
		phases[phase.ID] = true
	}
	for _, want := range []string{"DRAFT", "AUTHORIZATION", "READY", "EXECUTION", "INSPECTION", "ACCEPTED", "CLOSED"} {
		if !phases[want] {
			t.Errorf("published template missing phase %s", want)
		}
	}
	if !restored.AllowsRequest("BUDGET", "AUTHORIZATION") || !restored.AllowsRequest("BILLING_REVIEW", "ACCEPTED") {
		t.Error("published template does not expose the budget and billing requests at their governed phases")
	}
}
