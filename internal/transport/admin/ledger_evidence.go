package admin

import (
	"context"
	"sort"
	"time"

	"google.golang.org/grpc"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// LedgerEvidenceExport is the application-owned read port that builds an
// offline-verifiable package for the caller's tenant and recorded-time window.
type LedgerEvidenceExport func(context.Context, string, time.Time, time.Time) (map[string][]byte, error)

type ledgerEvidenceServer struct {
	adminv1.UnimplementedLedgerEvidenceServiceServer
	export LedgerEvidenceExport
}

// RegisterLedgerEvidence adds the read-only evidence export service to the
// admitted gRPC server. The composition root supplies the data-plane exporter.
func RegisterLedgerEvidence(srv *grpc.Server, export LedgerEvidenceExport) {
	adminv1.RegisterLedgerEvidenceServiceServer(srv, &ledgerEvidenceServer{export: export})
}

func (s *ledgerEvidenceServer) ExportLedgerEvidence(ctx context.Context, req *adminv1.ExportLedgerEvidenceRequest) (*adminv1.ExportLedgerEvidenceResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if s.export == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.ledger_evidence_unconfigured",
			"the ledger evidence export is not configured").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	if req.GetScope() == nil || req.GetScope().GetTenantId() == "" {
		return nil, evidenceInvalid(principal, inv, "tenant scope is required")
	}
	if req.GetScope().GetTenantId() != principal.Tenant().String() {
		return nil, envelope.New(envelope.CodePermissionDenied,
			"admin.ledger_evidence_tenant_denied",
			"the request tenant does not match the caller tenant").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	from, to := req.GetFrom(), req.GetTo()
	if from == nil || to == nil || from.CheckValid() != nil || to.CheckValid() != nil || !from.AsTime().Before(to.AsTime()) {
		return nil, evidenceInvalid(principal, inv, "from and to must define a valid non-empty half-open window")
	}
	files, err := s.export(ctx, principal.Tenant().String(), from.AsTime(), to.AsTime())
	if err != nil {
		return nil, envelope.Coerce(err)
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	response := &adminv1.ExportLedgerEvidenceResponse{
		EvidenceRef: evidenceRef(principal, "admin.export_ledger_evidence"),
		Files:       make([]*adminv1.EvidencePackageFile, 0, len(paths)),
	}
	for _, path := range paths {
		response.Files = append(response.Files, &adminv1.EvidencePackageFile{
			Path: path, Content: append([]byte(nil), files[path]...),
		})
	}
	return response, nil
}

func evidenceInvalid(principal *trust.Principal, inv *transport.Invocation, reason string) *envelope.Error {
	return envelope.New(envelope.CodeInvalidArgument,
		"admin.ledger_evidence_request_invalid", reason).
		WithCorrelation(inv.RequestID()).
		WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
}
