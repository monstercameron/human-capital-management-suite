package chat

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

const restrictedConversationReference = "Restricted conversation"

// projectConversationReferences returns only conversation references the
// current reader can open. A restricted reference keeps its type so clients
// can render an inert placeholder, while dropping every target identifier and
// the sender's display snapshot to avoid a discovery leak.
func (s *Service) projectConversationReferences(ctx context.Context, p Principal, posts []Post) []Post {
	projected := make([]Post, len(posts))
	visible := make(map[string]bool)
	for i, post := range posts {
		projected[i] = post
		projected[i].References = append([]Reference(nil), post.References...)
		for j, ref := range projected[i].References {
			if ref.Kind != ConversationMention {
				continue
			}
			key := ref.TenantID + "\x00" + ref.ConversationID
			allowed, known := visible[key]
			if !known {
				target, err := s.store.GetConversation(ctx, ref.TenantID, ref.ConversationID)
				allowed = err == nil && s.authorize(ctx, p, target, chatpolicy.ActionRead) == nil
				visible[key] = allowed
			}
			if !allowed {
				projected[i].References[j] = Reference{Kind: ConversationMention, Display: restrictedConversationReference}
			}
		}
	}
	return projected
}

func (s *Service) projectWatchEvents(ctx context.Context, p Principal, events <-chan WatchEvent, failures <-chan error) (<-chan WatchEvent, <-chan error) {
	projected := make(chan WatchEvent)
	var projectedFailures chan error
	if failures != nil {
		projectedFailures = make(chan error, 1)
	}
	go func() {
		defer close(projected)
		if projectedFailures != nil {
			defer close(projectedFailures)
		}
		for events != nil || failures != nil {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				if event.Event.Post != nil {
					post := s.projectConversationReferences(ctx, p, []Post{*event.Event.Post})[0]
					event.Event.Post = &post
				}
				if event.Event.Pin != nil && event.Event.Pin.Post != nil {
					post := s.projectConversationReferences(ctx, p, []Post{*event.Event.Pin.Post})[0]
					pin := *event.Event.Pin
					pin.Post = &post
					event.Event.Pin = &pin
				}
				select {
				case projected <- event:
				case <-ctx.Done():
					return
				}
			case err, ok := <-failures:
				if !ok {
					failures = nil
					continue
				}
				select {
				case projectedFailures <- err:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return projected, projectedFailures
}
