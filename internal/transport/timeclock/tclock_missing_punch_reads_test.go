package timeclock

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/encoding/protojson"
)

type missingPunchReadFake struct{}

func (missingPunchReadFake) GetCorrectionContext(context.Context, *trust.Principal, string) (MissingPunchCorrectionContext, []MissingPunchCorrection, error) {
	return MissingPunchCorrectionContext{SessionID: "session-1", WorkerRef: "worker-1", Revision: 4, Timezone: "America/New_York", PeriodRef: "period-1", PeriodClosed: true, OriginalIn: &MissingPunchPunchFact{EventType: "CLOCK_IN", ObservationID: "obs-in-1", OccurredAt: time.Unix(1, 0).UTC()}, OriginalWorkflowID: "hcmnext.workflows.time.clock_in_out", OriginalWorkflowInstanceRef: "00000000-0000-0000-0000-000000000003"}, []MissingPunchCorrection{{RequestID: "req-1", Status: "PENDING"}}, nil
}

func (missingPunchReadFake) ListPendingCorrections(context.Context, *trust.Principal, uint32) ([]MissingPunchCorrection, error) {
	return []MissingPunchCorrection{{RequestID: "req-1", Status: "PENDING"}}, nil
}

type missingPunchReadApp struct {
	missingPunchFake
	missingPunchReadFake
}

func TestTodo_TCLOCK011_ReadProjectionUsesPrincipalBoundPort(t *testing.T) {
	s := NewMissingPunchServer(&missingPunchReadApp{})
	p := missingPunchPrincipal(t, trust.SubjectKindHuman)
	got, err := s.GetCorrectionContext(trust.WithPrincipal(context.Background(), p), &timev1.GetCorrectionContextRequest{SessionId: "session-1"})
	if err != nil || got.GetContext().GetWorkerRef() != "worker-1" || got.GetContext().GetOriginalIn().GetEventType() != "CLOCK_IN" || got.GetContext().GetOriginalIn().GetObservationId() != "obs-in-1" || !got.GetContext().GetPeriodClosed() || len(got.GetPendingCorrections()) != 1 {
		t.Fatalf("projection=%v err=%v", got, err)
	}
	list, err := s.ListPendingCorrections(trust.WithPrincipal(context.Background(), p), &timev1.ListPendingCorrectionsRequest{PageSize: 1})
	if err != nil || len(list.GetCorrections()) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
}

func TestTodo_TCLOCK011_ReadProjectionHTTPParityAndAuth(t *testing.T) {
	server := NewMissingPunchServer(&missingPunchReadApp{})
	body, err := protojson.Marshal(&timev1.GetCorrectionContextRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/time/missing-punch/GetCorrectionContext", strings.NewReader(string(body))).WithContext(trust.WithPrincipal(context.Background(), missingPunchPrincipal(t, trust.SubjectKindHuman)))
	w := httptest.NewRecorder()
	server.HTTPHandler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "worker_ref") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "/v1/time/missing-punch/ListPendingCorrections", strings.NewReader(`{"page_size":101}`)).WithContext(trust.WithPrincipal(context.Background(), missingPunchPrincipal(t, trust.SubjectKindHuman)))
	w = httptest.NewRecorder()
	server.HTTPHandler().ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("oversized page status=%d body=%s", w.Code, w.Body.String())
	}
}
