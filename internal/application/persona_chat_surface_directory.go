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
}

var _ personachat.Surface = (*PersonaChatSurface)(nil)

func personaSurfacePrincipal(ctx context.Context) (*trust.Principal, bool) {
	p, ok := trust.FromContext(ctx)
	return p, ok && p != nil && p.SubjectKind() == trust.SubjectKindHuman
}

func (s *PersonaChatSurface) member(ctx context.Context, conversationID string) (*trust.Principal, chat.Conversation, error) {
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
	if room.ID != conversationID || room.TenantID != principal.TenantID || room.Archived {
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
	p, room, err := s.member(ctx, conversationID)
	if err != nil {
		return personachat.Directory{}, err
	}
	if s.References == nil || s.Personas == nil || s.Skills == nil {
		return personachat.Directory{}, personachat.ErrUnavailable
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
	// indistinguishable from a conversation with no agent.
	omitted := func(item, reason string) {
		slog.InfoContext(ctx, "hcmnext.persona_projection_item_omitted", "projection", "chat_directory", "item_type", "persona_reference", "item_id", item, "conversation_id", room.ID, "reason", reason)
	}
	if len(profiles) == 0 || len(candidates) == 0 {
		slog.InfoContext(ctx, "hcmnext.persona_projection_empty", "projection", "chat_directory", "conversation_id", room.ID, "available_profiles", len(profiles), "reference_candidates", len(candidates))
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
			discovered, err := s.Skills.Discover(ctx, p, personaChatReplyPurpose)
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
			})
			break
		}
		if !matched {
			omitted(candidate.ID, "no_available_profile_for_reference")
		}
	}
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
