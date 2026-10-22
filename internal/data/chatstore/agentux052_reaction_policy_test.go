package chatstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// The reactions switch is one row per workspace and, where set, one per
// conversation: a stored row is read back, a missing row reads as on, and one
// tenant's rows are invisible to another (AGENTUX-052).
func TestAgentUX052_ReactionPolicyRoundTrip(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	workspace, channel, err := s.ReadAgentReactionPolicy(ctx, "tenant-a", "room-a")
	if err != nil || !workspace.ReactionsEnabled || channel != nil {
		t.Fatalf("a missing row reads %+v/%v %v, want on/nil", workspace, channel, err)
	}
	off := chat.AgentReactionPolicy{ReactionsEnabled: false}
	if err := s.WriteAgentReactionPolicy(ctx, "tenant-a", "", chat.AgentReactionPolicy{ReactionsEnabled: true}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAgentReactionPolicy(ctx, "tenant-a", "room-a", off, "admin"); err != nil {
		t.Fatal(err)
	}
	workspace, channel, err = s.ReadAgentReactionPolicy(ctx, "tenant-a", "room-a")
	if err != nil || !workspace.ReactionsEnabled || channel == nil || channel.ReactionsEnabled {
		t.Fatalf("after the writes: %+v/%v %v", workspace, channel, err)
	}
	// Writing the workspace row again replaces it; the conversation row is
	// untouched by the workspace write.
	if err := s.WriteAgentReactionPolicy(ctx, "tenant-a", "", off, "admin"); err != nil {
		t.Fatal(err)
	}
	workspace, channel, err = s.ReadAgentReactionPolicy(ctx, "tenant-a", "room-a")
	if err != nil || workspace.ReactionsEnabled || channel == nil || channel.ReactionsEnabled {
		t.Fatalf("after the workspace rewrite: %+v/%v %v", workspace, channel, err)
	}
	// Another tenant sees none of it.
	foreign, foreignChannel, err := s.ReadAgentReactionPolicy(ctx, "tenant-b", "room-a")
	if err != nil || !foreign.ReactionsEnabled || foreignChannel != nil {
		t.Fatalf("cross-tenant read: %+v/%v %v", foreign, foreignChannel, err)
	}
}
