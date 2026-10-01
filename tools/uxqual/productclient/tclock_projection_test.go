package productclient

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_TCLOCK_RuntimeProjectionReaderFailsClosedWhenUnconfigured(t *testing.T) {
	got := FetchClockProjection(context.Background(), nil)
	if got.Projection.State != productui.ClockProjectionUnavailable || got.Reason != ClockProjectionReasonNotConfigured || got.Retryable {
		t.Fatalf("unconfigured reader result = %+v, want unavailable/not configured/non-retryable", got)
	}
}

func TestTodo_TCLOCK_RuntimeProjectionReaderPreservesAuthoritativeReadyProjection(t *testing.T) {
	want := productui.ClockProjection{
		State: productui.ClockProjectionReady, WorkerLabel: "Taylor", ScheduleLabel: "Day shift",
		StatusLabel: "Clocked in", LastEventLabel: "09:00",
		ClockOut: &productui.ActionLinkProps{Label: "Clock out", Href: "/workspace/app/time/clock/out"},
	}
	got := FetchClockProjection(context.Background(), ClockProjectionReaderFunc(func(context.Context) (productui.ClockProjection, error) {
		return want, nil
	}))
	if got.Reason != ClockProjectionReasonNone || got.Projection.State != productui.ClockProjectionReady || got.Retryable {
		t.Fatalf("ready result = %+v, want ready/none/non-retryable", got)
	}
	if got.Projection.WorkerLabel != want.WorkerLabel || got.Projection.StatusLabel != want.StatusLabel || got.Projection.ClockOut.Href != want.ClockOut.Href {
		t.Fatalf("ready projection changed: got %+v want %+v", got.Projection, want)
	}
}

func TestTodo_TCLOCK_RuntimeProjectionReaderDoesNotPromoteUnavailableSource(t *testing.T) {
	got := FetchClockProjection(context.Background(), ClockProjectionReaderFunc(func(context.Context) (productui.ClockProjection, error) {
		return productui.ClockProjection{State: productui.ClockProjectionUnavailable, StatusLabel: "Clocked in"}, nil
	}))
	if got.Projection.State != productui.ClockProjectionUnavailable || got.Reason != ClockProjectionReasonSourceUnavailable || !got.Retryable {
		t.Fatalf("source unavailable result = %+v", got)
	}
	if got.Projection.StatusLabel != "" {
		t.Fatalf("unavailable result retained a partial status: %+v", got.Projection)
	}
}

func TestTodo_TCLOCK_RuntimeProjectionReaderRejectsIncompleteReadySource(t *testing.T) {
	got := FetchClockProjection(context.Background(), ClockProjectionReaderFunc(func(context.Context) (productui.ClockProjection, error) {
		return productui.ClockProjection{State: productui.ClockProjectionReady, WorkerLabel: "Taylor", ScheduleLabel: "Day shift", StatusLabel: "Clocked out"}, nil
	}))
	if got.Projection.State != productui.ClockProjectionUnavailable || got.Reason != ClockProjectionReasonIncomplete || !got.Retryable {
		t.Fatalf("incomplete result = %+v", got)
	}
}

func TestTodo_TCLOCK_RuntimeProjectionReaderMapsReadAndCancellationFailures(t *testing.T) {
	readFailure := FetchClockProjection(context.Background(), ClockProjectionReaderFunc(func(context.Context) (productui.ClockProjection, error) {
		return productui.ClockProjection{}, errors.New("permission denied")
	}))
	if readFailure.Reason != ClockProjectionReasonReadFailed || !readFailure.Retryable || readFailure.Projection.State != productui.ClockProjectionUnavailable {
		t.Fatalf("read failure result = %+v", readFailure)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := FetchClockProjection(canceled, ClockProjectionReaderFunc(func(ctx context.Context) (productui.ClockProjection, error) {
		return productui.ClockProjection{}, ctx.Err()
	}))
	if cancelled.Reason != ClockProjectionReasonCanceled || !cancelled.Retryable || cancelled.Projection.State != productui.ClockProjectionUnavailable {
		t.Fatalf("cancellation result = %+v", cancelled)
	}
}
