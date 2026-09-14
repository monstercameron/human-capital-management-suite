package journey_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// TestListWorkersForwardsBothPopulationsAndTheOptions drives the read half of
// the workforce surface through a real gRPC client: both sources arrive with
// their Source token intact, the created worker keeps the compensation
// baseline only it carries, and the options travel in the same response as
// the list -- which is what makes the list actionable rather than decorative.
func TestListWorkersForwardsBothPopulationsAndTheOptions(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

	resp, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	if len(resp.GetWorkers()) != 2 {
		t.Fatalf("workers = %d, want 2", len(resp.GetWorkers()))
	}

	created, corpus := resp.GetWorkers()[0], resp.GetWorkers()[1]
	if created.GetSource() != workspace.WorkerSourceCreated {
		t.Errorf("first worker source = %q, want CREATED (created workers list first)", created.GetSource())
	}
	if corpus.GetSource() != workspace.WorkerSourceCorpus {
		t.Errorf("second worker source = %q, want CORPUS", corpus.GetSource())
	}
	want := fixtureCreatedWorker()
	if created.GetWorkerRef() != want.WorkerRef || created.GetWorkerId() != want.WorkerID {
		t.Errorf("created identity = %q/%q, want %q/%q",
			created.GetWorkerRef(), created.GetWorkerId(), want.WorkerRef, want.WorkerID)
	}
	if created.GetBasePay() != want.BasePay || created.GetCurrency() != want.Currency ||
		created.GetBonusTarget() != want.BonusTarget {
		t.Errorf("created baseline = %s/%s/%s, want %s/%s/%s",
			created.GetBasePay(), created.GetCurrency(), created.GetBonusTarget(),
			want.BasePay, want.Currency, want.BonusTarget)
	}
	if created.GetCreatedAt() == nil {
		t.Error("a created worker arrives with no creation instant")
	}
	// A corpus worker has no baseline and no creation instant, and the
	// conversion must not invent either.
	if corpus.GetBasePay() != "" || corpus.GetCreatedAt() != nil {
		t.Errorf("a corpus worker arrived with a baseline or a creation instant: %+v", corpus)
	}

	options := resp.GetOptions()
	if options.GetCurrency() != "USD" {
		t.Errorf("options currency = %q, want USD", options.GetCurrency())
	}
	if len(options.GetJobCodes()) == 0 || len(options.GetGrades()) == 0 ||
		len(options.GetOrgUnits()) == 0 || len(options.GetPayZones()) == 0 ||
		len(options.GetPositions()) == 0 {
		t.Fatalf("options arrived incomplete: %+v", options)
	}
}

// TestCreateWorkerForwardsTheFormAndReturnsTheWorker proves the write half
// reaches the port as the port's own plain Go type, field for field, and that
// the engine's answer comes back whole.
func TestCreateWorkerForwardsTheFormAndReturnsTheWorker(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

	in := fixtureWorkerInput()
	resp, err := client.CreateWorker(testContext(t), &journeyv1.CreateWorkerRequest{
		LegalName: in.LegalName, PreferredName: in.PreferredName,
		JobCode: in.JobCode, Grade: in.Grade, OrgUnit: in.OrgUnit,
		PositionId: in.PositionID, Location: in.Location, PayZone: in.PayZone,
		BasePay: in.BasePay, Currency: in.Currency, BonusTarget: in.BonusTarget,
		HireDate: in.HireDate, ManagerRef: in.ManagerRef,
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if engine.lastWorkerInput != in {
		t.Fatalf("the port received %+v, want %+v", engine.lastWorkerInput, in)
	}
	if got := resp.GetWorker().GetWorkerRef(); got != fixtureCreatedWorker().WorkerRef {
		t.Fatalf("worker_ref = %q, want %q", got, fixtureCreatedWorker().WorkerRef)
	}
}

// TestWorkforceRefusalsProjectOntoTheOwnedErrorModel is the effect-class
// contract the proto states, made checkable: no engine is UNAVAILABLE, no
// operator role is PERMISSION_DENIED, and a rejected form is INVALID_ARGUMENT
// carrying a violation that names the field.
func TestWorkforceRefusalsProjectOntoTheOwnedErrorModel(t *testing.T) {
	t.Run("no engine is UNAVAILABLE", func(t *testing.T) {
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{}))
		ctx := testContext(t)
		if _, err := client.ListWorkers(ctx, &journeyv1.ListWorkersRequest{}); err != nil {
			assertOwnedCode(t, err, envelope.CodeUnavailable)
		} else {
			t.Fatal("ListWorkers with no engine succeeded")
		}
		if _, err := client.CreateWorker(ctx, &journeyv1.CreateWorkerRequest{}); err != nil {
			assertOwnedCode(t, err, envelope.CodeUnavailable)
		} else {
			t.Fatal("CreateWorker with no engine succeeded")
		}
	})

	for name, tc := range map[string]struct {
		err  error
		want envelope.Code
	}{
		"denied":      {fmt.Errorf("%w: the operator role is required", workspace.ErrDenied), envelope.CodePermissionDenied},
		"input":       {&workspace.JourneyInputError{FieldPath: "job_code", ReasonRef: "journey.input.invalid", Detail: "no pay band covers it"}, envelope.CodeInvalidArgument},
		"unavailable": {fmt.Errorf("%w: no execution database", workspace.ErrJourneyUnavailable), envelope.CodeUnavailable},
		"stage":       {fmt.Errorf("%w: not now", workspace.ErrJourneyStage), envelope.CodeFailedPrecondition},
		"unclassified": {
			errors.New("the workforce store fell over"), envelope.CodeUnspecified,
		},
	} {
		t.Run("CreateWorker "+name, func(t *testing.T) {
			engine := newFakeEngine()
			engine.createWorkerErr = tc.err
			client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
			_, err := client.CreateWorker(testContext(t), &journeyv1.CreateWorkerRequest{})
			owned := assertOwnedCode(t, err, tc.want)
			if tc.want == envelope.CodeInvalidArgument {
				violations := owned.Violations()
				if len(violations) == 0 {
					t.Fatalf("an input refusal carries no violation: %+v", owned)
				}
				if violations[0].FieldPath != "job_code" {
					t.Fatalf("violation names %q, want job_code", violations[0].FieldPath)
				}
			}
		})

		t.Run("ListWorkers "+name, func(t *testing.T) {
			engine := newFakeEngine()
			engine.listWorkersErr = tc.err
			client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
			_, err := client.ListWorkers(testContext(t), &journeyv1.ListWorkersRequest{})
			assertOwnedCode(t, err, tc.want)
		})
	}
}

