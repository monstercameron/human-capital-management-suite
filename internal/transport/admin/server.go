package admin

import (
	"context"
	"time"

	"github.com/google/uuid"

	"google.golang.org/grpc"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	adminpolicy "github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/explorer"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/onboardingruns"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// OperatorRole re-exports internal/operations/admin.OperatorRole so a caller
// wiring [Register] does not have to import the policy package separately
// just to mint or check for the one role name that matters here.
const OperatorRole = adminpolicy.OperatorRole

// requireOperator reads the already-admitted principal and invocation from
// ctx - populated by the shared internal/transport/grpcserver.UnaryInterceptor
// chain before this package's handler ever runs - and fails closed unless
// internal/operations/admin.RequireOperator accepts the principal. It never
// inspects transport metadata or headers directly: by the time a handler
// runs, the only trusted context that exists is what transport.Admit already
// resolved.
func requireOperator(ctx context.Context) (*trust.Principal, *transport.Invocation, *envelope.Error) {
	inv, ok := transport.InvocationFromContext(ctx)
	if !ok {
		return nil, nil, envelope.New(envelope.CodeUnauthenticated,
			"admin.no_trusted_context",
			"the request carries no trusted context")
	}
	principal, ok := trust.FromContext(ctx)
	if !ok {
		principal = nil
	}
	if err := adminpolicy.RequireOperator(principal); err != nil {
		if err == adminpolicy.ErrNoPrincipal {
			return nil, inv, envelope.New(envelope.CodeUnauthenticated,
				"admin.no_principal",
				"the request carries no authenticated principal").
				WithCorrelation(inv.RequestID())
		}
		return nil, inv, envelope.New(envelope.CodePermissionDenied,
			"admin.operator_role_required",
			"this method requires the operator profile").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	return principal, inv, nil
}

// Dependencies are the optional read ports AdminService forwards to. Every
// field is optional: a method whose backing port is nil returns a typed
// UNAVAILABLE rather than panicking (ADMIN-001's FAULT case), so a process
// can host this service before every port is wired without any method
// crashing the process.
type Dependencies struct {
	// Intent backs ListIntents. It is exactly the port
	// internal/transport/grpcserver's IntentService already forwards to
	// (transport.IntentHandler), so ListIntents can never diverge from what
	// the ordinary IntentService.ListIntents call resolves for the caller's
	// trusted scope.
	Intent transport.IntentHandler
	// WorkerFacts backs GetWorkerState.
	WorkerFacts people.WorkerFacts
	// TransactionHistory backs ExplainTransaction.
	TransactionHistory intelligence.TransactionHistory
	// CapabilityRegistry backs ListCapabilityProfiles and the capabilities
	// section of GetReleaseManifest. Nil means [capability.NewBootstrapRegistry].
	CapabilityRegistry *capability.Registry
	// Now supplies the current time for a request that does not pin its own
	// known_at. Nil means time.Now.
	Now func() time.Time

	// WorkflowInstances backs GetWorkflowInstance: the application-side port
	// that loads one instance's durable record (the instance row, its node
	// executions, its work items and their transitions) for the caller's
	// tenant. internal/intent/app.NewWorkflowInstanceReader over the pool
	// the workflow runtime writes through is what every real composition
	// passes. This package reads no runtime or work-item table itself -
	// package-dependency-policy.yaml's transport-must-not-import-store rule
	// is exactly that - it hands the loaded rows to internal/workflow/inspect,
	// which is the pure projection and redaction ADMIN-008 renders. Nil
	// leaves GetWorkflowInstance UNAVAILABLE, matching every other optional
	// Dependencies port.
	WorkflowInstances app.WorkflowInstanceReader
	// Onboarding backs the OnboardingService run lifecycle (REV-036-01): the
	// application-side operator registry a run moves through. Nil leaves
	// every onboarding RPC UNAVAILABLE, matching every other optional port.
	Onboarding onboardingruns.OnboardingOperator
	// OnboardingCutoverSigner signs approved cutover epochs for
	// ExecuteOnboardingCutover. Nil leaves that RPC UNAVAILABLE.
	OnboardingCutoverSigner onboarding.CutoverSigner
	// The composition root supplies read-only explorer operations, keeping
	// storage adapters and hash-chain mechanics outside transport.
	ListLedgerStream  func(context.Context, uuid.UUID, string) (explorer.StreamListingView, error)
	VerifyLedgerChain func(context.Context, uuid.UUID, string) (explorer.ChainView, error)
	// RecordLedgerCorrection records one governed business correction
	// through internal/operations/explorer.RecordCorrection (which invokes
	// internal/data/ledger/lineage.Append plus the correction's hash-chain
	// link in one transaction). The composition root supplies the closure
	// over its ledger handle and chain appender, exactly as it does for
	// ListLedgerStream; nil leaves correction recording unconfigured,
	// matching every other optional Dependencies port.
	RecordLedgerCorrection func(context.Context, uuid.UUID, explorer.CorrectionRequest) (explorer.RecordCorrectionView, error)
	// GetLedgerLineage walks one event's correction/supersession ancestry
	// and descendants through internal/operations/explorer.Lineage. Nil
	// leaves lineage walks unconfigured, matching every other optional
	// Dependencies port.
	GetLedgerLineage func(context.Context, uuid.UUID, explorer.EventRef) (explorer.LineageResultView, error)
	// GetLedgerEffectiveCurrent resolves one event's current-effective
	// truth through internal/operations/explorer.EffectiveCurrent (which
	// reads via internal/data/ledger/lineage.EffectiveCurrent). Nil leaves
	// effective-current reads unconfigured, matching every other optional
	// Dependencies port.
	GetLedgerEffectiveCurrent func(context.Context, uuid.UUID, explorer.EventRef) (explorer.EffectiveCurrentView, error)
	// ConfigPromotions backs the connector/config operations center
	// (REV-037-02): the promotion registry inspect/test/redrive/reconcile/
	// diff/simulate/promote/rollback run against. Nil leaves those RPCs
	// UNAVAILABLE.
	ConfigPromotions *promotion.Registry
}

// server adapts [Dependencies] to the generated adminv1.AdminServiceServer
// interface. Every method is a thin forward: [requireOperator] runs first,
// and every remaining line either calls a real capability (a domain
// function, a compiled-in registry, the reused intent handler port) or maps
// its typed result onto the wire message. No method here decides an HCM
// business rule.
type server struct {
	adminv1.UnimplementedAdminServiceServer

	deps Dependencies
}

// Register adds hcmnext.admin.v1.AdminService to srv. srv must already
// carry the shared trusted-request interceptor chain (normally
// internal/transport/grpcserver.UnaryInterceptor, installed when srv was
// constructed via internal/transport/grpcserver.NewServer): Register itself
// installs no interceptor and performs no admission of its own, exactly
// like a second internal/transport/grpcserver.RegisterXServer call would.
//
// This is the hook the orchestrator wires into the modular application's
// composition root once the shared *grpc.Server exists (SVC-011: "hosted by
// the modular application"); it is deliberately not called from
// internal/intent/app/cell.go by this change.
func Register(srv *grpc.Server, deps Dependencies) {
	adminv1.RegisterAdminServiceServer(srv, &server{deps: deps})
}

// capabilityRegistry returns the configured registry, or the compiled-in
// BOOTSTRAP registry when none was configured.
func (s *server) capabilityRegistry() (*capability.Registry, error) {
	if s.deps.CapabilityRegistry != nil {
		return s.deps.CapabilityRegistry, nil
	}
	return capability.NewBootstrapRegistry()
}

// ListIntents forwards to the same transport.IntentHandler port
// internal/transport/grpcserver's ordinary IntentService.ListIntents calls,
// after translating between the admin-local wire shape and the
// hcmnext.intents.v1 shape it mirrors (buf's RPC_REQUEST_RESPONSE_UNIQUE
// lint rule forbids reusing one message as the request/response of two
// RPCs; see admin_service.proto).
func (s *server) ListIntents(ctx context.Context, req *adminv1.ListIntentsRequest) (*adminv1.ListIntentsResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if s.deps.Intent == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.list_intents_unconfigured",
			"the intent read port is not configured").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}

	inner := &intentsv1.ListIntentsRequest{
		Scope:     req.GetScope(),
		Page:      req.GetPage(),
		Freshness: req.GetFreshness(),
	}
	res, err := s.deps.Intent.ListIntents(ctx, inner)
	if err != nil {
		return nil, envelope.Coerce(err)
	}
	return &adminv1.ListIntentsResponse{
		Intents:     res.GetIntents(),
		Page:        res.GetPage(),
		EvidenceRef: evidenceRef(principal, "admin.list_intents"),
	}, nil
}

