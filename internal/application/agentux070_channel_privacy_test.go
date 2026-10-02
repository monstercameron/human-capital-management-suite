package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// agentUX070Policies is the room's persona policy as Chat holds it: one row with
// a revision, readable by anyone and, as a manager, by a manager only.
type agentUX070Policies struct {
	snapshot chatstore.PersonaChannelPolicySnapshot
	managers map[string]bool
	puts     int
}

func (p *agentUX070Policies) CapturePersonaChannelPolicy(_ context.Context, tenant, conversation, manager string) (chatstore.PersonaChannelPolicySnapshot, error) {
	if manager != "" && !p.managers[manager] {
		return chatstore.PersonaChannelPolicySnapshot{}, chatstore.ErrNotMember
	}
	out := p.snapshot
	out.TenantID, out.ConversationID = tenant, conversation
	return out, nil
}

func (p *agentUX070Policies) PutPersonaChannelPolicy(_ context.Context, _, _ string, expected int64, policy chatstore.PersonaChannelPolicy) (int64, error) {
	if expected != p.snapshot.PolicyRevision {
		return 0, chatstore.ErrAudiencePolicyConflict
	}
	p.puts++
	p.snapshot.PolicyRevision++
	p.snapshot.Policy = policy
	return p.snapshot.PolicyRevision, nil
}

func agentUX070Ceiling() chatstore.PersonaChannelPolicy {
	return chatstore.PersonaChannelPolicy{PlacementClass: "ANY_INTERNAL", MaxTier: "T1", AllowedDataClasses: []string{"INTERNAL", "PUBLIC"}, AllowedChannelClasses: []string{"PUBLIC"}, ConversationSearchAllowed: true}
}

// The channel's administrator requires private agent answers in the channel's
// own persona policy. A manager reads and changes it, anyone else only reads it,
// the rest of the room's policy is kept as it was, a change made against an old
// revision is refused, and every agent in a channel that requires it is shown as
// answering privately.
func TestTodo_AGENTUX_070_ChannelPrivacy(t *testing.T) {
	s, ctx, _, _, _ := personaSurfaceFixture(t)
	policies := &agentUX070Policies{snapshot: chatstore.PersonaChannelPolicySnapshot{PolicyRevision: 4, Policy: agentUX070Ceiling()}, managers: map[string]bool{"user-a": true}}
	s.ChannelPolicies = policies

	directory, err := s.Directory(ctx, "channel-a")
	if err != nil || directory.ChannelPrivacy == nil || directory.ChannelPrivacy.Private || !directory.ChannelPrivacy.CanChange || directory.Personas[0].ReplyPlacement != "private_audience" {
		t.Fatalf("a manager's directory = %+v %v", directory.ChannelPrivacy, err)
	}

	got, err := s.SetChannelPrivacy(ctx, "channel-a", true)
	if err != nil || !got.Private || !got.CanChange || policies.puts != 1 || !policies.snapshot.Policy.AlwaysPrivate {
		t.Fatalf("require private = %+v %v puts=%d", got, err, policies.puts)
	}
	if kept := agentUX070Ceiling(); policies.snapshot.Policy.MaxTier != kept.MaxTier || policies.snapshot.Policy.PlacementClass != kept.PlacementClass || len(policies.snapshot.Policy.AllowedDataClasses) != 2 || !policies.snapshot.Policy.ConversationSearchAllowed || policies.snapshot.PolicyRevision != 5 {
		t.Fatalf("the rest of the room's policy changed: %+v", policies.snapshot)
	}
	directory, err = s.Directory(ctx, "channel-a")
	if err != nil || directory.ChannelPrivacy == nil || !directory.ChannelPrivacy.Private || directory.Personas[0].ReplyPlacement != "private_always" {
		t.Fatalf("a channel that requires private answers shows %+v placement=%q %v", directory.ChannelPrivacy, directory.Personas[0].ReplyPlacement, err)
	}
	// Asking for what already stands writes nothing.
	if _, err := s.SetChannelPrivacy(ctx, "channel-a", true); err != nil || policies.puts != 1 {
		t.Fatalf("an unchanged setting was written again: %v puts=%d", err, policies.puts)
	}
	if got, err := s.SetChannelPrivacy(ctx, "channel-a", false); err != nil || got.Private || policies.snapshot.Policy.AlwaysPrivate || policies.puts != 2 {
		t.Fatalf("stop requiring = %+v %v", got, err)
	}

	// Someone who does not manage the channel reads the requirement and cannot change it.
	policies.managers = map[string]bool{}
	directory, err = s.Directory(ctx, "channel-a")
	if err != nil || directory.ChannelPrivacy == nil || directory.ChannelPrivacy.CanChange {
		t.Fatalf("a member's directory = %+v %v", directory.ChannelPrivacy, err)
	}
	puts := policies.puts
	if _, err := s.SetChannelPrivacy(ctx, "channel-a", true); !errors.Is(err, personachat.ErrDenied) || policies.puts != puts {
		t.Fatalf("a member changed the requirement: %v", err)
	}
	if _, err := s.SetChannelPrivacy(context.Background(), "channel-a", true); !errors.Is(err, personachat.ErrUnauthenticated) {
		t.Fatalf("an unauthenticated change = %v", err)
	}
	if _, err := s.SetChannelPrivacy(ctx, " ", true); !errors.Is(err, personachat.ErrInvalid) {
		t.Fatalf("a blank conversation = %v", err)
	}
	s.ChannelPolicies = nil
	if _, err := s.SetChannelPrivacy(ctx, "channel-a", true); !errors.Is(err, personachat.ErrUnavailable) {
		t.Fatalf("a surface with no policy store = %v", err)
	}
	if directory, err := s.Directory(ctx, "channel-a"); err != nil || directory.ChannelPrivacy != nil {
		t.Fatalf("a surface with no policy store offers the setting: %+v %v", directory.ChannelPrivacy, err)
	}
}

// A change made after somebody else's is refused, not written over theirs.
func TestTodo_AGENTUX_070_ChannelPrivacy_Conflict(t *testing.T) {
	s, ctx, _, _, _ := personaSurfaceFixture(t)
	stale := &agentUX070StalePolicies{agentUX070Policies: agentUX070Policies{snapshot: chatstore.PersonaChannelPolicySnapshot{PolicyRevision: 2, Policy: agentUX070Ceiling()}, managers: map[string]bool{"user-a": true}}}
	s.ChannelPolicies = stale
	if _, err := s.SetChannelPrivacy(ctx, "channel-a", true); !errors.Is(err, personachat.ErrConflict) || stale.puts != 0 {
		t.Fatalf("a change over a newer policy = %v puts=%d", err, stale.puts)
	}
}

// agentUX070StalePolicies is a room whose policy is changed by someone else
// between the read and the write.
type agentUX070StalePolicies struct{ agentUX070Policies }

func (p *agentUX070StalePolicies) CapturePersonaChannelPolicy(ctx context.Context, tenant, conversation, manager string) (chatstore.PersonaChannelPolicySnapshot, error) {
	out, err := p.agentUX070Policies.CapturePersonaChannelPolicy(ctx, tenant, conversation, manager)
	p.snapshot.PolicyRevision++
	return out, err
}
