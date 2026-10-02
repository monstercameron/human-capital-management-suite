package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/agentcost"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentconnectionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentconsolestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcoststore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// AgentAccessOptions are the parts of the agent access pages that depend on a
// provider the deployment has to supply. Each is optional; a missing one makes
// that action answer "unavailable" on the page rather than pretend.
type AgentAccessOptions struct {
	// Providers are the authorization endpoints administrators published for
	// user-delegated connections. With none, no connection can be linked.
	Providers agentaccess.Providers
	// Exchange trades an authorization code and PKCE verifier for the binding of
	// the user's own grant, through the custody service. With none, a callback is
	// refused after its state has been checked and spent.
	Exchange agentaccess.Exchange
	// Snapshots reads an MCP tool snapshot already captured for a connection.
	Snapshots agentaccess.MCPSnapshots
	// Connections are the connectivity connections the console may publish
	// revisions of. The cell's own connection is always included.
	Connections []*connectivity.ConnectorConnection
}

var errAgentAccessNoExchange = errors.New("application: no provider token exchange is composed")

type agentAccessNoProviders struct{}

func (agentAccessNoProviders) Provider(string, string) (agentaccess.Provider, bool) {
	return agentaccess.Provider{}, false
}

// agentAccessRefusingIssuer is the lease service of a cell that has no custody
// authority for external systems: it mints nothing, so no agent can call a
// connection until one is composed.
type agentAccessRefusingIssuer struct{}

func (agentAccessRefusingIssuer) Mint(lease.Request) (lease.CredentialLease, lease.Evidence, error) {
	return lease.CredentialLease{}, lease.Evidence{}, agentconnect.ErrCredential
}
func (agentAccessRefusingIssuer) Use(lease.CredentialLease, string, custody.Operation) (lease.Evidence, error) {
	return lease.Evidence{}, agentconnect.ErrCredential
}
func (agentAccessRefusingIssuer) Revoke(string, string) (lease.Evidence, error) {
	return lease.Evidence{}, agentconnect.ErrCredential
}

// agentAccessRegistry is the connection registry the pages and the console
// share. A tenant is loaded the first time it is asked about: the registry's
// own persisted state (the first revision and every user's link) is restored,
// and then the console's published revisions are replayed over it, so what was
// published last is what is live after a restart.
type agentAccessRegistry struct {
	*agentconnect.Registry
	connections []*connectivity.ConnectorConnection
	// revisions is where the console's published revisions are read back from. It
	// is the store, not the console: the console calls Publish while it holds its
	// own lock.
	revisions agentaccess.RevisionStore

	mu    sync.Mutex
	ready map[string]bool
}

func (r *agentAccessRegistry) connection(tenant, id string) (*connectivity.ConnectorConnection, error) {
	for _, connection := range r.connections {
		if connection != nil && connection.TenantID() == tenant && connection.ID() == id {
			return connection, nil
		}
	}
	return nil, fmt.Errorf("%w: connection %q is not a connection of this workspace", agentaccess.ErrInvalid, id)
}

func (r *agentAccessRegistry) ensure(tenant string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ready[tenant] {
		return nil
	}
	if err := r.Registry.Restore(tenant, r.connection); err != nil {
		return err
	}
	if r.revisions != nil {
		stored, err := r.revisions.LoadRevisions(tenant)
		if err != nil {
			return err
		}
		sort.Slice(stored, func(i, j int) bool { return stored[i].Number < stored[j].Number })
		for _, revision := range stored {
			if revision.Status != agentaccess.StatusPublished {
				continue
			}
			if err := r.apply(revision); err != nil {
				return err
			}
		}
	}
	r.ready[tenant] = true
	return nil
}

// Ready loads the tenant and says whether it could be loaded, so a page can say
// "unavailable" instead of drawing an empty list.
func (r *agentAccessRegistry) Ready(tenant string) error { return r.ensure(tenant) }

