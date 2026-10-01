package timeclock

import (
	"context"
	"errors"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestConvertShapes(t *testing.T) {
	ts := timestamppb.New(time.Unix(10, 0))
	if _, err := validTimestamp(ts); err != nil {
		t.Fatal(err)
	}
	if _, err := validTimestamp(nil); !errors.Is(err, clockservice.ErrInvalidRequest) {
		t.Fatalf("timestamp error = %v", err)
	}
	if got, err := punchKind(timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN); err != nil || string(got) != "IN" {
		t.Fatalf("punch kind = %q, %v", got, err)
	}
	if _, err := punchKind(timev1.PunchEventType_PUNCH_EVENT_TYPE_UNSPECIFIED); !errors.Is(err, clockservice.ErrInvalidRequest) {
		t.Fatalf("punch error = %v", err)
	}
	if got, err := identMethod(timev1.IdentificationMethod_IDENTIFICATION_METHOD_BADGE); err != nil || string(got) != "BADGE" {
		t.Fatalf("method = %q, %v", got, err)
	}
	if _, err := identMethod(timev1.IdentificationMethod_IDENTIFICATION_METHOD_UNSPECIFIED); !errors.Is(err, clockservice.ErrInvalidRequest) {
		t.Fatalf("method error = %v", err)
	}
	got := roster(clockservice.RosterDelta{SnapshotRevision: "42", Workers: []clockservice.RosterWorker{{WorkerRef: "w", PINSalt: []byte{1}, PINHash: []byte{2}}}})
	if got.GetSnapshotRevision() != 42 || got.GetWorkers()[0].GetVerifierRef() != "v1:01:02" {
		t.Fatalf("roster = %v", got)
	}
}

func TestServerRequiresTrustedPrincipal(t *testing.T) {
	s := New(nil)
	_, err := s.Heartbeat(context.Background(), &timev1.HeartbeatRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("code = %v, err = %v", status.Code(err), err)
	}
}

func TestServiceErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want codes.Code
	}{
		{"auth", clockservice.ErrInvalidPrincipal, codes.Unauthenticated},
		{"invalid", clockservice.ErrInvalidRequest, codes.InvalidArgument},
		{"unavailable", clockservice.ErrUnavailable, codes.Unavailable},
		{"locked", clockservice.ErrLockedOut, codes.FailedPrecondition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := status.Code(serviceError(tc.err)); got != tc.want {
				t.Fatalf("code=%v", got)
			}
		})
	}
}
