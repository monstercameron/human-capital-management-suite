package chat

import "context"

type channelMutationCheckKey struct{}

// WithChannelMutationCheck carries the composition's current authority recheck.
// Stores invoke it while holding the conversation write fence.
func WithChannelMutationCheck(ctx context.Context, check func(context.Context) error) context.Context {
	return context.WithValue(ctx, channelMutationCheckKey{}, check)
}
func RecheckChannelMutation(ctx context.Context) error {
	if check, ok := ctx.Value(channelMutationCheckKey{}).(func(context.Context) error); ok {
		return check(ctx)
	}
	return nil
}
