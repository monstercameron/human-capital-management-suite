package chatsearch

import "sync"

// Recent is a bounded private, in-process history. Deployments can retain a
// value per session or replace this port with a person-scoped durable store.
type Recent struct {
	mu      sync.Mutex
	entries map[Actor][]string
}

func NewRecent() *Recent { return &Recent{entries: map[Actor][]string{}} }
func (r *Recent) Remember(actor Actor, query string) error {
	if actor.TenantID == "" || actor.HomeTenantID == "" || actor.PersonID == "" || len(query) > 512 {
		return ErrInvalid
	}
	if _, e := Parse(query); e != nil {
		return e
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{query}
	for _, v := range r.entries[actor] {
		if v != query && len(out) < 10 {
			out = append(out, v)
		}
	}
	r.entries[actor] = out
	return nil
}
func (r *Recent) List(actor Actor) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.entries[actor]...)
}
func (r *Recent) Clear(actor Actor) { r.mu.Lock(); defer r.mu.Unlock(); delete(r.entries, actor) }
