package application

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

const (
	localAgentDemoTaskCatcherPersonaID = "hcmnext.local.persona.task_catcher"
	localAgentDemoReminderPersonaID    = "hcmnext.local.persona.reminder"
	// localAgentDemoAmbientRoom is the channel Task Catcher and Reminder are
	// installed in: a working channel whose members make commitments and state
	// deadlines. The owner turns "Reads every message here" on from its details.
	localAgentDemoAmbientRoom = "engineering"
)

// agentUXDemoSupportCapabilities lets the support effect skills be pinned for
// the demo agent. The skills' operations are capabilities of the support
// executor, not entries of the served skill registry, so a pin is all this
// catalog has to resolve.
type agentUXDemoSupportCapabilities struct{}

func (agentUXDemoSupportCapabilities) Lookup(k capability.Key) (capability.Record, bool) {
	return capability.Record{Definition: capability.Definition{ID: k.ID, Version: k.Version, AgentEligible: true, EffectClass: capability.EffectInternalMutation}, Status: capability.StatusActive}, true
}

// agentUXDemoSupportStarter is Support Desk's starter with its two effect
// skills pinned from a catalog that holds exactly those skills.
func agentUXDemoSupportStarter() (agenttemplate.PersonaStarter, error) {
	catalog := agentskills.NewRegistry(agentUXDemoSupportCapabilities{})
	for _, effect := range agentskills.SupportEffectSkills() {
		if err := catalog.Publish(effect.Skill); err != nil {
			return agenttemplate.PersonaStarter{}, err
		}
	}
	return AgentUXDemoSupportStarter(catalog)
}

// agentUXDemoStarterForManifest finds the starter a manifest of one of the
// agents added after Policy Helper and Assistant was made from. They are not in
// the template registry: each is built by its own constructor.
func agentUXDemoStarterForManifest(manifest agentmanifest.Manifest) (agenttemplate.PersonaStarter, bool) {
	switch {
	case manifest.ID == "agent.starter.birthday_buddy":
		return AgentUXDemoBirthdayStarter(), true
	case manifest.ID == "agent.starter.support_desk":
		starter, err := agentUXDemoSupportStarter()
		return starter, err == nil
	case strings.HasPrefix(manifest.ID, "hcmnext.ambient."):
		for _, starter := range AgentUXAmbientStarters() {
			if manifest.ID == "hcmnext.ambient."+starter.Handle {
				return starter, true
			}
		}
	}
	return agenttemplate.PersonaStarter{}, false
}

// agentUXDemoCandidateContracts reports whether the manifest pins the output
// schema and the skills its starter is meant to have. A candidate model profile
// is derived only from a manifest that does.
func agentUXDemoCandidateContracts(manifest agentmanifest.Manifest, starter agenttemplate.PersonaStarter) (schemaPinned, toolsPinned bool) {
	switch starter.Handle {
	case "support-desk":
		for _, record := range AgentUXDemoPolicyRecords() {
			if record.Reference.ID == AgentUXDemoSupportPlanSchema {
				schemaPinned = manifest.OutputSchema == record.Reference
			}
		}
	case "task-catcher", "reminder":
		schemaPinned = manifest.OutputSchema.ID == AgentUXAmbientProposalSchemaID && manifest.OutputSchema.Digest == personaRunBytesDigest([]byte(AgentUXAmbientProposalSchema))
	default:
		schemaPinned = manifest.OutputSchema.ID == PersonaChatReplySchema && manifest.OutputSchema.Digest == PersonaChatReplySchemaDigest
	}
	var want []string
	for _, pin := range starter.SkillPins {
		want = append(want, pin.ID)
	}
	var have []string
	for _, tool := range manifest.ToolCeiling {
		have = append(have, tool.ID)
	}
	slices.Sort(want)
	slices.Sort(have)
	return schemaPinned, slices.Equal(want, have)
}

