package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

type personaStarterProvisionStoreFake struct {
	manifests map[string]agentmanifest.Manifest
	contents  map[string]string
	saves     int
}

func (s *personaStarterProvisionStoreFake) SaveInstructionContent(_ context.Context, tenant uuid.UUID, content string) (string, error) {
	if tenant == uuid.Nil {
		return "", errors.New("tenant required")
	}
	s.contents[content] = content
	digest := personaInstructionDigest(content)
	return digest, nil
}

func (s *personaStarterProvisionStoreFake) SaveManifest(_ context.Context, tenant uuid.UUID, manifest agentmanifest.Manifest, expected uint64) (uint64, error) {
	if tenant == uuid.Nil || expected != 0 {
		return 0, errors.New("invalid tenant or revision")
	}
	if _, ok := s.manifests[manifest.ID]; ok {
		return 0, agentstore.ErrConflict
	}
	s.manifests[manifest.ID] = manifest
	s.saves++
	return 1, nil
}

func (s *personaStarterProvisionStoreFake) CurrentManifest(_ context.Context, tenant uuid.UUID, id string) (agentmanifest.Manifest, uint64, error) {
	if tenant == uuid.Nil {
		return agentmanifest.Manifest{}, 0, errors.New("tenant required")
	}
	manifest, ok := s.manifests[id]
	if !ok {
		return agentmanifest.Manifest{}, 0, agentstore.ErrNotFound
	}
	return manifest, 1, nil
}

type personaStarterProvisionSkillsFake struct {
	missing string
	status  agentskills.Status
	wrong   bool
}

func (s personaStarterProvisionSkillsFake) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if pin.ID == s.missing {
		return agentskills.SkillRecord{}, errors.New("unknown skill")
	}
	digest := pin.Digest
	if s.wrong {
		digest = "sha256:" + string(make([]byte, 64))
	}
	status := s.status
	if status == "" {
		status = agentskills.StatusActive
	}
	return agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version}, Digest: digest, Status: status}, nil
}

type personaStarterProvisionReferencesFake struct {
	missing string
	zero    string
}

func (r personaStarterProvisionReferencesFake) ResolveModelPolicy(context.Context) (agentmanifest.Reference, error) {
	return r.reference("model", "model-policy.default", 'a')
}

func (r personaStarterProvisionReferencesFake) ResolveOutputSchema(context.Context) (agentmanifest.Reference, error) {
	return r.reference("schema", "schema.persona-answer", 'b')
}

func (r personaStarterProvisionReferencesFake) ResolveEvaluationSuite(_ context.Context, id string) (agentmanifest.Reference, error) {
	return r.reference("evaluation", id, 'c')
}

func (r personaStarterProvisionReferencesFake) reference(kind, id string, digestByte rune) (agentmanifest.Reference, error) {
	if r.missing == kind {
		return agentmanifest.Reference{}, errors.New("reference not registered")
	}
	digest := strings.Repeat(string(digestByte), 64)
	if r.zero == kind {
		digest = strings.Repeat("0", 64)
	}
	return agentmanifest.Reference{ID: id, Version: 2, SchemaVersion: 1, Digest: "sha256:" + digest}, nil
}

func TestTodo_AGENTP_023_LocalDevStarterManifestsAreDurableAndIdempotent(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	store := &personaStarterProvisionStoreFake{manifests: map[string]agentmanifest.Manifest{}, contents: map[string]string{}}
	refs := personaStarterProvisionReferencesFake{}
	if err := ProvisionLocalDevPersonaStarterManifests(ctx, store, tenant, personaStarterProvisionSkillsFake{}, refs); err != nil {
		t.Fatalf("first provision: %v", err)
	}
	if len(store.manifests) != 4 || len(store.contents) != 4 || store.saves != 4 {
		t.Fatalf("provisioned manifests=%d instructions=%d saves=%d", len(store.manifests), len(store.contents), store.saves)
	}
	for _, starter := range agenttemplate.PersonaStarters() {
		id := "agent.starter." + personaStarterProvisionSlug(starter.ID)
		manifest, ok := store.manifests[id]
		if !ok || manifest.InstructionsDigest != personaInstructionDigest(personaStarterInstructions(starter)) {
			t.Fatalf("starter %s manifest missing or not bound to instruction bytes: %+v", starter.ID, manifest)
		}
		if err := manifest.Validate(); err != nil {
			t.Fatalf("starter %s manifest invalid: %v", starter.ID, err)
		}
		if len(manifest.ToolCeiling) != len(starter.SkillPins) {
			t.Fatalf("starter %s cannot execute its pinned skills: tools=%d skills=%d", starter.ID, len(manifest.ToolCeiling), len(starter.SkillPins))
		}
		for _, pin := range starter.SkillPins {
			found := false
			for _, tool := range manifest.ToolCeiling {
				if tool == (agentmanifest.Reference{ID: pin.ID, Version: uint64(pin.Version), SchemaVersion: 1, Digest: personaRuntimeToolAdmissionDigest(pin.Digest)}) {
					found = true
				}
			}
			if !found {
				t.Fatalf("starter %s omits exact executable skill %s", starter.ID, pin.ID)
			}
		}
		if len(manifest.EvaluationRefs) != 1 || manifest.EvaluationRefs[0].ID != starter.EvaluationSuite || manifest.ModelPolicy.Digest == "sha256:"+strings.Repeat("0", 64) || manifest.OutputSchema.Digest == "sha256:"+strings.Repeat("0", 64) {
			t.Fatalf("starter %s has incomplete trusted refs: %+v", starter.ID, manifest)
		}
	}
	if err := ProvisionLocalDevPersonaStarterManifests(ctx, store, tenant, personaStarterProvisionSkillsFake{}, refs); err != nil {
		t.Fatalf("repeat provision: %v", err)
	}
	if store.saves != 4 {
		t.Fatalf("repeat provision appended %d manifest versions", store.saves-4)
	}
}

