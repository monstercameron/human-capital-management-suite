package workspace_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// The port file declares types and sentinels, not behaviour, so what there is
// to hold to account is the contract those declarations encode: the sentinels
// are distinct (a caller switching on them must not match two), the source
// tokens are the two the engine and the transport both spell out, and the
// interface really is satisfiable by an implementation outside
// internal/intent/app -- which is what makes it a port rather than a shape one
// package happens to fit.

// TestJourneySentinelsAreDistinct proves no refusal is another refusal. A
// caller renders a 403, a 404, a 409 and a 400 from these, and two that
// matched each other would render the wrong one half the time.
func TestJourneySentinelsAreDistinct(t *testing.T) {
	t.Parallel()
	sentinels := map[string]error{
		"denied":      workspace.ErrDenied,
		"unavailable": workspace.ErrJourneyUnavailable,
		"unknown":     workspace.ErrJourneyUnknown,
		"stage":       workspace.ErrJourneyStage,
		"input":       workspace.ErrJourneyInput,
		"conflict":    workspace.ErrJourneyActiveConflict,
	}
	for name, err := range sentinels {
		for otherName, other := range sentinels {
			if name == otherName {
				continue
			}
			if errors.Is(err, other) {
				t.Errorf("%s matches %s; a caller cannot tell them apart", name, otherName)
			}
		}
	}
}

// TestWorkerSourceTokensAreTheTwoPopulations pins the vocabulary the engine
// stamps and the wire carries. A third population would add a token beside
// these, and this test is what makes that a deliberate change.
func TestWorkerSourceTokensAreTheTwoPopulations(t *testing.T) {
	t.Parallel()
	if workspace.WorkerSourceCorpus != "CORPUS" {
		t.Errorf("WorkerSourceCorpus = %q, want CORPUS", workspace.WorkerSourceCorpus)
	}
	if workspace.WorkerSourceCreated != "CREATED" {
		t.Errorf("WorkerSourceCreated = %q, want CREATED", workspace.WorkerSourceCreated)
	}
	if workspace.WorkerSourceCorpus == workspace.WorkerSourceCreated {
		t.Fatal("the two populations share one token")
	}
}

// TestJourneyStagesAreDistinct pins the same property for the stage
// vocabulary the page renders from.
func TestJourneyStagesAreDistinct(t *testing.T) {
	t.Parallel()
	seen := map[workspace.JourneyStage]bool{}
	for _, stage := range []workspace.JourneyStage{
		workspace.JourneyStageProposed, workspace.JourneyStageBlocked,
		workspace.JourneyStageAwaitingApproval, workspace.JourneyStageCompleted,
		workspace.JourneyStageRejected, workspace.JourneyStageFailed,
		workspace.JourneyStageFinanceApproval, workspace.JourneyStageManagerApproval,
		workspace.JourneyStageWaitingEffectiveDate, workspace.JourneyStageRevalidation,
		workspace.JourneyStageReapproval, workspace.JourneyStageExecuted,
		workspace.JourneyStageObservingEffects, workspace.JourneyStageRecorded,
		workspace.JourneyStageRepairRequired,
	} {
		if stage == "" {
			t.Error("a stage token is empty")
		}
		if seen[stage] {
			t.Errorf("stage %q is declared twice", stage)
		}
		seen[stage] = true
	}
	if len(seen) != 15 {
		t.Fatalf("declared %d distinct stages, want 15", len(seen))
	}
}

// portStub is a JourneyEngine implemented outside internal/intent/app. Its
// only job is to prove the interface is satisfiable from another package,
// which is the structural half of "this is a port".
type portStub struct{}

var _ workspace.JourneyEngine = portStub{}

func (portStub) ListJourneys(context.Context) ([]workspace.JourneySummary, error) { return nil, nil }

func (portStub) Propose(context.Context, workspace.ProposalInput) (workspace.JourneySummary, error) {
	return workspace.JourneySummary{}, nil
}

func (portStub) Inspect(context.Context, string) (workspace.JourneyDetail, error) {
	return workspace.JourneyDetail{}, nil
}

func (portStub) Execute(context.Context, string) (workspace.JourneyDetail, error) {
	return workspace.JourneyDetail{}, nil
}

func (portStub) Decide(context.Context, string, workspace.Decision) (workspace.JourneyDetail, error) {
	return workspace.JourneyDetail{}, nil
}

func (portStub) EditProposal(context.Context, string, uint64, string, string, workspace.EditProposalInput) (workspace.JourneySummary, string, error) {
	return workspace.JourneySummary{}, "", nil
}

func (portStub) PreviewIntervention(context.Context, string, workspace.JourneyInterventionKind) (workspace.JourneyInterventionPreview, error) {
	return workspace.JourneyInterventionPreview{}, nil
}

func (portStub) RequestIntervention(context.Context, string, workspace.JourneyInterventionRequest) (workspace.JourneyInterventionResult, error) {
	return workspace.JourneyInterventionResult{}, nil
}

func (portStub) ListWorkers(context.Context) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	return []workspace.WorkerSummary{{
		WorkerRef: "ada-1a2b3c4d", Source: workspace.WorkerSourceCreated, CreatedAt: time.Unix(0, 0).UTC(),
	}}, workspace.WorkforceOptions{Currency: "USD"}, nil
}

func (portStub) CreateWorker(context.Context, workspace.WorkerInput) (workspace.WorkerSummary, error) {
	return workspace.WorkerSummary{Source: workspace.WorkerSourceCreated}, nil
}

// TestJourneyEngineIsSatisfiableOutsideTheEnginePackage exercises the stub
// through the interface, so the two workforce methods are part of the port's
// checked surface rather than only of one implementation.
func TestJourneyEngineIsSatisfiableOutsideTheEnginePackage(t *testing.T) {
	t.Parallel()
	var engine workspace.JourneyEngine = portStub{}
	workers, options, err := engine.ListWorkers(context.Background())
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	if len(workers) != 1 || workers[0].Source != workspace.WorkerSourceCreated {
		t.Fatalf("ListWorkers returned %+v", workers)
	}
	if options.Currency != "USD" {
		t.Fatalf("options = %+v", options)
	}
	created, err := engine.CreateWorker(context.Background(), workspace.WorkerInput{})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if created.Source != workspace.WorkerSourceCreated {
		t.Fatalf("CreateWorker returned %+v", created)
	}
}
