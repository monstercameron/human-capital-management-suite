package application

import (
	"slices"
	"strings"
)

// Local development has one priced OpenAI model (LocalPersonaOpenAIModelID,
// pinned by NewLocalPersonaOpenAIModelDeployment). The shared defaults for the
// translation and writing-style routes name gpt-6-luna, which the local base
// has no price for, so without these helpers every local run needed the model
// named by hand. The defaults apply only to the local profile and only when
// nothing is configured: a configured model always wins, and no other profile
// or base deployment ever has a model substituted.

// chatlangModelForProfile is the translation model to bind: the configured one,
// else the local model in the local-dev profile when the qualified deployment
// is for that model, else the global default. A local deployment loaded from a
// file that qualifies another model keeps the global default, so it is never
// turned into a mismatch.
func chatlangModelForProfile(env func(string) string, local bool, dep PersonaModelDeployment) string {
	if local && env != nil && strings.TrimSpace(env(EnvChatTranslationModel)) == "" && chatlangDeploymentQualifiesModel(dep, LocalPersonaOpenAIModelID) {
		return LocalPersonaOpenAIModelID
	}
	return ChatlangModelFromEnv(env)
}

// chatlangDeploymentQualifiesModel reports whether a profile qualified for the
// translation task names the model.
func chatlangDeploymentQualifiesModel(dep PersonaModelDeployment, model string) bool {
	task := ChatlangTaskProfile().ID
	for i := range dep.Profiles {
		if slices.Contains(dep.Profiles[i].TaskProfileIDs, task) && dep.Profiles[i].Identity.ModelID == model {
			return true
		}
	}
	return false
}

// isLocalPersonaOpenAIBase reports whether a deployment was composed by
// NewLocalPersonaOpenAIModelDeployment: its credential, worker and cell are the
// ones that function pins.
func isLocalPersonaOpenAIBase(dep PersonaModelDeployment) bool {
	return dep.Credential.ID == "local-openai-model" && dep.Worker.ID == "local-persona-worker" && dep.Worker.Cell == "local-dev" && len(dep.Profiles) > 0 && dep.Profiles[0].Identity.ModelID == LocalPersonaOpenAIModelID
}

// configuredModel is the model to qualify: the one named, else the local model
// when the base deployment is the local one, else empty (the global default).
func (c ChattoneQualifyConfig) configuredModel() string {
	if strings.TrimSpace(c.Model) == "" && isLocalPersonaOpenAIBase(c.Base) {
		return LocalPersonaOpenAIModelID
	}
	return c.Model
}
