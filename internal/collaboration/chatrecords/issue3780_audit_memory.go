package chatrecords

import "context"

// ListAudit is the in-memory audit read (#3780): newest first, tenant-scoped.
// An event belongs to a conversation when it targets the conversation or a
// record the inventory places in it.
func (r *MemoryRepository) ListAudit(_ context.Context, tenant string, q AuditQuery) ([]AuditEvent, error) {
	if tenant == "" {
		return nil, ErrInvalid
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	inConversation := func(target string) bool {
		if q.ConversationID == "" || target == q.ConversationID {
			return true
		}
		for _, rec := range r.records {
			if rec.TenantID == tenant && rec.ConversationID == q.ConversationID && (rec.RecordID == target || rec.SourceID == target) {
				return true
			}
		}
		return false
	}
	events := r.events[tenant]
	out := []AuditEvent{}
	for i := len(events) - 1; i >= 0 && len(out) < q.Limit; i-- {
		e := events[i]
		if q.Before != 0 && e.Sequence >= q.Before {
			continue
		}
		if q.Matches(e) && inConversation(e.TargetID) {
			out = append(out, e)
		}
	}
	return out, nil
}

var _ AuditReadRepository = (*MemoryRepository)(nil)
