package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

func TestClockSignalReceiver_RejectsBeforeTransaction(t *testing.T) {
	tenant := uuid.New()
	base := clockservice.SignalDelivery{TenantID: tenant, SessionID: "s", Signal: "BREAK_START", EventType: "event", Source: "clock", CorrelationKey: "subject", CorrelationValue: "s", SchemaRef: "time/v1", IdempotencyKey: "obs", OccurredAt: time.Unix(1, 0), ExpectedInstanceID: uuid.New()}
	tests := []struct {
		name     string
		receiver ClockSignalReceiver
		delivery clockservice.SignalDelivery
	}{
		{"missing database", ClockSignalReceiver{Verify: acceptingSignalVerifier{}, Now: func() time.Time { return time.Unix(2, 0) }}, base},
		{"missing verifier", ClockSignalReceiver{Now: func() time.Time { return time.Unix(2, 0) }}, base},
		{"missing expected instance", ClockSignalReceiver{DB: rejectingBeginner{}, Verify: acceptingSignalVerifier{}, Now: func() time.Time { return time.Unix(2, 0) }}, func() clockservice.SignalDelivery { d := base; d.ExpectedInstanceID = uuid.Nil; return d }()},
		{"missing occurred time", ClockSignalReceiver{DB: rejectingBeginner{}, Verify: acceptingSignalVerifier{}, Now: func() time.Time { return time.Unix(2, 0) }}, func() clockservice.SignalDelivery { d := base; d.OccurredAt = time.Time{}; return d }()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.receiver.Receive(context.Background(), tc.delivery); err == nil {
				t.Fatal("invalid signal delivery accepted")
			}
		})
	}
}

type rejectingBeginner struct{}

func (rejectingBeginner) Begin(context.Context) (dbport.Tx, error) { return nil, context.Canceled }

type acceptingSignalVerifier struct{}

func (acceptingSignalVerifier) Verify(stepSignal.Signal) error { return nil }
