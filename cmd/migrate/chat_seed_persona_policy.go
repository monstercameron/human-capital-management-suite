package main

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// provisionChatSeedPersonaPolicies administers only the versioned demo rooms
// created or validated by this operator-invoked seeder. It leaves existing
// review, evaluation, publication and installation evidence untouched.
func provisionChatSeedPersonaPolicies(ctx context.Context, store *chatstore.Store, tenant string, rooms []roomSpec) (int, error) {
	created := 0
	for _, room := range rooms {
		n, err := application.ProvisionLocalDevPersonaChatPolicy(ctx, store, application.ServeProfileLocalDev, tenant, chatSeedConversationID(tenant, room.Key), room.Key)
		if err != nil {
			return created, fmt.Errorf("chat seed persona policy %s: %w", room.Key, err)
		}
		created += n
	}
	return created, nil
}

// chatSeedPersonaPostSource pins the exact just-written seed identity and
// rederives its body and author from the checked-in fictional room corpus.
type chatSeedPersonaPostSource struct {
	in           seedRoomInput
	index, reply int
	id, parent   string
}

func (s chatSeedPersonaPostSource) IsKnownSyntheticPersonaPost(_ context.Context, post chat.Post) bool {
	if s.in.room.Kind != roomPublic || s.index < 0 || len(s.in.room.Members) == 0 || post.ID != s.id || s.id == "" || post.TenantID != s.in.opts.Tenant || post.ConversationID != chatSeedConversationID(s.in.opts.Tenant, s.in.room.Key) || post.ConversationID != s.in.conversation || post.AuthorHomeTenantID != s.in.opts.Tenant || post.ParentID != s.parent || post.Deleted {
		return false
	}
	member := s.in.room.Members[s.index%len(s.in.room.Members)]
	if s.reply >= 0 {
		member = s.in.room.Members[(s.index+s.reply+1)%len(s.in.room.Members)]
	}
	if member < 0 || member >= len(s.in.people) || post.AuthorID != s.in.people[member] {
		return false
	}
	body, _ := seedBody(s.in, s.index, post.AuthorID)
	if s.reply >= 0 {
		body = threadReplies[(s.index+s.reply)%len(threadReplies)]
	}
	return post.Body == body
}

func classifyChatSeedPersonaPost(ctx context.Context, in seedRoomInput, index, reply int, parent string, post chat.Post) error {
	if in.room.Kind != roomPublic || in.classifier == nil {
		return nil
	}
	return in.classifier.ClassifyKnownSyntheticPost(ctx, post, chatSeedPersonaPostSource{in: in, index: index, reply: reply, id: post.ID, parent: parent})
}
