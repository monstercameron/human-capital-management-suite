package agentpersonastore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_018_DurableCatalogFacts(t *testing.T) {
	f := newFixture(t, "catalog-owner", "catalog-other")
	owner := f.store(t, "catalog-owner")
	other := f.store(t, "catalog-other")
	persona := createCatalogPersona(t, owner, "catalog-owner", "persona-admin", StatePublished)
	installation := PersonaInstallation{
		TenantID: persona.TenantID, InstallationID: "placement-a", PersonaID: persona.PersonaID,
		PersonaVersion: persona.Version, ConversationID: "conversation-a", ConversationClass: ConversationPrivate, InstallerID: "user:installer",
		ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1, CreatedAt: f.when, UpdatedAt: f.when,
	}
	if err := owner.Install(context.Background(), installation); err != nil {
		t.Fatal(err)
	}

	got, err := owner.ListCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("catalog entries = %d, want one: %+v", len(got), got)
	}
	entry := got[0]
	if entry.Version.PersonaID != persona.PersonaID || entry.Version.Version != persona.Version || entry.Lifecycle != StatePublished {
		t.Fatalf("catalog version facts = %+v", entry)
	}
	if entry.PublicationProof != PublicationEvidencePinned {
		t.Fatalf("catalog publication proof = %q, want PINNED", entry.PublicationProof)
	}
	if entry.BusinessOwner != "user:business-owner" || entry.Steward != "user:technical-steward" {
		t.Fatalf("catalog ownership = %q/%q", entry.BusinessOwner, entry.Steward)
	}
	wantInstallation := CatalogInstallation{ID: "placement-a", PersonaID: persona.PersonaID, PersonaVersion: persona.Version, ConversationID: "conversation-a", ConversationClass: ConversationPrivate, ChannelPolicy: testChannelPolicy(), Revision: 1, RevocationEpoch: 1, State: string(InstallationActive)}
	if len(entry.Installations) != 1 || !reflect.DeepEqual(entry.Installations[0], wantInstallation) {
		t.Fatalf("catalog placements = %+v", entry.Installations)
	}
	foreign, err := other.ListCatalog(context.Background())
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign tenant catalog = %+v, %v; want empty", foreign, err)
	}
}

