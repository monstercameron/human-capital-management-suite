package schedule

import "sort"

// ListActive returns every activated trigger revision in trigger-ref
// order. It is the enumeration seam a scheduler sweep uses to load the
// published triggers due for occurrence calculation; revisions that were
// published but never activated are not due and are not returned.
func (r *Registry) ListActive() []PublishedTrigger {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	active := make([]PublishedTrigger, 0, len(r.active))
	for _, ref := range r.active {
		published, ok := r.records[ref]
		if !ok {
			continue
		}
		active = append(active, clonePublished(published))
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].Ref().String() < active[j].Ref().String()
	})
	return active
}
