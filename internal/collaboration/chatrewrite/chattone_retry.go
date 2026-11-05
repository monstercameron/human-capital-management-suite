package chatrewrite

import (
	"context"
	"time"
)

const (
	// CallTimeout bounds one model call: near the slowest ordinary answer, so
	// a stalled call is asked once more instead of holding the rewrite for
	// the whole wait (CHATTONE-006).
	CallTimeout = 10 * time.Second
	// MaxCallAttempts is the one retry rule the served rewrite and the
	// qualification tool share: a call the provider does not answer in time
	// is asked once more, never twice.
	MaxCallAttempts = 2
)

// RetryStalledCall runs call with timeout per attempt and asks once more when
// an attempt stalls: its own bound passes while the parent may continue. Any
// other error, and a parent that will not continue, return at once without
// another call. When every attempt stalls it returns ErrStalled, so the writer
// can be told to press the retry instead of the general unavailable.
func RetryStalledCall(ctx context.Context, timeout time.Duration, call func(context.Context) (Checked, error)) (Checked, error) {
	if timeout <= 0 {
		timeout = CallTimeout
	}
	for attempt := 1; ; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		out, err := call(callCtx)
		stalled := err != nil && callCtx.Err() != nil && ctx.Err() == nil
		cancel()
		if err == nil {
			return out, nil
		}
		if !stalled || attempt >= MaxCallAttempts {
			if stalled {
				return Checked{}, ErrStalled
			}
			return Checked{}, err
		}
	}
}
