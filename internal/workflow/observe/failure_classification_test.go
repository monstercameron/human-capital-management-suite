package observe_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_OBS_025_Fault(t *testing.T) {
	refusal := &runtime.Error{Code: runtime.CodeFenceRefused}
	for _, tc := range []struct {
		name    string
		err     error
		refused bool
	}{
		{"governed refusal", refusal, true},
		{"nested storage", &runtime.Error{Code: runtime.CodeFenceRefused, Err: &runtime.Error{Code: runtime.CodeStorageFailed}}, false},
		{"joined storage", errors.Join(refusal, &runtime.Error{Code: runtime.CodeStorageFailed}), false},
		{"nested cancellation", &runtime.Error{Code: runtime.CodeFenceRefused, Err: context.Canceled}, false},
		{"joined deadline", errors.Join(refusal, context.DeadlineExceeded), false},
		{"plain error", errors.New("private-detail"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := observe.Refused(tc.err); got != tc.refused {
				t.Fatalf("Refused=%v want %v", got, tc.refused)
			}
		})
	}
}
