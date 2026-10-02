package chatstream

import "context"

// SignalSidebarLayoutChanged names the person-scoped notice that one of the
// person's sidebar layouts was saved. Its payload is the new revision in
// decimal. It tells the person's other tabs to read the layout again; it carries
// no layout, so nothing private rides on the stream.
const SignalSidebarLayoutChanged = "sidebar.layout.changed"

// PublishSignal hands a person-scoped notice to every live subscription the
// person holds, whichever conversation it watches, and to nobody else. It is not
// a conversation event: it has no sequence, never moves a subscriber's cursor
// and is not replayed, so a subscriber that misses one (queue full, no
// subscription open) simply reads the state on its next periodic read. It
// returns how many subscriptions it reached.
func (s *Stream) PublishSignal(_ context.Context, tenantID, subjectID, signal string, payload []byte) int {
	if s == nil || s.h == nil || tenantID == "" || subjectID == "" || signal == "" {
		return 0
	}
	s.h.mu.RLock()
	var subscribers []*Subscription
	for _, group := range s.h.subs {
		for sub := range group {
			if sub.access.SubjectID == subjectID && sub.access.HomeTenantID == tenantID {
				subscribers = append(subscribers, sub)
			}
		}
	}
	s.h.mu.RUnlock()
	reached := 0
	event := Event{TenantID: tenantID, Signal: signal, RecipientSubjectID: subjectID, RecipientHomeTenantID: tenantID, Payload: payload}
	for _, sub := range subscribers {
		if sub.deliverSignal(event) {
			reached++
		}
	}
	return reached
}

// deliverSignal queues a signal without touching the sequence or the cursor. A
// full queue drops it rather than closing the subscription: a signal is only a
// hint to read again.
func (s *Subscription) deliverSignal(event Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.queue <- cloneEvent(event):
		return true
	default:
		return false
	}
}
