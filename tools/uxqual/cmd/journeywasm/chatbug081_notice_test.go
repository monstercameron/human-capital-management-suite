package main

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTodo_CHATBUG_081 holds the notice of a refused write to what happened.
// The delete of CHATBUG-070 was refused by the database and reached the page as
// an internal error; the page said "The service did not answer", which is the
// sentence for a service that could not be reached.
func TestTodo_CHATBUG_081(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"the database refused the write", status.Error(codes.Unknown, "chat.internal_error"), "We couldn't delete this message. The service reported an error. Try again."},
		{"an internal error", status.Error(codes.Internal, "chat.internal_error"), "We couldn't delete this message. The service reported an error. Try again."},
		{"the service could not be reached", status.Error(codes.Unavailable, "transport.unclassified_failure"), "We couldn't delete this message. The service did not answer. Try again."},
		{"the request ran out of time", status.Error(codes.DeadlineExceeded, "transport.deadline_exceeded"), "We couldn't delete this message. The service did not answer. Try again."},
		{"someone else changed it", status.Error(codes.Aborted, "chat.conflict"), "We couldn't delete this message. Someone else changed it first. Try again."},
		{"not allowed", status.Error(codes.PermissionDenied, "chat.permission_denied"), "We couldn't delete this message. You do not have access to it."},
		{"not a service error", errors.New("boom"), "We couldn't delete this message. Something went wrong. Try again."},
	} {
		if got := actionFailureNotice("delete this message", tc.err); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
