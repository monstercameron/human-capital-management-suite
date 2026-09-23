package evidence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"

	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/cryptoagility"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Procedure paths, mirroring internal/transport/operations's own constants.
const (
	GetExecutionReceiptProcedure  = evidencev1.EvidenceService_GetExecutionReceipt_FullMethodName
	ExportIntentEvidenceProcedure = evidencev1.EvidenceService_ExportIntentEvidence_FullMethodName
)

const (
	capabilityGetExecutionReceipt  = "evidence.get_execution_receipt"
	capabilityExportIntentEvidence = "evidence.export_intent_evidence"

	requestTypeExport = "evidence.export_intent_evidence/v1"

	reasonInvalidRequest      = "evidence.invalid_request"
	reasonUnauthorizedPurpose = "evidence.purpose_not_authorized"
	reasonNotFound            = "evidence.not_found"
	reasonUnavailable         = "evidence.unavailable"
	reasonLineageIncomplete   = "evidence.lineage_incomplete"
	reasonIdempotencyConflict = "evidence.idempotency_key_reused"
)

// Dispatcher hands an export job off outside the request path.
// GoDispatcher (the production default) launches a goroutine; a test
// substitutes a gated fake to prove the request returns before the job
// finishes.
type Dispatcher interface {
	Dispatch(ctx context.Context, job exportJob, run func(context.Context, exportJob))
}

// defaultExportTimeout bounds one export job's package assembly (frozen
// lineage snapshot, receipt assembly, artifact rendering, sealing and a
// single persist): all local clerical work, so two minutes is generous and
// a hung dependency still fails the operation closed instead of leaking a
// goroutine forever.
const defaultExportTimeout = 2 * time.Minute

// GoDispatcher runs the job in a new goroutine on a request-derived context
// (the job must outlive the request that started it, so cancellation is
// detached but values and the caller identity travel with it) with a
// timeout. Drain blocks until every dispatched job has finished, so the
// composition root can drain exports on shutdown instead of abandoning
// them mid-assembly. The zero value is usable with the default timeout.
type GoDispatcher struct {
	Timeout time.Duration

	mu sync.Mutex
	wg sync.WaitGroup
}

func (d *GoDispatcher) Dispatch(ctx context.Context, job exportJob, run func(context.Context, exportJob)) {
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = defaultExportTimeout
	}
	// WithoutCancel: a client disconnecting mid-export must not abort the
	// durable assembly it already admitted; the timeout still bounds it.
	jobCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	d.mu.Lock()
	d.wg.Add(1)
	d.mu.Unlock()
	go func() {
		defer d.wg.Done()
		defer cancel()
		run(jobCtx, job)
	}()
}

// Drain blocks until every job Dispatch started has finished.
func (d *GoDispatcher) Drain() {
	d.wg.Wait()
}

// Dependencies is everything the composition root supplies. Every field is
// required; NewServer fails closed if one is missing rather than treating a
// nil dependency as "feature disabled".
type Dependencies struct {
	Receipts      ReceiptStore
	Lineage       LineageSource
	Purposes      PurposePolicy
	Operations    *OperationJournal
	Artifacts     ArtifactSink
	Idempotency   *endpoint.Coordinator
	PackageKey    PackageKey
	Signer        cryptoagility.Key
	SigningPolicy cryptoagility.AlgorithmPolicy
	Dispatcher    Dispatcher
	Clock         func() time.Time
}

type server struct {
	evidencev1.UnimplementedEvidenceServiceServer
	deps Dependencies
}

// NewServer validates deps and returns a server. A missing dependency is
// refused at composition time, before any request can observe a
// half-configured cell.
func newServer(deps Dependencies) (*server, error) {
	if deps.Receipts == nil || deps.Lineage == nil || deps.Purposes == nil ||
		deps.Operations == nil || deps.Artifacts == nil || deps.Idempotency == nil ||
		deps.Signer.Algorithm == "" {
		return nil, fmt.Errorf("evidence: %w: every dependency is required", ErrNotConfigured)
	}
	if deps.Dispatcher == nil {
		deps.Dispatcher = &GoDispatcher{}
	}
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &server{deps: deps}, nil
}

