package agenttemplate

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

// TestRegistryZeroValuePublishInstall proves a Registry is usable without the
// constructor: publishing to (and installing from) the zero value must behave
// like a NewRegistry instead of panicking on the nil entries map.
func TestRegistryZeroValuePublishInstall(t *testing.T) {
	var registry Registry
	definition := Definition{
		ID: "policy-guide", Version: 1, TemplateSchemaVersion: 1,
		ManifestSchemaVersion: agentmanifest.CurrentSchemaVersion,
		DisplayName:           "Policy Guide", Summary: "Policy question starting point.",
		Purpose:      "Answer policy questions from approved sources.",
		Instructions: "Use only approved sources and cite them.",
	}
	pin, err := registry.Publish(definition)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := registry.Install(pin, InstallRequest{
		DraftID: "draft-1", TenantID: "tenant-a", OwnerID: "owner-a",
		ManifestSchemaVersion: agentmanifest.CurrentSchemaVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Template != pin || draft.TenantID != "tenant-a" || draft.State != "DRAFT" {
		t.Fatalf("zero-value install lost provenance or ownership: %+v", draft)
	}
	if len(draft.SourceCeiling) != 0 || len(draft.ToolCeiling) != 0 {
		t.Fatalf("zero-value install granted authority: %+v", draft)
	}
	if _, err := registry.Publish(definition); err == nil {
		t.Fatal("zero-value registry allowed republishing an immutable version")
	}
}
