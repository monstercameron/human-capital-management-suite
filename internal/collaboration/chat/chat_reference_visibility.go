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
	// CHATBUG-014: a source cited by several answers on the page is resolved
	// once for this read (chatperf_sources.go).
	sources := newAgentSourceMemo(s.agentSourceAccess)
	for i, post := range posts {
		projected[i] = post
		projected[i].Body = projectAgentSourcesWith(ctx, sources, p, post.TenantID, post.ConversationID, post.Body)
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
	// CHATMOD-002: what a reader sees of a filtered message is decided here, on
	// the server, for every history read, search hit, pin and live event.
	s.maskReaderBodies(ctx, p, projected)
	return projected
}

// ProjectWatchEvent is the reader projection of one watch event: the agent
// sources of a post or private answer are decided for this reader, and a
// conversation reference the reader may not open is made inert. A watch that
// does not run on this service (the served stream) calls it for every event so a
// live answer is projected exactly as the same answer read from history.
func (s *Service) ProjectWatchEvent(ctx context.Context, p Principal, tenant, conversation string, event WatchEvent) WatchEvent {
	if event.Event.Post != nil {
		post := s.projectConversationReferences(ctx, p, []Post{*event.Event.Post})[0]
		event.Event.Post = &post
	}
	if event.EphemeralDelivery != nil {
		delivery := *event.EphemeralDelivery
		delivery.Body = s.projectAgentSources(ctx, p, tenant, conversation, delivery.Body)
		event.EphemeralDelivery = &delivery
	}
	if event.Event.Pin != nil && event.Event.Pin.Post != nil {
		post := s.projectConversationReferences(ctx, p, []Post{*event.Event.Pin.Post})[0]
		pin := *event.Event.Pin
		pin.Post = &post
		event.Event.Pin = &pin
	}
	return event
}

func (s *Service) projectWatchEvents(ctx context.Context, p Principal, tenant, conversation string, events <-chan WatchEvent, failures <-chan error) (<-chan WatchEvent, <-chan error) {
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
				event = s.ProjectWatchEvent(ctx, p, tenant, conversation, event)
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
