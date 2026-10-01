package timeclock

import (
	"context"
	"io"
	"net/http"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MissingPunchCorrectionContext is the server-owned session projection used
// by an administrator to understand a correction without loading raw events.
type MissingPunchCorrectionContext struct {
	SessionID, WorkerRef, Timezone, PeriodRef       string
	Revision                                        uint64
	PeriodClosed                                    bool
	OriginalIn, OriginalOut                         *MissingPunchPunchFact
	OriginalWorkflowID, OriginalWorkflowInstanceRef string
}

// MissingPunchPunchFact is one immutable original punch fact.
type MissingPunchPunchFact struct {
	EventType, ObservationID string
	OccurredAt               time.Time
}

// MissingPunchReadApplication is an optional read port for the admin
// projection. Commands remain available when this port is not wired.
type MissingPunchReadApplication interface {
	GetCorrectionContext(context.Context, *trust.Principal, string) (MissingPunchCorrectionContext, []MissingPunchCorrection, error)
	ListPendingCorrections(context.Context, *trust.Principal, uint32) ([]MissingPunchCorrection, error)
}

// GetCorrectionContext returns server-resolved session facts and pending
// requests visible to the authenticated administrator.
func (s *MissingPunchServer) GetCorrectionContext(ctx context.Context, in *timev1.GetCorrectionContextRequest) (*timev1.GetCorrectionContextResponse, error) {
	if in == nil || in.GetSessionId() == "" {
		return nil, statusInvalid("session_id is required")
	}
	p, app, err := s.readPort(ctx)
	if err != nil {
		return nil, err
	}
	projection, pending, err := app.GetCorrectionContext(ctx, p, in.GetSessionId())
	if err != nil {
		return nil, missingPunchError(err)
	}
	return &timev1.GetCorrectionContextResponse{Context: correctionContext(projection), PendingCorrections: missingPunchCorrections(pending)}, nil
}

// ListPendingCorrections returns only requests scoped by the authenticated
// administrator; worker and supervisor scope never come from the wire.
func (s *MissingPunchServer) ListPendingCorrections(ctx context.Context, in *timev1.ListPendingCorrectionsRequest) (*timev1.ListPendingCorrectionsResponse, error) {
	if in == nil {
		return nil, statusInvalid("request is required")
	}
	pageSize := in.GetPageSize()
	if pageSize == 0 {
		pageSize = 100
	}
	if pageSize > 100 {
		return nil, statusInvalid("page_size must be between 1 and 100")
	}
	p, app, err := s.readPort(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := app.ListPendingCorrections(ctx, p, pageSize)
	if err != nil {
		return nil, missingPunchError(err)
	}
	return &timev1.ListPendingCorrectionsResponse{Corrections: missingPunchCorrections(rows)}, nil
}

func (s *MissingPunchServer) readPort(ctx context.Context) (*trust.Principal, MissingPunchReadApplication, error) {
	p, err := s.trustedHuman(ctx)
	if err != nil {
		return nil, nil, err
	}
	app, ok := s.app.(MissingPunchReadApplication)
	if !ok {
		return nil, nil, statusUnavailable("missing punch read projection is unavailable")
	}
	return p, app, nil
}

func correctionContext(in MissingPunchCorrectionContext) *timev1.CorrectionContext {
	out := &timev1.CorrectionContext{SessionId: in.SessionID, WorkerRef: in.WorkerRef, Revision: in.Revision, Timezone: in.Timezone, PeriodRef: in.PeriodRef, OriginalWorkflowId: in.OriginalWorkflowID, OriginalWorkflowInstanceRef: in.OriginalWorkflowInstanceRef, PeriodClosed: in.PeriodClosed}
	if in.OriginalIn != nil {
		out.OriginalIn = correctionPunchFact(in.OriginalIn)
	}
	if in.OriginalOut != nil {
		out.OriginalOut = correctionPunchFact(in.OriginalOut)
	}
	return out
}

func correctionPunchFact(in *MissingPunchPunchFact) *timev1.CorrectionPunchFact {
	return &timev1.CorrectionPunchFact{EventType: in.EventType, OccurredAt: timestamppb.New(in.OccurredAt), ObservationId: in.ObservationID}
}

func missingPunchCorrections(in []MissingPunchCorrection) []*timev1.MissingPunchCorrection {
	out := make([]*timev1.MissingPunchCorrection, 0, len(in))
	for _, row := range in {
		out = append(out, missingPunchCorrection(row))
	}
	return out
}

func statusInvalid(message string) error     { return status.Error(codes.InvalidArgument, message) }
func statusUnavailable(message string) error { return status.Error(codes.Unavailable, message) }

func (s *MissingPunchServer) readHTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, status.Error(codes.Unimplemented, "method not allowed"))
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			writeAPIError(w, http.StatusRequestEntityTooLarge, status.Error(codes.ResourceExhausted, "request body exceeds 1 MiB"))
			return
		}
		var out proto.Message
		switch r.URL.Path {
		case "/v1/time/missing-punch/GetCorrectionContext":
			in := &timev1.GetCorrectionContextRequest{}
			if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, in); err != nil {
				writeAPIError(w, http.StatusBadRequest, statusInvalid("invalid protobuf JSON"))
				return
			}
			out, err = s.GetCorrectionContext(r.Context(), in)
		case "/v1/time/missing-punch/ListPendingCorrections":
			in := &timev1.ListPendingCorrectionsRequest{}
			if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, in); err != nil {
				writeAPIError(w, http.StatusBadRequest, statusInvalid("invalid protobuf JSON"))
				return
			}
			out, err = s.ListPendingCorrections(r.Context(), in)
		default:
			writeAPIError(w, http.StatusNotFound, status.Error(codes.NotFound, "missing punch read method not found"))
			return
		}
		if err != nil {
			writeAPIError(w, httpStatus(err), err)
			return
		}
		payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(out)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, status.Error(codes.Internal, "encode response"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	})
}
