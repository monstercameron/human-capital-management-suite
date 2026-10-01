package cell

import (
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"google.golang.org/grpc"
)

// registerWorkerClock mounts the browser worker self-clock service on the
// already-admitted native gRPC server. The application port remains the sole
// authority for human, tenant, worker, revision and workflow checks.
func registerWorkerClock(server *grpc.Server, service timeclock.WorkerSelfService) {
	if server == nil || service == nil {
		return
	}
	timeclock.RegisterWorkerClock(server, service)
}

// registerTunnelWorkerClock puts the worker self-clock service on the browser
// tunnel's server: composed when supplied, the "not turned on" answer
// otherwise, so the tunnel's registered services and its allowlist agree in
// both modes and the page is told the reason instead of a generic outage.
func registerTunnelWorkerClock(server *grpc.Server, service timeclock.WorkerSelfService) {
	if server == nil {
		return
	}
	if service == nil {
		timeclock.RegisterNotEnabledWorkerClock(server)
		return
	}
	timeclock.RegisterWorkerClock(server, service)
}

// workerClockHTTP adapts the self-clock handler to the canonical HTTP
// principal admission chain. It deliberately exposes only the application
// worker-self routes; worker or assignment identity never enters the request.
func workerClockHTTP(cfg transport.Config, service timeclock.WorkerSelfService) http.Handler {
	return trustedPrincipalHTTP(cfg, timeclock.WorkerSelfHTTPHandler(service))
}
