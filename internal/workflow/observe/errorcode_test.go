package observe_test

import (
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/intervention"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrate/artifacts"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/quarantine"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/recover"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/replay"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/shadow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestEveryWorkflowErrorTypeClassifiesByCode proves each workflow package's
// typed refusal reaches telemetry as its stable code -- even wrapped -- and
// never as its message text, and that a nil typed error reports no code.
func TestEveryWorkflowErrorTypeClassifiesByCode(t *testing.T) {
	const code = "SAMPLE_CODE"
	for name, err := range map[string]error{
		"workflow":     workflow.Error{Code: code, Detail: "secret"},
		"frontier":     &frontier.Error{Code: code, Detail: "secret"},
		"intervention": &intervention.Error{Code: code, Detail: "secret"},
		"lease":        &lease.Error{Code: code, Detail: "secret"},
		"migrate":      &migrate.Error{Code: code, Detail: "secret"},
		"artifacts":    &artifacts.Error{Code: code, Detail: "secret"},
		"quarantine":   &quarantine.Error{Code: code, Detail: "secret"},
		"recover":      &recover.Error{Code: code, Detail: "secret"},
		"replay":       &replay.Error{Code: code, Detail: "secret"},
		"runtime":      &runtime.Error{Code: code, Detail: "secret"},
		"shadow":       &shadow.Error{Code: code, Detail: "secret"},
		"simulate":     &simulate.Error{Code: code, Detail: "secret"},
		"timer":        &timer.Error{Code: code, Detail: "secret"},
		"version":      &version.Error{Code: code, Detail: "secret"},
	} {
		if got := observe.ErrorCode(fmt.Errorf("wrapped: %w", err)); got != code {
			t.Errorf("%s: ErrorCode = %q, want %q", name, got, code)
		}
		if !observe.Refused(err) {
			t.Errorf("%s: a coded refusal was not classified as REFUSED", name)
		}
	}
	for name, nilErr := range map[string]interface{ ErrorCode() string }{
		"lease": (*lease.Error)(nil), "runtime": (*runtime.Error)(nil), "timer": (*timer.Error)(nil),
		"recover": (*recover.Error)(nil), "migrate": (*migrate.Error)(nil), "artifacts": (*artifacts.Error)(nil),
		"replay": (*replay.Error)(nil), "intervention": (*intervention.Error)(nil), "quarantine": (*quarantine.Error)(nil),
		"version": (*version.Error)(nil), "frontier": (*frontier.Error)(nil), "shadow": (*shadow.Error)(nil),
		"simulate": (*simulate.Error)(nil),
	} {
		if nilErr.ErrorCode() != "" {
			t.Errorf("%s: nil error reported a code", name)
		}
	}
}