// apply makes one console revision the live one: the first revision of a
// connection is registered, a later one replaces it.
func (r *agentAccessRegistry) apply(revision agentaccess.Revision) error {
	if revision.CredentialMode == agentconnect.Brokered {
		return fmt.Errorf("%w: a brokered credential needs a custody binding this console does not collect yet", agentaccess.ErrInvalid)
	}
	connection, err := r.connection(revision.TenantID, revision.ConnectionID)
	if err != nil {
		return err
	}
	live := agentconnect.ConnectionRevision{
		ID: revision.ConnectionID, TenantID: revision.TenantID, Revision: revision.Number,
		Endpoint: "connector://" + connection.ConnectorID() + "/" + revision.ConnectionID, Connection: connection, CredentialMode: revision.CredentialMode,
		Grants: revision.Grants,
	}
	for _, skill := range revision.Skills {
		live.Skills = append(live.Skills, agentconnect.SkillExposure{
			ID: skill.ID, Version: "1", Tier: skill.Tier, CredentialOperation: custody.LeaseOperation, SharedRead: skill.SharedRead, RecordFilter: skill.RecordFilter,
			Tool: agentsecurity.ToolDescriptor{Name: skill.ID, Capability: skill.Capability, Version: 1, Class: agentAccessToolClass(skill.Tier), Cost: 1, Schema: "{}"},
		})
	}
	existing, err := r.Registry.Revision(revision.TenantID, revision.ConnectionID)
	switch {
	case errors.Is(err, agentconnect.ErrNotFound):
		return r.Registry.Register(live)
	case err != nil:
		return err
	case revision.Number > existing.Revision:
		return r.Registry.Replace(live)
	}
	return nil
}

func agentAccessToolClass(tier agentconnect.SideEffectTier) agentsecurity.ToolClass {
	switch tier {
	case agentconnect.TierT0:
		return agentsecurity.ToolRead
	case agentconnect.TierT1:
		return agentsecurity.ToolDraft
	case agentconnect.TierT2:
		return agentsecurity.ToolSend
	case agentconnect.TierT3:
		return agentsecurity.ToolWrite
	}
	return agentsecurity.ToolExecute
}

