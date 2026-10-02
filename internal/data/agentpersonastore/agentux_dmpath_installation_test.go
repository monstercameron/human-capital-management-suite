package agentpersonastore

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_AGENTUX_025_StoreUninstallBumpsRevocationEpoch(t *testing.T) {
	f := newFixture(t, "dm-path-tenant")
	s := f.store(t, "dm-path-tenant")
	ctx := context.Background()
	v := version("dm-path-tenant", "persona-a", 1)
	if err := s.PutVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	for _, event := range []LifecycleEvent{
		lifecycle(v.TenantID, v.PersonaID, v.Version, "dm-path-draft", "", StateDraft),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "dm-path-review", StateDraft, StateInReview),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "dm-path-publish", StateInReview, StatePublished),
	} {
		if err := appendLifecycleForTest(t, s, ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Install(ctx, PersonaInstallation{TenantID: v.TenantID, InstallationID: "install-a", PersonaID: v.PersonaID, PersonaVersion: v.Version, ConversationID: "room-a", ConversationClass: ConversationPrivate, InstallerID: "admin", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	retired, changed, err := s.RetireActiveInstallation(ctx, v.PersonaID, "room-a", "admin", "remove")
	if err != nil || !changed {
		t.Fatalf("retire changed=%t err=%v", changed, err)
	}
	if retired.State != InstallationRetired || retired.Revision != 2 || retired.RevocationEpoch != 2 {
		t.Fatalf("retired installation = %+v", retired)
	}
	if _, changed, err = s.RetireActiveInstallation(ctx, v.PersonaID, "room-a", "admin", "remove"); err != nil || changed {
		t.Fatalf("retire replay changed=%t err=%v", changed, err)
	}
	if _, err := s.GetInstallation(ctx, "install-a"); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_AGENTUX_025_StoreReinstallRefreshesPolicyIdempotently(t *testing.T) {
	f := newFixture(t, "dm-path-reinstall")
	s := f.store(t, "dm-path-reinstall")
	ctx := context.Background()
	v := version("dm-path-reinstall", "persona-a", 1)
	if err := s.PutVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	for _, event := range []LifecycleEvent{
		lifecycle(v.TenantID, v.PersonaID, v.Version, "dm-re-draft", "", StateDraft),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "dm-re-review", StateDraft, StateInReview),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "dm-re-publish", StateInReview, StatePublished),
	} {
		if err := appendLifecycleForTest(t, s, ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	stale := testChannelPolicy()
	stale.ConversationSearchAllowed = false
	if err := s.Install(ctx, PersonaInstallation{TenantID: v.TenantID, InstallationID: "install-stale", PersonaID: v.PersonaID, PersonaVersion: v.Version, ConversationID: "room-a", ConversationClass: ConversationPrivate, InstallerID: "admin", ChannelPolicy: stale, State: InstallationActive, Revision: 1, RevocationEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	fresh := PersonaInstallation{TenantID: v.TenantID, InstallationID: "install-fresh", PersonaID: v.PersonaID, PersonaVersion: v.Version, ConversationID: "room-a", ConversationClass: ConversationPrivate, InstallerID: "admin", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}
	fresh.ChannelPolicy.ConversationSearchAllowed = true
	got, changed, err := s.ReplaceActiveInstallation(ctx, fresh)
	if err != nil || !changed || got.InstallationID != fresh.InstallationID {
		t.Fatalf("replace got=%+v changed=%t err=%v", got, changed, err)
	}
	old, err := s.GetInstallation(ctx, "install-stale")
	if err != nil || old.State != InstallationRetired || old.RevocationEpoch != 2 {
		t.Fatalf("old install=%+v err=%v", old, err)
	}
	replay := fresh
	replay.InstallationID = "install-replay"
	got, changed, err = s.ReplaceActiveInstallation(ctx, replay)
	if err != nil || changed || got.InstallationID != fresh.InstallationID {
		t.Fatalf("replay got=%+v changed=%t err=%v", got, changed, err)
	}
	if _, err = s.GetInstallation(ctx, "install-replay"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replay inserted a duplicate: %v", err)
	}
}
