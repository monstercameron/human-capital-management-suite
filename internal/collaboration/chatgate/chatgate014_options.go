package chatgate

import "context"

// CHATGATE-014: what the composer offers under "Visible to". A writer may address
// a message to the segments ResolveAudience lets them: a channel manager any
// segment of the rules in force, a member only the segments they are in. A
// restricted message is offered only where the channel's own levels and its
// privacy allow it.

// AudienceChoice is one segment a writer may address, with how many people are in
// it now. The count is of a segment the writer is in or manages, so it tells them
// nothing a member list does not.
type AudienceChoice struct {
	Name   string `json:"name"`
	People int    `json:"people"`
}

// AudienceOptions is the choice list of one writer in one channel.
type AudienceOptions struct {
	Segments []AudienceChoice `json:"segments"`
	// Restricted is true when the writer may also make a message readable by the
	// segment's members alone.
	Restricted bool `json:"restricted"`
}

// AudienceOptions lists what the person may address in the channel. It is empty
// for a channel where addressing is off, and refused for somebody who is not in
// the channel.
func (s *Service) AudienceOptions(ctx context.Context, a Actor, scope Scope) (AudienceOptions, error) {
	out := AudienceOptions{Segments: []AudienceChoice{}}
	if s.Repository == nil {
		return out, ErrUnavailable
	}
	if e := s.access(ctx, a, scope, PermissionMember); e != nil {
		return out, e
	}
	var segments SegmentState
	if r, ok := s.Repository.(memberSegmentReader); ok {
		var e error
		if segments, e = r.SegmentState(ctx, scope); e != nil {
			return out, e
		}
	} else if e := s.view(ctx, scope, func(st State) error {
		segments = st.Segments
		return nil
	}); e != nil {
		return out, e
	}
	if !segments.Addressing {
		return out, nil
	}
	policy, e := s.Authority.Policy(ctx, scope)
	if e != nil {
		return out, e
	}
	manager := s.access(ctx, a, scope, "admin") == nil
	for _, name := range segments.SegmentNames() {
		if manager || segments.InAny(a.Person, []string{name}) {
			out.Segments = append(out.Segments, AudienceChoice{Name: name, People: len(segments.Members([]string{name}))})
		}
	}
	out.Restricted = segments.Restricted && policy.Private
	return out, nil
}
