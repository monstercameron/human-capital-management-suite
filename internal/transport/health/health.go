// Package health owns the non-disclosing process liveness and admission
// readiness endpoints.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const ReadyService = "ready"

// Dependencies supplies bounded, role-aware readiness checks. ReadyCheck is
// expected to check the local admission prerequisites and return no details;
// details belong in authorized telemetry, never in the public response.
type Dependencies struct {
	Role          bootstrap.Role
	Live          func() bool
	ReadyCheck    func(context.Context) error
	CheckInterval time.Duration
	CheckTimeout  time.Duration
	Now           func() time.Time
}

type Server struct {
	healthpb.UnimplementedHealthServer
	deps      Dependencies
	mu        sync.Mutex
	checkMu   sync.Mutex
	checkedAt time.Time
	ready     bool
}

func Version() int { return 1 }

func Explain(ready bool) string {
	if ready {
		return "health v1 ready"
	}
	return "health v1 not-ready"
}

func New(deps Dependencies) *Server {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.CheckInterval <= 0 {
		deps.CheckInterval = 5 * time.Second
	}
	if deps.CheckTimeout <= 0 {
		deps.CheckTimeout = 2 * time.Second
	}
	return &Server{deps: deps}
}

func Register(srv *grpc.Server, deps Dependencies) { RegisterServer(srv, New(deps)) }

// RegisterServer installs an already composed health server. Sharing the
// same value across transports gives probes one cache and one dependency
// check budget for the process.
func RegisterServer(srv *grpc.Server, server *Server) {
	if srv == nil || server == nil {
		return
	}
	healthpb.RegisterHealthServer(srv, server)
}

func (s *Server) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	service := ""
	if req != nil {
		service = req.GetService()
	}
	serving, err := s.serving(ctx, service)
	if err != nil {
		return nil, err
	}
	return &healthpb.HealthCheckResponse{Status: servingStatus(serving)}, nil
}

// List returns the two intentionally public health names. It exposes no
// dependency names, role names or readiness failure reasons.
func (s *Server) List(ctx context.Context, _ *healthpb.HealthListRequest) (*healthpb.HealthListResponse, error) {
	live, err := s.serving(ctx, "")
	if err != nil {
		return nil, err
	}
	ready, err := s.serving(ctx, ReadyService)
	if err != nil {
		return nil, err
	}
	return &healthpb.HealthListResponse{Statuses: map[string]*healthpb.HealthCheckResponse{
		"":           {Status: servingStatus(live)},
		ReadyService: {Status: servingStatus(ready)},
	}}, nil
}

// Watch implements the standard gRPC health watch contract. The initial
// response is immediate; later responses are emitted only when the observed
// status changes. Polling is deliberately no faster than the configured
// readiness cache interval, keeping health traffic bounded.
func (s *Server) Watch(req *healthpb.HealthCheckRequest, stream grpc.ServerStreamingServer[healthpb.HealthCheckResponse]) error {
	if stream == nil {
		return status.Error(codes.InvalidArgument, "health stream is required")
	}
	service := ""
	if req != nil {
		service = req.GetService()
	}
	if service != "" && service != ReadyService {
		if err := stream.Send(&healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_UNKNOWN}); err != nil {
			return err
		}
		<-stream.Context().Done()
		return stream.Context().Err()
	}

	last, err := s.serving(stream.Context(), service)
	if err != nil {
		return err
	}
	if err := stream.Send(&healthpb.HealthCheckResponse{Status: servingStatus(last)}); err != nil {
		return err
	}
	interval := s.deps.CheckInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
			current, checkErr := s.serving(stream.Context(), service)
			if checkErr != nil {
				return checkErr
			}
			if current == last {
				continue
			}
			if err := stream.Send(&healthpb.HealthCheckResponse{Status: servingStatus(current)}); err != nil {
				return err
			}
			last = current
		}
	}
}

func (s *Server) serving(ctx context.Context, service string) (bool, error) {
	switch service {
	case "":
		return s.isLive(), nil
	case ReadyService:
		return s.isReady(ctx), nil
	default:
		return false, status.Error(codes.NotFound, "health service is not registered")
	}
}

func servingStatus(serving bool) healthpb.HealthCheckResponse_ServingStatus {
	if serving {
		return healthpb.HealthCheckResponse_SERVING
	}
	return healthpb.HealthCheckResponse_NOT_SERVING
}

func (s *Server) Healthz(w http.ResponseWriter, r *http.Request) { s.writeStatus(w, s.isLive()) }
func (s *Server) Readyz(w http.ResponseWriter, r *http.Request) {
	s.writeStatus(w, s.isReady(r.Context()))
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Healthz)
	mux.HandleFunc("/readyz", s.Readyz)
	return mux
}

func (s *Server) isLive() bool { return s.deps.Live == nil || s.deps.Live() }

func (s *Server) isReady(parent context.Context) bool {
	// Serialize refreshes so concurrent probes cannot stampede a dependency.
	// The lock is held only for the bounded check and is not held while reading
	// or writing the cached result.
	s.checkMu.Lock()
	defer s.checkMu.Unlock()

	now := s.deps.Now().UTC()
	s.mu.Lock()
	if !s.checkedAt.IsZero() && now.Sub(s.checkedAt) < s.deps.CheckInterval {
		ready := s.ready
		s.mu.Unlock()
		return ready
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, s.deps.CheckTimeout)
	defer cancel()
	results := make(chan error, 1)
	go func() {
		if s.deps.ReadyCheck == nil {
			results <- nil
			return
		}
		results <- s.deps.ReadyCheck(ctx)
	}()
	var err error
	select {
	case err = <-results:
	case <-ctx.Done():
		err = ctx.Err()
	}
	ready := err == nil && ctx.Err() == nil
	s.mu.Lock()
	s.checkedAt, s.ready = now, ready
	s.mu.Unlock()
	return ready
}

func (s *Server) writeStatus(w http.ResponseWriter, ok bool) {
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		OK bool `json:"ok"`
	}{OK: ok})
}
