package chatsearch

import "context"

// SourceFuncs lets a store register its SearchSaved, SearchTranscripts,
// SearchLocations, SearchGates, SearchFilters or SearchModeration adapter
// without introducing dependencies between the owners of those stores.
// SearchRows must authorize before matching; OpenRow reloads current state.
type SourceFuncs struct {
	SearchRows func(context.Context, Request) ([]Row, error)
	OpenRow    func(context.Context, Actor, Row) (bool, error)
}

func (s SourceFuncs) Search(ctx context.Context, q Request) ([]Row, error) {
	if s.SearchRows == nil || s.OpenRow == nil {
		return nil, ErrRegistry
	}
	return s.SearchRows(ctx, q)
}

func (s SourceFuncs) CanOpen(ctx context.Context, a Actor, row Row) (bool, error) {
	if s.OpenRow == nil {
		return false, ErrRegistry
	}
	return s.OpenRow(ctx, a, row)
}

// RegisterSource uses the registry's declaration for an existing content kind.
// New kinds must declare their indexed text, audience and opening destination
// through Register instead. The registry belongs to the composition root.
func (r *Registry) RegisterSource(kind Kind, source Source) error {
	if f, ok := source.(SourceFuncs); ok && (f.SearchRows == nil || f.OpenRow == nil) {
		return ErrRegistry
	}
	d, ok := r.Declaration(kind)
	if !ok {
		return ErrRegistry
	}
	return r.Register(d, source)
}
