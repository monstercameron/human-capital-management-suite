package app

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

func TestTodo_UXBLIND_076(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "domain", err: envelope.New(envelope.CodeUnavailable, journeyFailureDomainUnavailable, "unavailable"), want: journeyFailureDomainUnavailable},
		{name: "storage", err: envelope.New(envelope.CodeFailedPrecondition, journeyFailureStorageFailed, "storage failed"), want: journeyFailureStorageFailed},
		{name: "stage", err: workspace.ErrJourneyStage, want: journeyFailureStagePrecondition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := journeyFailureReason(tc.err); got != tc.want {
				t.Fatalf("journeyFailureReason = %q, want %q", got, tc.want)
			}
		})
	}
	if got := journeyFailureReason(nil); got != "" || journeyFailureReason(errors.New("unclassified")) != "" {
		t.Fatalf("unclassified failure reason = %q, want empty", got)
	}
}
