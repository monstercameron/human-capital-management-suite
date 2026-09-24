// Package transporttest holds the shared transport-edge qualification
// fixture: one signing setup, one canonical Protobuf vector and one pair of
// deterministic handler-port fakes.
//
// Semantic owner: experience-and-transport. Phase: P1A. Todo: TOOL-008.
//
// It is a normal package rather than a _test.go file on purpose. The whole
// point of the qualification fixture is that native gRPC and the HTTP edge run
// the same vector against the same handlers; if each transport's test package
// built its own fake, "the same vector" would be a claim about two pieces of
// code rather than a fact about one.
//
// The fakes are deterministic and depend only on their request and on the
// principal in the context. They contain no HCM business rule: they exist to
// make one domain result, one typed error of each interesting owned code, one
// blocking call and one raw non-owned failure reachable from both transports.
package transporttest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	capabilitiesv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/capabilities/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Fixture identity configuration. The signing key is a fixed test key: it
// never leaves this package and it authenticates nothing outside a test
// process.
const (
	Issuer   = "https://issuer.test.hcm-next.invalid"
	Audience = "hcm-next-api"

	Tenant              = "acme-corp"
	Subject             = "user-0191f3c4"
	OrganizationScopeID = "org-north-america"
	PurposeOperations   = "hcm_operations"
	PurposeAnalytics    = "workforce_analytics"
	PurposeUnauthorized = "marketing_enrichment"
	SessionRef          = "session-8fb2c1"
	RoleIntentAuthor    = "intent_author"
	AuthorityRef        = "authority:position:vp-engineering"
)

// signingKey is the fixture's HMAC key.
var signingKey = []byte("hcm-next-transport-fixture-signing-key-32+")

// Scenario selectors. A request that names one of these gets the described
// deterministic outcome from the fakes, on either transport.
const (
	// KnownIntentID resolves to a domain result.
	KnownIntentID = "intent-2f9c41a0"
	// MissingIntentID resolves to a typed NOT_FOUND.
	MissingIntentID = "intent-000000ff"
	// BlockingIntentID makes SimulateIntent wait for the context to end,
	// which is how deadline and cancellation propagation are observed.
	BlockingIntentID = "intent-blocking"
	// CurrentInstanceVersion is the version SubmitIntent expects; anything
	// else is a typed FAILED_PRECONDITION.
	CurrentInstanceVersion = 7
	// RawFaultReasonRef makes CancelIntent return an unowned provider-shaped
	// error, to prove that raw internal text never crosses the edge.
	RawFaultReasonRef = "fault/raw-provider-error"
	// RawFaultDiagnostic is the unowned text the fault scenario raises. No
	// projection may contain it.
	RawFaultDiagnostic = `pq: duplicate key value violates unique constraint "intents_pkey" at internal/data/postgres/intents.go:214`
	// KnownDefinitionID is the intent definition the canonical vector names.
	KnownDefinitionID = "hcm.people.promote_worker"
	// KnownCapabilityID is the capability the registry fixture serves.
	KnownCapabilityID = "capability.people.promote_worker"
	// EvidenceKindDomain labels the fixture's domain evidence references.
	EvidenceKindDomain = "domain_decision"
)

// NewVerifier returns the fixture verifier. now supplies the clock, so a test
// can move time forward to expire a credential without sleeping.
func NewVerifier(now func() time.Time) (*trust.HMACVerifier, error) {
	return trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      signingKey,
		Issuer:   Issuer,
		Audience: Audience,
		Now:      now,
	})
}

// DefaultClaims returns the claims for the fixture's canonical principal,
// valid for one hour from now.
func DefaultClaims(now time.Time) trust.Claims {
	return trust.Claims{
		Issuer:               Issuer,
		Audience:             Audience,
		Subject:              Subject,
		SubjectKind:          "human",
		Tenant:               Tenant,
		OrganizationScopeID:  OrganizationScopeID,
		Roles:                []string{RoleIntentAuthor},
		AuthorityRefs:        []string{AuthorityRef},
		Purposes:             []string{PurposeOperations, PurposeAnalytics},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           SessionRef,
		IssuedAtUnix:         now.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        now.Add(time.Hour).Unix(),
	}
}