// TestWorkforceRPCsRequireAnAdmittedPrincipal proves the two new methods sit
// behind the same shared trusted-request boundary as the five that were
// already there: an unrecognized credential never reaches the engine.
func TestWorkforceRPCsRequireAnAdmittedPrincipal(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	ctx := withToken(t.Context(), fixtureUnknownToken)

	if _, err := client.ListWorkers(ctx, &journeyv1.ListWorkersRequest{}); err == nil {
		t.Fatal("ListWorkers admitted an unrecognized credential")
	}
	if _, err := client.CreateWorker(ctx, &journeyv1.CreateWorkerRequest{}); err == nil {
		t.Fatal("CreateWorker admitted an unrecognized credential")
	}
	if engine.listWorkersCalls != 0 || engine.createWorkerCalls != 0 {
		t.Fatalf("the engine was reached %d/%d times by an unadmitted caller",
			engine.listWorkersCalls, engine.createWorkerCalls)
	}
}

// TestWorkforceConversionsAreTotalInBothDirections is the conversion-layer
// contract convert.go states: a fully populated value survives the wire and
// comes back identical. A conversion that silently drops a field is the one
// defect a handler test cannot see -- the RPC still succeeds, the page just
// shows less than the engine knows.
func TestWorkforceConversionsAreTotalInBothDirections(t *testing.T) {
	worker := fixtureCreatedWorker()
	if got := journey.WorkerRoundTripForTest(worker); got != worker {
		t.Errorf("worker round trip:\n got %+v\nwant %+v", got, worker)
	}
	corpus := fixtureCorpusWorker()
	if got := journey.WorkerRoundTripForTest(corpus); got != corpus {
		t.Errorf("corpus worker round trip:\n got %+v\nwant %+v", got, corpus)
	}
	in := fixtureWorkerInput()
	if got := journey.CreateWorkerRequestRoundTripForTest(in); got != in {
		t.Errorf("create form round trip:\n got %+v\nwant %+v", got, in)
	}
	options := fixtureWorkforceOptions()
	if got := journey.WorkforceOptionsRoundTripForTest(options); !reflect.DeepEqual(got, options) {
		t.Errorf("options round trip:\n got %+v\nwant %+v", got, options)
	}
}

// TestWorkforceConversionsSurviveEmptyMessages proves the inverses treat an
// omitted message as the zero value rather than as a decoding failure: a
// client that sends nothing must get a refusal from the engine's own rules,
// not a panic in the transport.
func TestWorkforceConversionsSurviveEmptyMessages(t *testing.T) {
	if got := journey.WorkforceOptionsRoundTripForTest(workspace.WorkforceOptions{}); got.Currency != "" {
		t.Errorf("zero options round-tripped to %+v", got)
	}
	if got := journey.WorkerRoundTripForTest(workspace.WorkerSummary{}); got != (workspace.WorkerSummary{}) {
		t.Errorf("zero worker round-tripped to %+v", got)
	}
	if got := journey.CreateWorkerRequestRoundTripForTest(workspace.WorkerInput{}); got != (workspace.WorkerInput{}) {
		t.Errorf("zero create form round-tripped to %+v", got)
	}
}
