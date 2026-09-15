package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type unusedBeginner struct{}

func (unusedBeginner) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("redelivery reached the database")
}

type unusedSteps struct{}

func (unusedSteps) Run(context.Context, execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("redelivery reached a step")
}

// TestReadyRedeliveryAdapterForwardsTheDriverRefusal proves the served
// adapter is the cell's ReadyRedeliverer and hands the driver's own refusal
// back unchanged: a redelivery naming no instance is refused as invalid
// before the driver opens a transaction or runs a step. The success path is
// TestTodo_WF_RUN_003_ServeRedelivery in internal/application.
func TestReadyRedeliveryAdapterForwardsTheDriverRefusal(t *testing.T) {
	driver, err := execute.New(execute.Options{DB: unusedBeginner{}, Steps: unusedSteps{}})
	if err != nil {
		t.Fatal(err)
	}
	var redeliverer app.ReadyRedeliverer = executeDriverAdapter{driver: driver}
	result, err := redeliverer.RedeliverReady(context.Background(), app.ExecutionRedeliveryRequest{})
	if !errors.Is(err, execute.ErrInvalidConfiguration) || result.InstanceID != "" {
		t.Fatalf("RedeliverReady(empty) = %+v, %v; want ErrInvalidConfiguration and no result", result, err)
	}
}
