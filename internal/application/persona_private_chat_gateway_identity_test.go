package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

func TestTodo_AGENTP_008_PrivateChatGatewayIdentityUsesDurableGrantAndVerifiedWorker(t *testing.T) {
	record, run, now := privateChatGatewayIdentityFixture(t)
	grant := privateChatGatewayGrant(record, run, now)
	installation := privateChatGatewayInstallation(record)
	worker := privateChatGatewayVerifiedWorker(t, now)
	resolver := privateChatGatewayResolverFixture(grant, installation, worker, now)

	identity, chain, err := resolver.ResolvePrivateChatGatewayIdentity(t.Context(), record, run)
	if err != nil {
		t.Fatalf("resolve gateway identity: %v", err)
	}
	if identity.Identity == "" || identity.AgentID != record.Request.Agent.AgentID || identity.Tenant != record.Request.Source.TenantID ||
		identity.Purpose != record.Request.Purpose || !samePersonaReplyStrings(identity.ToolSet, []string{"persona.chat_reply"}) ||
		!samePersonaReplyStrings(identity.DataScope, []string{"chat.current"}) || identity.Budget != 1 {
		t.Fatalf("identity was not narrowed to verified private reply: %+v", identity)
	}
	if len(chain) != 1 || chain[0].GrantID != grant.GrantID || chain[0].Delegator != grant.UserID || chain[0].Delegate != grant.TargetAgentID ||
		chain[0].Tenant != grant.Tenant.String() || chain[0].Purpose != grant.Purpose || !samePersonaReplyStrings(chain[0].ToolSet, []string{"persona.chat_reply"}) ||
		!samePersonaReplyStrings(chain[0].DataScope, []string{"chat.current"}) || chain[0].Budget != 1 {
		t.Fatalf("delegation did not preserve the stored grant identity and narrow scope: %+v", chain)
	}
}

func TestTodo_AGENTP_008_PrivateChatGatewayIdentityRejectsGrantInstallationAndWorkerDrift(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		changeGrant   func(*agentdelegation.Grant)
		changeInstall func(*agentpersonastore.PersonaInstallation)
		changeRun     func(*runstate.Run)
		changeWorker  bool
		workerRole    workload.ProcessRole
		currentEpoch  uint64
	}{
		{name: "missing exact target", changeGrant: func(g *agentdelegation.Grant) { g.TargetAgentID = "" }},
		{name: "wrong target agent", changeGrant: func(g *agentdelegation.Grant) { g.TargetAgentID = "agent:other"; g.Authority.Delegate = "agent:other" }},
		{name: "wrong installation", changeGrant: func(g *agentdelegation.Grant) { g.InstallationID = "install:other" }},
		{name: "wrong run task", changeGrant: func(g *agentdelegation.Grant) { g.TaskID = "run:other" }},
		{name: "revoked durable grant", changeGrant: func(g *agentdelegation.Grant) { g.Revoked = true }},
		{name: "fake capability omitted from durable grant", changeGrant: func(g *agentdelegation.Grant) { g.SkillScopes["persona.chat_reply"] = []string{"chat.current"} }},
		{name: "durable scope widened", changeGrant: func(g *agentdelegation.Grant) {
			g.SkillScopes["persona.chat_reply"] = []string{"persona.reply", "people.read"}
		}},
		{name: "durable chat resource absent", changeGrant: func(g *agentdelegation.Grant) {
			auth := g.SkillAuthorities["persona.chat_reply"]
			auth.Resources = nil
			g.SkillAuthorities["persona.chat_reply"] = auth
			g.Authority.SkillAuthorities["persona.chat_reply"] = auth
		}},
		{name: "installation suspended", changeInstall: func(i *agentpersonastore.PersonaInstallation) { i.State = agentpersonastore.InstallationSuspended }},
		{name: "installation moved to other conversation", changeInstall: func(i *agentpersonastore.PersonaInstallation) { i.ConversationID = "conversation:other" }},
		{name: "public installation", changeInstall: func(i *agentpersonastore.PersonaInstallation) {
			i.ConversationClass = agentpersonastore.ConversationPublic
		}},
		{name: "current user epoch advanced", currentEpoch: 2},
		{name: "worker identity missing", changeWorker: true},
		{name: "non-worker process identity", workerRole: workload.RoleHCMNext},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record, run, fixtureNow := privateChatGatewayIdentityFixture(t)
			grant := privateChatGatewayGrant(record, run, fixtureNow)
			installation := privateChatGatewayInstallation(record)
			worker := privateChatGatewayVerifiedWorker(t, fixtureNow)
			if tc.workerRole != "" {
				worker = privateChatGatewayVerifiedWorkerWithRole(t, fixtureNow, tc.workerRole)
			}
			if tc.changeGrant != nil {
				tc.changeGrant(&grant)
			}
			if tc.changeInstall != nil {
				tc.changeInstall(&installation)
			}
			if tc.changeRun != nil {
				tc.changeRun(&run)
			}
			if tc.changeWorker {
				worker.Credentials = privateChatGatewayCredentialFake{}
			}
			epoch := grant.RevocationEpoch
			if tc.currentEpoch != 0 {
				epoch = tc.currentEpoch
			}
			resolver := privateChatGatewayResolverFixture(grant, installation, worker, now)
			resolver.Grants = privateChatGatewayGrantStoreFactoryFake{grant: grant, epoch: epoch}
			if identity, chain, err := resolver.ResolvePrivateChatGatewayIdentity(t.Context(), record, run); err == nil || identity.Identity != "" || len(chain) != 0 {
				t.Fatalf("drift granted identity=%+v chain=%+v err=%v", identity, chain, err)
			}
		})
	}
}

