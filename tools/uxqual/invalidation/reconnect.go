package invalidation

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	defaultReconnectAttempts = 4
	maxReconnectAttempts     = 16
	defaultReconnectBackoff  = 10 * time.Millisecond
	maxReconnectBackoff      = time.Second
)

var (
	ErrReconnectExhausted = errors.New("invalidation: reconnect attempts exhausted")
	ErrStreamFactory      = errors.New("invalidation: stream factory failed")
	ErrCatchUpFailed      = errors.New("invalidation: authoritative catch-up failed")
)

// Cursor is a read-only local checkpoint supplied to StreamFactory. Its
// fields are deliberately private: code opening a stream may inspect the
// committed position, but cannot construct a later position and feed it back
// as authority. It has no wire encoding and must never replace a server-issued
// opaque, authenticated cursor when a canonical invalidation endpoint exists.
type Cursor struct {
	sourceSequence uint64
	watermark      uint64
}

// Sequence reports the last source sequence committed by an authoritative
// catch-up or refetch.
func (c Cursor) Sequence() uint64 { return c.sourceSequence }

// Watermark reports the committed projection watermark at the checkpoint.
func (c Cursor) Watermark() uint64 { return c.watermark }

// CatchUpRequest describes the exact missing contiguous sequence range. It
// carries no display data and is an input to an injected authoritative read.
type CatchUpRequest struct {
	Tenant     values.TenantId
	Projection string
	From       Cursor
	ToSequence uint64
}

// CatchUpResult is the position an authoritative catch-up actually applied.
// Scope may contain a newly authorized subject allow-list from that same read.
// When present, it must name the same tenant and projection and the exact
// returned position; a partial or contradictory scope is refused.
type CatchUpResult struct {
	SourceSequence uint64
	Watermark      uint64
	Scope          *Scope
}

// CatchUp performs the authoritative read for a missing range. The callback
// must re-authorize through its owning RPC, apply the returned view, honor ctx,
// and only then return its committed result.
type CatchUp func(context.Context, CatchUpRequest) (CatchUpResult, error)

// StreamFactory opens one closeable stream from the last local committed
// checkpoint. The checkpoint is not an authorization credential or a wire
// cursor. The canonical endpoint, authentication, subprotocol, and any
// server-issued opaque cursor remain composition concerns when they exist.
type StreamFactory func(context.Context, Cursor) (CloseStream, error)

// ReconnectOptions bounds consecutive failed generations and exponential
// retry delay. Useful authoritative progress resets MaxAttempts; every later
// reconnect still waits at least InitialBackoff to avoid a clean-EOF hot loop.
type ReconnectOptions struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func normalizeReconnectOptions(options ReconnectOptions) ReconnectOptions {
	if options.MaxAttempts <= 0 || options.MaxAttempts > maxReconnectAttempts {
		options.MaxAttempts = defaultReconnectAttempts
	}
	if options.InitialBackoff <= 0 {
		options.InitialBackoff = defaultReconnectBackoff
	} else if options.InitialBackoff > maxReconnectBackoff {
		options.InitialBackoff = maxReconnectBackoff
	}
	if options.MaxBackoff <= 0 || options.MaxBackoff > maxReconnectBackoff {
		options.MaxBackoff = maxReconnectBackoff
	}
	if options.InitialBackoff > options.MaxBackoff {
		options.InitialBackoff = options.MaxBackoff
	}
	return options
}

