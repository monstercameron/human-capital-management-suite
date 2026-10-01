package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

var (
	errPersonaDMResolverUnavailable = errors.New("application: persona DM resolver unavailable")
	errPersonaDMAmbiguous           = errors.New("application: persona DM is ambiguous")
)

// personaDMStore is the read-only portion of the chat store needed to resolve
// a persona DM. The store remains authoritative for tenant and membership
// state; callers never provide a conversation ID.
type personaDMStore interface {
	ListConversations(context.Context, chatcore.Principal, string, chatcore.Page, chatcore.ConversationScope) (chatcore.ListConversationsResponse, error)
	ListMemberships(context.Context, string, string, chatcore.Page) (chatcore.ListMembershipsResponse, error)
}

// PersonaDMResolver resolves the configured persona's actual one-to-one chat
// with the authenticated invoker. Persona identifies a server-owned chat
// identity and is never taken from a request.
type PersonaDMResolver struct {
	Store   personaDMStore
	Persona chatcore.MemberRef
}

// NewPersonaDMResolver constructs a resolver bound to one server-owned
// persona identity. The persona identity must be tenant-scoped before the
// resolver can be installed in chat composition.
func NewPersonaDMResolver(store chatcore.Store, persona chatcore.MemberRef) (PersonaDMResolver, error) {
	if store == nil || strings.TrimSpace(persona.TenantID) == "" || strings.TrimSpace(persona.SubjectID) == "" {
		return PersonaDMResolver{}, errPersonaDMResolverUnavailable
	}
	return PersonaDMResolver{Store: store, Persona: persona}, nil
}

// ResolvePersonaDM returns the unique active direct conversation containing
// both the invoker and the configured persona. It deliberately lists only
// conversations in which the invoker is an active member, and rejects every
// result that is not an exact two-member direct conversation.
func (r PersonaDMResolver) ResolvePersonaDM(ctx context.Context, principal chatcore.Principal, tenant string) (string, error) {
	if r.Store == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(principal.TenantID) == "" ||
		strings.TrimSpace(principal.SubjectID) == "" || principal.TenantID != tenant ||
		strings.TrimSpace(r.Persona.TenantID) == "" || strings.TrimSpace(r.Persona.SubjectID) == "" ||
		r.Persona.TenantID != tenant {
		return "", errPersonaDMResolverUnavailable
	}

	page := chatcore.Page{PageSize: 200}
	var found string
	for {
		listed, err := r.Store.ListConversations(ctx, principal, tenant, page, chatcore.ConversationScope{})
		if err != nil {
			return "", fmt.Errorf("%w: list conversations: %v", errPersonaDMResolverUnavailable, err)
		}
		for _, conversation := range listed.Conversations {
			if conversation.TenantID != tenant || conversation.Kind != chatcore.Direct || conversation.Archived {
				continue
			}
			matches, err := r.matchesConversation(ctx, tenant, conversation.ID, principal)
			if err != nil {
				return "", err
			}
			if !matches {
				continue
			}
			if found != "" && found != conversation.ID {
				return "", errPersonaDMAmbiguous
			}
			found = conversation.ID
		}
		if listed.NextCursor == "" {
			break
		}
		page.Cursor = listed.NextCursor
	}
	if found == "" {
		return "", chatcore.ErrPermissionDenied
	}
	return found, nil
}

func (r PersonaDMResolver) matchesConversation(ctx context.Context, tenant, conversationID string, principal chatcore.Principal) (bool, error) {
	page := chatcore.Page{PageSize: 200}
	count := 0
	invokerFound, personaFound := false, false
	for {
		listed, err := r.Store.ListMemberships(ctx, tenant, conversationID, page)
		if err != nil {
			return false, fmt.Errorf("%w: list memberships: %v", errPersonaDMResolverUnavailable, err)
		}
		for _, member := range listed.Memberships {
			if member.TenantID != tenant || member.ConversationID != conversationID || member.LeftAt != nil {
				continue
			}
			count++
			if member.HomeTenantID == principal.TenantID && member.SubjectID == principal.SubjectID {
				invokerFound = true
			}
			if member.HomeTenantID == r.Persona.TenantID && member.SubjectID == r.Persona.SubjectID {
				personaFound = true
			}
		}
		if listed.NextCursor == "" {
			break
		}
		page.Cursor = listed.NextCursor
	}
	return count == 2 && invokerFound && personaFound, nil
}

var _ chatcore.PersonaDMResolver = PersonaDMResolver{}
