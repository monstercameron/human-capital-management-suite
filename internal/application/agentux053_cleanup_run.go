package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// LocalAgentDemoCleanupConfig is the operator's input to the cleanup command.
// The cleanup reads and tombstones posts in #general of the local demo
// workspace through the chat service, so it needs the core and chat databases
// and nothing else.
type LocalAgentDemoCleanupConfig struct {
	Profile, Tenant, DatabaseURL, ChatDatabaseURL string
	Now                                           func() time.Time
}

// ValidateLocalAgentDemoCleanupConfig refuses everything but the local
// development profile, the demo workspace and loopback databases, before any
// connection is opened.
func ValidateLocalAgentDemoCleanupConfig(config LocalAgentDemoCleanupConfig) error {
	if config.Profile != ServeProfileLocalDev || config.Tenant != localAgentDemoTenant {
		return fmt.Errorf("agent demo cleanup requires -profile local-dev and -tenant %s", localAgentDemoTenant)
	}
	for name, raw := range map[string]string{"database-url": config.DatabaseURL, "chat-database-url": config.ChatDatabaseURL} {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || !loopbackDatabaseHost(parsed.Hostname()) {
			return fmt.Errorf("-%s must be a loopback PostgreSQL URL", name)
		}
	}
	return nil
}

// localAgentDemoCleanupAuthors supplies the signed-in author of each post the
// cleanup removes: the administrator for the questions, the agent for its
// replies. They are local principals for the local demo workspace only; the
// cleanup never borrows an administrator's roles to delete an agent's post.
type localAgentDemoCleanupAuthors struct {
	now          func() time.Time
	organization string
}

func (a localAgentDemoCleanupAuthors) DemoPostAuthor(_ context.Context, tenant, subject string) (*trust.Principal, error) {
	if tenant != localAgentDemoTenant {
		return nil, errors.New("cleanup author is not in the demo workspace")
	}
	now := a.now()
	if subject == localAgentDemoAdmin {
		return localAgentDemoPrincipal(now, tenant, subject, a.organization)
	}
	if subject != localAgentDemoAgentID && subject != localAgentDemoPersonaID {
		return nil, errors.New("cleanup author is not a known demo author")
	}
	return trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "local-agent-demo-cleanup", CredentialDigest: "sha256:local-agent-demo-cleanup", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
}

// RunLocalAgentDemoCleanup archives the old verification questions in
// #general, and their Policy Helper replies, out of the demo channel. It is the
// separate cleanup command's body and is never called by the preparation: the
// owner decides when the old posts go. It returns the post identifiers it
// removed, including those removed before a failure.
func RunLocalAgentDemoCleanup(ctx context.Context, config LocalAgentDemoCleanupConfig) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("local agent demo cleanup requires a context")
	}
	if err := ValidateLocalAgentDemoCleanupConfig(config); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if config.Now != nil {
		now = config.Now().UTC()
	}
	now = now.Truncate(time.Microsecond)
	mapper := func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(tenant.String()) }
	core, err := pgxadapter.NewPool(ctx, config.DatabaseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("open local core authority: %w", err)
	}
	defer core.Close()
	organization, err := NewAgentHomeOrganizationDirectoryDB(core, mapper).CurrentHomeOrganization(ctx, values.TenantId(config.Tenant), localAgentDemoAdmin)
	if err != nil {
		return nil, fmt.Errorf("resolve administrator home organization: %w", err)
	}
	admin, err := localAgentDemoPrincipal(now, config.Tenant, localAgentDemoAdmin, organization)
	if err != nil {
		return nil, err
	}
	roleAccess := roleaccessstore.New(core, mapper, productFeatureCatalog()...)
	chatRuntime, err := composeChat(ctx, ServeConfig{Profile: config.Profile, DatabaseURL: config.DatabaseURL, ChatEnabled: true, ChatDatabaseURL: config.ChatDatabaseURL, ChatCursorKey: "local-agent-demo-cleanup"}, func() time.Time { return now }, newCurrentWorkerChatFacts(roleAccess, core, mapper, nil), core, nil)
	if err != nil {
		return nil, fmt.Errorf("compose local chat service: %w", err)
	}
	defer chatRuntime.close()
	cleanup := AgentUXDemoGeneralCleanup{Chat: chatRuntime.service, Authors: localAgentDemoCleanupAuthors{now: func() time.Time { return now }, organization: organization}}
	return cleanup.Run(ctx, admin, now)
}
