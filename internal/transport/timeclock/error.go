package timeclock

import (
	"errors"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func serviceError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, clockservice.ErrInvalidPrincipal):
		return status.Error(codes.Unauthenticated, "trusted principal required")
	case errors.Is(err, clockservice.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, clockservice.ErrUnavailable), errors.Is(err, clockservice.ErrRetryLater):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, clockservice.ErrDeviceNotEligible), errors.Is(err, clockservice.ErrWorkerNotEligible), errors.Is(err, clockservice.ErrWorkerNotOnRoster), errors.Is(err, clockservice.ErrLockedOut):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, "clock device operation failed")
	}
}
