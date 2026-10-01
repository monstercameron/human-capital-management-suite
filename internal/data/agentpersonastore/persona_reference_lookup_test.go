package agentpersonastore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_008(t *testing.T) {
	f := newFixture(t, "persona-reference-owner", "persona-reference-other")
	owner := f.store(t, "persona-reference-owner")
	other := f.store(t, "persona-reference-other")
	ctx := context.Background()
	v := version("persona-reference-owner", "persona.lookup", 1)
	createPublishedPersona(t, owner, v)
	if err := owner.Install(ctx, PersonaInstallation{TenantID: v.TenantID, InstallationID: "install:active", PersonaID: v.PersonaID, PersonaVersion: 1, ConversationID: "channel:exact", ConversationClass: ConversationPrivate, InstallerID: "user:admin", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}); err != nil {
		t.Fatal(err)
	}

	got, err := owner.LookupCurrentReferenceInstallation(ctx, v.PersonaID, "channel:exact")
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != v.TenantID || got.InstallationID != "install:active" || got.ConversationID != "channel:exact" || got.PersonaVersion != 1 || got.CurrentVersion != 1 || got.Lifecycle != StatePublished || got.InstallationState != InstallationActive {
		t.Fatalf("lookup=%+v", got)
	}
	if _, err := owner.LookupCurrentReferenceInstallation(ctx, v.PersonaID, "channel:other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong conversation error=%v", err)
	}
	if _, err := other.LookupCurrentReferenceInstallation(ctx, v.PersonaID, "channel:exact"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign tenant error=%v", err)
	}
}

func TestTodo_AGENTP_008FailsClosed(t *testing.T) {
	f := newFixture(t, "persona-reference-state")
	s := f.store(t, "persona-reference-state")
	ctx := context.Background()
	cases := []struct {
		name          string
		state         InstallationState
		lifecycle     LifecycleState
		addNewVersion bool
		want          error
	}{
		{name: "suspended installation", state: InstallationSuspended, lifecycle: StatePublished, want: ErrNotFound},
		{name: "draft persona", state: InstallationActive, lifecycle: StateDraft, want: ErrNotFound},
		{name: "draft latest version", state: InstallationActive, lifecycle: StatePublished, addNewVersion: true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := values.TenantId("persona-reference-state")
			personaID := []string{"persona.suspended", "persona.draft", "persona.stale"}[i]
			v := version(id, personaID, 1)
			createPersonaAtState(t, s, v, tc.lifecycle)
			installVersion := int64(1)
			if tc.addNewVersion {
				v2 := version(id, personaID, 2)
				if err := s.PutVersion(ctx, v2); err != nil {
					t.Fatal(err)
				}
				if err := appendLifecycleForTest(t, s, ctx, lifecycle(id, personaID, 2, personaID+"-draft", "", StateDraft)); err != nil {
					t.Fatal(err)
				}
				installVersion = 1
			}
			installation := PersonaInstallation{TenantID: id, InstallationID: "install:" + personaID, PersonaID: personaID, PersonaVersion: installVersion, ConversationID: "channel:exact", ConversationClass: ConversationPrivate, InstallerID: "user:admin", ChannelPolicy: testChannelPolicy(), State: tc.state, SuspensionReason: "test suspension", Revision: 1, RevocationEpoch: 1}
			if tc.lifecycle == StatePublished {
				if err := s.Install(ctx, installation); err != nil {
					t.Fatal(err)
				}
			} else {
				// Model a pre-existing/restored placement whose lifecycle is no
				// longer invocable; new placements are rejected by Install.
				f.db.Exec(t, `INSERT INTO persona_installations
					(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,now(),now())`, f.ids[id], installation.InstallationID,
					installation.PersonaID, installation.PersonaVersion, installation.ConversationID, installation.ConversationClass, installation.InstallerID, marshalChannelPolicy(installation.ChannelPolicy),
					installation.State, installation.SuspensionReason, installation.Revision, installation.RevocationEpoch)
			}
			got, err := s.LookupCurrentReferenceInstallation(ctx, personaID, "channel:exact")
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("error=%v want %v", err, tc.want)
				}
				return
			}
			if err != nil || got.PersonaVersion != 1 || got.CurrentVersion != 1 {
				t.Fatalf("draft version invalidated published installation: lookup=%+v error=%v", got, err)
			}
		})
	}
}

