package agentpersonastore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_007_InstallationQuery(t *testing.T) {
	f := newFixture(t, "viewer-tenant")
	s := f.store(t, "viewer-tenant")
	v := version("viewer-tenant", "persona-viewer", 7)
	v.AgentVersion = "agent-v7.3"
	v.Profile = json.RawMessage(`{"owner":"user:business-owner","audience":{"roles":["manager"],"populations":["people-leaders"],"organization_scopes":["org-west"]}}`)
	if err := s.PutVersion(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	for _, event := range []LifecycleEvent{
		lifecycle(v.TenantID, v.PersonaID, v.Version, "query-draft", "", StateDraft),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "query-review", StateDraft, StateInReview),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "query-publish", StateInReview, StatePublished),
	} {
		if err := appendLifecycleForTest(t, s, context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Install(context.Background(), PersonaInstallation{
		TenantID: v.TenantID, InstallationID: "install-active", PersonaID: v.PersonaID,
		PersonaVersion: v.Version, ConversationID: "conversation-1", ConversationClass: ConversationGroupDM,
		InstallerID: "manager-1", ChannelPolicy: ChannelPolicy{MaxTier: "T2", AllowedDataClasses: []string{"WORKFORCE"},
			AlwaysPrivate: false, ConversationSearchAllowed: true, AllowedChannelClasses: []ConversationClass{ConversationGroupDM},
			AllowExternalMembers: true, AllowCrossCompanyMembers: false}, State: InstallationActive, Revision: 4, RevocationEpoch: 9,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(context.Background(), PersonaInstallation{
		TenantID: v.TenantID, InstallationID: "install-other-room", PersonaID: v.PersonaID,
		PersonaVersion: v.Version, ConversationID: "conversation-2", ConversationClass: ConversationPrivate,
		InstallerID: "manager-1", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(context.Background(), PersonaInstallation{
		TenantID: v.TenantID, InstallationID: "install-suspended", PersonaID: v.PersonaID,
		PersonaVersion: v.Version, ConversationID: "conversation-1", ConversationClass: ConversationGroupDM,
		InstallerID: "manager-1", ChannelPolicy: testChannelPolicy(), State: InstallationSuspended, SuspensionReason: "guest added", Revision: 2, RevocationEpoch: 3,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListActiveInstallations(context.Background(), "conversation-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("active installations = %+v, want one", got)
	}
	item := got[0]
	if item.InstallationID != "install-active" || item.PersonaID != v.PersonaID || item.PersonaVersion != 7 || item.AgentVersion != "agent-v7.3" || item.ConversationClass != ConversationGroupDM || item.Revision != 4 || item.RevocationEpoch != 9 {
		t.Fatalf("exact installation projection = %+v", item)
	}
	if item.ChannelPolicy.MaxTier != "T2" || item.ChannelPolicy.AlwaysPrivate || !item.ChannelPolicy.ConversationSearchAllowed || !item.ChannelPolicy.AllowExternalMembers {
		t.Fatalf("channel policy projection = %+v", item.ChannelPolicy)
	}
	if len(item.Audience.Roles) != 1 || item.Audience.Roles[0] != "manager" || item.Audience.Populations[0] != "people-leaders" || item.Audience.OrganizationScope[0] != "org-west" {
		t.Fatalf("audience projection = %+v", item.Audience)
	}
	if err := s.AppendLifecycle(context.Background(), lifecycle(v.TenantID, v.PersonaID, v.Version, "query-suspend", StatePublished, StateSuspended)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListActiveInstallations(context.Background(), "conversation-1"); err != nil || len(got) != 0 {
		t.Fatalf("suspended persona installations = %+v, %v; want empty", got, err)
	}
}

func TestTodo_AGENTP_007_InstallationQuerySecurity(t *testing.T) {
	f := newFixture(t, values.TenantId("viewer-a"), values.TenantId("viewer-b"))
	a, b := f.store(t, "viewer-a"), f.store(t, "viewer-b")
	v := version("viewer-a", "private-persona", 1)
	if err := a.PutVersion(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	for _, event := range []LifecycleEvent{
		lifecycle(v.TenantID, v.PersonaID, v.Version, "security-draft", "", StateDraft),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "security-review", StateDraft, StateInReview),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "security-publish", StateInReview, StatePublished),
	} {
		if err := appendLifecycleForTest(t, a, context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Install(context.Background(), PersonaInstallation{TenantID: v.TenantID, InstallationID: "private-install", PersonaID: v.PersonaID, PersonaVersion: v.Version, ConversationID: "shared-name", ConversationClass: ConversationPrivate, InstallerID: "manager", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := b.ListActiveInstallations(context.Background(), "shared-name")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("foreign tenant installations = %+v", got)
	}
	if _, err := a.ListActiveInstallations(context.Background(), " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank conversation error = %v, want ErrInvalid", err)
	}
	if got, err := a.ListActiveByConversation(context.Background(), "missing"); err != nil || len(got) != 0 {
		t.Fatalf("missing conversation = %+v, %v; want empty", got, err)
	}
}

func TestTodo_AGENTP_008_ReadCurrentPersonaAuthority(t *testing.T) {
	f := newFixture(t, "authority-tenant")
	s := f.store(t, "authority-tenant")
	ctx := context.Background()
	for _, v := range []PersonaVersion{version("authority-tenant", "persona-a", 1), version("authority-tenant", "persona-a", 2)} {
		if err := s.PutVersion(ctx, v); err != nil {
			t.Fatal(err)
		}
		for _, event := range []LifecycleEvent{
			lifecycle(v.TenantID, v.PersonaID, v.Version, fmt.Sprintf("draft-%d", v.Version), "", StateDraft),
			lifecycle(v.TenantID, v.PersonaID, v.Version, fmt.Sprintf("review-%d", v.Version), StateDraft, StateInReview),
			lifecycle(v.TenantID, v.PersonaID, v.Version, fmt.Sprintf("publish-%d", v.Version), StateInReview, StatePublished),
		} {
			if err := appendLifecycleForTest(t, s, ctx, event); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Install(ctx, PersonaInstallation{TenantID: "authority-tenant", InstallationID: "stale", PersonaID: "persona-a", PersonaVersion: 1, ConversationID: "room-a", ConversationClass: ConversationPrivate, InstallerID: "manager", ChannelPolicy: testChannelPolicy(), State: InstallationSuspended, SuspensionReason: "superseded version", Revision: 2, RevocationEpoch: 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(ctx, PersonaInstallation{TenantID: "authority-tenant", InstallationID: "current", PersonaID: "persona-a", PersonaVersion: 2, ConversationID: "room-a", ConversationClass: ConversationPrivate, InstallerID: "manager", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 5, RevocationEpoch: 8}); err != nil {
		t.Fatal(err)
	}
	gotVersion, gotInstallation, err := s.ReadCurrentPersonaAuthority(ctx, "room-a", "persona-a")
	if err != nil {
		t.Fatal(err)
	}
	if gotVersion.Version != 2 || gotInstallation.InstallationID != "current" || gotInstallation.PersonaVersion != 2 || gotInstallation.Revision != 5 || gotInstallation.RevocationEpoch != 8 {
		t.Fatalf("authority = version=%+v installation=%+v", gotVersion, gotInstallation)
	}
	if _, _, err := s.ReadCurrentPersonaAuthority(ctx, "room-a", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing authority error = %v, want ErrNotFound", err)
	}
	if err := s.Install(ctx, PersonaInstallation{TenantID: "authority-tenant", InstallationID: "pinned-old-version", PersonaID: "persona-a", PersonaVersion: 1, ConversationID: "room-b", ConversationClass: ConversationPrivate, InstallerID: "manager", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	pinnedVersion, pinnedInstallation, err := s.ReadCurrentPersonaAuthority(ctx, "room-b", "persona-a")
	if err != nil || pinnedVersion.Version != 1 || pinnedInstallation.InstallationID != "pinned-old-version" {
		t.Fatalf("publishing a newer version displaced an exact installation: version=%+v installation=%+v err=%v", pinnedVersion, pinnedInstallation, err)
	}
	available, err := s.ListAvailable(ctx, []AvailableInstallation{
		{PersonaID: "persona-a", PersonaVersion: 1, InstallationID: "pinned-old-version", ConversationID: "room-b"},
		{PersonaID: "persona-a", PersonaVersion: 2, InstallationID: "current", ConversationID: "room-a"},
	})
	if err != nil || len(available) != 2 || available[0].Version != 1 || available[1].Version != 2 {
		t.Fatalf("available personas erased an independently pinned installation: %+v, %v", available, err)
	}
}

func TestTodo_AGENTP_008_ReadCurrentPersonaAuthorityRejectsAmbiguousActivePlacements(t *testing.T) {
	f := newFixture(t, "authority-ambiguous")
	s := f.store(t, "authority-ambiguous")
	ctx := context.Background()
	v := version("authority-ambiguous", "persona-a", 1)
	if err := s.PutVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	for _, event := range []LifecycleEvent{
		lifecycle(v.TenantID, v.PersonaID, v.Version, "amb-draft", "", StateDraft),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "amb-review", StateDraft, StateInReview),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "amb-publish", StateInReview, StatePublished),
	} {
		if err := appendLifecycleForTest(t, s, ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Install(ctx, PersonaInstallation{TenantID: v.TenantID, InstallationID: "active-a", PersonaID: v.PersonaID, PersonaVersion: v.Version, ConversationID: "room-a", ConversationClass: ConversationPrivate, InstallerID: "manager", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	// Simulate a restored historical duplicate that predates the admission
	// guard. The read path must still fail closed for corrupted durable state.
	f.db.Exec(t, `INSERT INTO persona_installations
		(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at)
		SELECT tenant_id,'active-b',persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at
		FROM persona_installations WHERE tenant_id=$1 AND installation_id='active-a'`, f.ids[v.TenantID])
	if _, _, err := s.ReadCurrentPersonaAuthority(ctx, "room-a", v.PersonaID); !errors.Is(err, ErrConflict) {
		t.Fatalf("ambiguous authority error = %v, want ErrConflict", err)
	}
}
