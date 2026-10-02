package chat

import "context"

// ModerationRunTrace resolves the immutable run that authored a post. The
// implementation belongs to the application composition, not to request JSON.
type ModerationRunTrace interface {
	RunForModerationPost(context.Context, string, string, string) (string, error)
}

type NoModerationRunTrace struct{}

func (NoModerationRunTrace) RunForModerationPost(context.Context, string, string, string) (string, error) {
	return "", nil
}
