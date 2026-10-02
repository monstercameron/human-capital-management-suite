package application

import (
	"context"
	"fmt"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

// AgentDocumentRuntimeTools composes the existing reviewed T0 tool runtime
// with referenced-document grounding. Tool discovery and execution remain
// entirely owned by the existing runtime; this value only adds current,
// invoker-authorized document observations to the existing citation path.
type AgentDocumentRuntimeTools struct {
	tools interface {
		PersonaRunT0ToolExecutionPort
		PersonaRuntimeToolGroundingSource
	}
	documents personaAgentDocumentGroundingSource
}

type personaAgentDocumentGroundingSource interface {
	ResolvePersonaAgentDocuments(context.Context, agentrun.Record) ([]agentdocref.ResolvedDocument, []agentdocref.Omission, error)
}

func (s *DatabasePersonaRunModelWorkSource) ResolvePersonaAgentDocuments(ctx context.Context, record agentrun.Record) ([]agentdocref.ResolvedDocument, []agentdocref.Omission, error) {
	if s == nil || ctx == nil {
		return nil, nil, errAgentDocumentResolution
	}
	profile, _, err := s.resolveProfileAndManifest(ctx, record)
	if err != nil {
		return nil, nil, err
	}
	return s.resolveAgentDocuments(ctx, record, profile)
}

func NewAgentDocumentRuntimeTools(tools interface {
	PersonaRunT0ToolExecutionPort
	PersonaRuntimeToolGroundingSource
}, documents personaAgentDocumentGroundingSource) (*AgentDocumentRuntimeTools, error) {
	if isNilPersonaOutputPort(tools) || isNilPersonaOutputPort(documents) {
		return nil, errAgentDocumentResolution
	}
	return &AgentDocumentRuntimeTools{tools: tools, documents: documents}, nil
}

func (t *AgentDocumentRuntimeTools) ToolSchemas(ctx context.Context, record agentrun.Record, run runstate.Run) ([]agentmodel.ToolSchema, error) {
	if t == nil || isNilPersonaOutputPort(t.tools) {
		return nil, errAgentDocumentResolution
	}
	return t.tools.ToolSchemas(ctx, record, run)
}

func (t *AgentDocumentRuntimeTools) Execute(ctx context.Context, record agentrun.Record, run runstate.Run, proposal agentmodel.ToolProposal) ([]byte, string, string, error) {
	if t == nil || isNilPersonaOutputPort(t.tools) {
		return nil, "", "", errAgentDocumentResolution
	}
	return t.tools.Execute(ctx, record, run, proposal)
}

func (t *AgentDocumentRuntimeTools) RecheckPersonaRuntimeToolResult(ctx context.Context, record agentpersonastore.ToolResultRecord) error {
	if t == nil || isNilPersonaOutputPort(t.tools) {
		return errAgentDocumentResolution
	}
	source, ok := t.tools.(PersonaRuntimeToolSourceValidator)
	if !ok || isNilPersonaOutputPort(source) {
		return errAgentDocumentResolution
	}
	return source.RecheckPersonaRuntimeToolResult(ctx, record)
}

func (t *AgentDocumentRuntimeTools) ReadPersonaRunToolGrounding(ctx context.Context, record agentrun.Record, run runstate.Run, gateway *agentsecurity.ToolGateway) ([]agentsecurity.Datum, error) {
	if t == nil || isNilPersonaOutputPort(t.tools) || isNilPersonaOutputPort(t.documents) || gateway == nil {
		return nil, errAgentDocumentResolution
	}
	grounding, err := t.tools.ReadPersonaRunToolGrounding(ctx, record, run, gateway)
	if err != nil {
		return nil, err
	}
	documents, _, err := t.documents.ResolvePersonaAgentDocuments(ctx, record)
	if err != nil {
		return nil, err
	}
	for _, document := range documents {
		sourceID := "document:" + document.Reference.DocumentID + "/version:" + strconv.FormatUint(document.Version, 10)
		location := sourceID
		if document.Reference.SectionAnchor != "" {
			location += "/section:" + document.Reference.SectionAnchor
		}
		datum, err := gateway.Observe(agentsecurity.SourceDocument, document.Content, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: sourceID, Location: location, Digest: personaRunT0ToolOutputDigest([]byte(document.Content))})
		if err != nil {
			return nil, fmt.Errorf("%w: observe reference document", errAgentDocumentResolution)
		}
		grounding = append(grounding, datum)
	}
	return grounding, nil
}

func personaCitedAgentDocuments(documents []agentdocref.ResolvedDocument, citations []agentsecurity.Citation) []agentdocref.ResolvedDocument {
	out := make([]agentdocref.ResolvedDocument, 0, len(documents))
	for _, document := range documents {
		sourceID := "document:" + document.Reference.DocumentID + "/version:" + strconv.FormatUint(document.Version, 10)
		location := sourceID
		if document.Reference.SectionAnchor != "" {
			location += "/section:" + document.Reference.SectionAnchor
		}
		digest := personaRunT0ToolOutputDigest([]byte(document.Content))
		for _, citation := range citations {
			if citation.SourceID == sourceID && citation.Location == location && citation.Digest == digest {
				out = append(out, document)
				break
			}
		}
	}
	return out
}

var _ PersonaRunT0ToolExecutionPort = (*AgentDocumentRuntimeTools)(nil)
var _ PersonaRuntimeToolGroundingSource = (*AgentDocumentRuntimeTools)(nil)
var _ PersonaRuntimeToolSourceValidator = (*AgentDocumentRuntimeTools)(nil)
