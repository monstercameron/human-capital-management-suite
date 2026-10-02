package chat

import "context"

// ChannelGatePort adds a requirement after ordinary admission authority. A nil
// port preserves existing ungated channels; configured ports fail closed.
type ChannelGatePort interface {
	CheckMembership(context.Context, Principal, Membership) error
}

func (s *Service) SetChannelGate(p ChannelGatePort) { s.channelGate = p }
func (s *Service) checkChannelGate(ctx context.Context, p Principal, m Membership) error {
	if s.channelGate == nil {
		return nil
	}
	return s.channelGate.CheckMembership(ctx, p, m)
}
