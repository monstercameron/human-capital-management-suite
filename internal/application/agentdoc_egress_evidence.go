package application

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type personaAuthoritativeModelField struct {
	value  string
	class  trustdlp.DataClass
	source string
	role   agentmodel.MessageRole
}

// personaAuthoritativeDocumentFields reconstructs document-derived fields from
// the sealed profile and the current invoker's document access. It never reads
// a value or class from the outbound request.
func personaAuthoritativeDocumentFields(ctx context.Context, resolver agentdocref.Resolver, threads agentinvoke.ThreadReader, admission agentrun.Record, profile agentpersona.PersonaProfile, manifest agentmanifest.Manifest, route PersonaRunModelRoute) (map[string]personaAuthoritativeModelField, error) {
	if admission.Request.Source.Kind == agentrun.SourceAnnouncement {
		return announcementAuthoritativeModelFields(ctx, admission, profile, manifest, route)
	}
	if ctx == nil || threads == nil {
		return nil, errAgentDocumentResolution
	}
	documents, _, err := resolvePersonaReferenceDocuments(ctx, resolver, admission, profile)
	if err != nil {
		return nil, err
	}
	profile.Guidance = renderPersonaGuidanceDocumentTokens(profile, documents)
	_, history, refs, err := (&DatabasePersonaRunModelWorkSource{threads: threads}).readThreadContext(ctx, admission, profile)
	if err != nil {
		return nil, err
	}
	fields := map[string]personaAuthoritativeModelField{
		"model.message.0": {value: manifest.Purpose, class: route.ProfileClass, source: "persona-profile", role: agentmodel.RoleSystem},
		"model.message.1": {value: personaDeveloperMessage(profile), class: route.ProfileClass, source: "persona-profile", role: agentmodel.RoleDeveloper},
	}
	if len(documents) == 0 {
		return fields, nil
	}
	documentClass, err := personaReferenceDocumentDataClass(ctx, resolver, admission, documents, route)
	if err != nil {
		return nil, err
	}
	content, documentRefs, err := quarantinedAgentDocumentData(documents)
	if err != nil {
		return nil, err
	}
	containmentIndex := 2 + len(history)
	fields[fmt.Sprintf("model.message.%d", containmentIndex)] = personaAuthoritativeModelField{value: agentDocumentContainmentMessage, class: route.ProfileClass, source: "persona-profile", role: agentmodel.RoleDeveloper}
	fields[fmt.Sprintf("model.message.%d", containmentIndex+1)] = personaAuthoritativeModelField{value: content, class: documentClass, source: "persona-untrusted-reference-document", role: agentmodel.RoleUser}
	for i, ref := range documentRefs {
		fields[fmt.Sprintf("model.context.%d", len(refs)+i)] = personaAuthoritativeModelField{value: fmt.Sprintf("%s\x00%s\x00%s", ref.ID, ref.Version, ref.Digest), class: documentClass, source: "persona-reference-document"}
	}
	return fields, nil
}

func personaAuthoritativeDocumentField(fields map[string]personaAuthoritativeModelField, source agentegress.SourceClassificationRequest) bool {
	field, ok := fields[source.FieldName]
	return ok && field.source == source.SourceClass && field.class == source.DataClass && (source.MessageRole == "" || source.MessageRole == field.role) && source.ValueDigest == personaRunBytesDigest([]byte(field.value))
}
