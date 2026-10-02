package application

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaSurfaceChat interface {
	GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
	ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error)
	ListPosts(context.Context, chat.ListPostsRequest) (chat.ListPostsResponse, error)
	SendPost(context.Context, chat.SendPostRequest) (chat.Post, error)
}

type personaSurfaceInvocations interface {
	ListPersonaInvocations(context.Context, string, string, string) ([]agentinvoke.Invocation, error)
	Lookup(context.Context, string, string, string) (agentinvoke.Invocation, error)
}

// PersonaChatSurface supplies the server-filtered browser directory and live,
// invoker-only execution state. Each call resolves current chat membership.
type PersonaChatSurface struct {
	Chat                 personaSurfaceChat
	References           personaChatReferenceSource
	Personas             AvailablePersonaReader
	Skills               AgentSkillDiscoverer
	Invocations          personaSurfaceInvocations
	Failures             personaPostFailureStore
	Receipts             personaReplyReceiptStore
	Tasks                PersonaInvocationTaskReader
	Authors              personaAnnouncementAuthors
	ChannelAlwaysPrivate func(context.Context, string, personaReferenceFacts) (bool, error)
	Executions           func(context.Context, string) (runstate.Store, error)
	Now                  func() time.Time
	// Share is the check made before a private answer is posted to its channel
	// (AGENTUX-070). Unset, sharing is unavailable.
	Share *PersonaAnswerShareGate
	// Cards reads the private cards a person was delivered. The served chat
	// service is a chain of decorators that does not pass that read through, so
	// the composition hands the surface the service that holds it; without it
	// "Share to channel" was refused for every answer (CHATBUG-067). Unset, the
	// surface asks Chat itself.
	Cards personaPrivateCardReader
	// memory is what the surface remembers between reads (CHATBUG-075). The
	// served composition supplies it; without it every read stands alone.
	memory *personaSurfaceMemory
	// Steps holds what each run in flight is doing now, for the working line under
	// the question (AGENTUX-075). Unset, the line follows the run's checkpoints.
	Steps *personaRunStepBoard
	// ChannelPolicies is the channel's persona policy, where a channel's
	// administrator requires private agent answers (AGENTUX-070). Unset, the
	// setting is not offered.
	ChannelPolicies personaChannelPolicies
}

// personaPrivateCardReader lists the private cards delivered to one person in
// one conversation. It checks that person's membership itself.
type personaPrivateCardReader interface {
	ListEphemeralPosts(context.Context, chat.ListEphemeralPostsRequest) ([]chat.EphemeralPost, uint64, error)
}

// privateCards is the reader of private cards this surface was composed with.
func (s *PersonaChatSurface) privateCards() (personaPrivateCardReader, bool) {
	if s.Cards != nil {
		return s.Cards, true
	}
	reader, ok := s.Chat.(personaPrivateCardReader)
	return reader, ok
}

var _ personachat.Surface = (*PersonaChatSurface)(nil)

func personaSurfacePrincipal(ctx context.Context) (*trust.Principal, bool) {
	p, ok := trust.FromContext(ctx)
	return p, ok && p != nil && p.SubjectKind() == trust.SubjectKindHuman
}

// member resolves the caller as a current member of a conversation in which
// something may still be done: asking again, sharing, rating, stopping. An
// archived conversation is refused, because nothing can be started there.
func (s *PersonaChatSurface) member(ctx context.Context, conversationID string) (*trust.Principal, chat.Conversation, error) {
	p, room, err := s.reader(ctx, conversationID)
	if err != nil {
		return nil, chat.Conversation{}, err
	}
	if room.Archived {
		return nil, chat.Conversation{}, personachat.ErrDenied
	}
	return p, room, nil
}

