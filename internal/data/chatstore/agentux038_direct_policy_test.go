package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_AGENTUX_038_Integration_Policies: a direct conversation with an
// agent gets its audience policy and its one-to-one agent policy together or
// not at all; policies already there are left as they are; and no other kind
// of conversation is given the one-to-one policy.
func TestTodo_AGENTUX_038_Integration_Policies(t *testing.T) {
	store, _ := chatFixture(t)
	ctx := context.Background()
	members := []Membership{{MemberID: "alice", Role: "manager", State: "active"}, {MemberID: "agent-1", Role: "member", State: "active"}}
	seedConversationRow(t, store, Conversation{ID: "dm", TenantID: "tenant-a", Kind: "DIRECT", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, members)
	audience := AudiencePolicy{RoleMode: 1, Classification: "INTERNAL"}
	agent := PersonaChannelPolicy{MaxTier: "T3", PlacementClass: "ONE_TO_ONE_DM", AlwaysPrivate: true, ConversationSearchAllowed: true, AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []string{"ONE_TO_ONE"}}

	count := func(table, conversation string) int {
		t.Helper()
		var n int
		if err := store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id=$1 AND conversation_id=$2`, "tenant-a", conversation).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	revision := func(conversation string) int64 {
		t.Helper()
		var n int64
		if err := store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT audience_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, "tenant-a", conversation).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// A policy that cannot be stored leaves the conversation with neither.
	broken := agent
	broken.MaxTier = "T9"
	if _, _, err := store.EnsurePersonaDirectPolicies(ctx, "tenant-a", "dm", audience, broken); !errors.Is(err, ErrAudienceEligibilityUnavailable) {
		t.Fatalf("an invalid agent policy: %v", err)
	}
	if _, _, err := store.EnsurePersonaDirectPolicies(ctx, "tenant-a", "dm", AudiencePolicy{RoleMode: 1}, agent); !errors.Is(err, ErrAudienceEligibilityUnavailable) {
		t.Fatalf("an audience policy with no classification: %v", err)
	}
	if count("chat_channel_policy", "dm") != 0 || count("chat_persona_channel_policy", "dm") != 0 {
		t.Fatal("a refused pair left a policy behind")
	}

	before := revision("dm")
	audienceCreated, agentCreated, err := store.EnsurePersonaDirectPolicies(ctx, "tenant-a", "dm", audience, agent)
	if err != nil || !audienceCreated || !agentCreated {
		t.Fatalf("first ensure: %v %v %v", audienceCreated, agentCreated, err)
	}
	// The audience revision moves, so a snapshot taken before the policies
	// existed is no longer current.
	if count("chat_channel_policy", "dm") != 1 || count("chat_persona_channel_policy", "dm") != 1 || revision("dm") <= before {
		t.Fatalf("after the first ensure: audience=%d agent=%d revision %d -> %d", count("chat_channel_policy", "dm"), count("chat_persona_channel_policy", "dm"), before, revision("dm"))
	}
	captured, err := store.CapturePersonaChannelPolicy(ctx, "tenant-a", "dm", "")
	if err != nil || !captured.Policy.AlwaysPrivate || captured.Policy.PlacementClass != "ONE_TO_ONE_DM" || captured.PolicyRevision != 1 {
		t.Fatalf("captured=%+v err=%v", captured, err)
	}

	// A second ensure changes nothing, and an administrator's narrower agent
	// policy is not replaced by the default one.
	if _, err = store.PutPersonaChannelPolicy(ctx, "tenant-a", "dm", 1, PersonaChannelPolicy{MaxTier: "T1", PlacementClass: "ONE_TO_ONE_DM", AlwaysPrivate: true, AllowedDataClasses: []string{"PUBLIC"}, AllowedChannelClasses: []string{"ONE_TO_ONE"}}); err != nil {
		t.Fatal(err)
	}
	held := revision("dm")
	if audienceCreated, agentCreated, err = store.EnsurePersonaDirectPolicies(ctx, "tenant-a", "dm", audience, agent); err != nil || audienceCreated || agentCreated {
		t.Fatalf("second ensure: %v %v %v", audienceCreated, agentCreated, err)
	}
	if captured, err = store.CapturePersonaChannelPolicy(ctx, "tenant-a", "dm", ""); err != nil || captured.Policy.MaxTier != "T1" || captured.PolicyRevision != 2 || revision("dm") != held {
		t.Fatalf("the administrator's policy was replaced: %+v %v", captured, err)
	}

	// A conversation that already has its audience policy gets the missing one.
	seedConversationRow(t, store, Conversation{ID: "dm-half", TenantID: "tenant-a", Kind: "DIRECT", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, members)
	if _, err = store.PutAudiencePolicy(ctx, "tenant-a", "dm-half", 0, audience); err != nil {
		t.Fatal(err)
	}
	if audienceCreated, agentCreated, err = store.EnsurePersonaDirectPolicies(ctx, "tenant-a", "dm-half", audience, agent); err != nil || audienceCreated || !agentCreated {
		t.Fatalf("repair of a half-made conversation: %v %v %v", audienceCreated, agentCreated, err)
	}

	// The one-to-one policy is for a direct conversation only, an archived one
	// gets nothing, and neither does one that is not there or is another
	// workspace's.
	seedConversationRow(t, store, Conversation{ID: "room", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, members)
	seedConversationRow(t, store, Conversation{ID: "dm-archived", TenantID: "tenant-a", Kind: "DIRECT", OwnerID: "alice", Lifecycle: "ARCHIVED", SettingsRevision: 1}, members)
	for _, conversation := range []string{"room", "dm-archived"} {
		if _, _, err = store.EnsurePersonaDirectPolicies(ctx, "tenant-a", conversation, audience, agent); !errors.Is(err, ErrAudienceEligibilityUnavailable) {
			t.Fatalf("%s: %v", conversation, err)
		}
		if count("chat_channel_policy", conversation) != 0 || count("chat_persona_channel_policy", conversation) != 0 {
			t.Fatalf("%s was given a policy", conversation)
		}
	}
	if _, _, err = store.EnsurePersonaDirectPolicies(ctx, "tenant-a", "no-such-conversation", audience, agent); err == nil {
		t.Fatal("a conversation that does not exist was given policies")
	}
	if _, _, err = store.EnsurePersonaDirectPolicies(ctx, "tenant-b", "dm", audience, agent); err == nil {
		t.Fatal("another workspace wrote this conversation's policies")
	}
}