// RunReconnect owns a sequence of streams until cancellation or until the
// bounded consecutive-failure budget is spent. A disconnect resumes from the
// last successful local checkpoint; a sequence gap is authoritatively caught
// up before the next hint is admitted.
func (c *Client) RunReconnect(ctx context.Context, factory StreamFactory, catchUp CatchUp, options ReconnectOptions) error {
	if c == nil {
		return errors.New("invalidation: nil client")
	}
	if factory == nil {
		return ErrStreamFactory
	}
	if catchUp == nil {
		return ErrCatchUpFailed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	options = normalizeReconnectOptions(options)

	managerCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.run != nil || c.reconnecting {
		c.mu.Unlock()
		cancel()
		return ErrAlreadyRunning
	}
	c.reconnecting = true
	c.reconnectCancel = cancel
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		c.reconnecting = false
		c.reconnectCancel = nil
		c.mu.Unlock()
	}()

	consecutiveFailures := 0
	first := true
	for {
		if err := managerCtx.Err(); err != nil {
			return err
		}
		if consecutiveFailures >= options.MaxAttempts {
			return ErrReconnectExhausted
		}
		if !first {
			delayLevel := consecutiveFailures
			if delayLevel < 1 {
				delayLevel = 1
			}
			if err := waitReconnect(managerCtx, reconnectDelay(options, delayLevel)); err != nil {
				return err
			}
		}
		first = false

		cursor := c.currentCursor()
		stream, err := openReconnectStream(managerCtx, cursor, factory)
		if err != nil {
			if managerCtx.Err() != nil {
				return managerCtx.Err()
			}
			consecutiveFailures++
			c.observe(Event{Kind: EventReconnectErr})
			continue
		}
		if err := managerCtx.Err(); err != nil {
			closeReconnectStream(stream)
			return err
		}

		wrapped := &catchUpStream{ctx: managerCtx, client: c, stream: stream, catchUp: catchUp}
		done, _, startErr := c.start(managerCtx, wrapped, true)
		if startErr != nil {
			closeReconnectStream(stream)
			if managerCtx.Err() != nil {
				return managerCtx.Err()
			}
			consecutiveFailures++
			c.observe(Event{Kind: EventReconnectErr})
			continue
		}
		terminal := <-done
		if managerCtx.Err() != nil {
			return managerCtx.Err()
		}

		committed := c.currentCursor()
		if committed.Sequence() > cursor.Sequence() {
			consecutiveFailures = 0
		} else {
			consecutiveFailures++
		}
		if terminal != nil && !errors.Is(terminal, context.Canceled) && !errors.Is(terminal, context.DeadlineExceeded) && !errors.Is(terminal, io.EOF) {
			c.observe(Event{Kind: EventReconnectErr})
		}
	}
}

func openReconnectStream(ctx context.Context, cursor Cursor, factory StreamFactory) (stream CloseStream, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	defer func() {
		if recover() != nil {
			stream = nil
			err = ErrStreamFactory
		}
	}()
	stream, err = factory(ctx, cursor)
	if contextErr := ctx.Err(); contextErr != nil {
		if !isNilInterface(stream) {
			closeReconnectStream(stream)
		}
		return nil, contextErr
	}
	if err != nil {
		if !isNilInterface(stream) {
			closeReconnectStream(stream)
		}
		return nil, ErrStreamFactory
	}
	if isNilInterface(stream) {
		return nil, ErrNoStream
	}
	return stream, nil
}

func closeReconnectStream(stream CloseStream) {
	if isNilInterface(stream) {
		return
	}
	defer func() { _ = recover() }()
	_ = stream.Close()
}

