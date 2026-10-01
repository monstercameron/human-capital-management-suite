package agentstore

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"strings"
	"testing"
)

func TestPortableDraftRejectsAuthorityAndBadInstructionContentBeforeDB(t *testing.T) {
	var nilStore *Store
	m := portableDraftTestManifest()
	for name, mutate := range map[string]func(*agentmanifest.Manifest, *string){
		"nil tenant": func(_ *agentmanifest.Manifest, _ *string) {},
		"version":    func(m *agentmanifest.Manifest, _ *string) { m.Version = 2 },
		"context grant": func(m *agentmanifest.Manifest, _ *string) {
			m.ContextGrants = []agentmanifest.Reference{{ID: "grant", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("a", 64)}}
		},
		"content": func(_ *agentmanifest.Manifest, body *string) { *body = "tampered" },
	} {
		t.Run(name, func(t *testing.T) {
			mm, body := m, "portable instructions"
			mutate(&mm, &body)
			tenant := uuid.New()
			if name == "nil tenant" {
				tenant = uuid.Nil
			}
			if err := nilStore.SavePortableDefinitionDraft(context.Background(), tenant, "owner", mm, body); !errors.Is(err, ErrPortableDraftInvalid) {
				t.Fatalf("error=%v, want ErrPortableDraftInvalid", err)
			}
		})
	}
}

func portableDraftTestManifest() agentmanifest.Manifest {
	body := "portable instructions"
	return agentmanifest.Manifest{SchemaVersion: 1, ID: "portable.test", Version: 1, OwnerID: "owner", Purpose: "test", InstructionsDigest: instructionDigest(body), SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{}, ModelPolicy: portableDraftTestRef("model", 'a'), AutonomyCeiling: "private_answer", Budget: agentmanifest.Budget{MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1, MaxConcurrentRuns: 1}, OutputSchema: portableDraftTestRef("output", 'b'), ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: []agentmanifest.Reference{portableDraftTestRef("eval", 'c')}}
}
func portableDraftTestRef(id string, c byte) agentmanifest.Reference {
	return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat(string(c), 64)}
}