func TestTodo_AGENTP_004_InstallRequiresPublishedVersion(t *testing.T) {
	f := newFixture(t, "persona-install-lifecycle")
	s := f.store(t, "persona-install-lifecycle")
	ctx := context.Background()
	v := version("persona-install-lifecycle", "persona.install-state", 1)
	owner, steward := draftOwners(v)
	if err := s.CreateDraft(ctx, v, owner, steward, "user:creator", v.CreatedAt); err != nil {
		t.Fatal(err)
	}
	base := PersonaInstallation{TenantID: v.TenantID, InstallationID: "install:draft", PersonaID: v.PersonaID, PersonaVersion: v.Version, ConversationID: "channel:exact", ConversationClass: ConversationPrivate, InstallerID: "user:admin", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}
	if err := s.Install(ctx, base); !errors.Is(err, ErrConflict) {
		t.Fatalf("draft install error=%v, want ErrConflict", err)
	}
	if _, err := s.GetInstallation(ctx, base.InstallationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft install persisted despite refusal: %v", err)
	}
	if err := appendLifecycleForTest(t, s, ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "install-review", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	if err := appendLifecycleForTest(t, s, ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "install-publish", StateInReview, StatePublished)); err != nil {
		t.Fatal(err)
	}
	base.InstallationID = "install:published"
	if err := s.Install(ctx, base); err != nil {
		t.Fatalf("published install error=%v", err)
	}
	if err := appendLifecycleForTest(t, s, ctx, lifecycle(v.TenantID, v.PersonaID, v.Version, "install-suspend", StatePublished, StateSuspended)); err != nil {
		t.Fatal(err)
	}
	base.InstallationID = "install:suspended"
	if err := s.Install(ctx, base); !errors.Is(err, ErrConflict) {
		t.Fatalf("suspended install error=%v, want ErrConflict", err)
	}
	if _, err := s.GetInstallation(ctx, base.InstallationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspended install persisted despite refusal: %v", err)
	}
}

func TestTodo_AGENTP_008PublishedVersionSuspension(t *testing.T) {
	f := newFixture(t, "persona-reference-suspend")
	s := f.store(t, "persona-reference-suspend")
	ctx := context.Background()
	id := values.TenantId("persona-reference-suspend")
	v3 := version(id, "persona.current", 3)
	createPublishedPersona(t, s, v3)
	if err := s.Install(ctx, PersonaInstallation{TenantID: id, InstallationID: "install:current", PersonaID: v3.PersonaID, PersonaVersion: 3, ConversationID: "channel:exact", ConversationClass: ConversationPrivate, InstallerID: "user:admin", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	v4 := version(id, v3.PersonaID, 4)
	if err := s.PutVersion(ctx, v4); err != nil {
		t.Fatal(err)
	}
	if err := appendLifecycleForTest(t, s, ctx, lifecycle(id, v3.PersonaID, 4, "persona.current-draft-v4", "", StateDraft)); err != nil {
		t.Fatal(err)
	}
	got, err := s.LookupCurrentReferenceInstallation(ctx, v3.PersonaID, "channel:exact")
	if err != nil || got.PersonaVersion != 3 || got.CurrentVersion != 3 || got.Lifecycle != StatePublished {
		t.Fatalf("draft v4 hid published v3: lookup=%+v error=%v", got, err)
	}
	if err := appendLifecycleForTest(t, s, ctx, lifecycle(id, v3.PersonaID, 3, "persona.current-suspend-v3", StatePublished, StateSuspended)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupCurrentReferenceInstallation(ctx, v3.PersonaID, "channel:exact"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspended published v3 remained resolvable: %v", err)
	}
}

func createPublishedPersona(t *testing.T, s *TenantStore, v PersonaVersion) {
	createPersonaAtState(t, s, v, StatePublished)
}

func createPersonaAtState(t *testing.T, s *TenantStore, v PersonaVersion, target LifecycleState) {
	t.Helper()
	owner, steward := draftOwners(v)
	if err := s.CreateDraft(context.Background(), v, owner, steward, "user:creator", v.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if target == StateDraft {
		return
	}
	if err := appendLifecycleForTest(t, s, context.Background(), lifecycle(v.TenantID, v.PersonaID, v.Version, v.PersonaID+"-review", StateDraft, StateInReview)); err != nil {
		t.Fatal(err)
	}
	if target == StateInReview {
		return
	}
	if target == StatePublished {
		if err := appendLifecycleForTest(t, s, context.Background(), lifecycle(v.TenantID, v.PersonaID, v.Version, v.PersonaID+"-publish", StateInReview, StatePublished)); err != nil {
			t.Fatal(err)
		}
		return
	}
	if target == StateSuspended {
		if err := appendLifecycleForTest(t, s, context.Background(), lifecycle(v.TenantID, v.PersonaID, v.Version, v.PersonaID+"-publish", StateInReview, StatePublished)); err != nil {
			t.Fatal(err)
		}
		if err := appendLifecycleForTest(t, s, context.Background(), lifecycle(v.TenantID, v.PersonaID, v.Version, v.PersonaID+"-suspend", StatePublished, StateSuspended)); err != nil {
			t.Fatal(err)
		}
	}
}
