package productui

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// PersonaAdminCommandRequest is the untrusted, JSON-safe input to one persona
// administration command. Tenant and actor identity are intentionally absent.
type PersonaAdminCommandRequest struct {
	Action             string   `json:"action"`
	PersonaID          string   `json:"persona_id"`
	StarterID          string   `json:"starter_id,omitempty"`
	StarterVersion     uint32   `json:"starter_version,omitempty"`
	AvatarRef          string   `json:"avatar_ref,omitempty"`
	OrganizationScopes []string `json:"organization_scopes,omitempty"`
	ManifestID         string   `json:"manifest_id,omitempty"`
	BusinessOwnerID    string   `json:"business_owner_id,omitempty"`
	TechnicalStewardID string   `json:"technical_steward_id,omitempty"`
	Handle             string   `json:"handle,omitempty"`
	DisplayName        string   `json:"display_name,omitempty"`
	Purpose            string   `json:"purpose,omitempty"`
	// Instructions carries administrator guidance. A nil pointer keeps the
	// current guidance on CREATE_VERSION; a non-nil empty string clears it.
	Instructions    *string  `json:"instructions,omitempty"`
	AllowedChannels []string `json:"allowed_channels,omitempty"`
	ConversationID  string   `json:"conversation_id,omitempty"`
	ReviewID        string   `json:"review_id,omitempty"`
	EvaluationRunID string   `json:"evaluation_run_id,omitempty"`
	Decision        string   `json:"decision,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	// DocumentReferences is nil for commands that do not edit references.
	// CREATE_VERSION sets a non-nil pointer so [] explicitly clears the prior
	// version while omission remains available to older callers.
	DocumentReferences *[]agentdocref.Reference `json:"document_references,omitempty"`
}

// PersonaAdminEvaluationResult is the safe command receipt shown after an
// evaluator run. Case names are scenario labels; prompts and model output are
// deliberately excluded.
type PersonaAdminEvaluationResult struct {
	Status       string   `json:"status"`
	Passed       int      `json:"passed"`
	Failed       int      `json:"failed"`
	FailingCases []string `json:"failing_cases,omitempty"`
}

// PersonaAdminCommandTransport executes a command after deriving the actor
// from the authenticated request context and reauthorizing the action.
type PersonaAdminCommandTransport interface {
	ExecutePersonaAdminCommand(context.Context, PersonaAdminCommandRequest) error
}

// PersonaAdminDraftInput is the proposed tenant-owned configuration created
// from a server-owned starter. The server validates every override against the
// starter's authority ceiling before persistence.
type PersonaAdminDraftInput struct {
	PersonaAdminCommandRequest
}

// PersonaAdminDraftClient submits an editor draft through the authenticated
// persona administration command boundary.
type PersonaAdminDraftClient interface {
	CreatePersonaDraft(context.Context, PersonaAdminDraftInput) error
}
