package bootstrap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
)

// The eight P1A intent contracts, in the order planning/next-steps.md lists
// them. The list is a constant rather than derived from the registry: the
// release ceiling is the thing under test, so a definition that quietly
// appeared in the catalog must make this suite fail rather than join it.
var p1aIntentTypes = []string{
	people.ExplainWorkerStateIntentType,
	promotion.IntentType,
	rewards.SimulateCompensationIntentType,
	rewards.EvaluatePayBandIntentType,
	intelligence.ExplainTransactionIntentType,
	dataops.DetectDriftIntentType,
	repair.CreateRepairPlanIntentType,
	repair.SimulateRepairIntentType,
}

// p1aCase is one intent's create request plus the identity the table reports
// it under.
type p1aCase struct {
	name       string
	typeID     string
	newRequest func(t *testing.T, c *cell, key string, transactionRef string) *intentsv1.CreateIntentRequest
	// wantFinding is a finding this intent's answer must carry, and
	// wantMessage a fragment that finding's message must contain.
	//
	// They exist so the table cannot pass on an empty answer: a receipt that
	// certifies zero effect is only worth having if something actually ran,
	// and for the four diagnostics "something ran" means a real comparison,
	// diagnosis, projection or reconstruction happened.
	wantFinding string
	wantMessage string
}

// p1aCases builds the table. Every case names the same corpus worker, so a
// difference between two answers is a difference in the contract rather than
// in the population.
func p1aCases() []p1aCase {
	return []p1aCase{
		{name: "explain_worker_state", typeID: people.ExplainWorkerStateIntentType, newRequest: explainWorkerStateRequest},
		{name: "promote_worker", typeID: promotion.IntentType, newRequest: promotionRequest},
		{name: "simulate_compensation", typeID: rewards.SimulateCompensationIntentType, newRequest: simulateCompensationRequest},
		{name: "evaluate_pay_band_position", typeID: rewards.EvaluatePayBandIntentType, newRequest: evaluatePayBandRequest},
		{
			name: "explain_transaction", typeID: intelligence.ExplainTransactionIntentType,
			newRequest:  explainTransactionRequest,
			wantFinding: "TRANSACTION_EXPLAINED",
			// PRESENT rules out the two answers that would look like success
			// and prove nothing: a transaction the ledger never had, and one
			// the caller may not be told about.
			wantMessage: "PRESENT",
		},
		{
			name: "detect_drift", typeID: dataops.DetectDriftIntentType,
			newRequest:  detectDriftRequest,
			wantFinding: "DRIFT_SUBJECT_COMPARED",
			// A compared subject with fifteen decided fields is the proof the
			// external read actually happened; a withheld or unobserved
			// population would report no compared subject at all.
			wantMessage: "15 match",
		},
		{
			name: "create_repair_plan", typeID: repair.CreateRepairPlanIntentType,
			newRequest:  createRepairPlanRequest,
			wantFinding: "REPAIR_DIAGNOSIS",
			wantMessage: "HIGH confidence",
		},
		{
			name: "simulate_repair", typeID: repair.SimulateRepairIntentType,
			newRequest:  simulateRepairRequest,
			wantFinding: "REPAIR_SIMULATION_STATUS",
			wantMessage: "NO_LONGER_REQUIRED",
		},
	}
}

