package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatappstore"
)

// chatInstallationSearchAuthority adapts a chat app installation to
// [AgentInstallationAuthority] for agent document search (HUB-031). The agent
// acts inside one conversation, so its installation is the one chat itself
// admits it with (tenant:conversation:app, as in machineAdmission): current,
// same tenant and conversation, same app, a real revision. Anything else,
// including a store fault, is "not installed" and never "unrestricted".
type chatInstallationSearchAuthority struct {
	apps interface {
		Get(context.Context, string) (chatapps.Installation, error)
	}
	conversationID string
	now            func() time.Time
}

// newChatInstallationSearchAuthority binds the installation lookup to the
// conversation the agent was invoked from.
func newChatInstallationSearchAuthority(apps interface {
	Get(context.Context, string) (chatapps.Installation, error)
}, conversationID string, now func() time.Time) chatInstallationSearchAuthority {
	return chatInstallationSearchAuthority{apps: apps, conversationID: conversationID, now: now}
}

// InstalledScopes implements [AgentInstallationAuthority].
func (a chatInstallationSearchAuthority) InstalledScopes(ctx context.Context, tenantID, agentID string) ([]string, error) {
	if a.apps == nil || a.now == nil || tenantID == "" || agentID == "" || a.conversationID == "" {
		return nil, ErrAgentNotInstalled
	}
	v, err := a.apps.Get(chatappstore.WithTenant(ctx, tenantID), tenantID+":"+a.conversationID+":"+agentID)
	if err != nil {
		return nil, errors.Join(ErrAgentNotInstalled, err)
	}
	if !v.Current(a.now()) || v.Tenant != tenantID || v.Conversation != a.conversationID || v.AppID != agentID || v.Revision == 0 {
		return nil, ErrAgentNotInstalled
	}
	return append([]string(nil), v.GrantedScopes...), nil
}
