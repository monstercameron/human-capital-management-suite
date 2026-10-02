package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func (s *ModeratedAdapter) projectModerationEvent(ctx context.Context, event chat.WatchEvent) (chat.WatchEvent, bool, error) {
	if event.Event.Post != nil {
		p := event.Event.Post
		current, err := s.GetPost(ctx, p.TenantID, p.ConversationID, p.ID)
		if err != nil {
			return chat.WatchEvent{}, false, err
		}
		if current.Deleted {
			event.Event.Post = &current
			event.Event.Kind = chat.PostDeleted
			event.Event.Revision = current.Revision
		}
	}
	var t, cid, id string
	if p := event.Event.Pin; p != nil {
		t, cid, id = p.TenantID, p.ConversationID, p.PostID
	}
	if r := event.Event.Reaction; r != nil {
		t, cid, id = r.TenantID, r.ConversationID, r.PostID
	}
	if id != "" {
		p, err := s.GetPost(ctx, t, cid, id)
		if err != nil {
			return chat.WatchEvent{}, false, err
		}
		if p.Deleted {
			return event, false, nil
		}
	}
	return event, true, nil
}

func (s *ModeratedAdapter) ReadConversationEvents(ctx context.Context, r chat.WatchConversationRequest, after uint64, limit int) (EventPage, error) {
	page, err := s.Adapter.ReadConversationEvents(ctx, r, after, limit)
	if err != nil {
		return EventPage{}, err
	}
	var events []chat.WatchEvent
	for _, event := range page.Events {
		projected, keep, e := s.projectModerationEvent(ctx, event)
		if e != nil {
			return EventPage{}, e
		}
		if keep {
			events = append(events, projected)
		}
	}
	page.Events = events
	return page, nil
}

func (s *ModeratedAdapter) Watch(ctx context.Context, r chat.WatchConversationRequest) (<-chan chat.WatchEvent, error) {
	events, _, err := s.WatchWithErrors(ctx, r)
	return events, err
}

func (s *ModeratedAdapter) WatchWithErrors(ctx context.Context, r chat.WatchConversationRequest) (<-chan chat.WatchEvent, <-chan error, error) {
	child, cancel := context.WithCancel(ctx)
	source, sourceErrors, err := s.Adapter.WatchWithErrors(child, r)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	out := make(chan chat.WatchEvent)
	fail := make(chan error, 1)
	go func() {
		defer cancel()
		defer close(out)
		defer close(fail)
		for source != nil || sourceErrors != nil {
			select {
			case <-child.Done():
				return
			case err, ok := <-sourceErrors:
				if !ok {
					sourceErrors = nil
					continue
				}
				if err != nil {
					fail <- err
					return
				}
			case event, ok := <-source:
				if !ok {
					source = nil
					continue
				}
				projected, keep, e := s.projectModerationEvent(child, event)
				if e != nil {
					fail <- e
					return
				}
				if keep {
					select {
					case out <- projected:
					case <-child.Done():
						return
					}
				}
			}
		}
	}()
	return out, fail, nil
}

// CanDeliverPost is the notification/agent-context fence. Queued deliveries
// must recheck immediately before showing a post; old outbox bodies are records.
func (s *ModeratedAdapter) CanDeliverPost(ctx context.Context, t, cid, id string) (bool, error) {
	p, err := s.GetPost(ctx, t, cid, id)
	return err == nil && !p.Deleted, err
}