// reader resolves the caller as a current member of a conversation for a read.
// An archived conversation is still read by its members: they see who answered
// in it and what became of their own questions. The page asks for both on every
// load of the conversation, and used to be answered 403 three times over for an
// archived channel.
func (s *PersonaChatSurface) reader(ctx context.Context, conversationID string) (*trust.Principal, chat.Conversation, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return nil, chat.Conversation{}, personachat.ErrUnauthenticated
	}
	if p.SubjectKind() != trust.SubjectKindHuman {
		return nil, chat.Conversation{}, personachat.ErrDenied
	}
	at := time.Now().UTC()
	if s != nil && s.Now != nil {
		at = s.Now().UTC()
	}
	if !p.ExpiresAt().After(at) {
		return nil, chat.Conversation{}, personachat.ErrUnauthenticated
	}
	if strings.TrimSpace(conversationID) != conversationID || conversationID == "" {
		return nil, chat.Conversation{}, personachat.ErrInvalid
	}
	if s == nil || s.Chat == nil {
		return nil, chat.Conversation{}, personachat.ErrUnavailable
	}
	principal := chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
	room, err := s.Chat.GetConversation(ctx, chat.GetConversationRequest{Principal: principal, TenantID: principal.TenantID, ConversationID: conversationID})
	if err != nil {
		return nil, chat.Conversation{}, surfaceChatError(err)
	}
	if room.ID != conversationID || room.TenantID != principal.TenantID {
		return nil, chat.Conversation{}, personachat.ErrDenied
	}
	page := chat.Page{PageSize: 200}
	seen := make(map[string]bool)
	for {
		members, err := s.Chat.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: principal, TenantID: principal.TenantID, ConversationID: room.ID, Page: page})
		if err != nil {
			return nil, chat.Conversation{}, surfaceChatError(err)
		}
		for _, m := range members.Memberships {
			if m.TenantID == principal.TenantID && m.ConversationID == room.ID && m.HomeTenantID == principal.TenantID && m.SubjectID == p.Subject() && m.JoinedAt != nil && m.LeftAt == nil {
				return p, room, nil
			}
		}
		if members.NextCursor == "" {
			break
		}
		if seen[members.NextCursor] {
			return nil, chat.Conversation{}, personachat.ErrUnavailable
		}
		seen[members.NextCursor], page.Cursor = true, members.NextCursor
	}
	return nil, chat.Conversation{}, personachat.ErrDenied
}

func surfaceChatError(err error) error {
	if errors.Is(err, chat.ErrPermissionDenied) || errors.Is(err, chat.ErrNotFound) {
		return personachat.ErrDenied
	}
	return personachat.ErrUnavailable
}

