package chatrecords

import (
	"context"
	"sort"
)

// Issue 3778, slice 1: a read-only preview of what the saved retention policy
// would remove. It reads the tenant's record inventory and holds, groups every
// derived copy with the record it came from, and asks EvaluateRetention which
// groups are due. It never disposes, appends or writes anything.

// RetentionKindCount is how many records of one kind a preview would remove.
type RetentionKindCount struct {
	Kind  Kind
	Count int
}

// RetentionPreviewReport is the answer shown to a retention administrator.
type RetentionPreviewReport struct {
	PolicyConfigured bool
	Mode             RetentionMode
	PolicyRevision   uint64
	// Scanned counts the records still in the inventory (not already disposed).
	Scanned int
	// WouldRemove counts the records a sweep would remove, by kind, ordered by kind.
	WouldRemove      []RetentionKindCount
	WouldRemoveTotal int
	// HeldSkipped counts records kept because a legal hold covers them (or the
	// hold is not known). UnknownSkipped counts records kept because every
	// copy of them could not be proven.
	HeldSkipped, UnknownSkipped int
	// UsageUnknown is true for a storage budget: chat has no byte accounting
	// for media and derived copies, so a budget removes nothing until it has.
	UsageUnknown bool
}

type retentionGroup struct {
	root     Record
	members  []Record
	holds    map[string]bool
	complete bool
}

// PreviewRetention reports what the tenant-wide policy would remove. The caller
// must be authorised to read retention before anything is read.
func (s *Service) PreviewRetention(ctx context.Context, actor, tenant string) (RetentionPreviewReport, error) {
	if _, err := s.authorize(ctx, actor, tenant, "retention.preview", tenant); err != nil {
		return RetentionPreviewReport{}, err
	}
	repo, ok := s.Repo.(RetentionRepository)
	if !ok {
		return RetentionPreviewReport{}, ErrInvalid
	}
	policy, found, err := repo.GetRetentionPolicy(ctx, tenant, "")
	if err != nil {
		return RetentionPreviewReport{}, err
	}
	if !found {
		return RetentionPreviewReport{}, nil
	}
	report := RetentionPreviewReport{PolicyConfigured: true, Mode: policy.Mode, PolicyRevision: policy.Revision, UsageUnknown: policy.Mode == RetentionBudget}
	records, err := s.Repo.List(ctx, tenant)
	if err != nil {
		return RetentionPreviewReport{}, err
	}
	holds, err := s.Repo.Holds(ctx, tenant)
	if err != nil {
		return RetentionPreviewReport{}, err
	}
	active := make(map[string]bool, len(holds))
	for _, h := range holds {
		active[h.HoldID] = h.Active()
	}
	conversationHolds := activeConversationHoldIDs(holds)
	now := s.now()
	byID := make(map[string]Record, len(records))
	for _, r := range records {
		if r.TenantID == tenant {
			byID[r.RecordID] = r
		}
	}
	groups := map[string]*retentionGroup{}
	for _, r := range records {
		if r.TenantID != tenant || r.Disposition == "DISPOSED" {
			continue
		}
		report.Scanned++
		rootID, complete := r.RecordID, true
		for cursor, steps := r, 0; cursor.DerivedFrom != ""; steps++ {
			parent, known := byID[cursor.DerivedFrom]
			if !known || steps > len(byID) {
				complete = false
				break
			}
			rootID, cursor = parent.RecordID, parent
		}
		g := groups[rootID]
		if g == nil {
			root := byID[rootID]
			g = &retentionGroup{root: root, holds: map[string]bool{}, complete: true}
			groups[rootID] = g
		}
		g.complete = g.complete && complete
		g.members = append(g.members, r)
		for _, id := range r.HoldIDs {
			g.holds[id] = true
		}
		// A hold on the conversation covers the record (#3779).
		for _, id := range conversationHolds[r.ConversationID] {
			g.holds[id] = true
		}
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]RetentionItem, 0, len(ids))
	for _, id := range ids {
		g := groups[id]
		if g.root.CreatedAt.IsZero() || g.root.CreatedAt.After(now) {
			// A record with no usable date cannot be placed against a cutoff.
			report.UnknownSkipped += len(g.members)
			delete(groups, id)
			continue
		}
		holdIDs := make([]string, 0, len(g.holds))
		for h := range g.holds {
			holdIDs = append(holdIDs, h)
		}
		sort.Strings(holdIDs)
		items = append(items, RetentionItem{
			TenantID: tenant, RecordID: id, ConversationID: g.root.ConversationID, CreatedAt: g.root.CreatedAt,
			// Byte sizes only matter to a storage budget; a date cutoff does not read them.
			BytesKnown: policy.Mode != RetentionBudget, Complete: g.complete, HoldsKnown: true, HoldIDs: holdIDs,
		})
	}
	preview, err := EvaluateRetention(policy, items, active, now)
	if err != nil {
		return RetentionPreviewReport{}, err
	}
	counts := map[Kind]int{}
	for _, c := range preview.Candidates {
		for _, m := range groups[c.RecordID].members {
			counts[m.Kind]++
			report.WouldRemoveTotal++
		}
	}
	for _, b := range preview.Blocked {
		n := len(groups[b.RecordID].members)
		if b.Reason == "LEGAL_HOLD" {
			report.HeldSkipped += n
		} else {
			report.UnknownSkipped += n
		}
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		report.WouldRemove = append(report.WouldRemove, RetentionKindCount{Kind: Kind(k), Count: counts[Kind(k)]})
	}
	return report, nil
}
