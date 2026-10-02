package application

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// AGENTUX-070: a channel's administrator can require every agent answer in the
// channel to be private. The requirement is the channel's own persona policy
// (the room ceiling Chat reads on each use), not a property of any agent or
// installation, so it outranks both and takes effect on the next answer.

// personaChannelPolicies is the channel's current persona policy: read as a
// manager of the channel, and written with the revision that was read.
type personaChannelPolicies interface {
	CapturePersonaChannelPolicy(ctx context.Context, tenant, conversation, manager string) (chatstore.PersonaChannelPolicySnapshot, error)
	PutPersonaChannelPolicy(ctx context.Context, tenant, conversation string, expectedRevision int64, policy chatstore.PersonaChannelPolicy) (int64, error)
}

// channelPrivacy is the requirement as the viewer may see it. A channel with no
// persona policy yet (no agent was ever placed in it) has nothing to read, and
// the page shows no setting.
func (s *PersonaChatSurface) channelPrivacy(ctx context.Context, tenant, subject string, room chat.Conversation) *personachat.ChannelPrivacy {
	if s.ChannelPolicies == nil || room.Kind == chat.Direct {
		return nil
	}
	snapshot, err := s.ChannelPolicies.CapturePersonaChannelPolicy(ctx, tenant, room.ID, "")
	if err != nil || snapshot.PolicyRevision <= 0 {
		return nil
	}
	out := &personachat.ChannelPrivacy{Private: snapshot.Policy.AlwaysPrivate}
	// A manager of the channel may change it: the read that names the manager
	// succeeds only for one.
	if _, err := s.ChannelPolicies.CapturePersonaChannelPolicy(ctx, tenant, room.ID, subject); err == nil {
		out.CanChange = true
	}
	return out
}

// SetChannelPrivacy requires, or stops requiring, private agent answers in a
// channel. Only a manager of the channel may; the change keeps every other part
// of the room's policy as it was and is refused if the policy changed since it
// was read.
func (s *PersonaChatSurface) SetChannelPrivacy(ctx context.Context, conversationID string, private bool) (personachat.ChannelPrivacy, error) {
	if s == nil || s.ChannelPolicies == nil {
		return personachat.ChannelPrivacy{}, personachat.ErrUnavailable
	}
	if conversationID == "" || conversationID != strings.TrimSpace(conversationID) {
		return personachat.ChannelPrivacy{}, personachat.ErrInvalid
	}
	p, room, err := s.member(ctx, conversationID)
	if err != nil {
		return personachat.ChannelPrivacy{}, err
	}
	if room.Kind == chat.Direct {
		return personachat.ChannelPrivacy{}, personachat.ErrDenied
	}
	snapshot, err := s.ChannelPolicies.CapturePersonaChannelPolicy(ctx, room.TenantID, room.ID, p.Subject())
	switch {
	case errors.Is(err, chatstore.ErrNotMember):
		return personachat.ChannelPrivacy{}, personachat.ErrDenied
	case err != nil || snapshot.PolicyRevision <= 0:
		return personachat.ChannelPrivacy{}, personachat.ErrUnavailable
	}
	policy := snapshot.Policy
	if policy.AlwaysPrivate != private {
		policy.AlwaysPrivate = private
		policy.AllowedDataClasses = slices.Clone(policy.AllowedDataClasses)
		policy.AllowedChannelClasses = slices.Clone(policy.AllowedChannelClasses)
		if _, err := s.ChannelPolicies.PutPersonaChannelPolicy(ctx, room.TenantID, room.ID, snapshot.PolicyRevision, policy); err != nil {
			if errors.Is(err, chatstore.ErrAudiencePolicyConflict) {
				return personachat.ChannelPrivacy{}, personachat.ErrConflict
			}
			return personachat.ChannelPrivacy{}, personachat.ErrUnavailable
		}
	}
	return personachat.ChannelPrivacy{Private: private, CanChange: true}, nil
}

var _ personachat.ChannelPrivacySurface = (*PersonaChatSurface)(nil)