func TestTodo_AGENTP_008_PrivateChatGatewayIdentityRequiresProductionDependencies(t *testing.T) {
	if _, err := NewDatabasePersonaPrivateChatGatewayIdentityResolver(nil, nil, nil, nil, nil); !errors.Is(err, errPersonaPrivateChatGatewayIdentity) {
		t.Fatalf("missing production dependencies error=%v", err)
	}
}

func privateChatGatewayIdentityFixture(t *testing.T) (agentrun.Record, runstate.Run, time.Time) {
	t.Helper()
	record, run, _ := personaPrivateChatBindingFixture(t)
	record.Request.Purpose = "persona-mention"
	record.Request.Agent.AgentID = "agent:comp-analyst"
	record.Request.InstallationID = "install:private"
	record.Request.Principal.DelegatedCredentialRef = "grant:private"
	record.Request.Persona.Version = "v2"
	record.Request.Audience.ID = "conversation:private"
	record.Request.Context.ID = "thread:private"
	record.Request.Source.Ref = "post:private"
	// Recompute durable admission identifiers after changing the fixture tuple.
	id, err := agentrun.AdmissionRequestID(record.Request.Source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentrun.AdmissionRequestDigest(record.Request)
	if err != nil {
		t.Fatal(err)
	}
	record.ID, record.RequestDigest = id, digest
	run.ID, run.AdmissionID, run.RequestDigest = id, id, digest
	run.AgentID, run.AgentVersion, run.AgentDigest = record.Request.Agent.AgentID, record.Request.Agent.Version, record.Request.Agent.Digest
	run.TenantID, run.ActorID = record.Request.Source.TenantID, record.Request.Principal.InvokerID
	return record, run, time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
}

func privateChatGatewayGrant(record agentrun.Record, run runstate.Run, now time.Time) agentdelegation.Grant {
	authority := trust.SkillAuthority{Capabilities: []string{"persona.reply"}, Resources: []string{"chat.current"}, Purposes: []string{record.Request.Purpose}}
	return agentdelegation.Grant{
		GrantID: record.Request.Principal.DelegatedCredentialRef, UserID: record.Request.Principal.InvokerID,
		Tenant: values.TenantId(record.Request.Source.TenantID), AgentVersion: record.Request.Persona.Version,
		TargetAgentID: record.Request.Agent.AgentID, InstallationID: record.Request.InstallationID, TaskID: record.Request.Source.Key,
		Purpose: record.Request.Purpose, Skills: []string{"persona.chat_reply"}, SkillScopes: map[string][]string{"persona.chat_reply": {"persona.reply"}},
		SkillAuthorities: trust.SkillAuthorities{"persona.chat_reply": authority},
		NotBefore:        now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), RevocationEpoch: 1,
		Authority: trust.DelegationGrant{GrantID: record.Request.Principal.DelegatedCredentialRef, RootID: record.Request.Principal.DelegatedCredentialRef,
			Delegator: record.Request.Principal.InvokerID, Delegate: record.Request.Agent.AgentID, Tenant: values.TenantId(record.Request.Source.TenantID),
			Purposes: []string{record.Request.Purpose}, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), RevocationEpoch: 1,
			SkillAuthorities: trust.SkillAuthorities{"persona.chat_reply": authority}},
	}
}

