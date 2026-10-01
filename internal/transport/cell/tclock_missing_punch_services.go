package cell

import (
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"google.golang.org/grpc"
)

// registerMissingPunch mounts correction commands on the existing trusted
// native gRPC server; all correction authority belongs to the application.
func registerMissingPunch(server *grpc.Server, service timeclock.MissingPunchApplication) {
	if server != nil && service != nil {
		timeclock.RegisterMissingPunch(server, service)
	}
}

// missingPunchHTTP admits the trusted principal before invoking correction
// commands through the same application port used by native gRPC.
func missingPunchHTTP(cfg transport.Config, service timeclock.MissingPunchApplication) http.Handler {
	return trustedPrincipalHTTP(cfg, timeclock.NewMissingPunchServer(service).HTTPHandler())
}
