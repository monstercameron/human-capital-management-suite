package agenttemplate

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

func TestTodo_AGENT_013(t *testing.T) {
	registry := NewRegistry()
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
	if draft.ID != "draft-1" || draft.TenantID != "tenant-a" || draft.OwnerID != "owner-a" || draft.State != "DRAFT" || draft.Version != 1 {
		t.Fatalf("draft identity/state = %+v", draft)
	}
	if draft.Template != pin || draft.Purpose != definition.Purpose || draft.Instructions != definition.Instructions {
		t.Fatalf("draft did not preserve template content and provenance: %+v", draft)
	}
	if draft.SourceCeiling == nil || len(draft.SourceCeiling) != 0 || draft.ToolCeiling == nil || len(draft.ToolCeiling) != 0 {
		t.Fatalf("template installation granted authority: source=%v tools=%v", draft.SourceCeiling, draft.ToolCeiling)
	}
	if _, err := registry.Publish(definition); !errors.Is(err, ErrAlreadyPublished) {
		t.Fatalf("republishing same version error = %v, want already published", err)
	}
	changed := definition
	changed.Version = 2
	changed.Instructions = "A later platform version has different instructions."
	if _, err := registry.Publish(changed); err != nil {
		t.Fatal(err)
	}
	oldDraft, err := registry.Install(pin, InstallRequest{
		DraftID: "draft-2", TenantID: "tenant-a", OwnerID: "owner-a",
		ManifestSchemaVersion: agentmanifest.CurrentSchemaVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if oldDraft.Instructions != definition.Instructions || oldDraft.Template != pin {
		t.Fatalf("installing the old pin changed with v2: %+v", oldDraft)
	}
}

func TestTodo_AGENT_013_Security(t *testing.T) {
	registry := NewRegistry()
	pin, err := registry.Publish(Definition{
		ID: "starter", Version: 1, TemplateSchemaVersion: 1,
		ManifestSchemaVersion: agentmanifest.CurrentSchemaVersion,
		DisplayName:           "Starter", Summary: "Safe starting point.",
		Purpose: "Start a reviewed tenant agent.", Instructions: "Wait for approved access.",
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Pin){
		"wrong digest":          func(value *Pin) { value.Digest = "sha256:forged" },
		"wrong template schema": func(value *Pin) { value.TemplateSchemaVersion++ },
		"unknown version":       func(value *Pin) { value.Version++ },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := pin
			mutate(&candidate)
			_, err := registry.Install(candidate, InstallRequest{
				DraftID: "draft", TenantID: "tenant-a", OwnerID: "owner-a",
				ManifestSchemaVersion: agentmanifest.CurrentSchemaVersion,
			})
			if err == nil {
				t.Fatal("Install accepted a modified or unknown template pin")
			}
		})
	}
	_, err = registry.Install(pin, InstallRequest{
		DraftID: "draft", TenantID: "tenant-a", OwnerID: "owner-a",
		ManifestSchemaVersion: agentmanifest.CurrentSchemaVersion + 1,
	})
	if !errors.Is(err, ErrIncompatibleSchema) {
		t.Fatalf("incompatible manifest schema error = %v", err)
	}
	_, err = registry.Install(pin, InstallRequest{
		DraftID: "draft", TenantID: "tenant-a", OwnerID: "owner-a",
		ManifestSchemaVersion: agentmanifest.CurrentSchemaVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record, ok := registry.Lookup(pin.ID, pin.Version); !ok || record.Digest != pin.Digest {
		t.Fatalf("tampered pin changed published record: record=%+v found=%t", record, ok)
	}
}

func TestTodo_AGENT_013_Conformance(t *testing.T) {
	registry, err := PlatformStarters()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"hcmnext.agent_template.policy_guide", "hcmnext.agent_template.project_assistant"} {
		record, ok := registry.Lookup(id, 1)
		if !ok {
			t.Fatalf("platform starter %q is absent", id)
		}
		pin := Pin{ID: id, Version: 1, TemplateSchemaVersion: record.Definition.TemplateSchemaVersion, Digest: record.Digest}
		draft, err := registry.Install(pin, InstallRequest{
			DraftID: "draft-" + id, TenantID: "tenant-a", OwnerID: "owner-a",
			ManifestSchemaVersion: record.Definition.ManifestSchemaVersion,
		})
		if err != nil {
			t.Fatalf("install %q: %v", id, err)
		}
		if draft.Template != pin || draft.TenantID != "tenant-a" || len(draft.SourceCeiling) != 0 || len(draft.ToolCeiling) != 0 {
			t.Fatalf("starter %q did not yield a provenance-pinned, capability-neutral tenant draft: %+v", id, draft)
		}
	}
}