func TestTodo_AGENTP_023_LocalDevStarterManifestsFailClosed(t *testing.T) {
	starters := agenttemplate.PersonaStarters()
	firstPin := starters[0].SkillPins[0]
	for _, tc := range []struct {
		name   string
		skills personaStarterProvisionSkillsFake
	}{
		{name: "missing exact pin", skills: personaStarterProvisionSkillsFake{missing: firstPin.ID}},
		{name: "retired pin", skills: personaStarterProvisionSkillsFake{status: agentskills.StatusRetired}},
		{name: "digest mismatch", skills: personaStarterProvisionSkillsFake{wrong: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &personaStarterProvisionStoreFake{manifests: map[string]agentmanifest.Manifest{}, contents: map[string]string{}}
			err := ProvisionLocalDevPersonaStarterManifests(context.Background(), store, uuid.New(), tc.skills, personaStarterProvisionReferencesFake{})
			if !errors.Is(err, ErrPersonaStarterSkillUnavailable) {
				t.Fatalf("provision error = %v", err)
			}
			if len(store.manifests) != 0 || len(store.contents) != 0 {
				t.Fatalf("failed preflight wrote manifests=%d instruction rows=%d", len(store.manifests), len(store.contents))
			}
		})
	}
}

func TestTodo_AGENTP_023_LocalDevStarterManifestsRefuseTenantOverwrite(t *testing.T) {
	store := &personaStarterProvisionStoreFake{manifests: map[string]agentmanifest.Manifest{}, contents: map[string]string{}}
	tenant := uuid.New()
	if err := ProvisionLocalDevPersonaStarterManifests(context.Background(), store, tenant, personaStarterProvisionSkillsFake{}, personaStarterProvisionReferencesFake{}); err != nil {
		t.Fatal(err)
	}
	manifest := store.manifests["agent.starter.onboarding_coordinator"]
	manifest.Purpose = "Tenant-authored replacement"
	store.manifests[manifest.ID] = manifest
	err := ProvisionLocalDevPersonaStarterManifests(context.Background(), store, tenant, personaStarterProvisionSkillsFake{}, personaStarterProvisionReferencesFake{})
	if !errors.Is(err, errPersonaStarterProvisioningUnavailable) {
		t.Fatalf("overwrite error = %v", err)
	}
}

func TestTodo_AGENTP_023_LocalDevStarterManifestsRequireTrustedReferences(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolver LocalDevPersonaStarterReferenceResolver
	}{
		{name: "missing resolver"},
		{name: "missing model policy", resolver: personaStarterProvisionReferencesFake{missing: "model"}},
		{name: "missing output schema", resolver: personaStarterProvisionReferencesFake{missing: "schema"}},
		{name: "missing evaluation suite", resolver: personaStarterProvisionReferencesFake{missing: "evaluation"}},
		{name: "zero model policy digest", resolver: personaStarterProvisionReferencesFake{zero: "model"}},
		{name: "zero output schema digest", resolver: personaStarterProvisionReferencesFake{zero: "schema"}},
		{name: "zero evaluation digest", resolver: personaStarterProvisionReferencesFake{zero: "evaluation"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &personaStarterProvisionStoreFake{manifests: map[string]agentmanifest.Manifest{}, contents: map[string]string{}}
			err := ProvisionLocalDevPersonaStarterManifests(context.Background(), store, uuid.New(), personaStarterProvisionSkillsFake{}, tc.resolver)
			if !errors.Is(err, errPersonaStarterProvisioningUnavailable) {
				t.Fatalf("provision error = %v", err)
			}
			if len(store.manifests) != 0 || len(store.contents) != 0 {
				t.Fatalf("invalid references wrote manifests=%d instruction rows=%d", len(store.manifests), len(store.contents))
			}
		})
	}
}

func personaStarterProvisionSlug(id string) string {
	const prefix = "hcmnext.persona_template."
	return id[len(prefix):]
}

var _ agentpersona.SkillResolver = personaStarterProvisionSkillsFake{}
