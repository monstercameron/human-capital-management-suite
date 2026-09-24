package document

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// ProcedurePrefix is the canonical Connect/gRPC HTTP path prefix for the
// document service. A composition mounts this prefix after applying the
// same admission the gRPC surface applies, exactly as
// internal/transport/chat.ProcedurePrefix is mounted for chat.
const ProcedurePrefix = "/hcmnext.document.v1.DocumentService/"

// MaxRequestBytes bounds a decoded Connect request body, matching the
// canonical edge's own bound.
const MaxRequestBytes = 4 << 20

// Dependencies is the HTTP projection's composition root. It carries the
// same Service port and page-cursor signing key the gRPC surface
// ([Register]) uses, so a caller reaches identical idempotency, revision,
// authorization and pagination behavior over either protocol.
type Dependencies struct {
	Service   Service
	CursorKey []byte
}

// NewHandler builds the Connect HTTP projection of the document RPC methods.
// Every handler calls the same *server method the gRPC adapter calls, so the
// HTTP and native RPC paths share one implementation and can never diverge on
// idempotency, revision checks, authorization or typed errors.
func NewHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{service: deps.Service, cursorKey: append([]byte(nil), deps.CursorKey...)}
	// A caller-supplied option still wins: the bound is prepended, not
	// appended, so a composition that needs a different ceiling can say so.
	opts = append([]connect.HandlerOption{connect.WithReadMaxBytes(MaxRequestBytes)}, opts...)
	mux := http.NewServeMux()
	registerDocumentUnary(mux, opts, documentv1.DocumentService_ListDocuments_FullMethodName, s.ListDocuments)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_CreateDocument_FullMethodName, s.CreateDocument)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_GetDocument_FullMethodName, s.GetDocument)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_ShareDocument_FullMethodName, s.ShareDocument)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_ListDocumentComments_FullMethodName, s.ListDocumentComments)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_AddDocumentComment_FullMethodName, s.AddDocumentComment)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_ResolveDocumentComment_FullMethodName, s.ResolveDocumentComment)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_CreateDocumentVersion_FullMethodName, s.CreateDocumentVersion)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_GetDocumentLibrary_FullMethodName, s.GetDocumentLibrary)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_CreateDocumentFolder_FullMethodName, s.CreateDocumentFolder)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_RenameDocumentFolder_FullMethodName, s.RenameDocumentFolder)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_DeleteDocumentFolder_FullMethodName, s.DeleteDocumentFolder)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_MoveDocuments_FullMethodName, s.MoveDocuments)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_SetDocumentStarred_FullMethodName, s.SetDocumentStarred)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_ListDocumentAccess_FullMethodName, s.ListDocumentAccess)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_RevokeDocumentAccess_FullMethodName, s.RevokeDocumentAccess)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_GetDocumentPreviews_FullMethodName, s.GetDocumentPreviews)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_PlaceDocument_FullMethodName, s.PlaceDocument)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_GetDocumentPlacement_FullMethodName, s.GetDocumentPlacement)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_TransferDocumentOwnership_FullMethodName, s.TransferDocumentOwnership)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_ListDocumentOwnershipHistory_FullMethodName, s.ListDocumentOwnershipHistory)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_ProposeCrossCompanyGrant_FullMethodName, s.ProposeCrossCompanyGrant)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_AcceptCrossCompanyGrant_FullMethodName, s.AcceptCrossCompanyGrant)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_RevokeCrossCompanyGrant_FullMethodName, s.RevokeCrossCompanyGrant)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_SearchDocuments_FullMethodName, s.SearchDocuments)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_AgentSearchDocuments_FullMethodName, s.AgentSearchDocuments)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_GetDocumentVersion_FullMethodName, s.GetDocumentVersion)
	registerDocumentUnary(mux, opts, documentv1.DocumentService_GetDocumentBacklinks_FullMethodName, s.GetDocumentBacklinks)
	return mux
}

func registerDocumentUnary[Req, Res any](mux *http.ServeMux, opts []connect.HandlerOption, proc string, fn func(context.Context, *Req) (*Res, error)) {
	mux.Handle(proc, connect.NewUnaryHandler(proc, func(c context.Context, r *connect.Request[Req]) (*connect.Response[Res], error) {
		v, err := fn(c, r.Msg)
		if err != nil {
			return nil, httpError(err)
		}
		return connect.NewResponse(v), nil
	}, opts...))
}

// httpError projects an owned condition onto the Connect wire error the
// gRPC adapter already carries via [envelope.Error.GRPCStatus]. Without this,
// a bare envelope.Error reaching the Connect runtime falls back to
// CodeUnknown, so an HTTP caller would see a different, less specific error
// than a native RPC caller for the identical refusal; converting here keeps
// idempotency, revision, authorization and validation refusals typed
// identically on both protocols even when no admission interceptor is
// composed in front of this handler.
func httpError(err error) error {
	if err == nil {
		return nil
	}
	owned, ok := envelope.As(err)
	if !ok {
		return err
	}
	return edge.ToConnectError(owned)
}
