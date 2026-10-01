package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaIdentityDirectoryFake struct {
	identity personaRegisteredChatIdentity
	err      error
	calls    int
}

func (f *personaIdentityDirectoryFake) LookupPersonaChatIdentity(_ context.Context, tenant, reference string) (personaRegisteredChatIdentity, error) {
	f.calls++
	if f.err != nil {
		return personaRegisteredChatIdentity{}, f.err
	}
	return f.identity, nil
}

func TestProductionPersonaReferenceLookup_BindsExactPublishedInstallation(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("migrate isolated persona store: %v", err)
	}
	tenant := values.TenantId("persona-reference-application")
	tenantUUID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, tenantUUID)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	root, err := agentpersonastore.NewWithPublicationAuthorities(conn, func(key values.TenantId) uuid.UUID {
		if key == tenant {
			return tenantUUID
		}
		return uuid.Nil
	}, personaReferencePublicationEvidence{}, personaReferencePublicationEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := root.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	version := agentpersonastore.PersonaVersion{
		TenantID: tenant, PersonaID: "persona.comp", Version: 3, AgentVersion: "agent-v7",
		Handle: "comp-analyst", DisplayName: "Comp Analyst", Profile: json.RawMessage(`{"tier":"T1","owner":"user:owner"}`),
		ContentDigest: "sha256:persona-v3", CreatedAt: created,
	}
	owner := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: version.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: "user:owner"}
	steward := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: version.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: "user:steward"}
	if err := store.CreateDraft(ctx, version, owner, steward, "user:creator", created); err != nil {
		t.Fatal(err)
	}
	appendLifecycle := func(eventID string, from, to agentpersonastore.LifecycleState) {
		t.Helper()
		event := agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: eventID, PersonaID: version.PersonaID, PersonaVersion: version.Version, From: from, To: to, Reason: "reviewed", ActorID: "user:reviewer", OccurredAt: created.Add(time.Hour)}
		var err error
		if to == agentpersonastore.StatePublished {
			err = store.Publish(ctx, event, agentpersonastore.PublicationEvidence{ReviewID: "reference-review", EvaluationRunID: "reference-evaluation"})
		} else {
			err = store.AppendLifecycle(ctx, event)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	appendLifecycle("persona-comp-review", agentpersonastore.StateDraft, agentpersonastore.StateInReview)
	appendLifecycle("persona-comp-publish", agentpersonastore.StateInReview, agentpersonastore.StatePublished)
	if err := store.Install(ctx, agentpersonastore.PersonaInstallation{TenantID: tenant, InstallationID: "install:comp", PersonaID: version.PersonaID, PersonaVersion: version.Version, ConversationID: "conversation:private", ConversationClass: agentpersonastore.ConversationPrivate, InstallerID: "user:manager", ChannelPolicy: agentpersonastore.ChannelPolicy{MaxTier: "T1", AllowedDataClasses: []string{"WORKFORCE"}, AlwaysPrivate: true, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}}, State: agentpersonastore.InstallationActive, Revision: 1, RevocationEpoch: 1, CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterPersonaChatIdentity(ctx, "agent:comp", version.PersonaID, created); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterPersonaChatIdentity(ctx, "agent:other", "persona.other", created); err != nil {
		t.Fatal(err)
	}
	identities, err := newProductionPersonaChatIdentityDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := newProductionPersonaReferenceLookup(identities, root)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := lookup.LookupPersonaReference(ctx, string(tenant), "conversation:private", "agent:comp")
	if err != nil {
		t.Fatal(err)
	}
	want := personaReferenceFacts{ReferenceID: "agent:comp", TenantID: string(tenant), ConversationID: "conversation:private", PersonaID: version.PersonaID, InstallationID: "install:comp", PersonaVersion: 3, CurrentVersion: 3, InstallationState: personaReferenceActive, PersonaLifecycle: personaReferencePublished}
	if facts != want {
		t.Fatalf("facts=%+v want %+v", facts, want)
	}
	listed, err := identities.ListPersonaChatIdentities(ctx, string(tenant))
	if err != nil || len(listed) != 2 || listed[0].ReferenceID != "agent:comp" || listed[1].ReferenceID != "agent:other" || !listed[0].Active || !listed[1].Active {
		t.Fatalf("listed identities=%+v error=%v", listed, err)
	}
	if _, err := lookup.LookupPersonaReference(ctx, string(tenant), "conversation:private", "agent:unknown"); !errors.Is(err, errPersonaReferenceNotPersona) {
		t.Fatalf("unknown canonical identity error=%v", err)
	}
	if err := store.RevokePersonaChatIdentity(ctx, "agent:comp", created.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	listed, err = identities.ListPersonaChatIdentities(ctx, string(tenant))
	if err != nil || len(listed) != 1 || listed[0].ReferenceID != "agent:other" {
		t.Fatalf("revoked identity remained in active listing: %+v error=%v", listed, err)
	}
	if _, err := lookup.LookupPersonaReference(ctx, string(tenant), "conversation:private", "agent:comp"); !errors.Is(err, errPersonaReferenceInactive) {
		t.Fatalf("revoked canonical identity error=%v", err)
	}
}

// The reference lookup starts from a reviewed, published persona. These test
// authorities supply evidence for that exact image through the publication
// API; production publication gates remain part of the fixture setup.
type personaReferencePublicationEvidence struct{}

func (personaReferencePublicationEvidence) ResolvePersonaReview(_ context.Context, _ dbport.Tx, tenant values.TenantId, persona string, version int64, digest, review string) (agentpersonastore.VerifiedReview, error) {
	return agentpersonastore.VerifiedReview{ReviewID: review, TenantID: string(tenant), PersonaID: persona, PersonaVersion: version, ProfileDigest: digest,
		ReviewerID: "user:reviewer", Permission: "persona:review", Decision: "APPROVE", ReviewDigest: "sha256:reference-review", GrantCurrent: true}, nil
}

func (personaReferencePublicationEvidence) ResolvePersonaEvaluation(_ context.Context, _ dbport.Tx, tenant values.TenantId, run, persona string, version int64, digest string) (agentpersonastore.VerifiedEvaluation, error) {
	return agentpersonastore.VerifiedEvaluation{RunID: run, TenantID: string(tenant), PersonaID: persona, PersonaVersion: version, ProfileDigest: digest,
		SuiteDigest: "sha256:reference-suite", RunDigest: "sha256:reference-evaluation", Passed: true, Fresh: true}, nil
}

func TestProductionPersonaChatIdentityDirectory_RequiresExactTenantAndReference(t *testing.T) {
	if _, err := newProductionPersonaChatIdentityDirectory(nil); err == nil {
		t.Fatal("nil stores accepted")
	}
	directory, err := newProductionPersonaChatIdentityDirectory(&personaStoreFactoryFake{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tenant, reference string
		ctx                     context.Context
	}{
		{name: "nil context", tenant: "tenant-a", reference: "agent:one"},
		{name: "foreign spacing", tenant: " tenant-a", reference: "agent:one", ctx: context.Background()},
		{name: "malformed reference", tenant: "tenant-a", reference: "../agent", ctx: context.Background()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := directory.LookupPersonaChatIdentity(tc.ctx, tc.tenant, tc.reference); !errors.Is(err, errPersonaReferenceInvalid) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

type personaStoreFactoryFake struct{ calls int }

func (f *personaStoreFactoryFake) Scoped(_ values.TenantId) (*agentpersonastore.TenantStore, error) {
	f.calls++
	return nil, errors.New("unexpected store access")
}

func (f *personaStoreFactoryFake) ListPersonaChatIdentities(context.Context, string) ([]agentpersonastore.PersonaChatIdentity, error) {
	return nil, errors.New("unexpected identity listing")
}

func TestProductionPersonaReferenceLookup_RequiresCanonicalRegisteredIdentity(t *testing.T) {
	generic := errors.New("application: chat agent reference is not a persona")
	identities := &personaIdentityDirectoryFake{err: generic}
	stores := &personaStoreFactoryFake{}
	lookup, err := newProductionPersonaReferenceLookup(identities, stores)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lookup.LookupPersonaReference(context.Background(), "tenant-a", "channel-a", "chatapp-install"); !errors.Is(err, generic) {
		t.Fatalf("generic app result error=%v", err)
	}
	if stores.calls != 0 {
		t.Fatalf("generic app reached persona store %d times", stores.calls)
	}
}

func TestProductionPersonaReferenceLookup_FailsClosedOnForeignOrInactiveIdentity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity personaRegisteredChatIdentity
		want     error
	}{
		{name: "foreign tenant", identity: personaRegisteredChatIdentity{TenantID: "tenant-b", ReferenceID: "agent:one", PersonaID: "persona.one", Active: true}, want: errPersonaReferenceInvalid},
		{name: "wrong reference", identity: personaRegisteredChatIdentity{TenantID: "tenant-a", ReferenceID: "agent:two", PersonaID: "persona.one", Active: true}, want: errPersonaReferenceInvalid},
		{name: "inactive", identity: personaRegisteredChatIdentity{TenantID: "tenant-a", ReferenceID: "agent:one", PersonaID: "persona.one"}, want: errPersonaReferenceInactive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identities := &personaIdentityDirectoryFake{identity: tc.identity}
			stores := &personaStoreFactoryFake{}
			lookup, _ := newProductionPersonaReferenceLookup(identities, stores)
			if _, err := lookup.LookupPersonaReference(context.Background(), "tenant-a", "channel-a", "agent:one"); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
			if stores.calls != 0 {
				t.Fatalf("invalid identity reached store %d times", stores.calls)
			}
		})
	}
}

func TestProductionPersonaReferenceLookup_ValidatesInputsAndDependencies(t *testing.T) {
	if _, err := newProductionPersonaReferenceLookup(nil, &personaStoreFactoryFake{}); err == nil {
		t.Fatal("nil identity directory accepted")
	}
	lookup, _ := newProductionPersonaReferenceLookup(&personaIdentityDirectoryFake{}, &personaStoreFactoryFake{})
	for _, tc := range []struct {
		name, tenant, conversation, reference string
		ctx                                   context.Context
	}{
		{name: "nil context", tenant: "tenant-a", conversation: "channel-a", reference: "agent:one", ctx: nil},
		{name: "bad reference", tenant: "tenant-a", conversation: "channel-a", reference: "../app", ctx: context.Background()},
		{name: "missing tenant", conversation: "channel-a", reference: "agent:one", ctx: context.Background()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := lookup.LookupPersonaReference(tc.ctx, tc.tenant, tc.conversation, tc.reference); !errors.Is(err, errPersonaReferenceInvalid) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