// BearerToken mints the Authorization header value for claims.
func BearerToken(verifier *trust.HMACVerifier, claims trust.Claims) (string, error) {
	token, err := verifier.Issue(claims)
	if err != nil {
		return "", err
	}
	return "Bearer " + token, nil
}

// Config returns the shared admission configuration both transports use. The
// request identifier is fixed so that two equivalent requests differ in
// nothing at all, which is what the parity assertions need.
func Config(verifier trust.Verifier, now func() time.Time, requestID string, logger transport.Logger) transport.Config {
	return transport.Config{
		Verifier:     verifier,
		Audience:     Audience,
		Now:          now,
		NewRequestID: func() string { return requestID },
		MaxDeadline:  1500 * time.Millisecond,
		Logger:       logger,
	}
}

// CanonicalCreateIntentRequest is the fixture's Protobuf vector: a fully
// populated, valid CreateIntent request that leaves every trusted field for
// the server to fill in.
func CanonicalCreateIntentRequest() *intentsv1.CreateIntentRequest {
	return &intentsv1.CreateIntentRequest{
		IdempotencyKey: "idem-6d1f0b3a",
		Definition: &intentsv1.DefinitionReference{
			IntentTypeId: KnownDefinitionID,
			Version:      3,
		},
		Subjects: []*intentsv1.SubjectReference{{
			SubjectKind:     "worker",
			SubjectId:       "worker-77c1e2",
			AuthorityDomain: "people",
		}},
		Request: &intentsv1.TypedPayload{
			Schema: &intentsv1.SchemaReference{
				SchemaId:         "hcm.people.promote_worker.request",
				Version:          3,
				ProtobufFullName: "hcmnext.people.v1.PromoteWorkerRequest",
				DescriptorDigest: "sha256:8b1a9953c4611296a827abf8c47804d7",
			},
			ProtobufWireBytes: []byte{0x0a, 0x06, 0x77, 0x6f, 0x72, 0x6b, 0x65, 0x72},
		},
		ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_EXECUTE,
	}
}

// Call is one recorded handler invocation. It captures what the handler
// actually saw, which is the only trustworthy evidence that admission derived
// the same context on both transports.
type Call struct {
	Method               string
	TenantID             string
	SubjectID            string
	Purpose              string
	OrganizationScopeID  string
	EvidenceID           string
	PrincipalFingerprint string
	TrustedFingerprint   string
	Transport            transport.Kind
	Initiator            *intentsv1.PrincipalReference
	ScopeInRequest       *commonv1.ScopeContext
	HadDeadline          bool
	// Request is a clone of the message the handler received, after admission
	// overwrote its trusted fields. Presence assertions read it directly.
	Request proto.Message
}

// recorder collects Call records.
type recorder struct {
	mu    sync.Mutex
	calls []Call
}