// TestAllEightP1AIntentsSimulateOverBothTransportsWithZeroEffects is the
// release-ceiling conformance test for P1A.
//
// It drives every one of the eight executable contracts through create and
// simulate on both published transports, and then checks the three things the
// release actually sells: every answer carries a zero-effect receipt, the
// external system of record saw reads and nothing else, and the database
// outside the chronology tables is byte-identical to what it was before.
func TestAllEightP1AIntentsSimulateOverBothTransportsWithZeroEffects(t *testing.T) {
	c := newCell(t)
	seedWorkforce(t, c)

	incumbent, ok := c.app.Incumbent.(*fakeincumbent.Incumbent)
	if !ok {
		t.Fatalf("the composed cell observes %T, not the stand-in incumbent", c.app.Incumbent)
	}
	incumbent.ResetCalls()

	// The transaction every explain_transaction case reconstructs is a real
	// recorded intent, created here so the explanation reads the chronology
	// this cell actually wrote rather than a reference invented for the test.
	seed, err := c.grpcIntent.CreateIntent(c.grpcContext(context.Background()),
		explainWorkerStateRequest(t, c, "seed-transaction", ""))
	if err != nil {
		t.Fatalf("create the transaction under explanation: %v", err)
	}
	transactionRef := seed.GetIntent().GetIntentId()

	before := snapshotDatabase(t, c)

	if got := len(p1aCases()); got != len(p1aIntentTypes) {
		t.Fatalf("the table covers %d intents, P1A has %d", got, len(p1aIntentTypes))
	}
	covered := map[string]bool{}
	for _, tc := range p1aCases() {
		covered[tc.typeID] = true
	}
	for _, typeID := range p1aIntentTypes {
		if !covered[typeID] {
			t.Fatalf("the table does not cover %s", typeID)
		}
	}

	for _, tc := range p1aCases() {
		for _, surface := range []string{"grpc", "edge"} {
			t.Run(tc.name+"/"+surface, func(t *testing.T) {
				key := tc.name + "-" + surface
				if tc.typeID == promotion.IntentType {
					// Both transports simulate the same active promotion. A
					// second material request for Omar would rightly conflict.
					key = tc.name + "-transport-parity"
				}
				req := tc.newRequest(t, c, key, transactionRef)

				var (
					intentID string
					artifact *intentsv1.SimulationArtifact
				)
				switch surface {
				case "grpc":
					ctx := c.grpcContext(context.Background())
					created, err := c.grpcIntent.CreateIntent(ctx, req)
					if err != nil {
						t.Fatalf("CreateIntent: %v", err)
					}
					intentID = created.GetIntent().GetIntentId()
					simulated, err := c.grpcIntent.SimulateIntent(ctx,
						&intentsv1.SimulateIntentRequest{IntentId: intentID})
					if err != nil {
						t.Fatalf("SimulateIntent: %v", err)
					}
					artifact = simulated.GetSimulation()
				case "edge":
					created, err := c.edgeIntent.CreateIntent(context.Background(), edgeRequest(c, req))
					if err != nil {
						t.Fatalf("CreateIntent: %v", err)
					}
					intentID = created.Msg.GetIntent().GetIntentId()
					simulated, err := c.edgeIntent.SimulateIntent(context.Background(),
						edgeRequest(c, &intentsv1.SimulateIntentRequest{IntentId: intentID}))
					if err != nil {
						t.Fatalf("SimulateIntent: %v", err)
					}
					artifact = simulated.Msg.GetSimulation()
				}

				if artifact == nil {
					t.Fatal("the simulation returned no artifact")
				}
				if artifact.GetIntentId() != intentID {
					t.Fatalf("artifact is about %s, simulated %s", artifact.GetIntentId(), intentID)
				}
				receipt := artifact.GetZeroEffectReceipt()
				if receipt == nil {
					t.Fatal("the simulation carries no zero-effect receipt")
				}
				if !receipt.GetZeroEffect() {
					t.Fatalf("the simulation reports effects: %s", receipt.GetReasonRef())
				}
				// The reason reference is the receipt's whole claim: it
				// names the intent contract, the mode and the result digest
				// the zero-effect statement is about. A receipt that said
				// "zero effect" without saying zero effect on what would be
				// the one thing this release must not ship.
				if !strings.Contains(receipt.GetReasonRef(), ":") {
					t.Fatalf("receipt reason %q does not identify the result it certifies",
						receipt.GetReasonRef())
				}
				if tc.wantFinding != "" {
					assertFinding(t, artifact, tc.wantFinding, tc.wantMessage)
				}
			})
		}
	}

	t.Run("the incumbent saw reads and nothing else", func(t *testing.T) {
		calls := incumbent.Calls()
		if len(calls) == 0 {
			t.Fatal("the cross-system diagnostics never reached the external system")
		}
		if mutating := incumbent.MutatingCalls(); mutating != 0 {
			t.Fatalf("the incumbent recorded %d mutating call(s)", mutating)
		}
		for _, call := range calls {
			switch call.Op {
			case fakeincumbent.OpRead, fakeincumbent.OpSnapshot, fakeincumbent.OpSchemaVersion:
			default:
				t.Fatalf("the incumbent recorded a %s call at sequence %d", call.Op, call.Sequence)
			}
		}
	})

	t.Run("every observation was recorded as immutable evidence", func(t *testing.T) {
		observations, err := c.app.Observations.List(context.Background(), observeQuery())
		if err != nil {
			t.Fatalf("list observations: %v", err)
		}
		if len(observations) == 0 {
			t.Fatal("no observation evidence was recorded for the comparisons that ran")
		}
		for _, obs := range observations {
			if obs.Classification != "EXTERNAL_OBSERVATION" {
				t.Fatalf("observation %s is classified %s", obs.ObservationID, obs.Classification)
			}
			if obs.ContentDigest == "" {
				t.Fatalf("observation %s carries no content digest", obs.ObservationID)
			}
		}
	})

	t.Run("nothing outside the chronology changed", func(t *testing.T) {
		assertSnapshotsEqual(t, before, snapshotDatabase(t, c))
	})
}