// evidenceRef builds the wire evidence reference every successful
// AdminService response carries: the caller's own authentication evidence
// identifier, so a CLI or console can print "which credential answered
// this" without this package inventing a second evidence mechanism
// alongside internal/trust.Principal.EvidenceID.
func evidenceRef(principal *trust.Principal, kind string) *commonv1.EvidenceRef {
	return &commonv1.EvidenceRef{EvidenceId: principal.EvidenceID(), EvidenceKind: kind}
}

// GetReleaseManifest renders the compiled-in discovery document
// (internal/transport/manifest) and projects it onto the admin wire shape.
// The render is pure and deterministic: it performs no I/O and depends on
// nothing the caller supplies.
func (s *server) GetReleaseManifest(ctx context.Context, _ *adminv1.GetReleaseManifestRequest) (*adminv1.GetReleaseManifestResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}

	doc, err := manifest.RenderDefaultDiscoveryDocument()
	if err != nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.release_manifest_unavailable",
			"the release manifest could not be rendered").WithDiagnostic(err).
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}

	resp := &adminv1.GetReleaseManifestResponse{
		ManifestDigest: doc.ManifestDigest,
		EvidenceRef:    evidenceRef(principal, "admin.get_release_manifest"),
	}
	for _, e := range doc.Endpoints {
		resp.Endpoints = append(resp.Endpoints, &adminv1.EndpointProfile{
			EndpointId:        e.EndpointID,
			ServiceFullName:   e.ServiceFullName,
			MethodName:        e.MethodName,
			OwnerDomain:       e.OwnerDomain,
			CapabilityRefs:    append([]string(nil), e.CapabilityRefs...),
			RequestType:       e.RequestType,
			ResponseType:      e.ResponseType,
			Disposition:       string(e.Disposition),
			DispositionReason: e.DispositionReason,
		})
	}
	for _, c := range doc.Capabilities {
		resp.Capabilities = append(resp.Capabilities, toCapabilityProfile(c.CapabilityID, c.Version, c.OwnerDomain, c.EffectClass, c.RiskClass, "", c.AgentEligible, ""))
	}
	for _, d := range doc.IntentDefinitions {
		resp.IntentDefinitions = append(resp.IntentDefinitions, &adminv1.IntentDefinitionProfile{
			DefinitionRef: d.DefinitionRef,
			DisplayName:   d.DisplayName,
			OwnerDomain:   d.OwnerDomain,
		})
	}
	return resp, nil
}

