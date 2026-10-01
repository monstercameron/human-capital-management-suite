package subscription

import "testing"

func TestTodo_TCLOCK_012_Conformance(t *testing.T) {
	for _, kind := range []EventKind{EventClockPunchAccepted, EventClockPunchRejected,
		EventClockSessionOpened, EventClockSessionClosed, EventClockExceptionRaised,
		EventClockTimecardApproved, EventClockTimecardReopened, EventClockDeviceOffline} {
		t.Run(string(kind), func(t *testing.T) {
			if !kind.Valid() {
				t.Fatal("declared clock event rejected")
			}
			schema, err := NewEventSchema(kind, 1, []SchemaField{{Name: "subject_id", Type: TypeRef, Classification: ClassificationConfidential}})
			if err != nil {
				t.Fatal(err)
			}
			registry := NewSchemaRegistry()
			if err := registry.Register(schema); err != nil {
				t.Fatal(err)
			}
			if _, ok := registry.Schema(kind, 1); !ok {
				t.Fatal("clock schema not registered")
			}
		})
	}
	for _, kind := range []EventKind{"clock.*", "clock.punch", "clock.punch.deleted", "CLOCK.PUNCH.ACCEPTED"} {
		if kind.Valid() {
			t.Fatalf("undeclared event accepted: %q", kind)
		}
	}
}
