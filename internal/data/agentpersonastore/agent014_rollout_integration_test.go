package agentpersonastore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_AGENT_014_Integration runs the per-conversation installation
// transitions against real PostgreSQL: each conversation has its own
// revisioned installation, a suspension or removal is fenced by that
// installation's exact revision and bound to the preview and approval that
// reviewed it, and removing one installation never changes another, in this
// tenant or the next.
func TestTodo_AGENT_014_Integration(t *testing.T) {
	f := newFixture(t, "rollout-a", "rollout-b")
	ctx := context.Background()
	stores := map[values.TenantId]*TenantStore{"rollout-a": f.store(t, "rollout-a"), "rollout-b": f.store(t, "rollout-b")}
	for tenant, store := range stores {
		v := version(tenant, "persona-a", 1)
		if err := store.PutVersion(ctx, v); err != nil {
			t.Fatal(err)
		}
		for _, event := range []LifecycleEvent{
			lifecycle(tenant, "persona-a", 1, "draft-"+string(tenant), "", StateDraft),
			lifecycle(tenant, "persona-a", 1, "review-"+string(tenant), StateDraft, StateInReview),
			lifecycle(tenant, "persona-a", 1, "publish-"+string(tenant), StateInReview, StatePublished),
		} {
			if err := appendLifecycleForTest(t, store, ctx, event); err != nil {
				t.Fatal(err)
			}
		}
		// The same installation ids in both tenants: an id is not authority.
		for _, room := range []string{"room-a", "room-b", "room-c"} {
			if err := store.Install(ctx, PersonaInstallation{TenantID: tenant, InstallationID: "install-" + room, PersonaID: "persona-a", PersonaVersion: 1, ConversationID: room, ConversationClass: ConversationPrivate, InstallerID: "manager-a", ChannelPolicy: testChannelPolicy(), State: InstallationActive, Revision: 1, RevocationEpoch: 1}); err != nil {
				t.Fatalf("%s install in %s: %v", tenant, room, err)
			}
		}
	}
	a := stores["rollout-a"]
	state := func(t *testing.T, store *TenantStore, id string) PersonaInstallation {
		t.Helper()
		installation, err := store.GetInstallation(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return installation
	}
	refs := func(t *testing.T, tenant values.TenantId, id string) (preview, approval string) {
		t.Helper()
		if err := f.db.QueryRow(ctx, `SELECT rollout_preview_ref,rollout_approval_ref FROM persona_installations WHERE tenant_id=$1 AND installation_id=$2`, f.ids[tenant], id).Scan(&preview, &approval); err != nil {
			t.Fatal(err)
		}
		return preview, approval
	}
	// Three conversations, three independent installations at revision one.
	for _, room := range []string{"room-a", "room-b", "room-c"} {
		if got := state(t, a, "install-"+room); got.State != InstallationActive || got.Revision != 1 || got.RevocationEpoch != 1 || got.ConversationID != room {
			t.Fatalf("installation in %s = %+v", room, got)
		}
	}
	if schema, err := a.CheckRolloutSchema(ctx); err != nil || !schema.Ready() {
		t.Fatalf("the schema cannot keep the review references: %+v err=%v", schema, err)
	}

	// A transition that names no reviewed preview and approval is refused.
	if _, err := a.RemoveInstallation(ctx, RolloutMutation{InstallationID: "install-room-a", ExpectedRevision: 1, Reason: "manager removed the agent"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a removal with no review references = %v", err)
	}
	// One at the wrong revision is refused.
	reviewed := RolloutMutation{InstallationID: "install-room-a", ExpectedRevision: 2, PreviewRef: "preview-14", ApprovalRef: "approval-14", Reason: "manager removed the agent"}
	if _, err := a.RemoveInstallation(ctx, reviewed); !errors.Is(err, ErrConflict) {
		t.Fatalf("a removal at a stale revision = %v", err)
	}
	if got := state(t, a, "install-room-a"); got.State != InstallationActive || got.Revision != 1 {
		t.Fatalf("refused removals changed the installation: %+v", got)
	}

	// The reviewed removal retires exactly this installation.
	reviewed.ExpectedRevision = 1
	removed, err := a.RemoveInstallation(ctx, reviewed)
	if err != nil || removed.State != InstallationRetired || removed.Revision != 2 || removed.RevocationEpoch != 2 || removed.SuspensionReason != reviewed.Reason {
		t.Fatalf("removal = %+v err=%v", removed, err)
	}
	if preview, approval := refs(t, "rollout-a", "install-room-a"); preview != "preview-14" || approval != "approval-14" {
		t.Fatalf("the removal is bound to preview %q, approval %q", preview, approval)
	}
	// Replaying it does nothing more.
	if _, err := a.RemoveInstallation(ctx, reviewed); !errors.Is(err, ErrConflict) {
		t.Fatalf("the same removal replayed = %v", err)
	}
	// A membership change suspends one installation, with its reason.
	suspended, err := a.PauseInstallation(ctx, RolloutMutation{InstallationID: "install-room-b", ExpectedRevision: 1, PreviewRef: "preview-14", ApprovalRef: "approval-14", Reason: "ROLLOUT_PREVIEW_STALE"})
	if err != nil || suspended.State != InstallationSuspended || suspended.Revision != 2 || suspended.RevocationEpoch != 2 || suspended.SuspensionReason != "ROLLOUT_PREVIEW_STALE" {
		t.Fatalf("suspension = %+v err=%v", suspended, err)
	}

	// Nothing else moved: the third conversation here, and all three in the
	// other tenant, are exactly as installed.
	if got := state(t, a, "install-room-c"); got.State != InstallationActive || got.Revision != 1 || got.RevocationEpoch != 1 {
		t.Fatalf("an untouched conversation changed: %+v", got)
	}
	if preview, approval := refs(t, "rollout-a", "install-room-c"); preview != "" || approval != "" {
		t.Fatalf("an untouched installation carries references %q, %q", preview, approval)
	}
	b := stores["rollout-b"]
	for _, room := range []string{"room-a", "room-b", "room-c"} {
		if got := state(t, b, "install-"+room); got.State != InstallationActive || got.Revision != 1 || got.RevocationEpoch != 1 {
			t.Fatalf("the other tenant's installation in %s changed: %+v", room, got)
		}
	}
	// The run path agrees: no authority where the agent was removed or
	// suspended, authority where it was left alone.
	for room, want := range map[string]bool{"room-a": false, "room-b": false, "room-c": true} {
		_, installation, err := a.ReadCurrentPersonaAuthority(ctx, room, "persona-a")
		if (err == nil) != want {
			t.Errorf("run authority in %s: err=%v installation=%+v, want authority=%v", room, err, installation, want)
		}
	}
	if rows, err := b.ListActiveInstallations(ctx, "room-a"); err != nil || len(rows) != 1 {
		t.Fatalf("the other tenant's active installations in room-a = %+v err=%v", rows, err)
	}
}