func waitReconnect(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func reconnectDelay(options ReconnectOptions, level int) time.Duration {
	delay := options.InitialBackoff
	for i := 1; i < level; i++ {
		if delay >= options.MaxBackoff/2 {
			return options.MaxBackoff
		}
		delay *= 2
	}
	if delay > options.MaxBackoff {
		return options.MaxBackoff
	}
	return delay
}

func (c *Client) currentCursor() Cursor {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Cursor{sourceSequence: c.scope.SourceSequence, watermark: c.scope.Watermark}
}

func (c *Client) advanceCursor(generation uint64, request CatchUpRequest, result CatchUpResult) error {
	var nextScope *Scope
	if result.Scope != nil {
		normalized, err := normalizeScope(*result.Scope)
		if err != nil {
			return ErrCatchUpFailed
		}
		nextScope = &normalized
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run == nil || c.run.generation != generation || c.run.ctx.Err() != nil {
		return context.Canceled
	}
	current := Cursor{sourceSequence: c.scope.SourceSequence, watermark: c.scope.Watermark}
	if request.From != current || request.Tenant != c.scope.Tenant || request.Projection != c.scope.Projection {
		return ErrCatchUpFailed
	}
	if result.SourceSequence != request.ToSequence || result.SourceSequence <= current.Sequence() || result.Watermark < current.Watermark() || result.Watermark > result.SourceSequence {
		return ErrCatchUpFailed
	}
	if nextScope != nil {
		if nextScope.Tenant != c.scope.Tenant || nextScope.Projection != c.scope.Projection || nextScope.SourceSequence != result.SourceSequence || nextScope.Watermark != result.Watermark {
			return ErrCatchUpFailed
		}
		c.scope = *nextScope
		return nil
	}
	c.scope.SourceSequence = result.SourceSequence
	c.scope.Watermark = result.Watermark
	return nil
}

type catchUpStream struct {
	ctx        context.Context
	client     *Client
	stream     CloseStream
	catchUp    CatchUp
	generation uint64
}

func (s *catchUpStream) setGeneration(generation uint64) { s.generation = generation }

func (s *catchUpStream) Recv() ([]byte, error) {
	raw, err := safeRecv(s.stream)
	if err != nil {
		return raw, err
	}
	message, err := Decode(raw, s.client.options.MaxMessageBytes)
	if err != nil {
		return raw, nil
	}
	s.client.mu.Lock()
	scope := s.client.scope
	from := Cursor{sourceSequence: scope.SourceSequence, watermark: scope.Watermark}
	s.client.mu.Unlock()
	if message.Tenant != scope.Tenant || message.Projection != scope.Projection || message.SourceSequence <= from.Sequence() {
		return raw, nil
	}
	// Subtraction is safe after the strict greater-than check and avoids the
	// uint64 overflow in from+1 at the maximum sequence value.
	if message.SourceSequence-from.Sequence() == 1 {
		return raw, nil
	}
	request := CatchUpRequest{Tenant: scope.Tenant, Projection: scope.Projection, From: from, ToSequence: message.SourceSequence - 1}
	result, err := callCatchUp(s.ctx, s.catchUp, request)
	if err != nil {
		if contextErr := s.ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		s.client.recordCatchUp(false)
		s.client.observe(Event{Kind: EventCatchUpErr})
		return nil, ErrCatchUpFailed
	}
	if err := s.client.advanceCursor(s.generation, request, result); err != nil {
		if contextErr := s.ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		s.client.recordCatchUp(false)
		s.client.observe(Event{Kind: EventCatchUpErr})
		return nil, ErrCatchUpFailed
	}
	s.client.recordCatchUp(true)
	s.client.observe(Event{Kind: EventCaughtUp, SourceSequence: result.SourceSequence})
	return raw, nil
}

func (s *catchUpStream) Close() error { return s.stream.Close() }

func (c *Client) recordCatchUp(success bool) {
	c.mu.Lock()
	if success {
		c.catchUps++
	} else {
		c.catchUpErrors++
	}
	c.mu.Unlock()
}

func callCatchUp(ctx context.Context, catchUp CatchUp, request CatchUpRequest) (result CatchUpResult, err error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return CatchUpResult{}, contextErr
	}
	completed := make(chan catchUpCompletion, 1)
	go func() {
		var completion catchUpCompletion
		defer func() {
			if recover() != nil {
				completion.err = ErrCatchUpFailed
			}
			completed <- completion
		}()
		completion.result, completion.err = catchUp(ctx, request)
	}()
	select {
	case completion := <-completed:
		if contextErr := ctx.Err(); contextErr != nil {
			return CatchUpResult{}, contextErr
		}
		if completion.err != nil {
			return CatchUpResult{}, ErrCatchUpFailed
		}
		return completion.result, nil
	case <-ctx.Done():
		return CatchUpResult{}, ctx.Err()
	}
}

type catchUpCompletion struct {
	result CatchUpResult
	err    error
}
