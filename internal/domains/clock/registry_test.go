package clock

import (
	"errors"
	"testing"
)

func validRegistration(id string) Registration {
	return Registration{ID: id, Version: "v1", State: Active, OwnerRef: "owner:workforce", LocationRef: "loc:nyc", ClockTrustPolicy: "clock-trust/v1", OfflinePolicy: "offline:bounded", ReplayPolicy: "replay:sequence", SignaturePolicy: "sig:ed25519", FirmwarePolicy: "firmware:current", CertificatePolicy: "cert:device", RetentionPolicy: "retention:time-punch"}
}

// TestTodo_CLOCK_001 is the primary matrix entry: a valid complete
// registration is accepted and incomplete governance is refused.
func TestTodo_CLOCK_001(t *testing.T) {
	if _, err := NewRegistry([]TimeDevice{validRegistration("device-1")}, []TimeSource{validRegistration("source-1")}); err != nil {
		t.Fatalf("valid registration rejected: %v", err)
	}
	for _, field := range []string{"OwnerRef", "LocationRef", "ClockTrustPolicy", "OfflinePolicy", "ReplayPolicy", "SignaturePolicy", "FirmwarePolicy", "CertificatePolicy", "RetentionPolicy"} {
		r := validRegistration("device-1")
		switch field {
		case "OwnerRef":
			r.OwnerRef = ""
		case "LocationRef":
			r.LocationRef = ""
		case "ClockTrustPolicy":
			r.ClockTrustPolicy = ""
		case "OfflinePolicy":
			r.OfflinePolicy = ""
		case "ReplayPolicy":
			r.ReplayPolicy = ""
		case "SignaturePolicy":
			r.SignaturePolicy = ""
		case "FirmwarePolicy":
			r.FirmwarePolicy = ""
		case "CertificatePolicy":
			r.CertificatePolicy = ""
		case "RetentionPolicy":
			r.RetentionPolicy = ""
		}
		var rej *Rejection
		if _, err := NewRegistry([]TimeDevice{r}, nil); !errors.As(err, &rej) || rej.Code != "CLOCK_001_REJECTED" {
			t.Errorf("%s: got %v, want CLOCK_001_REJECTED", field, err)
		}
	}
}

func TestTodo_CLOCK_001_Property(t *testing.T) {
	input := []TimeDevice{validRegistration("d")}
	r, err := NewRegistry(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	input[0].OwnerRef = "mutated"
	if r.Devices[0].OwnerRef == "mutated" {
		t.Fatal("registry aliases caller slice")
	}
}

func TestTodo_CLOCK_001_Conformance(t *testing.T) {
	r, err := NewRegistry(nil, []TimeSource{validRegistration("s")})
	if err != nil || r.Version != RegistryVersion || len(r.Sources) != 1 {
		t.Fatalf("registry=%+v err=%v", r, err)
	}
}

func TestTodo_CLOCK_001_Recovery(t *testing.T) {
	if _, err := NewRegistry([]TimeDevice{validRegistration("same"), validRegistration("same")}, nil); err == nil {
		t.Fatal("duplicate registrations accepted")
	}
}

func TestTodo_CLOCK_001_Mutation(t *testing.T) {
	r := validRegistration("d")
	r.RetentionPolicy = " "
	var rej *Rejection
	if _, err := NewRegistry([]TimeDevice{r}, nil); !errors.As(err, &rej) || rej.Field != "retention_policy" {
		t.Fatalf("rejection=%v", err)
	}
}

func TestTodo_CLOCK_001_ConformanceAdmission(t *testing.T) {
	active := validRegistration("device-1")
	revokedDevice := validRegistration("device-2")
	revokedDevice.State = Revoked
	revoked := validRegistration("source-1")
	revoked.State = Revoked
	registry, err := NewRegistry([]TimeDevice{active, revokedDevice}, []TimeSource{revoked})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := registry.Device("device-1"); !ok || got.ID != "device-1" {
		t.Fatalf("device lookup = %+v, %v", got, ok)
	}
	if got, ok := registry.Source("source-1"); !ok || got.State != Revoked {
		t.Fatalf("source lookup = %+v, %v", got, ok)
	}
	if err := registry.AcceptsDevice("device-1"); err != nil {
		t.Fatalf("active device rejected: %v", err)
	}
	if err := registry.AcceptsDevice("device-2"); !errors.Is(err, ErrInvalidRegistration) {
		t.Fatalf("revoked device error = %v", err)
	}
	if err := registry.AcceptsSource("source-1"); !errors.Is(err, ErrInvalidRegistration) {
		t.Fatalf("revoked source error = %v", err)
	}
	if err := registry.AcceptsDevice("missing"); !errors.Is(err, ErrInvalidRegistration) {
		t.Fatalf("missing device error = %v", err)
	}
}
