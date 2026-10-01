package productui

import (
	"net/url"
	"strings"
)

// AgentAccessLoadState is the server-backed state of the access surface. A
// missing client is not treated as an empty answer: the UI must say that the
// connection is unavailable instead of suggesting that the user has no
// grants.
type AgentAccessLoadState string

const (
	AgentAccessStateReady       AgentAccessLoadState = "ready"
	AgentAccessStateLoading     AgentAccessLoadState = "loading"
	AgentAccessStateUnavailable AgentAccessLoadState = "unavailable"
)

// AgentAccessClient is the narrow client seam for the user's own access page.
// Implementations own authorization, CSRF, current-user checks and the
// server-side recheck immediately before unlink or revoke. The browser never
// receives a connector credential.
type AgentAccessClient interface {
	StartProviderAuthorization(providerID string) (AgentAuthorizationStart, error)
	UnlinkConnection(connectionID string) error
	RevokeDelegation(taskID string) error
}

// AgentAuthorizationStart is the server-created OAuth authorization handoff.
// PKCE fields are included as evidence for the client boundary; the page does
// not render the URL as a link in chat or accept a user-supplied URL.
type AgentAuthorizationStart struct {
	AuthorizationURL    string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

func (start AgentAuthorizationStart) ValidPKCE() bool {
	authorizationURL, err := url.Parse(strings.TrimSpace(start.AuthorizationURL))
	if err != nil || authorizationURL == nil || authorizationURL.Host == "" {
		return false
	}
	scheme := strings.ToLower(authorizationURL.Scheme)
	if scheme != "https" && scheme != "http" {
		return false
	}
	return strings.TrimSpace(start.AuthorizationURL) != "" &&
		strings.TrimSpace(start.State) != "" &&
		strings.TrimSpace(start.CodeChallenge) != "" &&
		strings.EqualFold(strings.TrimSpace(start.CodeChallengeMethod), "S256")
}

// AgentAccessSkill is an authorization-filtered skill projection. Tier is
// deliberately shown beside the skill so T3/T4 reach is never implied by a
// generic "connected" badge.
type AgentAccessSkill struct {
	ID          string
	Name        string
	Tier        string
	Scope       string
	Description string
}

type AgentAccessConnection struct {
	ID             string
	Provider       string
	AccountLabel   string
	LinkState      string
	CredentialMode string
	Skills         []AgentAccessSkill
	CanLink        bool
	CanUnlink      bool
}

type AgentDelegationGrant struct {
	ID        string
	TaskID    string
	TaskLabel string
	Scope     string
	ExpiresAt string
	Revocable bool
}

type AgentAccessSnapshot struct {
	Connections []AgentAccessConnection
	Delegations []AgentDelegationGrant
}

type AgentAccessPageProps struct {
	I18nProps
	State             AgentAccessLoadState
	UnavailableReason string
	Snapshot          AgentAccessSnapshot
	Client            AgentAccessClient
	OnAuthorization   func(AgentAuthorizationStart)
}

// AgentAdminAccessClient is the mutation seam for the administrator console.
// Each operation is expected to be authorized and revision-checked by the
// server. The UI only chooses which typed operation to request.
type AgentAdminAccessClient interface {
	CreateConnectionRevision(AgentConnectionRevision) error
	ImportMCPSnapshot(connectionID, snapshotID string) error
	PublishConnectionRevision(revisionID string) error
	RequestSecondAdminApproval(revisionID string) error
}

type AgentGrantScope struct {
	Kind  string
	Value string
}

type AgentAdminSkillGrant struct {
	SkillID             string
	SkillName           string
	Tier                string
	Scopes              []AgentGrantScope
	RequiresSecondAdmin bool
}

type AgentConnectionRevision struct {
	ID                  string
	Provider            string
	Revision            string
	Status              string
	CredentialMode      string
	MCPSnapshotID       string
	Grants              []AgentAdminSkillGrant
	RequiresSecondAdmin bool
	SecondAdminApproved bool
}

type AgentEffectiveAccessPreview struct {
	SubjectLabel string
	ScopeLabel   string
	Connections  []AgentAccessConnection
	Warnings     []string
}

type AgentAdminAccessSnapshot struct {
	Revisions      []AgentConnectionRevision
	Preview        AgentEffectiveAccessPreview
	PreviewReady   bool
	PreviewSubject string
}

type AgentAdminAccessPageProps struct {
	I18nProps
	State             AgentAccessLoadState
	UnavailableReason string
	Snapshot          AgentAdminAccessSnapshot
	Client            AgentAdminAccessClient
}
