package chatsearch

import (
	"context"
	"sync"
)

type scopeKey struct{}

// Scope remembers answers that are the same for every row of one search phase,
// such as whether the reader may open a conversation, so a page of results asks
// the policy once per conversation and not once per row. A scope lives for one
// phase of one search and is never shared between searches.
type Scope struct {
	mu      sync.Mutex
	entries map[string]*scopeEntry
}

// scopeEntry is one answer, possibly still being loaded: done closes when value
// and err are final.
type scopeEntry struct {
	done  chan struct{}
	value any
	err   error
}

// WithScope returns a context carrying a fresh Scope.
func WithScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, scopeKey{}, &Scope{entries: map[string]*scopeEntry{}})
}

// Memo returns the remembered answer for key within ctx's scope, loading it
// once. Sources of one search ask about the same conversations at the same
// moment, so a caller that finds the answer being loaded waits for it instead of
// loading it again. Without a scope it loads every time.
func Memo(ctx context.Context, key string, load func() (any, error)) (any, error) {
	scope, _ := ctx.Value(scopeKey{}).(*Scope)
	if scope == nil {
		return load()
	}
	scope.mu.Lock()
	entry, ok := scope.entries[key]
	if !ok {
		entry = &scopeEntry{done: make(chan struct{})}
		scope.entries[key] = entry
	}
	scope.mu.Unlock()
	if ok {
		select {
		case <-entry.done:
			return entry.value, entry.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer func() {
		// A load that panics still releases everyone waiting on it.
		if recovered := recover(); recovered != nil {
			entry.err = ErrSourceUnavailable
			close(entry.done)
			panic(recovered)
		}
	}()
	entry.value, entry.err = load()
	close(entry.done)
	return entry.value, entry.err
}