func privateChatGatewayInstallation(record agentrun.Record) agentpersonastore.PersonaInstallation {
	return agentpersonastore.PersonaInstallation{
		TenantID: values.TenantId(record.Request.Source.TenantID), InstallationID: record.Request.InstallationID,
		PersonaID: record.Request.Persona.ID, PersonaVersion: 2, ConversationID: record.Request.Audience.ID,
		ConversationClass: agentpersonastore.ConversationPrivate, State: agentpersonastore.InstallationActive, Revision: 4, RevocationEpoch: 3,
	}
}

func privateChatGatewayVerifiedWorker(t *testing.T, now time.Time) *VerifiedPersonaPrivateChatWorkloadIdentitySource {
	return privateChatGatewayVerifiedWorkerWithRole(t, now, workload.RoleWorker)
}

func privateChatGatewayVerifiedWorkerWithRole(t *testing.T, now time.Time, role workload.ProcessRole) *VerifiedPersonaPrivateChatWorkloadIdentitySource {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := workload.NewIssuer(workload.IssuerConfig{Name: "hcm-tests", KeyID: "worker-key-1", Private: private, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := issuer.Issue(workload.IssueSpec{Subject: "agent-worker/replica-1", Role: role, Cell: "cell-a", Lifetime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	keys := workload.NewStaticKeySource().WithKey("hcm-tests", "worker-key-1", public)
	verifier, err := workload.NewVerifier(workload.VerifierConfig{Keys: keys, Cell: "cell-a", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return &VerifiedPersonaPrivateChatWorkloadIdentitySource{
		Verifier: verifier, Credentials: privateChatGatewayCredentialFake{credential: token},
	}
}

type privateChatGatewayCredentialFake struct{ credential string }

func (f privateChatGatewayCredentialFake) PersonaChatWorkerCredential(context.Context) (string, error) {
	return f.credential, nil
}

type privateChatGatewayGrantStoreFake struct {
	grant agentdelegation.Grant
	epoch uint64
}

func (f privateChatGatewayGrantStoreFake) Save(agentdelegation.Grant) error {
	return errors.New("unused")
}
func (f privateChatGatewayGrantStoreFake) Get(id string) (agentdelegation.Grant, error) {
	if id != f.grant.GrantID {
		return agentdelegation.Grant{}, agentdelegation.ErrGrantNotFound
	}
	return f.grant, nil
}
func (f privateChatGatewayGrantStoreFake) Revoke(string, string) error { return errors.New("unused") }
func (f privateChatGatewayGrantStoreFake) CurrentRevocationEpoch(values.TenantId, string) uint64 {
	return f.epoch
}
func (f privateChatGatewayGrantStoreFake) BumpRevocationEpoch(values.TenantId, string, string) (uint64, error) {
	return 0, errors.New("unused")
}

type privateChatGatewayGrantStoreFactoryFake struct {
	grant agentdelegation.Grant
	epoch uint64
}

func (f privateChatGatewayGrantStoreFactoryFake) ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error) {
	return privateChatGatewayGrantStoreFake{grant: f.grant, epoch: f.epoch}, nil
}

type privateChatGatewayInstallationReaderFake struct {
	installation agentpersonastore.PersonaInstallation
}

func (f privateChatGatewayInstallationReaderFake) GetInstallation(context.Context, string) (agentpersonastore.PersonaInstallation, error) {
	return f.installation, nil
}

type privateChatGatewayInstallationFactoryFake struct {
	installation agentpersonastore.PersonaInstallation
}

func (f privateChatGatewayInstallationFactoryFake) ForTenant(context.Context, values.TenantId) (PersonaPrivateChatInstallationReader, error) {
	return privateChatGatewayInstallationReaderFake{installation: f.installation}, nil
}

func privateChatGatewayResolverFixture(grant agentdelegation.Grant, installation agentpersonastore.PersonaInstallation, worker PersonaPrivateChatWorkloadIdentitySource, now time.Time) *DatabasePersonaPrivateChatGatewayIdentityResolver {
	return &DatabasePersonaPrivateChatGatewayIdentityResolver{
		Grants:        privateChatGatewayGrantStoreFactoryFake{grant: grant, epoch: grant.RevocationEpoch},
		Installations: privateChatGatewayInstallationFactoryFake{installation: installation}, Workload: worker,
		Now: func() time.Time { return now },
	}
}

var _ PersonaPrivateChatWorkloadCredentialSource = privateChatGatewayCredentialFake{}
var _ agentdelegation.GrantStore = privateChatGatewayGrantStoreFake{}
