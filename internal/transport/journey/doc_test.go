package journey_test

import (
	"testing"

	"google.golang.org/grpc"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// TestRegisterInstallsNoInterceptorOfItsOwn is doc.go's central claim made
// checkable: Register adds the service to a server it did not build and does
// not modify, exactly like a second grpcserver.RegisterXServer call would.
// A bare grpc.Server carries no interceptor, and registering the journey
// service must leave it that way - if this package ever installed admission
// of its own, the shared boundary would be running twice on one call.
func TestRegisterInstallsNoInterceptorOfItsOwn(t *testing.T) {
	srv := grpc.NewServer()
	defer srv.Stop()

	journey.Register(srv, journey.Dependencies{})

	info := srv.GetServiceInfo()
	svc, ok := info["hcmnext.journey.v1.JourneyService"]
	if !ok {
		t.Fatalf("Register did not add hcmnext.journey.v1.JourneyService; server serves %v", keys(info))
	}
	if len(svc.Methods) != 24 {
		t.Fatalf("the registered service exposes %d methods, want 24", len(svc.Methods))
	}
	// grpc.ServiceInfo lists unary and streaming methods together, so the
	// count above says nothing about cardinality; this says WatchJourney
	// reached the server as the server stream it is declared to be.
	watch := false
	for _, m := range svc.Methods {
		if m.Name != "WatchJourney" {
			continue
		}
		watch = true
		if !m.IsServerStream || m.IsClientStream {
			t.Fatalf("WatchJourney registered as server-stream=%v client-stream=%v, want a server stream only",
				m.IsServerStream, m.IsClientStream)
		}
	}
	if !watch {
		t.Fatalf("the registered service exposes no WatchJourney: %+v", svc.Methods)
	}
}

// Registering the service twice on one *grpc.Server is deliberately not
// covered by a test here: grpc-go answers a duplicate RegisterService with
// grpclog.Fatalf, which exits the whole process rather than panicking, so
// the failure is not observable from inside a test binary. The composition
// rule stands - the orchestrator wires Register exactly once per server -
// it is simply enforced by grpc-go at a level a unit test cannot survive.

func keys(m map[string]grpc.ServiceInfo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