func Register(srv *grpc.Server, deps Dependencies) error {
	s, err := newServer(deps)
	if err != nil {
		return err
	}
	evidencev1.RegisterEvidenceServiceServer(srv, s)
	return nil
}

func NewHandler(deps Dependencies, opts ...connect.HandlerOption) (http.Handler, error) {
	s, err := newServer(deps)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle(GetExecutionReceiptProcedure, connect.NewUnaryHandler(GetExecutionReceiptProcedure,
		func(ctx context.Context, req *connect.Request[evidencev1.GetExecutionReceiptRequest]) (*connect.Response[evidencev1.GetExecutionReceiptResponse], error) {
			res, err := s.GetExecutionReceipt(ctx, req.Msg)
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(res), nil
		}, opts...))
	mux.Handle(ExportIntentEvidenceProcedure, connect.NewUnaryHandler(ExportIntentEvidenceProcedure,
		func(ctx context.Context, req *connect.Request[evidencev1.ExportIntentEvidenceRequest]) (*connect.Response[evidencev1.ExportIntentEvidenceResponse], error) {
			res, err := s.ExportIntentEvidence(ctx, req.Msg)
			if err != nil {
				return nil, err
			}
			return connect.NewResponse(res), nil
		}, opts...))
	return mux, nil
}

func trustedContext(ctx context.Context) (*trust.Principal, *transport.Invocation, *envelope.Error) {
	inv, ok := transport.InvocationFromContext(ctx)
	if !ok {
		return nil, nil, envelope.New(envelope.CodeUnauthenticated, "evidence.no_trusted_context", "the request carries no trusted context")
	}
	p, ok := trust.FromContext(ctx)
	if !ok {
		return nil, inv, envelope.New(envelope.CodeUnauthenticated, "evidence.no_principal", "the request carries no authenticated principal").WithCorrelation(inv.RequestID())
	}
	return p, inv, nil
}

func requireField(field string) *envelope.Error {
	return envelope.New(envelope.CodeInvalidArgument, reasonInvalidRequest, "the request is malformed or structurally invalid").
		WithViolation(field, "the field is required and may not be the zero value", "evidence.request")
}

func notFound(inv *transport.Invocation) *envelope.Error {
	err := envelope.New(envelope.CodeNotFound, reasonNotFound, "the resource does not exist or is not visible")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	return err
}

func purposeDenied(inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	err := envelope.New(envelope.CodePermissionDenied, reasonUnauthorizedPurpose, "the caller is not authorized for the declared purpose of processing")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		err.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authorization"})
	}
	return err
}

func unavailable(inv *transport.Invocation, err error) *envelope.Error {
	out := envelope.New(envelope.CodeUnavailable, reasonUnavailable, "evidence services are unavailable")
	if inv != nil {
		out.WithCorrelation(inv.RequestID())
	}
	if err != nil {
		out.WithDiagnostic(err)
	}
	return out
}

func idempotencyError(err error) *envelope.Error {
	var payloadConflict *endpoint.PayloadConflict
	if errors.As(err, &payloadConflict) {
		return envelope.New(envelope.CodeAborted, reasonIdempotencyConflict, "the idempotency key was already used for a different request")
	}
	if errors.Is(err, endpoint.ErrIdempotencyKeyRequired) {
		return requireField("idempotency_key")
	}
	if errors.Is(err, endpoint.ErrScopeRequired) {
		return envelope.New(envelope.CodeInvalidArgument, reasonInvalidRequest, "the request is malformed or structurally invalid").WithDiagnostic(err)
	}
	if errors.Is(err, ErrLineageNotFound) {
		return notFound(nil)
	}
	if errors.Is(err, ErrLineageIncomplete) {
		return envelope.New(envelope.CodeFailedPrecondition, reasonLineageIncomplete, "a precondition for the operation is not met").WithDiagnostic(err)
	}
	return envelope.New(envelope.CodeUnavailable, reasonUnavailable, "the operation could not be completed").WithDiagnostic(err)
}
