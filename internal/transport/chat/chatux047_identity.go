package chat

import (
	"context"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATUX-047: a list answer names the people and agents it carries. A client
// that drew a row from the id alone had to wait for the people directory and the
// agent directory before the row had a name, so for several seconds an author
// line read "..." and a direct-message row was an empty tile. The service the
// transport fronts may also implement IdentitySource; when it does, the answer
// carries each author's and each direct peer's name and picture beside the ids,
// and the directory reads a client makes later only refine them. A service that
// does not implement it answers exactly as before.

// IdentitySource names subjects for the caller. It is best effort by contract:
// a subject it cannot name is left out of the answer and the client falls back
// to the directory, so it never fails a read.
type IdentitySource interface {
	// Peers names the other side of each direct conversation in rooms, keyed by
	// conversation id. A conversation the source cannot resolve is left out.
	Peers(ctx context.Context, caller chatcore.Principal, rooms []chatcore.Conversation) map[string]*chatv1.Identity
	// Authors names the given subjects of one host tenant, keyed by subject id.
	Authors(ctx context.Context, caller chatcore.Principal, tenantID string, subjectIDs []string) map[string]*chatv1.Identity
}

func (s *server) identitySource() IdentitySource {
	source, _ := s.deps.Service.(IdentitySource)
	return source
}

// namePeers sets Conversation.peer on the rows of a direct conversation the
// source can resolve. rooms and out are the same rows, in the same order.
func (s *server) namePeers(ctx context.Context, caller chatcore.Principal, rooms []chatcore.Conversation, out []*chatv1.Conversation) {
	source := s.identitySource()
	if source == nil || len(rooms) == 0 {
		return
	}
	direct := make([]chatcore.Conversation, 0, len(rooms))
	for _, room := range rooms {
		if room.Kind == chatcore.Direct {
			direct = append(direct, room)
		}
	}
	if len(direct) == 0 {
		return
	}
	peers := source.Peers(ctx, caller, direct)
	for i, room := range rooms {
		if peer := peers[room.ID]; peer != nil && i < len(out) && out[i] != nil {
			out[i].Peer = peer
		}
	}
}

// nameAuthors sets Post.author on the posts written by a subject of the host
// tenant the source can name.
func (s *server) nameAuthors(ctx context.Context, caller chatcore.Principal, tenantID string, posts []*chatv1.Post) {
	source := s.identitySource()
	if source == nil || len(posts) == 0 {
		return
	}
	seen := make(map[string]bool, len(posts))
	ids := make([]string, 0, len(posts))
	for _, post := range posts {
		if post == nil || post.GetAuthorId() == "" || (post.GetAuthorHomeTenantId() != "" && post.GetAuthorHomeTenantId() != tenantID) || seen[post.GetAuthorId()] {
			continue
		}
		seen[post.GetAuthorId()] = true
		ids = append(ids, post.GetAuthorId())
	}
	if len(ids) == 0 {
		return
	}
	names := source.Authors(ctx, caller, tenantID, ids)
	for _, post := range posts {
		if post == nil || (post.GetAuthorHomeTenantId() != "" && post.GetAuthorHomeTenantId() != tenantID) {
			continue
		}
		if author := names[post.GetAuthorId()]; author != nil {
			post.Author = author
		}
	}
}