// record appends one call for method, reading the trusted context from ctx.
func (r *recorder) record(ctx context.Context, method string, req proto.Message, initiator *intentsv1.PrincipalReference, scope *commonv1.ScopeContext) {
	call := Call{Method: method, Initiator: initiator, ScopeInRequest: scope}
	if req != nil {
		call.Request = proto.Clone(req)
	}
	if _, ok := ctx.Deadline(); ok {
		call.HadDeadline = true
	}
	if principal, ok := trust.FromContext(ctx); ok {
		call.TenantID = principal.Tenant().String()
		call.SubjectID = principal.Subject()
		call.OrganizationScopeID = principal.OrganizationScopeID()
		call.EvidenceID = principal.EvidenceID()
		call.PrincipalFingerprint = principal.Fingerprint()
	}
	if inv, ok := transport.InvocationFromContext(ctx); ok {
		call.Purpose = inv.Purpose()
		call.TrustedFingerprint = inv.TrustedFingerprint()
		call.Transport = inv.Kind()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

// Calls returns a copy of the recorded calls.
func (r *recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Call, len(r.calls))
	copy(out, r.calls)
	return out
}

// Reset drops the recorded calls.
func (r *recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

// IntentHandler is the deterministic transport.IntentHandler fake.
type IntentHandler struct {
	recorder
}

// Assert at compile time that the fakes satisfy the ports the transports
// depend on. A port change should break here, not in a test body.
var (
	_ transport.IntentHandler   = (*IntentHandler)(nil)
	_ transport.RegistryHandler = (*RegistryHandler)(nil)
)

// CreateIntent returns a deterministic IntentInstance derived from the request
// and from the server-derived trusted context.
func (h *IntentHandler) CreateIntent(ctx context.Context, req *intentsv1.CreateIntentRequest) (*intentsv1.CreateIntentResponse, error) {
	h.record(ctx, "CreateIntent", req, req.GetInitiator(), req.GetScope())
	principal, err := trust.MustFromContext(ctx)
	if err != nil {
		return nil, envelope.New(envelope.CodeUnauthenticated, "fixture.no_principal",
			"the request carries no valid authentication")
	}
	return &intentsv1.CreateIntentResponse{
		Intent: &intentsv1.IntentInstance{
			IntentId:            DeterministicIntentID(req.GetIdempotencyKey()),
			Definition:          req.GetDefinition(),
			TenantId:            req.GetScope().GetTenantId(),
			OrganizationScopeId: req.GetScope().GetOrganizationScopeId(),
			Initiator:           req.GetInitiator(),
			Purpose:             req.GetScope().GetPurpose(),
			Subjects:            req.GetSubjects(),
			Request:             req.GetRequest(),
			IdempotencyKey:      req.GetIdempotencyKey(),
			CorrelationId:       principal.EvidenceID(),
			ExecutionMode:       req.GetExecutionMode(),
			InstanceVersion:     1,
			Lifecycle: &intentsv1.LifecycleDimensions{
				Request:     intentsv1.RequestState_REQUEST_STATE_DRAFT,
				Execution:   intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED,
				Business:    intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED,
				Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT,
				Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE,
			},
		},
	}, nil
}

// GetIntent resolves [KnownIntentID] and returns a typed NOT_FOUND for
// anything else.
func (h *IntentHandler) GetIntent(ctx context.Context, req *intentsv1.GetIntentRequest) (*intentsv1.GetIntentResponse, error) {
	h.record(ctx, "GetIntent", req, nil, req.GetScope())
	if req.GetIntentId() != KnownIntentID {
		return nil, envelope.New(envelope.CodeNotFound, "intent.not_found",
			"the resource does not exist or is not visible").
			WithViolation("intent_id", "no intent is visible at this identifier", "intent.visibility").
			WithEvidence(envelope.Evidence{ID: "ev:decision:not-found", Kind: EvidenceKindDomain})
	}
	return &intentsv1.GetIntentResponse{
		Intent: &intentsv1.IntentInstance{
			IntentId:        KnownIntentID,
			TenantId:        req.GetScope().GetTenantId(),
			Purpose:         req.GetScope().GetPurpose(),
			InstanceVersion: CurrentInstanceVersion,
		},
	}, nil
}

// ListIntents returns one deterministic page.
func (h *IntentHandler) ListIntents(ctx context.Context, req *intentsv1.ListIntentsRequest) (*intentsv1.ListIntentsResponse, error) {
	h.record(ctx, "ListIntents", req, nil, req.GetScope())
	return &intentsv1.ListIntentsResponse{
		Intents: []*intentsv1.IntentInstance{{
			IntentId:        KnownIntentID,
			TenantId:        req.GetScope().GetTenantId(),
			InstanceVersion: CurrentInstanceVersion,
		}},
		Page: &commonv1.PageResponse{NextCursor: "cursor:" + DeterministicIntentID(req.GetScope().GetTenantId())},
	}, nil
}

// SimulateIntent blocks for [BlockingIntentID] until the context ends, and
// otherwise returns a deterministic zero-effect simulation.
func (h *IntentHandler) SimulateIntent(ctx context.Context, req *intentsv1.SimulateIntentRequest) (*intentsv1.SimulateIntentResponse, error) {
	h.record(ctx, "SimulateIntent", req, nil, req.GetScope())
	if req.GetIntentId() == BlockingIntentID {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &intentsv1.SimulateIntentResponse{
		Simulation: &intentsv1.SimulationArtifact{
			IntentId:           req.GetIntentId(),
			ProposalRevisionId: "revision-1",
			ZeroEffectReceipt: &intentsv1.ZeroEffectReceipt{
				ZeroEffect: true,
				ReasonRef:  "simulation.no_material_change",
			},
		},
	}, nil
}

// ExecuteIntent enforces the expected instance version, exactly like
// SubmitIntent, and otherwise returns a deterministic parked receipt.
func (h *IntentHandler) ExecuteIntent(ctx context.Context, req *intentsv1.ExecuteIntentRequest) (*intentsv1.ExecuteIntentResponse, error) {
	h.record(ctx, "ExecuteIntent", req, nil, req.GetScope())
	if req.GetExpectedInstanceVersion() != CurrentInstanceVersion {
		return nil, envelope.New(envelope.CodeFailedPrecondition, "intent.stale_revision",
			"a precondition for the operation is not met").
			WithViolation("expected_instance_version", "the expected instance version is stale", "intent.expected_revision").
			WithEvidence(envelope.Evidence{ID: "ev:decision:stale-revision", Kind: EvidenceKindDomain})
	}
	return &intentsv1.ExecuteIntentResponse{
		Execution: &intentsv1.ExecutionReceipt{
			InstanceId:   DeterministicIntentID(req.GetScope().GetTenantId()),
			VisitedNodes: []string{"approve_promotion"},
			//lint:ignore SA1019 wire compatibility: the fixture speaks the still-supported deprecated wire field.
			ParkedContinuations: []string{"approval.prototype.promotion/v1"},
			InstanceVersion:     1,
			ReceiptDigest:       "execution:" + req.GetIntentId() + ":PARKED",
		},
	}, nil
}

// SubmitIntent enforces the expected instance version and returns a typed
// FAILED_PRECONDITION otherwise. FAILED_PRECONDITION is the case where
// connect-go's default HTTP mapping disagrees with the canonical projection
// table, so this scenario is what proves the edge's override works.
func (h *IntentHandler) SubmitIntent(ctx context.Context, req *intentsv1.SubmitIntentRequest) (*intentsv1.SubmitIntentResponse, error) {
	h.record(ctx, "SubmitIntent", req, nil, req.GetScope())
	if req.GetExpectedInstanceVersion() != CurrentInstanceVersion {
		return nil, envelope.New(envelope.CodeFailedPrecondition, "intent.stale_revision",
			"a precondition for the operation is not met").
			WithViolation("expected_instance_version", "the expected instance version is stale", "intent.expected_revision").
			WithEvidence(envelope.Evidence{ID: "ev:decision:stale-revision", Kind: EvidenceKindDomain})
	}
	return &intentsv1.SubmitIntentResponse{
		Intent: &intentsv1.IntentInstance{
			IntentId:        req.GetIntentId(),
			InstanceVersion: CurrentInstanceVersion + 1,
		},
	}, nil
}

// CancelIntent raises an unowned provider-shaped failure for
// [RawFaultReasonRef] and otherwise succeeds.
func (h *IntentHandler) CancelIntent(ctx context.Context, req *intentsv1.CancelIntentRequest) (*intentsv1.CancelIntentResponse, error) {
	h.record(ctx, "CancelIntent", req, nil, req.GetScope())
	if req.GetReasonRef() == RawFaultReasonRef {
		return nil, errors.New(RawFaultDiagnostic)
	}
	return &intentsv1.CancelIntentResponse{
		Intent: &intentsv1.IntentInstance{
			IntentId:        req.GetIntentId(),
			InstanceVersion: req.GetExpectedInstanceVersion() + 1,
		},
	}, nil
}

// SupersedeIntent returns a deterministic successor.
func (h *IntentHandler) SupersedeIntent(ctx context.Context, req *intentsv1.SupersedeIntentRequest) (*intentsv1.SupersedeIntentResponse, error) {
	h.record(ctx, "SupersedeIntent", req, nil, req.GetScope())
	return &intentsv1.SupersedeIntentResponse{
		SupersedingIntent: &intentsv1.IntentInstance{
			IntentId:        DeterministicIntentID(req.GetSupersededIntentId()),
			Definition:      req.GetDefinition(),
			InstanceVersion: 1,
		},
	}, nil
}

// ExplainIntent returns a deterministic explanation.
func (h *IntentHandler) ExplainIntent(ctx context.Context, req *intentsv1.ExplainIntentRequest) (*intentsv1.ExplainIntentResponse, error) {
	h.record(ctx, "ExplainIntent", req, nil, req.GetScope())
	return &intentsv1.ExplainIntentResponse{
		IntentId:        req.GetIntentId(),
		EvidenceRefs:    []*commonv1.EvidenceRef{{EvidenceId: "ev:decision:explanation", EvidenceKind: EvidenceKindDomain}},
		ExplanationText: "fixture explanation",
	}, nil
}

// ListIntentTimeline returns a deterministic empty timeline page.
func (h *IntentHandler) ListIntentTimeline(ctx context.Context, req *intentsv1.ListIntentTimelineRequest) (*intentsv1.ListIntentTimelineResponse, error) {
	h.record(ctx, "ListIntentTimeline", req, nil, req.GetScope())
	return &intentsv1.ListIntentTimelineResponse{
		Page: &commonv1.PageResponse{NextCursor: ""},
	}, nil
}

// RecommendIntentAction returns a deterministic draft proposal.
func (h *IntentHandler) RecommendIntentAction(ctx context.Context, req *intentsv1.RecommendIntentActionRequest) (*intentsv1.RecommendIntentActionResponse, error) {
	h.record(ctx, "RecommendIntentAction", req, nil, req.GetScope())
	return &intentsv1.RecommendIntentActionResponse{
		ProposalId: "proposal:fixture", Status: "DRAFT", Family: "CHANGE_REQUEST",
		TenantId: req.GetTenantId(), Explanation: "fixture recommendation",
	}, nil
}

// GetIntentDeepLink returns a deterministic link token.
func (h *IntentHandler) GetIntentDeepLink(ctx context.Context, req *intentsv1.GetIntentDeepLinkRequest) (*intentsv1.GetIntentDeepLinkResponse, error) {
	h.record(ctx, "GetIntentDeepLink", req, nil, req.GetScope())
	return &intentsv1.GetIntentDeepLinkResponse{IntentId: req.GetIntentId(), LinkToken: "intent/fixture?rev=1"}, nil
}

// InspectIntentFields returns a deterministic field set.
func (h *IntentHandler) InspectIntentFields(ctx context.Context, req *intentsv1.InspectIntentFieldsRequest) (*intentsv1.InspectIntentFieldsResponse, error) {
	h.record(ctx, "InspectIntentFields", req, nil, req.GetScope())
	return &intentsv1.InspectIntentFieldsResponse{IntentId: req.GetIntentId(), Revision: 1, RevisionDigest: "sha256:fixture"}, nil
}

// ExportIntentFields returns a deterministic field set.
func (h *IntentHandler) ExportIntentFields(ctx context.Context, req *intentsv1.ExportIntentFieldsRequest) (*intentsv1.ExportIntentFieldsResponse, error) {
	h.record(ctx, "ExportIntentFields", req, nil, req.GetScope())
	return &intentsv1.ExportIntentFieldsResponse{IntentId: req.GetIntentId(), Purpose: req.GetPurpose(), Revision: 1}, nil
}

// RegistryHandler is the deterministic transport.RegistryHandler fake.
type RegistryHandler struct {
	recorder
}

// ListIntentDefinitions returns one definition.
func (h *RegistryHandler) ListIntentDefinitions(ctx context.Context, req *registryv1.ListIntentDefinitionsRequest) (*registryv1.ListIntentDefinitionsResponse, error) {
	h.record(ctx, "ListIntentDefinitions", req, nil, req.GetScope())
	return &registryv1.ListIntentDefinitionsResponse{
		IntentDefinitions: []*intentsv1.IntentDefinition{fixtureDefinition()},
		Page:              &commonv1.PageResponse{NextCursor: ""},
	}, nil
}

// GetIntentDefinition resolves [KnownDefinitionID] and returns a typed
// NOT_FOUND for anything else.
func (h *RegistryHandler) GetIntentDefinition(ctx context.Context, req *registryv1.GetIntentDefinitionRequest) (*registryv1.GetIntentDefinitionResponse, error) {
	h.record(ctx, "GetIntentDefinition", req, nil, req.GetScope())
	if req.GetDefinition().GetIntentTypeId() != KnownDefinitionID {
		return nil, envelope.New(envelope.CodeNotFound, "definition.not_found",
			"the resource does not exist or is not visible").
			WithViolation("definition.intent_type_id", "no definition is visible at this identifier", "registry.visibility")
	}
	return &registryv1.GetIntentDefinitionResponse{IntentDefinition: fixtureDefinition()}, nil
}

// ListCapabilities returns one capability.
func (h *RegistryHandler) ListCapabilities(ctx context.Context, req *registryv1.ListCapabilitiesRequest) (*registryv1.ListCapabilitiesResponse, error) {
	h.record(ctx, "ListCapabilities", req, nil, req.GetScope())
	return &registryv1.ListCapabilitiesResponse{
		Capabilities: []*capabilitiesv1.CapabilityDefinition{{CapabilityId: KnownCapabilityID, Version: 1}},
		Page:         &commonv1.PageResponse{NextCursor: ""},
	}, nil
}

// GetCapability resolves [KnownCapabilityID] and returns a typed NOT_FOUND for
// anything else.
func (h *RegistryHandler) GetCapability(ctx context.Context, req *registryv1.GetCapabilityRequest) (*registryv1.GetCapabilityResponse, error) {
	h.record(ctx, "GetCapability", req, nil, req.GetScope())
	if req.GetCapabilityId() != KnownCapabilityID {
		return nil, envelope.New(envelope.CodeNotFound, "capability.not_found",
			"the resource does not exist or is not visible").
			WithViolation("capability_id", "no capability is visible at this identifier", "registry.visibility")
	}
	return &registryv1.GetCapabilityResponse{
		Capability: &capabilitiesv1.CapabilityDefinition{CapabilityId: KnownCapabilityID, Version: 1},
	}, nil
}

// fixtureDefinition is the single IntentDefinition the registry fake serves.
func fixtureDefinition() *intentsv1.IntentDefinition {
	return &intentsv1.IntentDefinition{
		Reference:    &intentsv1.DefinitionReference{IntentTypeId: KnownDefinitionID, Version: 3},
		DisplayName:  "Promote worker",
		KernelFamily: intentsv1.KernelFamily_KERNEL_FAMILY_CHANGE_REQUEST,
		Maturity:     intentsv1.DefinitionMaturity_DEFINITION_MATURITY_PUBLISHED,
	}
}

// DeterministicIntentID derives a stable identifier from seed, so that the
// same vector produces the same domain result on every transport and every
// run.
func DeterministicIntentID(seed string) string {
	sum := sha256.Sum256([]byte("fixture-intent|" + seed))
	return "intent-" + hex.EncodeToString(sum[:8])
}