// localAgentDemoBirthdaySpec prepares Birthday Buddy for #general (AGENTUX-053).
func localAgentDemoBirthdaySpec() (localAgentDemoStarterSpec, error) {
	starter := AgentUXDemoBirthdayStarter()
	instructions, _ := AgentUXDemoStarterInstructions(starter)
	return localAgentDemoStarterSpec{
		starter: starter, grantSkills: true, rooms: []string{"general"},
		plan: localAgentDemoStarterPlan{PersonaID: AgentUXDemoBirthdayPersonaID, Avatar: "avatar:birthday-buddy", Instructions: instructions, Tier: agentskills.TierT0, UpgradeEvent: "birthday-buddy-upgrade", UpgradeReason: "Birthday Buddy upgrade"},
		manifest: agentUXDemoManifestBuilder(starter, instructions, func(starter agenttemplate.PersonaStarter) (agentmanifest.Manifest, error) {
			return AgentUXDemoManifest(starter)
		}),
	}, nil
}

// localAgentDemoSupportSpec prepares Support Desk. It is published and left
// unplaced: it acts on its inbox and its project, not on a conversation.
func localAgentDemoSupportSpec() (localAgentDemoStarterSpec, error) {
	starter, err := agentUXDemoSupportStarter()
	if err != nil {
		return localAgentDemoStarterSpec{}, err
	}
	instructions, _ := AgentUXDemoStarterInstructions(starter)
	return localAgentDemoStarterSpec{
		starter: starter, grantSkills: true, unplaced: true,
		plan: localAgentDemoStarterPlan{PersonaID: AgentUXDemoSupportPersonaID, Avatar: "avatar:support-desk", Instructions: instructions, Tier: agentskills.TierT3, UpgradeEvent: "support-desk-upgrade", UpgradeReason: "Support Desk upgrade"},
		manifest: agentUXDemoManifestBuilder(starter, instructions, func(starter agenttemplate.PersonaStarter) (agentmanifest.Manifest, error) {
			return AgentUXDemoManifest(starter)
		}),
	}, nil
}

// localAgentDemoAmbientSpec prepares Task Catcher or Reminder for the working
// channel (AGENTUX-067, AGENTUX-068). The preparation never turns on "Reads
// every message here": that is the channel administrator's choice.
func localAgentDemoAmbientSpec(handle string) (localAgentDemoStarterSpec, error) {
	for _, starter := range AgentUXAmbientStarters() {
		if starter.Handle != handle {
			continue
		}
		persona := localAgentDemoTaskCatcherPersonaID
		if handle == "reminder" {
			persona = localAgentDemoReminderPersonaID
		}
		instructions := AgentUXAmbientInstructions(handle)
		return localAgentDemoStarterSpec{
			starter: starter, grantSkills: true, rooms: []string{localAgentDemoAmbientRoom},
			plan: localAgentDemoStarterPlan{PersonaID: persona, Avatar: "avatar:" + handle, Instructions: instructions, Tier: agentskills.TierT1, UpgradeEvent: handle + "-upgrade", UpgradeReason: starter.DisplayName + " upgrade"},
			manifest: agentUXDemoManifestBuilder(starter, instructions, func(starter agenttemplate.PersonaStarter) (agentmanifest.Manifest, error) {
				selection := LocalPersonaOpenAIPolicySelection()
				base := localDevPersonaStarterManifest(starter, instructions, localDevPersonaStarterReferences{modelPolicy: selection.ModelPolicy, outputSchema: selection.OutputSchema, evaluation: selection.EvaluationSuites[starter.EvaluationSuite]})
				return AgentUXAmbientManifest(base, starter)
			}),
		}, nil
	}
	return localAgentDemoStarterSpec{}, ErrAgentUXAmbientInvalid
}

// agentUXDemoManifestBuilder saves the manifest a constructor makes, with the
// instruction text it pins, as the starter's manifest.
func agentUXDemoManifestBuilder(starter agenttemplate.PersonaStarter, instructions string, build func(agenttemplate.PersonaStarter) (agentmanifest.Manifest, error)) func(context.Context, *agentstore.Store, uuid.UUID) (agentmanifest.Manifest, error) {
	return func(ctx context.Context, agents *agentstore.Store, tenant uuid.UUID) (agentmanifest.Manifest, error) {
		manifest, err := build(starter)
		if err != nil {
			return agentmanifest.Manifest{}, err
		}
		return ensureStarterManifest(ctx, agents, tenant, manifest, instructions)
	}
}
