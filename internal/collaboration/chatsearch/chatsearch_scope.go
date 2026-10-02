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
	entries map[string]scopeEntry
}

type scopeEntry struct {
	value any
	err   error
}

// WithScope returns a context carrying a fresh Scope.
func WithScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, scopeKey{}, &Scope{entries: map[string]scopeEntry{}})
}

// Memo returns the remembered answer for key within ctx's scope, loading it
// once. Without a scope it loads every time.
func Memo(ctx context.Context, key string, load func() (any, error)) (any, error) {
	scope, _ := ctx.Value(scopeKey{}).(*Scope)
	if scope == nil {
		return load()
	}
	scope.mu.Lock()
	entry, ok := scope.entries[key]
	scope.mu.Unlock()
	if ok {
		return entry.value, entry.err
	}
	value, err := load()
	scope.mu.Lock()
	scope.entries[key] = scopeEntry{value, err}
	scope.mu.Unlock()
	return value, err
}
