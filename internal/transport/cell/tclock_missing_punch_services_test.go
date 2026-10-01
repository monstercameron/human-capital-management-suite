package cell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
)

type missingPunchCellFake struct{ calls int }

func (f *missingPunchCellFake) SubmitCorrection(context.Context, *trust.Principal, timeclock.MissingPunchSubmit) (timeclock.MissingPunchCorrection, error) {
	f.calls++
	return timeclock.MissingPunchCorrection{}, nil
}

func (f *missingPunchCellFake) ReviewCorrection(context.Context, *trust.Principal, timeclock.MissingPunchReview) (timeclock.MissingPunchCorrection, error) {
	f.calls++
	return timeclock.MissingPunchCorrection{}, nil
}

func TestTodo_TCLOCK_011_MissingPunchMountedOnTrustedSurfaces(t *testing.T) {
	service := &missingPunchCellFake{}
	server := grpc.NewServer()
	registerMissingPunch(server, service)
	if _, ok := server.GetServiceInfo()["hcmnext.time.v1.MissingPunchService"]; !ok {
		t.Fatal("native correction service is absent")
	}
	registerMissingPunch(nil, service)
	registerMissingPunch(server, nil)
	now := time.Unix(100, 0).UTC()
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	handler := missingPunchHTTP(transporttest.Config(verifier, func() time.Time { return now }, "clock-cell", nil), service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/time/missing-punch/SubmitCorrection", nil))
	if response.Code != http.StatusUnauthorized || service.calls != 0 {
		t.Fatalf("unauthenticated correction reached application: status=%d calls=%d", response.Code, service.calls)
	}
}