// Directory joins current canonical references to their sealed profile. It
// never returns hidden profiles, instructions, audience facts or grant data.
func (s *PersonaChatSurface) Directory(ctx context.Context, conversationID string) (personachat.Directory, error) {
	p, room, err := s.reader(ctx, conversationID)
	if err != nil {
		return personachat.Directory{}, err
	}
	if s.References == nil || s.Personas == nil || s.Skills == nil {
		return personachat.Directory{}, personachat.ErrUnavailable
	}
	if room.Archived {
		// Nobody can be asked anything in an archived conversation, so it lists
		// no agent to mention. The agents that answered there are still named, so
		// its history reads as it did.
		return s.directoryPostActors(ctx, p, room, personachat.Directory{Personas: make([]personachat.Profile, 0)})
	}
	profiles, err := s.Personas.ListAvailable(ctx, p)
	if err != nil {
		return personachat.Directory{}, personachat.ErrUnavailable
	}
	candidates, err := s.References.ListPersonaReferenceCandidates(ctx, chat.Principal{TenantID: room.TenantID, SubjectID: p.Subject()}, room.TenantID, room.ID, "")
	if err != nil {
		return personachat.Directory{}, personachat.ErrUnavailable
	}
	result := personachat.Directory{Personas: make([]personachat.Profile, 0)}
	// Each omission is logged with its reason: an empty agent list is otherwise
	// indistinguishable from a conversation with no agent. The directory is read
	// again and again while a conversation is open, so each finding is logged
	// once per conversation for the life of the process (CHATBUG-075): an
	// omission at INFO, and "no agent here", which is the ordinary state of most
	// conversations, at DEBUG.
	omitted := func(item, reason string) {
		if s.memory.firstTime("omitted\x00" + room.TenantID + "\x00" + room.ID + "\x00" + item + "\x00" + reason) {
			slog.InfoContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "chat_directory", "item_type", "persona_reference", "item_id", item, "conversation_id", room.ID, "reason", reason)
		}
	}
	// CHATBUG-014: the caller's skills are the same for every agent of the
	// conversation, so they are read once for the directory, when the first
	// agent needs them, and not once per agent.
	var discovered []agentskills.SkillRecord
	var discoverErr error
	discoveredRead := false
	discoverSkills := func() ([]agentskills.SkillRecord, error) {
		if !discoveredRead {
			discovered, discoverErr = s.Skills.Discover(ctx, p, personaChatReplyPurpose)
			discoveredRead = true
		}
		return discovered, discoverErr
	}
	if (len(profiles) == 0 || len(candidates) == 0) && s.memory.firstTime("empty\x00"+room.TenantID+"\x00"+room.ID) {
		slog.DebugContext(ctx, "hcmnext.persona_projection_empty", "projection", "chat_directory", "conversation_id", room.ID, "available_profiles", len(profiles), "reference_candidates", len(candidates))
	}
	for _, candidate := range candidates {
		if candidate.Kind != chat.AgentMention {
			continue
		}
		if !candidate.Eligible || candidate.TenantID != room.TenantID || candidate.ConversationID != room.ID {
			omitted(candidate.ID, "candidate_not_eligible")
			continue
		}
		facts, err := s.References.LookupPersonaReference(ctx, room.TenantID, room.ID, candidate.ID)
		at := time.Now().UTC()
		if s.Now != nil {
			at = s.Now().UTC()
		}
		if err != nil || !currentPersonaReference(facts, room.TenantID, room.ID, candidate.ID, at) {
			omitted(candidate.ID, "reference_not_current")
			continue
		}
		matched := false
		for _, version := range profiles {
			profile := version.Profile
			if version.Verify() != nil || profile.PersonaID != facts.PersonaID || uint64(profile.Version) != facts.PersonaVersion || candidate.Display != profile.DisplayName {
				continue
			}
			matched = true
			discovered, err := discoverSkills()
			if err != nil {
				return personachat.Directory{}, personachat.ErrUnavailable
			}
			if !hasExactPinnedSkills(profile.SkillPins, discovered) {
				continue
			}
			byKey := make(map[agentskills.SkillKey]agentskills.SkillRecord, len(discovered))
			for _, skill := range discovered {
				byKey[skill.Definition.Key()] = skill
			}
			skills := make([]personachat.Skill, 0, len(profile.SkillPins))
			for _, pin := range profile.SkillPins {
				skill := byKey[pin.Key()]
				skills = append(skills, personachat.Skill{Name: skill.Definition.Description, Tier: skill.Definition.SideEffectTier.String()})
			}
			placement := "private_always"
			alwaysPrivate := profile.AlwaysPrivate
			if s.ChannelAlwaysPrivate != nil {
				private, err := s.ChannelAlwaysPrivate(ctx, room.TenantID, facts)
				if err != nil {
					continue
				}
				alwaysPrivate = alwaysPrivate || private
			}
			if room.Kind == chat.PublicChannel && !alwaysPrivate {
				placement = "private_audience"
			}
			cannotDo := []string{"Act beyond your current access", "Use skills outside this published version"}
			if profile.TierCeiling < agentskills.TierSubmitGoverned {
				cannotDo = append(cannotDo, "Change governed records")
			}
			if profile.TierCeiling < agentskills.TierExternalWrite {
				cannotDo = append(cannotDo, "Write to external systems")
			}
			classes := normalizeFacts(append(append([]string{}, version.DerivedDataClassesRead...), version.DerivedDataClassesWritten...))
			result.Personas = append(result.Personas, personachat.Profile{
				Reference: personachat.Reference{Kind: string(candidate.Kind), TenantID: room.TenantID, ID: candidate.ID, Display: profile.DisplayName, ConversationID: room.ID},
				Purpose:   profile.Purpose, Owner: profile.Owner, Version: strconv.FormatUint(uint64(profile.Version), 10), Skills: skills,
				DataClasses: classes, CannotDo: cannotDo, ReplyPlacement: placement,
				DocumentScope: personaChatDocumentScope(profile.SkillPins),
				Examples:      append([]string(nil), profile.ExampleQuestions...),
			})
			break
		}
		if !matched {
			omitted(candidate.ID, "no_available_profile_for_reference")
		}
	}
	if len(result.Personas) > 0 {
		// AGENTUX-070: the channel's requirement on agent answers, with the agents it applies to.
		result.ChannelPrivacy = s.channelPrivacy(ctx, room.TenantID, p.Subject(), room)
		if result.ChannelPrivacy != nil && result.ChannelPrivacy.Private {
			// The requirement is the room's own: every agent here answers privately.
			for index := range result.Personas {
				result.Personas[index].ReplyPlacement = "private_always"
			}
		}
	}
	return s.directoryPostActors(ctx, p, room, result)
}

// directoryPostActors adds to the directory the agents that authored messages
// the caller can see in the conversation.
func (s *PersonaChatSurface) directoryPostActors(ctx context.Context, p *trust.Principal, room chat.Conversation, result personachat.Directory) (personachat.Directory, error) {
	actors, err := s.visiblePostActors(ctx, p, room)
	if err != nil {
		return personachat.Directory{}, err
	}
	// CHATLIVE-002: stored announcements are attested by their registered agent author.
	if actors, err = s.announcementPostActors(ctx, p, room, actors); err != nil {
		return personachat.Directory{}, err
	}
	result.PostActors = actors
	return result, nil
}
