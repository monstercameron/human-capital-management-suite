package application

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// AGENTUX-076: the list_documents tool. A question about which documents an
// agent can read is answered from a list the server reads, not from a keyword
// search of the question's own words. The tool returns the same list the facts
// block carries (placed here and workspace-wide, titles, identifiers and counts),
// is journaled like the search tools, and is read again before its result leaves.

var personaListDocumentsSchema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

func listDocumentsToolSchema() agentmodel.ToolSchema {
	return agentmodel.ToolSchema{Name: personaListDocumentsTool, Description: "List the documents you can read in this conversation: the ones placed in it and the ones every workspace member may read, with titles, identifiers and counts. Use it for any question about which documents you can read. It lists titles only; to say what a document says, search for it. Takes no arguments.", InputSchema: bytes.Clone(personaListDocumentsSchema)}
}

// executeListDocuments reads the asker's reach now and journals the list. The
// reach is the one the agent's pinned skills allow: the conversation's placed
// documents with the conversation search skill, the workspace's with the
// workspace skill.
func (e *PersonaRuntimeTools) executeListDocuments(ctx context.Context, record agentrun.Record, run runstate.Run, proposal agentmodel.ToolProposal) ([]byte, string, string, error) {
	if ctx == nil || isNilPersonaOutputPort(e.cfg.Lister) || proposal.ID == "" || len(proposal.ID) > 512 || strings.TrimSpace(proposal.ID) != proposal.ID || !personaRunJSONObject(proposal.Arguments) {
		return nil, "", "", personaRuntimeToolDeniedHere()
	}
	ctx = WithPersonaBackgroundAdmission(ctx, record)
	identity, pin, err := e.authorize(ctx, record, run)
	if err != nil {
		return nil, "", "", personaRuntimeToolDeniedHere()
	}
	workspacePin, err := e.workspacePinAuthorized(ctx, record, run, identity)
	if err != nil || (pin.ID == "" && workspacePin.ID == "") {
		return nil, "", "", personaRuntimeToolDeniedHere()
	}
	skill := pin
	if skill.ID == "" {
		skill = workspacePin
	}
	facts := personaAgentFacts{Reads: true}
	documents, listErr := e.cfg.Lister.ListPersonaReadableDocuments(ctx, identity.TenantID, identity.ConversationID, identity.InvokerID, pin.ID != "", workspacePin.ID != "", true)
	if listErr != nil {
		facts.Failed = true
	} else {
		facts.Documents = documents
		facts.Documents.PlacedTotal, facts.Documents.WorkspaceTotal = len(documents.Placed), len(documents.Workspace)
	}
	encoded, err := facts.personaListDocumentsResult()
	if err != nil || len(encoded) == 0 || len(encoded) > 64*1024 {
		return nil, "", "", personaRuntimeToolDeniedHere()
	}
	digest := personaRunT0ToolOutputDigest(encoded)
	// Titles are company-internal reference data, the class an empty search
	// result carries.
	r := agentpersonastore.ToolResultRecord{TenantID: values.TenantId(identity.TenantID), RunID: run.ID, AdmissionDigest: personaRuntimeToolAdmissionDigest(record.RequestDigest), InvocationID: identity.InvocationID, InvokerID: identity.InvokerID, ConversationID: identity.ConversationID, ThreadID: identity.ThreadID, PersonaID: identity.PersonaID, PersonaVersion: identity.PersonaVersion, InstallationID: identity.InstallationID, AgentID: identity.AgentID, ToolCallID: proposal.ID, ToolName: proposal.Name, SkillID: skill.ID, SkillVersion: skill.Version, SkillDigest: skill.Digest, Arguments: bytes.Clone(proposal.Arguments), Output: encoded, OutputDigest: digest, DataClass: trustdlp.ClassInternal, CreatedAt: e.cfg.Now().UTC()}
	if err := e.cfg.Journal.PutPersonaRuntimeToolResult(ctx, r); err != nil {
		return nil, "", "", personaRuntimeToolDeniedHere()
	}
	return encoded, "persona-tool-result:" + run.ID + ":" + proposal.ID, digest, nil
}

// recheckListDocuments is the check before a journaled list leaves for the
// model. What the list named must still be readable by the asker; a document
// placed or shared since the list was read is not a reason to refuse it.
func (e *PersonaRuntimeTools) recheckListDocuments(ctx context.Context, r agentpersonastore.ToolResultRecord) error {
	if isNilPersonaOutputPort(e.cfg.Lister) || r.TenantID.Validate() != nil || r.ToolCallID == "" || r.InvokerID == "" || r.ConversationID == "" || r.AgentID == "" || r.OutputDigest != personaRunT0ToolOutputDigest(r.Output) || !r.DataClass.Valid() {
		return personaRuntimeToolDeniedHere()
	}
	pin := agentskills.SkillPin{ID: r.SkillID, Version: r.SkillVersion, Digest: r.SkillDigest}
	if (r.SkillID != personaPolicyHelperSkillID || !personaRunT0PinHasDocumentSearchOperation(e.cfg.Catalog, pin)) && (r.SkillID != personaWorkspaceSearchSkillID || !personaWorkspacePinHasSearchOperation(e.cfg.Catalog, pin)) {
		return personaRuntimeToolDeniedHere()
	}
	var recorded personaFactsListing
	if json.Unmarshal(r.Output, &recorded) != nil {
		return personaRuntimeToolDeniedHere()
	}
	named := append(append([]PersonaReadableDocument(nil), recorded.PlacedHere...), recorded.Workspace...)
	if len(named) == 0 {
		return nil
	}
	current, err := e.cfg.Lister.ListPersonaReadableDocuments(ctx, r.TenantID.String(), r.ConversationID, r.InvokerID, true, true, true)
	if err != nil {
		return personaRuntimeToolDeniedHere()
	}
	readable := make(map[string]bool, len(current.Placed)+len(current.Workspace))
	for _, list := range [][]PersonaReadableDocument{current.Placed, current.Workspace} {
		for _, document := range list {
			readable[document.DocumentID] = true
		}
	}
	for _, document := range named {
		if !readable[document.DocumentID] {
			return personaRuntimeToolDeniedHere()
		}
	}
	return nil
}