// TestP1ARefusesAReadTheBootstrapPolicyDenies proves the authorization plane
// is load-bearing rather than decorative.
//
// The credential is valid, admission passes, and the purpose is one the
// principal genuinely holds - so the only thing that can refuse the call is
// the evaluated policy. A compensation simulation under a purpose the
// BOOTSTRAP policy grants no compensation access under is refused with
// PERMISSION_DENIED, and the same request under the granted purpose succeeds,
// which is what rules out "it was refused for some other reason".
func TestP1ARefusesAReadTheBootstrapPolicyDenies(t *testing.T) {
	c := newCell(t)
	ctx := c.grpcContext(context.Background())

	created, err := c.grpcIntent.CreateIntent(ctx,
		simulateCompensationRequest(t, c, "authz-denied", ""))
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	intentID := created.GetIntent().GetIntentId()

	t.Run("the granted purpose is answered", func(t *testing.T) {
		simulated, err := c.grpcIntent.SimulateIntent(ctx,
			&intentsv1.SimulateIntentRequest{IntentId: intentID})
		if err != nil {
			t.Fatalf("SimulateIntent under %s: %v", testPurpose, err)
		}
		if !simulated.GetSimulation().GetZeroEffectReceipt().GetZeroEffect() {
			t.Fatal("the granted answer is not zero-effect")
		}
	})

	t.Run("the denied purpose is refused, on both transports", func(t *testing.T) {
		scoped := &intentsv1.SimulateIntentRequest{
			IntentId: intentID,
			Scope:    &commonv1.ScopeContext{Purpose: deniedPurpose},
		}

		_, grpcErr := c.grpcIntent.SimulateIntent(ctx, scoped)
		if grpcErr == nil {
			t.Fatal("grpc: the denied purpose was answered")
		}
		if got := status.Code(grpcErr); got != codes.PermissionDenied {
			t.Fatalf("grpc: refusal code is %s, want %s (%v)", got, codes.PermissionDenied, grpcErr)
		}
		// The message separates the two ways this call could be denied. The
		// transport refuses a purpose the principal does not hold; the policy
		// refuses a purpose it does hold but has no grant under. Only the
		// second one proves the authorization plane is doing anything.
		assertPolicyRefusal(t, "grpc", grpcErr)

		_, edgeErr := c.edgeIntent.SimulateIntent(context.Background(), edgeRequest(c, scoped))
		if edgeErr == nil {
			t.Fatal("edge: the denied purpose was answered")
		}
		if got := connect.CodeOf(edgeErr); got != connect.CodePermissionDenied {
			t.Fatalf("edge: refusal code is %s, want %s (%v)", got, connect.CodePermissionDenied, edgeErr)
		}
		assertPolicyRefusal(t, "edge", edgeErr)
	})
}

