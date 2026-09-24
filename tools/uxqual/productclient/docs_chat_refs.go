package productclient

import (
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// projectDocumentChatRefs carries the reader's resolved chat references.
// A locked channel and an unreadable message keep only their key or token,
// whatever else the wire carried.
func projectDocumentChatRefs(response *documentv1.GetDocumentResponse) productui.DocumentChatRefs {
	var refs productui.DocumentChatRefs
	for _, c := range response.GetChannels() {
		if c == nil || c.GetKey() == "" {
			continue
		}
		if c.GetLocked() {
			refs.Channels = append(refs.Channels, productui.DocumentChatChannelReference{Key: c.GetKey(), Locked: true})
			continue
		}
		if c.GetConversationId() == "" || c.GetName() == "" {
			continue
		}
		refs.Channels = append(refs.Channels, productui.DocumentChatChannelReference{Key: c.GetKey(), ConversationID: c.GetConversationId(), Name: c.GetName(), MemberCount: int(c.GetMemberCount()), Joined: c.GetJoined(), Private: c.GetPrivate()})
	}
	for _, p := range response.GetPeople() {
		if p == nil || p.GetKey() == "" || p.GetSubjectId() == "" || p.GetDisplayName() == "" {
			continue
		}
		refs.People = append(refs.People, productui.DocumentChatPersonReference{Key: p.GetKey(), SubjectID: p.GetSubjectId(), DisplayName: p.GetDisplayName()})
	}
	for _, m := range response.GetMessages() {
		if m == nil || m.GetToken() == "" {
			continue
		}
		if !m.GetReadable() {
			refs.Messages = append(refs.Messages, productui.DocumentChatMessageReference{Token: m.GetToken()})
			continue
		}
		msg := productui.DocumentChatMessageReference{Token: m.GetToken(), Readable: true, ConversationID: m.GetConversationId(), ChannelName: m.GetChannelName(), PostID: m.GetPostId(), AuthorID: m.GetAuthorId(), AuthorName: m.GetAuthorName(), Body: m.GetBody()}
		if at := m.GetCreatedAt(); at != nil && at.IsValid() {
			msg.CreatedAt = at.AsTime().UTC()
		}
		refs.Messages = append(refs.Messages, msg)
	}
	return refs
}
