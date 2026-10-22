package chatgate

import (
	"context"
	"sort"
)

// CHATGATE-014: the labels of a channel and who holds each, for Conversation
// details. A label is shown to members by definition: a rule may only make one
// from a question that is shown to members (validateSegmentRules), so listing
// who holds it tells a member nothing an answer's visibility did not already
// allow. Segments are not listed: a person reads their own, an administrator
// counts them.

// LabelHolders is one label and the people who hold it, in a stable order.
type LabelHolders struct {
	Name   string   `json:"name"`
	People []string `json:"people"`
}

// memberSegmentReader is implemented by a repository that can hand over the
// labels and segments of people who are still in the channel only.
type memberSegmentReader interface {
	SegmentState(context.Context, Scope) (SegmentState, error)
}

// LabelHolders lists a channel's labels with the people who hold them. Anyone in
// the channel may read it.
func (s *Service) LabelHolders(ctx context.Context, a Actor, scope Scope) ([]LabelHolders, error) {
	if s.Repository == nil {
		return nil, ErrUnavailable
	}
	if e := s.access(ctx, a, scope, PermissionMember); e != nil {
		return nil, e
	}
	var segments SegmentState
	if r, ok := s.Repository.(memberSegmentReader); ok {
		var e error
		if segments, e = r.SegmentState(ctx, scope); e != nil {
			return nil, e
		}
	} else if e := s.view(ctx, scope, func(st State) error {
		segments = st.Segments
		return nil
	}); e != nil {
		return nil, e
	}
	byLabel := map[string]*LabelHolders{}
	for person, placements := range segments.Assignments {
		for _, p := range placements {
			if p.Kind != PlacementLabel {
				continue
			}
			held := byLabel[p.Name]
			if held == nil {
				held = &LabelHolders{Name: p.Name}
				byLabel[p.Name] = held
			}
			held.People = append(held.People, person)
		}
	}
	out := make([]LabelHolders, 0, len(byLabel))
	for _, held := range byLabel {
		sort.Strings(held.People)
		out = append(out, *held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