// Publish implements agentaccess.Publisher.
func (r *agentAccessRegistry) Publish(revision agentaccess.Revision) error {
	if err := r.ensure(revision.TenantID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.apply(revision)
}

func (r *agentAccessRegistry) ConnectionIDs(tenant string) []string {
	if r.ensure(tenant) != nil {
		return nil
	}
	return r.Registry.ConnectionIDs(tenant)
}

func (r *agentAccessRegistry) Revision(tenant, connectionID string) (agentconnect.ConnectionRevision, error) {
	if err := r.ensure(tenant); err != nil {
		return agentconnect.ConnectionRevision{}, err
	}
	return r.Registry.Revision(tenant, connectionID)
}

func (r *agentAccessRegistry) GrantedSkills(user agentconnect.UserContext, connectionID string) ([]agentconnect.SkillExposure, error) {
	if err := r.ensure(user.TenantID); err != nil {
		return nil, err
	}
	return r.Registry.GrantedSkills(user, connectionID)
}

func (r *agentAccessRegistry) LinkedAccount(user agentconnect.UserContext, connectionID string) (agentconnect.AccountLink, error) {
	if err := r.ensure(user.TenantID); err != nil {
		return agentconnect.AccountLink{}, err
	}
	return r.Registry.LinkedAccount(user, connectionID)
}

func (r *agentAccessRegistry) UnlinkAccount(user agentconnect.UserContext, connectionID, reason string) error {
	if err := r.ensure(user.TenantID); err != nil {
		return err
	}
	return r.Registry.UnlinkAccount(user, connectionID, reason)
}

func (r *agentAccessRegistry) LinkAccount(user agentconnect.UserContext, connectionID string, binding agentconnect.CredentialBinding, externalAccountID string) error {
	if err := r.ensure(user.TenantID); err != nil {
		return err
	}
	return r.Registry.LinkAccount(user, connectionID, binding, externalAccountID)
}

// agentAccessDelegations lists and revokes a user's run-bound grants over the
// durable grant store.
type agentAccessDelegations struct {
	store *agentdelegationstore.Store
}

func (d agentAccessDelegations) ActiveGrants(tenant, userID string, at time.Time) ([]agentaccess.DelegationGrant, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	grants, err := d.store.ActiveUserGrants(ctx, values.TenantId(tenant), userID, at)
	if err != nil {
		return nil, err
	}
	out := make([]agentaccess.DelegationGrant, 0, len(grants))
	for _, grant := range grants {
		out = append(out, agentaccess.DelegationGrant{ID: grant.GrantID, TaskID: grant.TaskID, TaskLabel: grant.Purpose, Scope: strings.Join(grant.Skills, ", "), ExpiresAt: grant.ExpiresAt})
	}
	return out, nil
}

func (d agentAccessDelegations) RevokeTaskGrant(tenant, userID, taskID, reason string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := d.store.RevokeUserTaskGrants(ctx, values.TenantId(tenant), userID, taskID, reason)
	return err
}

// agentAccessServices is what the served assembly holds from this composition.
type agentAccessServices struct {
	HTTP *AgentAccessHTTP
	Gate *PersonaRunCostGate
}

// composeAgentAccess builds the agent access, connection console and cost
// services over the agent database and the core database. It needs the agent
// database; without it the routes are not served.
func composeAgentAccess(in agentServedAssemblyInput, logger agentLogger) (*agentAccessServices, error) {
	if in.AgentDatabase.store == nil || in.Core == nil || in.TenantUUID == nil || in.Now == nil {
		return nil, nil
	}
	mapper := func(key string) uuid.UUID { return in.TenantUUID(values.TenantId(key)) }
	costStore, err := agentcoststore.New(in.AgentDatabase.store, mapper)
	if err != nil {
		return nil, fmt.Errorf("agent cost store: %w", err)
	}
	consoleStore, err := agentconsolestore.New(in.AgentDatabase.store, mapper)
	if err != nil {
		return nil, fmt.Errorf("agent console store: %w", err)
	}
	connectionStore, err := agentconnectionstore.New(in.Core, in.TenantUUID)
	if err != nil {
		return nil, fmt.Errorf("agent connection store: %w", err)
	}
	grantStore, err := agentdelegationstore.New(in.Core, in.TenantUUID)
	if err != nil {
		return nil, fmt.Errorf("agent grant store: %w", err)
	}
	admins, notices := newAgentRequestAdmins(), newAgentCostNotices()
	owners := agentAccessAgentOwners{store: costStore, admins: admins}
	// A day is midnight to midnight UTC until a tenant carries a time zone of its
	// own; "It resets at 00:00." is then true for everyone.
	gate, err := agentcost.NewGate(owners, notices, time.UTC, in.Now)
	if err != nil {
		return nil, err
	}
	gate.WithStore(costStore)
	meter := agentcost.Meter{Gate: gate, Ledger: (&agentcost.Ledger{}).WithStore(costStore)}

	options := in.AgentAccess
	connections := append([]*connectivity.ConnectorConnection(nil), options.Connections...)
	if in.Cell != nil && in.Cell.Connection != nil {
		connections = append(connections, in.Cell.Connection)
	}
	registry, err := agentconnect.NewPersistentRegistry(agentAccessRefusingIssuer{}, in.Now, connectionStore)
	if err != nil {
		return nil, fmt.Errorf("agent connection registry: %w", err)
	}
	access := &agentAccessRegistry{Registry: registry, connections: connections, ready: map[string]bool{}}
	console, err := agentaccess.NewConsole(admins, access, options.Snapshots, in.Now)
	if err != nil {
		return nil, err
	}
	console.WithStore(consoleStore)
	access.revisions = consoleStore

	providers := options.Providers
	if providers == nil {
		providers = agentAccessNoProviders{}
	}
	exchange := options.Exchange
	if exchange == nil {
		exchange = func(context.Context, agentaccess.Provider, string, string) (agentconnect.CredentialBinding, string, error) {
			return agentconnect.CredentialBinding{}, "", errAgentAccessNoExchange
		}
	}
	linker, err := agentaccess.NewLinker(providers, access, exchange, in.Now, nil)
	if err != nil {
		return nil, err
	}
	users, err := agentaccess.NewUserAccess(access, linker, agentAccessDelegations{store: grantStore}, in.Now)
	if err != nil {
		return nil, err
	}
	var extender AgentBudgetExtender
	if in.Cell != nil {
		extender, _ = in.Cell.AgentController.(AgentBudgetExtender)
	}
	return &agentAccessServices{
		HTTP: &AgentAccessHTTP{Users: users, Console: console, Roles: in.Cell.RoleAccess, Admins: admins, Meter: meter, Owners: owners, Notices: notices, Zone: time.UTC, Extender: extender, Now: in.Now},
		// A model call is expected to cost a cent or two; the figure only decides
		// whether a call that would pass a money limit is made at all.
		Gate: &PersonaRunCostGate{Meter: meter, Owners: costStore, EstimateMicros: 10_000, Now: in.Now, Logger: logger},
	}, nil
}
