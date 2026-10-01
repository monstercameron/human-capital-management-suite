package timeclock

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"unicode"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// HTTPHandler returns the JSON projection of the same application port used by
// gRPC. Paths are the lower-camel RPC names under /v1/time/clock-device/.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s == nil || s.app == nil {
			writeAPIError(w, http.StatusServiceUnavailable, status.Error(codes.Unavailable, "clock device service is unavailable"))
			return
		}
		if r.Method != http.MethodPost {
			writeAPIError(w, http.StatusMethodNotAllowed, status.Error(codes.Unimplemented, "method not allowed"))
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			writeAPIError(w, http.StatusRequestEntityTooLarge, status.Error(codes.ResourceExhausted, "request body exceeds 1 MiB"))
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/v1/time/clock-device/")
		if path == r.URL.Path {
			writeAPIError(w, http.StatusNotFound, status.Error(codes.NotFound, "clock device method not found"))
			return
		}
		var req, out interface{}
		switch path {
		case "CreateEnrollmentCode":
			req = &timev1.CreateEnrollmentCodeRequest{}
		case "EnrollDevice":
			req = &timev1.EnrollDeviceRequest{}
		case "RotateDeviceKey":
			req = &timev1.RotateDeviceKeyRequest{}
		case "RevokeDevice":
			req = &timev1.RevokeDeviceRequest{}
		case "SyncRoster":
			req = &timev1.SyncRosterRequest{}
		case "IdentifyWorker":
			req = &timev1.IdentifyWorkerRequest{}
		case "SubmitPunches":
			req = &timev1.SubmitPunchesRequest{}
		case "Heartbeat":
			req = &timev1.HeartbeatRequest{}
		case "GetWorkerStatus":
			req = &timev1.GetWorkerStatusRequest{}
		default:
			writeAPIError(w, http.StatusNotFound, status.Error(codes.NotFound, "clock device method not found"))
			return
		}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, req.(interface{ ProtoReflect() protoreflect.Message })); err != nil {
			writeAPIError(w, http.StatusBadRequest, status.Error(codes.InvalidArgument, "invalid protobuf JSON"))
			return
		}
		var callErr error
		switch v := req.(type) {
		case *timev1.CreateEnrollmentCodeRequest:
			out, callErr = s.CreateEnrollmentCode(r.Context(), v)
		case *timev1.EnrollDeviceRequest:
			out, callErr = s.EnrollDevice(r.Context(), v)
		case *timev1.RotateDeviceKeyRequest:
			out, callErr = s.RotateDeviceKey(r.Context(), v)
		case *timev1.RevokeDeviceRequest:
			out, callErr = s.RevokeDevice(r.Context(), v)
		case *timev1.SyncRosterRequest:
			out, callErr = s.SyncRoster(r.Context(), v)
		case *timev1.IdentifyWorkerRequest:
			out, callErr = s.IdentifyWorker(r.Context(), v)
		case *timev1.SubmitPunchesRequest:
			out, callErr = s.SubmitPunches(r.Context(), v)
		case *timev1.HeartbeatRequest:
			out, callErr = s.Heartbeat(r.Context(), v)
		case *timev1.GetWorkerStatusRequest:
			out, callErr = s.GetWorkerStatus(r.Context(), v)
		}
		if callErr != nil {
			writeAPIError(w, httpStatus(callErr), callErr)
			return
		}
		message, ok := out.(proto.Message)
		if !ok || message == nil || (reflect.ValueOf(message).Kind() == reflect.Ptr && reflect.ValueOf(message).IsNil()) {
			writeAPIError(w, http.StatusServiceUnavailable, status.Error(codes.Unavailable, "clock device service returned no response"))
			return
		}
		payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(message)
		if err != nil {
			http.Error(w, "encode response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	})
}

func writeAPIError(w http.ResponseWriter, httpCode int, err error) {
	st, ok := status.FromError(err)
	if !ok {
		st = status.New(codes.Internal, "internal error")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	_ = json.NewEncoder(w).Encode(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: grpcCodeName(st.Code()), Message: st.Message()})
}

func grpcCodeName(code codes.Code) string {
	name := code.String()
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func httpStatus(err error) int {
	code := status.Code(err)
	switch code {
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.FailedPrecondition:
		return http.StatusPreconditionFailed
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists:
		return http.StatusConflict
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Aborted:
		return http.StatusConflict
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}
