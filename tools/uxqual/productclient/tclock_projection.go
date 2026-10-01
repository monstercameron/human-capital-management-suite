package productclient

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// ClockProjectionReader is the authenticated adapter for the worker's own
// clock projection. The adapter must read the current worker record through a
// published product API; it must not use ClockDeviceService.GetWorkerStatus,
// which is scoped to an enrolled device and its short-lived punch token.
type ClockProjectionReader interface {
	ReadClockProjection(context.Context) (productui.ClockProjection, error)
}

// ClockProjectionReaderFunc adapts a function to ClockProjectionReader.
type ClockProjectionReaderFunc func(context.Context) (productui.ClockProjection, error)

// ReadClockProjection implements ClockProjectionReader.
func (f ClockProjectionReaderFunc) ReadClockProjection(ctx context.Context) (productui.ClockProjection, error) {
	if f == nil {
		return productui.ClockProjection{}, errors.New("productclient: nil clock projection reader")
	}
	return f(ctx)
}

// ClockProjectionRead is the terminal result of one authoritative clock read.
// Unavailable results intentionally retain no partial server data: a page
// cannot turn a failed or incomplete read into an actionable clock state.
type ClockProjectionRead struct {
	Projection productui.ClockProjection
	Reason     ClockProjectionUnavailableReason
	Retryable  bool
}

// ClockProjectionUnavailableReason explains why a projection was unavailable
// without exposing transport or authorization details in the UI.
type ClockProjectionUnavailableReason uint8

const (
	// ClockProjectionReasonNone means an authoritative projection is ready.
	ClockProjectionReasonNone ClockProjectionUnavailableReason = iota
	// ClockProjectionReasonNotConfigured means no reader was composed.
	ClockProjectionReasonNotConfigured
	// ClockProjectionReasonReadFailed means the reader refused or failed.
	ClockProjectionReasonReadFailed
	// ClockProjectionReasonSourceUnavailable means the source published no view.
	ClockProjectionReasonSourceUnavailable
	// ClockProjectionReasonIncomplete means the source omitted required facts.
	ClockProjectionReasonIncomplete
	// ClockProjectionReasonCanceled means the request context was canceled.
	ClockProjectionReasonCanceled
)

// FetchClockProjection performs one authoritative read and fails closed. It
// accepts an injected reader so HTTP and gRPC composition can share this
// boundary without making the product client own transport credentials.
func FetchClockProjection(ctx context.Context, reader ClockProjectionReader) ClockProjectionRead {
	if ctx == nil {
		ctx = context.Background()
	}
	if reader == nil {
		return unavailableClockProjection(ClockProjectionReasonNotConfigured, false)
	}
	projection, err := reader.ReadClockProjection(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return unavailableClockProjection(ClockProjectionReasonCanceled, true)
		}
		return unavailableClockProjection(ClockProjectionReasonReadFailed, true)
	}
	if projection.State != productui.ClockProjectionReady {
		// A decision about this worker keeps its closed reason and nothing
		// else, and is not worth retrying until something changes.
		if reason := productui.NormalizeClockReason(string(projection.Reason)); reason != productui.ClockReasonNone {
			read := unavailableClockProjection(ClockProjectionReasonSourceUnavailable, false)
			read.Projection.Reason = reason
			return read
		}
		return unavailableClockProjection(ClockProjectionReasonSourceUnavailable, true)
	}
	if !completeClockProjection(projection) {
		return unavailableClockProjection(ClockProjectionReasonIncomplete, true)
	}
	return ClockProjectionRead{Projection: projection, Reason: ClockProjectionReasonNone}
}

func completeClockProjection(projection productui.ClockProjection) bool {
	return strings.TrimSpace(projection.WorkerLabel) != "" &&
		strings.TrimSpace(projection.ScheduleLabel) != "" &&
		strings.TrimSpace(projection.StatusLabel) != "" &&
		strings.TrimSpace(projection.LastEventLabel) != ""
}

func unavailableClockProjection(reason ClockProjectionUnavailableReason, retryable bool) ClockProjectionRead {
	return ClockProjectionRead{
		Projection: productui.ClockProjection{State: productui.ClockProjectionUnavailable},
		Reason:     reason,
		Retryable:  retryable,
	}
}