func TestTodo_AGENTP_018_LegacyPublicationIsMarkedUnverified(t *testing.T) {
	f := newFixture(t, "catalog-legacy")
	store := f.store(t, "catalog-legacy")
	persona := version(values.TenantId("catalog-legacy"), "persona-legacy", 1)
	owner, steward := draftOwners(persona)
	if err := store.CreateDraft(context.Background(), persona, owner, steward, "user:creator", f.when); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLifecycle(context.Background(), lifecycle(persona.TenantID, persona.PersonaID, persona.Version, "legacy-review", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	// Recreate a row imported from before 00011, when publication proof columns
	// did not exist and therefore defaulted to empty.
	f.db.Exec(t, `ALTER TABLE persona_lifecycle_events DROP CONSTRAINT persona_publication_requires_evidence`)
	f.db.Exec(t, `INSERT INTO persona_lifecycle_events (tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at) VALUES ($1,'legacy-publish',$2,$3,'IN_REVIEW','PUBLISHED','legacy import','legacy-actor',now())`, f.ids[persona.TenantID], persona.PersonaID, persona.Version)
	f.db.Exec(t, `ALTER TABLE persona_lifecycle_events ADD CONSTRAINT persona_publication_requires_evidence CHECK (to_state <> 'PUBLISHED' OR (btrim(profile_digest) <> '' AND btrim(review_digest) <> '' AND btrim(reviewer_id) <> '' AND reviewer_id = actor_id AND btrim(evaluation_digest) <> '' AND evaluation_profile_digest = profile_digest AND btrim(evaluation_suite_digest) <> '')) NOT VALID`)
	f.db.Exec(t, `INSERT INTO persona_installations (tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES ($1,'legacy-install',$2,$3,'legacy-room','PRIVATE','user:installer','{"max_tier":"T1","allowed_data_classes":["WORKFORCE"],"always_private":true,"conversation_search_allowed":false,"allowed_channel_classes":["PRIVATE"],"allow_external_members":false,"allow_cross_company_members":false}','ACTIVE',1,1,now(),now())`, f.ids[persona.TenantID], persona.PersonaID, persona.Version)
	entries, err := store.ListCatalog(context.Background())
	if err != nil || len(entries) != 1 || entries[0].Lifecycle != StatePublished || entries[0].PublicationProof != PublicationEvidenceMissing {
		t.Fatalf("legacy catalog = %+v, %v; want PUBLISHED with MISSING evidence", entries, err)
	}
	if published, err := store.ListPublished(context.Background()); err != nil || len(published) != 0 {
		t.Fatalf("legacy ListPublished = %+v, %v; want empty", published, err)
	}
	if active, err := store.ListActiveInstallations(context.Background(), "legacy-room"); err != nil || len(active) != 0 {
		t.Fatalf("legacy active installations = %+v, %v; want empty", active, err)
	}
}

func TestTodo_AGENTP_018_DurableCatalogRejectsInvalidInstallationPolicy(t *testing.T) {
	f := newFixture(t, "catalog-invalid-policy")
	store := f.store(t, "catalog-invalid-policy")
	persona := createCatalogPersona(t, store, "catalog-invalid-policy", "persona-invalid-policy", StatePublished)
	f.db.Exec(t, `INSERT INTO persona_installations
		(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at)
		VALUES ($1,'placement-invalid',$2,$3,'conversation-invalid','PRIVATE','user:installer',
		'{"max_tier":"T2","allowed_data_classes":["WORKFORCE"],"always_private":false,"conversation_search_allowed":true,"allowed_channel_classes":["NOT_A_CLASS"],"allow_external_members":false,"allow_cross_company_members":false}',
		'ACTIVE',1,1,now(),now())`, f.ids[persona.TenantID], persona.PersonaID, persona.Version)
	if got, err := store.ListCatalog(context.Background()); !errors.Is(err, ErrInvalid) || got != nil {
		t.Fatalf("ListCatalog() = %+v, %v; want fail-closed ErrInvalid", got, err)
	}
}

func TestTodo_AGENTP_018_DurableCatalogRejectsMissingOwner(t *testing.T) {
	f := newFixture(t, "catalog-missing-owner")
	store := f.store(t, "catalog-missing-owner")
	persona := version(values.TenantId("catalog-missing-owner"), "persona-incomplete", 1)
	if err := store.PutVersion(context.Background(), persona); err != nil {
		t.Fatal(err)
	}
	if err := appendLifecycleForTest(t, store, context.Background(), lifecycle(persona.TenantID, persona.PersonaID, persona.Version, "incomplete-draft", "", StateDraft)); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ListCatalog(context.Background()); !errors.Is(err, ErrInvalid) || got != nil {
		t.Fatalf("ListCatalog() = %+v, %v; want fail-closed ErrInvalid", got, err)
	}
}

func TestTodo_AGENTP_018_DurableCatalogPreservesEveryExactVersion(t *testing.T) {
	f := newFixture(t, "catalog-ambiguous")
	store := f.store(t, "catalog-ambiguous")
	first := createCatalogPersona(t, store, "catalog-ambiguous", "persona-ambiguous", StatePublished)
	second := version(first.TenantID, first.PersonaID, 2)
	second.CreatedAt = time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	if err := store.PutVersion(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if err := appendLifecycleForTest(t, store, context.Background(), lifecycle(second.TenantID, second.PersonaID, second.Version, "ambiguous-draft", "", StateDraft)); err != nil {
		t.Fatal(err)
	}
	if err := store.Install(context.Background(), PersonaInstallation{TenantID: first.TenantID, InstallationID: "published-v1-installation", PersonaID: first.PersonaID, PersonaVersion: 1, ConversationID: "room-v1", ConversationClass: ConversationPrivate, InstallerID: "manager", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1, CreatedAt: f.when, UpdatedAt: f.when}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListCatalog(context.Background())
	if err != nil || len(got) != 2 || got[0].Version.Version != 1 || got[0].Lifecycle != StatePublished || len(got[0].Installations) != 1 || got[0].Installations[0].PersonaVersion != 1 || got[1].Version.Version != 2 || got[1].Lifecycle != StateDraft || len(got[1].Installations) != 0 {
		t.Fatalf("ListCatalog() = %+v, %v; want published v1 and draft v2 with exact placements", got, err)
	}
}
