package application

import (
	"context"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func announcementAuthoritativeModelFields(ctx context.Context, admission agentrun.Record, profile agentpersona.PersonaProfile, manifest agentmanifest.Manifest, route PersonaRunModelRoute) (map[string]personaAuthoritativeModelField, error) {
	runtime, ok := ctx.Value(announcementRuntimeKey{}).(*AgentAnnouncementRuntime)
	if !ok || runtime == nil {
		return nil, ErrAgentAnnouncementDenied
	}
	record, err := runtime.record(ctx, admission.Request)
	if err != nil {
		return nil, err
	}
	if record.PersonaID == AgentUXDemoBirthdayPersonaID {
		return runtime.agentuxDemoBirthdayAuthoritativeFields(ctx, record, profile, manifest, route)
	}
	documents, err := runtime.resolved(ctx, record)
	if err != nil {
		return nil, err
	}
	refs := announcementResolvedReferences(documents)
	class, err := runtime.documentClass(ctx, record, documents, route)
	if err != nil {
		return nil, err
	}
	profile.Guidance = renderPersonaGuidanceDocumentTokens(profile, refs)
	data, documentRefs, err := quarantinedAgentDocumentData(refs)
	if err != nil {
		return nil, err
	}
	goal, err := runtime.goal(ctx, record)
	if err != nil {
		return nil, err
	}
	fields := map[string]personaAuthoritativeModelField{
		"model.message.0": {value: manifest.Purpose, class: route.ProfileClass, source: "persona-profile", role: agentmodel.RoleSystem},
		"model.message.1": {value: personaDeveloperMessage(profile), class: route.ProfileClass, source: "persona-profile", role: agentmodel.RoleDeveloper},
		"model.message.2": {value: agentDocumentContainmentMessage, class: route.ProfileClass, source: "persona-profile", role: agentmodel.RoleDeveloper},
		"model.message.3": {value: data, class: class, source: "persona-untrusted-reference-document", role: agentmodel.RoleUser},
		"model.message.4": {value: goal, class: route.InvokerClass, source: "persona-invoking-post", role: agentmodel.RoleUser},
	}
	for i, ref := range documentRefs {
		fields[fmt.Sprintf("model.context.%d", i)] = personaAuthoritativeModelField{value: fmt.Sprintf("%s\x00%s\x00%s", ref.ID, ref.Version, ref.Digest), class: class, source: "persona-reference-document"}
	}
	return fields, nil
}
