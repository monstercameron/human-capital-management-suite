package people

import "testing"

func TestFacts_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestFacts_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

// TestTodo_WTIME_001_Facts proves the three fields WTIME-001 adds to the
// worker-state projection -- exemption status, time capture mode and the time
// profile reference -- are defined, validate, and are included in the
// default AllFields projection exactly like every other field this domain
// already carries.
func TestTodo_WTIME_001_Facts(t *testing.T) {
	for _, field := range []FieldID{FieldExemptionStatus, FieldTimeCaptureMode, FieldTimeProfileRef} {
		if err := field.Validate(); err != nil {
			t.Errorf("%s.Validate(): %v", field, err)
		}
	}

	all := AllFields()
	for _, field := range []FieldID{FieldExemptionStatus, FieldTimeCaptureMode, FieldTimeProfileRef} {
		found := false
		for _, f := range all {
			if f == field {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllFields() does not include %s", field)
		}
	}

	// The token spelling is part of the AuthZ contract: a client that
	// already recognises the "employment.*" family (FieldWorkerType,
	// FieldEmploymentID) recognises these the same way.
	for field, want := range map[FieldID]string{
		FieldExemptionStatus: "employment.exemption_status",
		FieldTimeCaptureMode: "employment.time_capture_mode",
		FieldTimeProfileRef:  "employment.time_profile_ref",
	} {
		if field.String() != want {
			t.Errorf("field token = %q, want %q", field.String(), want)
		}
	}
}

// TestTodo_WTIME_001_Facts_UnknownFieldStillRejected proves widening
// knownFields for the three new fields did not accidentally widen it for
// everything else: an undefined field is still refused.
func TestTodo_WTIME_001_Facts_UnknownFieldStillRejected(t *testing.T) {
	if err := FieldID("employment.made_up_field").Validate(); err == nil {
		t.Fatal("Validate() accepted an undefined field, want ErrUnknownField")
	}
}