// assertPolicyRefusal checks that the refusal came from the evaluated policy
// rather than from the transport's own purpose screen.
func assertPolicyRefusal(t *testing.T, surface string, err error) {
	t.Helper()
	const want = "not authorized to read this under the resolved purpose"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s: refusal is %q, want the policy refusal (%q)", surface, err.Error(), want)
	}
}

// TestP1AServesTheDiscoveryDocument checks that the API-001 served shape is
// actually served, to an authenticated caller only, and describes this build.
func TestP1AServesTheDiscoveryDocument(t *testing.T) {
	c := newCell(t)

	t.Run("an anonymous caller learns nothing", func(t *testing.T) {
		res, err := c.httpGet(app.DiscoveryPath, false)
		if err != nil {
			t.Fatalf("GET %s: %v", app.DiscoveryPath, err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous discovery answered %d, want %d", res.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("an authenticated caller reads this build's served shape", func(t *testing.T) {
		res, err := c.httpGet(app.DiscoveryPath, true)
		if err != nil {
			t.Fatalf("GET %s: %v", app.DiscoveryPath, err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("discovery answered %d, want %d", res.StatusCode, http.StatusOK)
		}

		var doc manifest.DiscoveryDocument
		if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
			t.Fatalf("decode discovery document: %v", err)
		}
		if doc.ManifestDigest == "" {
			t.Fatal("the discovery document carries no manifest digest")
		}
		if len(doc.Endpoints) == 0 {
			t.Fatal("the discovery document publishes no endpoint")
		}

		capabilities := map[string]bool{}
		for _, cap := range doc.Capabilities {
			capabilities[cap.CapabilityID] = true
		}
		for _, typeID := range p1aIntentTypes {
			if !capabilities[typeID] {
				t.Fatalf("the discovery document omits capability %s", typeID)
			}
		}
		definitions := map[string]bool{}
		for _, def := range doc.IntentDefinitions {
			definitions[def.DefinitionRef] = true
		}
		for _, typeID := range p1aIntentTypes {
			if !definitions[typeID+"@1"] && !hasPrefixedDefinition(definitions, typeID) {
				t.Fatalf("the discovery document omits definition %s", typeID)
			}
		}

		// The document is a projection of the same manifest the cell composed,
		// so the two must agree digest-for-digest. A served shape that drifted
		// from the built one would describe a cell nobody is running.
		if doc.ManifestDigest != c.app.Discovery.ManifestDigest {
			t.Fatalf("served digest %s, composed digest %s", doc.ManifestDigest, c.app.Discovery.ManifestDigest)
		}
	})

	t.Run("the discovery document is read, never written", func(t *testing.T) {
		res, err := c.httpPost(app.DiscoveryPath)
		if err != nil {
			t.Fatalf("POST %s: %v", app.DiscoveryPath, err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode == http.StatusOK {
			t.Fatal("the discovery endpoint answered a POST")
		}
	})
}

// hasPrefixedDefinition reports whether any published definition reference
// names this intent type, whatever version suffix the manifest renders.
func hasPrefixedDefinition(definitions map[string]bool, typeID string) bool {
	for ref := range definitions {
		if strings.HasPrefix(ref, typeID) {
			return true
		}
	}
	return false
}

// assertFinding checks that the answer carries a finding with this code, and
// that its message says what the case expects it to say.
func assertFinding(t *testing.T, artifact *intentsv1.SimulationArtifact, code, message string) {
	t.Helper()
	for _, finding := range artifact.GetFindings() {
		if finding.GetCode() != code {
			continue
		}
		if message == "" || strings.Contains(finding.GetMessage(), message) {
			return
		}
		t.Fatalf("finding %s says %q, want it to contain %q", code, finding.GetMessage(), message)
	}
	var codes []string
	for _, finding := range artifact.GetFindings() {
		codes = append(codes, finding.GetCode())
	}
	t.Fatalf("the answer carries no %s finding; it carries %v", code, codes)
}

// observeQuery selects every observation this cell recorded for the corpus
// tenant. The tenant is always required: there is no cross-tenant read.
func observeQuery() observe.Query { return observe.Query{TenantID: testTenant} }

// ---------------------------------------------------------------------------
// Request construction
// ---------------------------------------------------------------------------

// corpusWorker is the design-partner worker every case in the table is about.
const corpusWorker = "omar-reyes"

// createIntent is the shared shell of every P1A create request.
func createIntent(
	key, typeID string,
	mode intentsv1.ExecutionMode,
	schema string,
	subjects []*intentsv1.SubjectReference,
	payload []byte,
) *intentsv1.CreateIntentRequest {
	return &intentsv1.CreateIntentRequest{
		IdempotencyKey: key,
		Definition:     &intentsv1.DefinitionReference{IntentTypeId: typeID, Version: 1},
		Subjects:       subjects,
		Request: &intentsv1.TypedPayload{
			Schema: &intentsv1.SchemaReference{
				SchemaId:         schema,
				Version:          1,
				ProtobufFullName: schema,
			},
			ProtobufWireBytes: payload,
		},
		ExecutionMode: mode,
	}
}

// workerID resolves the corpus worker's entity identifier.
func workerID(t *testing.T) string {
	t.Helper()
	ref, err := fixtures.WorkerRef(corpusWorker)
	if err != nil {
		t.Fatalf("resolve worker: %v", err)
	}
	return ref.Id
}

func subject(kind, id, domain string) *intentsv1.SubjectReference {
	return &intentsv1.SubjectReference{SubjectKind: kind, SubjectId: id, AuthorityDomain: domain}
}

func explainWorkerStateRequest(t *testing.T, _ *cell, key, _ string) *intentsv1.CreateIntentRequest {
	t.Helper()
	return createIntent(key, people.ExplainWorkerStateIntentType,
		intentsv1.ExecutionMode_EXECUTION_MODE_EXECUTE,
		"hcmnext.people.v1.ExplainWorkerStateRequest",
		[]*intentsv1.SubjectReference{subject("WORKER", workerID(t), "PEOPLE")},
		mustStruct(t, map[string]any{
			"worker_ref":   corpusWorker,
			"effective_on": "2026-06-01",
			"known_at":     "2026-05-15",
		}))
}

func promotionRequest(t *testing.T, _ *cell, key, _ string) *intentsv1.CreateIntentRequest {
	t.Helper()
	return promoteWorkerRequest(t, key)
}

func simulateCompensationRequest(t *testing.T, _ *cell, key, _ string) *intentsv1.CreateIntentRequest {
	t.Helper()
	return createIntent(key, rewards.SimulateCompensationIntentType,
		intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
		"hcmnext.rewards.v1.SimulateCompensationRequest",
		[]*intentsv1.SubjectReference{
			subject("EMPLOYMENT", workerID(t), "PEOPLE"),
			subject("COMPENSATION", workerID(t), "REWARDS"),
		},
		mustStruct(t, map[string]any{
			"worker_ref":     corpusWorker,
			"effective_date": "2026-06-01",
			"known_at":       "2026-05-15",
			"current":        compensationSide("93000.00"),
			"proposed":       compensationSide("98000.00"),
		}))
}

func evaluatePayBandRequest(t *testing.T, _ *cell, key, _ string) *intentsv1.CreateIntentRequest {
	t.Helper()
	return createIntent(key, rewards.EvaluatePayBandIntentType,
		intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
		"hcmnext.rewards.v1.EvaluatePayBandPositionRequest",
		[]*intentsv1.SubjectReference{
			subject("EMPLOYMENT", workerID(t), "PEOPLE"),
			subject("PAY_BAND", "BAND-OPS-P2-USEAST", "REWARDS"),
		},
		mustStruct(t, map[string]any{
			"worker_ref": corpusWorker,
			"as_of":      "2026-06-01",
			"known_at":   "2026-05-15",
			"job_code":   "OPS-HRBP2",
			"grade":      "P2",
			"pay_zone":   "US-EAST",
			"currency":   "USD",
			"amount":     "93000.00",
		}))
}

func explainTransactionRequest(t *testing.T, _ *cell, key, transactionRef string) *intentsv1.CreateIntentRequest {
	t.Helper()
	if transactionRef == "" {
		t.Fatal("explain_transaction needs a recorded transaction to explain")
	}
	return createIntent(key, intelligence.ExplainTransactionIntentType,
		intentsv1.ExecutionMode_EXECUTION_MODE_EXECUTE,
		"hcmnext.intelligence.v1.ExplainTransactionRequest",
		[]*intentsv1.SubjectReference{subject("BUSINESS_TRANSACTION", transactionRef, "INTELLIGENCE")},
		mustStruct(t, map[string]any{
			"transaction_ref": transactionRef,
		}))
}

func detectDriftRequest(t *testing.T, _ *cell, key, _ string) *intentsv1.CreateIntentRequest {
	t.Helper()
	return createIntent(key, dataops.DetectDriftIntentType,
		intentsv1.ExecutionMode_EXECUTION_MODE_EXECUTE,
		"hcmnext.operations.v1.DetectDriftRequest",
		[]*intentsv1.SubjectReference{subject("EXTERNAL_RESOURCE", workerID(t), "RECONCILIATION")},
		mustStruct(t, map[string]any{
			"comparison_set_ref": "population:people-ops",
			"watermarks":         "source:incumbent-worker-snapshot",
			"worker_refs":        []any{corpusWorker},
			"as_of":              "2026-06-01",
			"known_at":           "2026-09-01",
		}))
}

func createRepairPlanRequest(t *testing.T, _ *cell, key, _ string) *intentsv1.CreateIntentRequest {
	t.Helper()
	return createIntent(key, repair.CreateRepairPlanIntentType,
		intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
		"hcmnext.operations.v1.CreateRepairPlanRequest",
		[]*intentsv1.SubjectReference{subject("DRIFT", workerID(t), "RECONCILIATION")},
		mustStruct(t, map[string]any{
			"diagnosis_digest":    "diagnosis:people-ops:omar",
			"policy_snapshot_ref": "policy:" + app.FieldAuthorityPolicyVersion,
			"worker_ref":          corpusWorker,
			"as_of":               "2026-06-01",
			"known_at":            "2026-09-01",
		}))
}

func simulateRepairRequest(t *testing.T, _ *cell, key, _ string) *intentsv1.CreateIntentRequest {
	t.Helper()
	return createIntent(key, repair.SimulateRepairIntentType,
		intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
		"hcmnext.operations.v1.SimulateRepairRequest",
		[]*intentsv1.SubjectReference{subject("REPAIR_PLAN", workerID(t), "REPAIR")},
		mustStruct(t, map[string]any{
			"repair_plan_digest": "plan:people-ops:omar",
			"state_watermarks":   "watermarks:incumbent-worker-snapshot",
			"worker_ref":         corpusWorker,
			"as_of":              "2026-06-01",
			"known_at":           "2026-09-01",
		}))
}

// compensationSide is the pinned compensation snapshot both rewards cases use.
func compensationSide(base string) map[string]any {
	return map[string]any{
		"base":              base,
		"currency":          "USD",
		"pay_basis":         "ANNUAL_SALARY",
		"bonus_target":      "0.0500",
		"effective_date":    "2026-06-01",
		"revision_stream":   "rewards.package.omar",
		"revision_sequence": "11",
	}
}

// ---------------------------------------------------------------------------
// Harness helpers
// ---------------------------------------------------------------------------

// httpGet issues a plain HTTP request against the edge, with or without the
// suite's credential.
func (c *cell) httpGet(path string, authenticated bool) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.edgeURL+path, nil)
	if err != nil {
		return nil, err
	}
	if authenticated {
		req.Header.Set(transport.AuthorizationMetadataKey, c.token)
	}
	return c.edgeClient.Do(req)
}

// httpPost issues an authenticated POST, for the routes that must refuse one.
func (c *cell) httpPost(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, c.edgeURL+path, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set(transport.AuthorizationMetadataKey, c.token)
	req.Header.Set("Content-Type", "application/json")
	return c.edgeClient.Do(req)
}
