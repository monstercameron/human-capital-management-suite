package application

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
)

func TestTodo_AGENTP_021_LocalCandidateExactToolCeiling(t *testing.T) {
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	policy, _ := LocalPersonaOpenAIModelPolicyReference()
	manifest := localDevPersonaStarterManifest(starter, personaStarterInstructions(starter), localDevPersonaStarterReferences{
		modelPolicy:  policy,
		outputSchema: agentmanifest.Reference{ID: PersonaChatReplySchema, Version: 1, SchemaVersion: 1, Digest: PersonaChatReplySchemaDigest},
		evaluation:   agentmanifest.Reference{ID: starter.EvaluationSuite, Version: 1, SchemaVersion: 1, Digest: personaInstructionDigest("test suite")},
	})
	manifest.Budget.MaxCostMicros = uint64(LocalPersonaOpenAIMaxCostMicros)
	candidate, route, err := NewLocalPersonaOpenAICandidate(manifest)
	if err != nil {
		t.Fatalf("trusted starter cannot construct an unqualified candidate: %v", err)
	}
	digest, _ := manifest.Digest()
	if candidate.Evaluation.Passed || route.Route.Pin.AgentVersionDigest != digest || route.Route.Pin.Primary.ProfileDigest != candidate.ProfileDigest {
		t.Fatal("candidate must remain unqualified and bound to the exact manifest and model")
	}
	terms := LocalPersonaOpenAIProcessingTerms(candidate.ID)
	if route.Processing.Retention != "BOUNDED:2592000000000000" || route.Egress.Retention != terms.Retention || route.Processing.TrainingUse != terms.TrainingUse || route.Processing.Logging != terms.Logging {
		t.Fatal("candidate processing contract differs from the reviewed provider terms")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*agentmanifest.Manifest)
	}{
		{"no tools", func(m *agentmanifest.Manifest) { m.ToolCeiling = []agentmanifest.Reference{} }},
		{"missing reply tool", func(m *agentmanifest.Manifest) { m.ToolCeiling = m.ToolCeiling[:1] }},
		{"changed digest", func(m *agentmanifest.Manifest) { m.ToolCeiling[0].Digest = personaInstructionDigest("changed") }},
		{"changed version", func(m *agentmanifest.Manifest) { m.ToolCeiling[0].Version++ }},
		{"changed schema", func(m *agentmanifest.Manifest) { m.ToolCeiling[0].SchemaVersion++ }},
		{"extra tool", func(m *agentmanifest.Manifest) {
			m.ToolCeiling = append(m.ToolCeiling, agentmanifest.Reference{ID: "zz.unapproved", Version: 1, SchemaVersion: 1, Digest: personaInstructionDigest("extra")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := manifest
			changed.ToolCeiling = append([]agentmanifest.Reference(nil), manifest.ToolCeiling...)
			tc.mutate(&changed)
			if _, _, err := NewLocalPersonaOpenAICandidate(changed); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
				t.Fatalf("unapproved tool ceiling accepted: %v", err)
			}
		})
	}
}
