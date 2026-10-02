package chat

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// The shared test store reports every channel as open. Sending and joining now
// read a channel's status first, and a store that could not answer made every
// such test end in ErrUnavailable. Tests of the status rules use their own
// fixture stores.
func (f *fakeStore) ReadChannelStatus(_ context.Context, tenant, conversation string) (ChannelStatus, error) {
	return ChannelStatus{TenantID: tenant, ConversationID: conversation, Status: chatpolicy.StatusOpen, Revision: 1}, nil
}

func (f *fakeStore) SweepChannelStatuses(context.Context, string, time.Time) (int, error) {
	return 0, ErrUnavailable
}

func (f *fakeStore) CommitChannelStatus(context.Context, ChangeChannelStatusRequest, time.Time, func(context.Context, ChannelStatus) error) (ChannelStatus, error) {
	return ChannelStatus{}, ErrUnavailable
}
