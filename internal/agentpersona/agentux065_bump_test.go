package agentpersona

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/semver"
)

func agentux065Profile() PersonaProfile {
	return PersonaProfile{
		PersonaID: "assistant", Version: 2, Handle: "assistant", DisplayName: "Assistant",
		Audience:           Audience{Populations: []string{"all"}},
		SkillPins:          []agentskills.SkillPin{{ID: "search", Version: 1}},
		TierCeiling:        agentskills.TierT1,
		ConversationKinds:  []ConversationKind{ConversationChannel},
		Instructions:       "Answer policy questions.",
		DocumentReferences: []agentdocref.Reference{{DocumentID: "doc-policy", VersionMode: agentdocref.ModeLatestPublished, Label: "Policy"}},
		Owner:              "o", Steward: "s",
		EvalSuiteRef: "suite", EvalLimits: EvaluationLimits{MaxCost: 10, MaxSteps: 5, MaxLatencyMS: 1000},
	}
}

// Only wording in the instructions (or the other display text) is a patch;
// anything that changes skills, audience or documents — or any other
// capability — needs at least a minor (AGENTUX-065).
func TestAgentUX065_RequiredBump(t *testing.T) {
	prev := agentux065Profile()
	patch := func(mutate func(*PersonaProfile)) PersonaProfile {
		next := agentux065Profile()
		mutate(&next)
		return next
	}
	for name, next := range map[string]PersonaProfile{
		"identical":           agentux065Profile(),
		"instructions edited": patch(func(p *PersonaProfile) { p.Instructions = "Answer policy questions kindly." }),
		"display renamed":     patch(func(p *PersonaProfile) { p.DisplayName = "Helpful Assistant" }),
		"purpose reworded":    patch(func(p *PersonaProfile) { p.Purpose = "help" }),
		"guidance edited":     patch(func(p *PersonaProfile) { p.Guidance = "be kind" }),
		"examples reordered":  patch(func(p *PersonaProfile) { p.ExampleQuestions = []string{"b", "a"} }),
		"owner rotated":       patch(func(p *PersonaProfile) { p.Owner = "o2" }),
	} {
		if got := RequiredBump(prev, next); got != semver.BumpPatch {
			t.Errorf("%s requires %v, want a patch", name, got)
		}
	}
	// Reordering the pins without changing them is not a change at all.
	reordered := agentux065Profile()
	reordered.SkillPins = []agentskills.SkillPin{{ID: "search", Version: 1}}
	if got := RequiredBump(prev, reordered); got != semver.BumpPatch {
		t.Errorf("reordered pins require %v, want a patch", got)
	}
	for name, next := range map[string]PersonaProfile{
		"skill added": patch(func(p *PersonaProfile) {
			p.SkillPins = append(p.SkillPins, agentskills.SkillPin{ID: "ticket", Version: 1})
		}),
		"skill removed":     patch(func(p *PersonaProfile) { p.SkillPins = nil }),
		"audience widened":  patch(func(p *PersonaProfile) { p.Audience.Populations = []string{"all", "partners"} }),
		"audience narrowed": patch(func(p *PersonaProfile) { p.Audience.Populations = []string{"staff"} }),
		"reference added": patch(func(p *PersonaProfile) {
			p.DocumentReferences = append(p.DocumentReferences, agentdocref.Reference{DocumentID: "doc-new", VersionMode: agentdocref.ModeLatestPublished})
		}),
		"reference removed": patch(func(p *PersonaProfile) { p.DocumentReferences = nil }),
		"reference repinned": patch(func(p *PersonaProfile) {
			p.DocumentReferences[0].VersionMode = agentdocref.ModePinned
			p.DocumentReferences[0].PinnedVersion = 3
		}),
		"tier raised":         patch(func(p *PersonaProfile) { p.TierCeiling = agentskills.TierT2 }),
		"kinds extended":      patch(func(p *PersonaProfile) { p.ConversationKinds = append(p.ConversationKinds, ConversationDirect) }),
		"template changed":    patch(func(p *PersonaProfile) { p.Template = &TemplateProvenance{ID: "t", Version: 2, Digest: "sha256:abc"} }),
		"eval suite changed":  patch(func(p *PersonaProfile) { p.EvalSuiteRef = "suite2" }),
		"instructions+skills": patch(func(p *PersonaProfile) { p.Instructions = "other"; p.SkillPins = nil }),
	} {
		if got := RequiredBump(prev, next); got != semver.BumpMinor {
			t.Errorf("%s requires %v, want at least a minor", name, got)
		}
	}
}
