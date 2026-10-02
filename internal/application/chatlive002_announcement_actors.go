package application

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// personaAnnouncementAuthor is a registered agent identity of the tenant, with
// the name its latest stored version carries.
type personaAnnouncementAuthor struct {
	PersonaID, AgentID, Display, Version string
}

// personaAnnouncementAuthors resolves a post's author id to the tenant's
// registered agent chat identity. It answers for an agent whether or not that
// agent is installed in the room, and never for a person.
type personaAnnouncementAuthors interface {
	ResolveAnnouncementAuthor(ctx context.Context, tenant, authorID string) (personaAnnouncementAuthor, bool, error)
}

type personaStoreAnnouncementAuthors struct{ store *agentpersonastore.Store }

func (s personaStoreAnnouncementAuthors) ResolveAnnouncementAuthor(ctx context.Context, tenant, authorID string) (personaAnnouncementAuthor, bool, error) {
	if s.store == nil || strings.TrimSpace(authorID) == "" {
		return personaAnnouncementAuthor{}, false, personachat.ErrUnavailable
	}
	identities, err := s.store.ListPersonaChatIdentities(ctx, tenant)
	if err != nil {
		return personaAnnouncementAuthor{}, false, personachat.ErrUnavailable
	}
	for _, identity := range identities {
		if !identity.Active || identity.TenantID != values.TenantId(tenant) || (identity.PersonaID != authorID && identity.AgentID != authorID) {
			continue
		}
		scoped, err := s.store.Scoped(values.TenantId(tenant))
		if err != nil {
			return personaAnnouncementAuthor{}, false, personachat.ErrUnavailable
		}
		versions, err := scoped.ListVersions(ctx, identity.PersonaID)
		if err != nil {
			return personaAnnouncementAuthor{}, false, personachat.ErrUnavailable
		}
		var latest agentpersonastore.PersonaVersion
		for _, version := range versions {
			if version.PersonaID == identity.PersonaID && version.Version > latest.Version {
				latest = version
			}
		}
		if latest.Version == 0 || strings.TrimSpace(latest.DisplayName) == "" {
			return personaAnnouncementAuthor{}, false, nil
		}
		return personaAnnouncementAuthor{PersonaID: identity.PersonaID, AgentID: identity.AgentID, Display: latest.DisplayName, Version: strconv.FormatInt(latest.Version, 10)}, true, nil
	}
	return personaAnnouncementAuthor{}, false, nil
}

// announcementPostActors attests the agent author of every announcement post
// the reader can see in room. Announcements are posted under the agent's
// persona id, which the room's agent directory does not list when the agent is
// not installed here, so the author is checked against the tenant's registered
// agent identities instead. A post whose author is a person, or whose body is
// not a well-formed announcement, is never attested: a person who types the
// announcement envelope gets an ordinary message.
func (s *PersonaChatSurface) announcementPostActors(ctx context.Context, p *trust.Principal, room chat.Conversation, known []personachat.PostActor) ([]personachat.PostActor, error) {
	if s.Authors == nil {
		return known, nil
	}
	principal := chat.Principal{TenantID: room.TenantID, SubjectID: p.Subject()}
	attested := make(map[string]bool, len(known))
	for _, actor := range known {
		attested[actor.PostID] = true
	}
	resolved := make(map[string]*personaAnnouncementAuthor)
	out := slices.Clone(known)
	page := chat.Page{PageSize: 200}
	seen := make(map[string]bool)
	for {
		posts, err := s.Chat.ListPosts(ctx, chat.ListPostsRequest{Principal: principal, TenantID: room.TenantID, ConversationID: room.ID, Page: page})
		if err != nil {
			return nil, surfaceChatError(err)
		}
		for _, post := range posts.Posts {
			if post.TenantID != room.TenantID || post.ConversationID != room.ID || (post.AuthorHomeTenantID != "" && post.AuthorHomeTenantID != room.TenantID) || post.Deleted || post.Revision != 1 || attested[post.ID] || !strings.HasPrefix(post.Body, chatui.AgentAnnouncementBodyPrefix) {
				continue
			}
			if _, ok := chatui.DecodeAnnouncementMessageBody(post.Body); !ok {
				continue
			}
			author, done := resolved[post.AuthorID]
			if !done {
				found, ok, err := s.Authors.ResolveAnnouncementAuthor(ctx, room.TenantID, post.AuthorID)
				if err != nil {
					return nil, err
				}
				author = nil
				if ok {
					author = &found
				}
				resolved[post.AuthorID] = author
			}
			if author == nil {
				continue
			}
			out = append(out, personachat.PostActor{PostID: post.ID, PersonaID: author.PersonaID, AgentID: author.AgentID, Display: author.Display, PersonaVersion: author.Version})
		}
		if posts.NextCursor == "" {
			break
		}
		if seen[posts.NextCursor] {
			return nil, personachat.ErrUnavailable
		}
		seen[posts.NextCursor], page.Cursor = true, posts.NextCursor
	}
	slices.SortFunc(out, func(a, b personachat.PostActor) int { return strings.Compare(a.PostID, b.PostID) })
	return out, nil
}

// announcementAuthorsFor is nil when the cell has no persona store, so a room
// without one keeps answering its directory.
func announcementAuthorsFor(store *agentpersonastore.Store) personaAnnouncementAuthors {
	if store == nil {
		return nil
	}
	return personaStoreAnnouncementAuthors{store: store}
}