// ListCapabilityProfiles lists every registered capability with its
// lifecycle status and content digest, straight from
// internal/capability.Registry.List.
func (s *server) ListCapabilityProfiles(ctx context.Context, _ *adminv1.ListCapabilityProfilesRequest) (*adminv1.ListCapabilityProfilesResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}

	reg, regErr := s.capabilityRegistry()
	if regErr != nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.capability_registry_unavailable",
			"the capability registry could not be loaded").WithDiagnostic(regErr).
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}

	resp := &adminv1.ListCapabilityProfilesResponse{EvidenceRef: evidenceRef(principal, "admin.list_capability_profiles")}
	for _, rec := range reg.List() {
		resp.Capabilities = append(resp.Capabilities, toCapabilityProfile(
			rec.Definition.ID, rec.Definition.Version, rec.Definition.OwnerDomain,
			string(rec.Definition.EffectClass), rec.Definition.RiskClass,
			string(rec.Status), rec.Definition.AgentEligible, rec.Digest))
	}
	return resp, nil
}

func toCapabilityProfile(id string, version uint32, owner, effectClass, riskClass, status string, agentEligible bool, digest string) *adminv1.CapabilityProfile {
	return &adminv1.CapabilityProfile{
		CapabilityId:  id,
		Version:       version,
		OwnerDomain:   owner,
		EffectClass:   effectClass,
		RiskClass:     riskClass,
		Status:        status,
		AgentEligible: agentEligible,
		Digest:        digest,
	}
}
