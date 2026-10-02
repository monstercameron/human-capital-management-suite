package agentpersonastore

import (
	"context"
	"errors"
	"testing"
)

func TestAgentUXGeneral_DuplicateRetirement_Integration(t *testing.T) {
	f := newFixture(t, "agentux-general-duplicate")
	s := f.store(t, "agentux-general-duplicate")
	ctx := context.Background()
	v := version("agentux-general-duplicate", "assistant", 1)
	if err := s.PutVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	for _, event := range []LifecycleEvent{
		lifecycle(v.TenantID, v.PersonaID, v.Version, "general-draft", "", StateDraft),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "general-review", StateDraft, StateInReview),
		lifecycle(v.TenantID, v.PersonaID, v.Version, "general-publish", StateInReview, StatePublished),
	} {
		if err := appendLifecycleForTest(t, s, ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	suspended := PersonaInstallation{TenantID: v.TenantID, InstallationID: "suspended", PersonaID: v.PersonaID, PersonaVersion: v.Version, ConversationID: "general", ConversationClass: ConversationPublic, InstallerID: "admin", ChannelPolicy: testChannelPolicy(), State: InstallationSuspended, SuspensionReason: "AGENT_PRINCIPAL_MISSING_AFTER_RESTORE", Revision: 2, RevocationEpoch: 2}
	if err := s.Install(ctx, suspended); err != nil {
		t.Fatal(err)
	}
	fresh := suspended
	fresh.InstallationID, fresh.State, fresh.SuspensionReason, fresh.Revision, fresh.RevocationEpoch = "fresh", InstallationActive, "", 1, 1
	if err := s.Install(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	old, err := s.GetInstallation(ctx, suspended.InstallationID)
	if err != nil || old.State != InstallationRetired || old.Revision != 3 || old.RevocationEpoch != 3 || old.SuspensionReason != retiredSuspendedDuplicateReason {
		t.Fatalf("retired duplicate=%+v err=%v", old, err)
	}
	if retired, err := s.RetireSuspendedDuplicateInstallations(ctx, v.PersonaID, "general"); err != nil || retired != 0 {
		t.Fatalf("repair replay retired=%d err=%v", retired, err)
	}
}

func TestAgentUXGeneral_DuplicateRepair_Security(t *testing.T) {
	f := newFixture(t, "general-a", "general-b")
	a, b := f.store(t, "general-a"), f.store(t, "general-b")
	ctx := context.Background()
	v := version("general-a", "assistant", 1)
	if err := a.PutVersion(ctx, v); err != nil {
		t.Fatal(err)
	}
	for _, event := range []LifecycleEvent{lifecycle(v.TenantID, v.PersonaID, 1, "d", "", StateDraft), lifecycle(v.TenantID, v.PersonaID, 1, "r", StateDraft, StateInReview), lifecycle(v.TenantID, v.PersonaID, 1, "p", StateInReview, StatePublished)} {
		if err := appendLifecycleForTest(t, a, ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	stopped := PersonaInstallation{TenantID: v.TenantID, InstallationID: "stopped", PersonaID: v.PersonaID, PersonaVersion: 1, ConversationID: "general", ConversationClass: ConversationPublic, InstallerID: "admin", ChannelPolicy: testChannelPolicy(), State: InstallationSuspended, SuspensionReason: "AGENT_PRINCIPAL_MISSING_AFTER_RESTORE", Revision: 2, RevocationEpoch: 2}
	if err := a.Install(ctx, stopped); err != nil {
		t.Fatal(err)
	}
	if count, err := a.RetireSuspendedDuplicateInstallations(ctx, "assistant", "general"); err != nil || count != 0 {
		t.Fatalf("repair without replacement=%d %v", count, err)
	}
	if count, err := b.RetireSuspendedDuplicateInstallations(ctx, "assistant", "general"); err != nil || count != 0 {
		t.Fatalf("foreign repair=%d %v", count, err)
	}
	invalid := stopped
	invalid.InstallationID = "new"
	invalid.State = InstallationActive
	invalid.PersonaVersion = 99
	if err := a.Install(ctx, invalid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed install err=%v", err)
	}
	current, err := a.GetInstallation(ctx, "stopped")
	if err != nil || current.State != InstallationSuspended || current.Revision != 2 {
		t.Fatalf("failed install retired placement=%+v %v", current, err)
	}
	if _, _, err := a.ReplaceActiveInstallation(ctx, invalid); !errors.Is(err, ErrConflict) {
		t.Fatalf("unpublished restart err=%v", err)
	}
	current, err = a.GetInstallation(ctx, "stopped")
	if err != nil || current.State != InstallationSuspended {
		t.Fatalf("invalid restart retired stopped row=%+v %v", current, err)
	}
	invalid.PersonaVersion = 1
	replacement, changed, err := a.ReplaceActiveInstallation(ctx, invalid)
	if err != nil || !changed || replacement.InstallationID != "new" {
		t.Fatalf("restart=%+v %t %v", replacement, changed, err)
	}
	current, err = a.GetInstallation(ctx, "stopped")
	if err != nil || current.State != InstallationRetired || current.Revision != 3 || current.RevocationEpoch != 3 {
		t.Fatalf("restart duplicate=%+v %v", current, err)
	}
	if _, changed, err := a.ReplaceActiveInstallation(ctx, invalid); err != nil || changed {
		t.Fatalf("restart replay=%t %v", changed, err)
	}
}

func TestAgentUXGeneral_RestoredDuplicateRepair_Integration(t *testing.T) {
	f := newFixture(t, "general-repair", "general-other")
	s, other := f.store(t, "general-repair"), f.store(t, "general-other")
	ctx := context.Background()
	v := createCatalogPersona(t, s, "general-repair", "assistant", StatePublished)
	for _, conversation := range []string{"general", "direct"} {
		active := PersonaInstallation{TenantID: v.TenantID, InstallationID: "active-" + conversation, PersonaID: v.PersonaID, PersonaVersion: v.Version, ConversationID: conversation, ConversationClass: ConversationPublic, InstallerID: "admin", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}
		if err := s.Install(ctx, active); err != nil {
			t.Fatal(err)
		}
		stopped := active
		stopped.InstallationID, stopped.State, stopped.SuspensionReason = "old-"+conversation, InstallationSuspended, "AGENT_PRINCIPAL_MISSING_AFTER_RESTORE"
		if err := s.Install(ctx, stopped); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := s.ListCatalog(ctx)
	if err != nil || len(catalog) != 1 || len(catalog[0].Installations) != 4 {
		t.Fatalf("restored catalog=%+v err=%v", catalog, err)
	}
	stopped := 0
	for _, in := range catalog[0].Installations {
		if in.State == string(InstallationSuspended) {
			stopped++
			if in.SuspensionReason != "AGENT_PRINCIPAL_MISSING_AFTER_RESTORE" {
				t.Fatalf("suspension reason was lost: %+v", in)
			}
		}
	}
	if stopped != 2 {
		t.Fatalf("stopped projection count=%d", stopped)
	}
	if foreign, err := other.ListCatalog(ctx); err != nil || len(foreign) != 0 {
		t.Fatalf("foreign stopped projection=%+v err=%v", foreign, err)
	}
	for _, conversation := range []string{"general", "direct"} {
		if retired, err := s.RetireSuspendedDuplicateInstallations(ctx, v.PersonaID, conversation); err != nil || retired != 1 {
			t.Fatalf("one-time repair %s=%d err=%v", conversation, retired, err)
		}
		active, err := s.GetInstallation(ctx, "active-"+conversation)
		if err != nil || active.State != InstallationActive || active.Revision != 1 || active.RevocationEpoch != 1 {
			t.Fatalf("repair changed active placement=%+v err=%v", active, err)
		}
		if retired, err := s.RetireSuspendedDuplicateInstallations(ctx, v.PersonaID, conversation); err != nil || retired != 0 {
			t.Fatalf("repair replay %s=%d err=%v", conversation, retired, err)
		}
	}
}
