package chatrecords

import (
	"context"
	"fmt"
	"time"
)

// PutHoldAudited places a hold and appends its audit and outbox rows together.
func (r *MemoryRepository) PutHoldAudited(_ context.Context, h Hold, event AuditEvent) (AuditEvent, error) {
	if h.TenantID == "" || h.HoldID == "" || h.Reason == "" || h.PlacedBy == "" || h.ConversationID == "" || event.TenantID != h.TenantID || event.Action != ActionHoldPlace || event.ActorID != h.PlacedBy || event.PolicyEvidence == "" {
		return AuditEvent{}, ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.holds[h.TenantID] {
		if x.HoldID == h.HoldID {
			return AuditEvent{}, ErrConflict
		}
	}
	r.holds[h.TenantID] = append(r.holds[h.TenantID], h)
	return r.appendHoldEvent(h.TenantID, event), nil
}

// ReleaseHoldAudited lifts a hold and appends its audit and outbox rows together.
func (r *MemoryRepository) ReleaseHoldAudited(_ context.Context, tenant, holdID, by, reason string, at time.Time, event AuditEvent) (Hold, AuditEvent, error) {
	if tenant == "" || holdID == "" || by == "" || reason == "" || event.TenantID != tenant || event.Action != ActionHoldRelease || event.ActorID != by || event.PolicyEvidence == "" {
		return Hold{}, AuditEvent{}, ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, x := range r.holds[tenant] {
		if x.HoldID != holdID {
			continue
		}
		if !x.Active() {
			return Hold{}, AuditEvent{}, ErrConflict
		}
		released := at
		x.ReleasedAt, x.ReleasedBy, x.ReleaseReason = &released, by, reason
		r.holds[tenant][i] = x
		return x, r.appendHoldEvent(tenant, event), nil
	}
	return Hold{}, AuditEvent{}, ErrHoldNotFound
}

func (r *MemoryRepository) appendHoldEvent(tenant string, event AuditEvent) AuditEvent {
	event.Sequence = uint64(len(r.events[tenant]) + 1)
	event.EventID = fmt.Sprintf("hold:%s:%d", event.TargetID, event.Sequence)
	event.Digest = DigestEvent(event)
	r.events[tenant] = append(r.events[tenant], event)
	r.outbox[tenant] = append(r.outbox[tenant], OutboxEvent{ID: event.EventID, Sequence: event.Sequence, EventID: event.EventID})
	return event
}

var _ HoldRepository = (*MemoryRepository)(nil)
