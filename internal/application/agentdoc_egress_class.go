package application

import (
	"context"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type personaReferenceDocumentClassificationSource interface {
	PersonaReferenceDocumentClass(context.Context, agentdocref.Invoker, agentdocref.ResolvedDocument) (trustdlp.DataClass, error)
}

// PersonaReferenceDocumentClass reads the same version classification used by
// policy search, under the same invoker as the content resolver.
func (r *agentDocumentResolver) PersonaReferenceDocumentClass(ctx context.Context, invoker agentdocref.Invoker, document agentdocref.ResolvedDocument) (trustdlp.DataClass, error) {
	if r == nil || ctx == nil || document.Version == 0 {
		return "", errAgentDocumentResolution
	}
	reader, ok := r.source.(documentHubAgentDocumentReader)
	if !ok || reader.store == nil {
		return "", errAgentDocumentResolution
	}
	var versionID string
	err := reader.store.RunTenantTx(ctx, invoker.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT ordered.id FROM
			(SELECT id,row_number() OVER (ORDER BY created_at,id) AS number FROM document_version WHERE tenant_id=$1 AND document_id=$2) ordered
			WHERE ordered.number=$3`, invoker.TenantID, document.Reference.DocumentID, document.Version).Scan(&versionID)
	})
	if err != nil {
		return "", errAgentDocumentResolution
	}
	return (DatabasePersonaRuntimeDocumentVersions{Store: reader.store}).PersonaRuntimeDocumentClass(ctx, PersonaRunT0ToolInvocation{TenantID: invoker.TenantID, InvokerID: invoker.SubjectID}, document.Reference.DocumentID, versionID)
}

func personaReferenceDocumentDataClass(ctx context.Context, resolver agentdocref.Resolver, admission agentrun.Record, documents []agentdocref.ResolvedDocument, route PersonaRunModelRoute) (trustdlp.DataClass, error) {
	if len(documents) == 0 {
		return "", nil
	}
	source, ok := resolver.(personaReferenceDocumentClassificationSource)
	if !ok {
		return "", errAgentDocumentResolution
	}
	invoker := agentdocref.Invoker{TenantID: admission.Request.Source.TenantID, SubjectID: admission.Request.Principal.InvokerID}
	var class trustdlp.DataClass
	for i, document := range documents {
		actual, err := source.PersonaReferenceDocumentClass(ctx, invoker, document)
		// As with policy search, a single result cannot mix classifications.
		if err != nil || !actual.Valid() || (i > 0 && actual != class) || !slices.Contains(route.Route.Task.DataClasses, string(actual)) || !slices.Contains(route.Egress.AllowedClasses, actual) || !personaOpenAISourceClassCovers(route.ProfileClass, actual) {
			return "", errAgentDocumentResolution
		}
		class = actual
	}
	return class, nil
}

func classifyPersonaReferenceDocumentFields(request *AgentModelExecutorRequest, class trustdlp.DataClass) {
	for i := range request.Outbound.Fields {
		field := &request.Outbound.Fields[i]
		if source := request.FieldSources[field.Name]; source == "persona-untrusted-reference-document" || source == "persona-reference-document" {
			field.Class = class
		}
	}
}
