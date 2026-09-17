package partnerapp

import (
	"errors"
	"testing"
	"time"
)

func app006Version(t *testing.T) ApplicationVersion {
	t.Helper()
	draft, err := NewDraft(partnerApplication(), partnerVersionRequest(), partnerAgreement())
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := draft.RecordReview("reviewer-1", true, "coverage checked")
	if err != nil {
		t.Fatal(err)
	}
	active, err := reviewed.Activate()
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func app006Evidence(at time.Time) []DimensionEvidence {
	dims := AllCertificationDimensions()
	out := make([]DimensionEvidence, 0, len(dims))
	for _, d := range dims {
		out = append(out, DimensionEvidence{Dimension: d, EvidenceRef: "evidence:" + string(d), Assessor: "assessor-2", AssessedAt: at, ValidFor: 30 * 24 * time.Hour})
	}
	return out
}

// TestTodo_APP_006 is the RED contract: certification demands repeatable
// evidence across all nine dimensions, and missing or expired evidence
// removes certified status.
func TestTodo_APP_006(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	version := app006Version(t)
	cert, err := Certify(version, app006Evidence(at), at)
	if err != nil {
		t.Fatalf("Certify: %v", err)
	}
	if cert.Status != StatusCertified || cert.Digest == "" || cert.Explain() == "" {
		t.Fatalf("certification = %+v", cert)
	}
	if err := cert.ValidAt(at.Add(time.Hour)); err != nil {
		t.Fatalf("ValidAt: %v", err)
	}

	// Seeded defect: the security evidence lapses but the marketplace still
	// shows the version as certified.
	stale := app006Evidence(at)
	stale[0].ValidFor = time.Hour
	lapsed, err := Certify(version, stale, at.Add(2*time.Hour))
	if !errors.Is(err, ErrCertificationEvidence) {
		t.Fatalf("expired err=%v, want evidence refusal", err)
	}
	if lapsed.Status != StatusDecertified {
		t.Fatalf("expired status = %s, want DECERTIFIED", lapsed.Status)
	}
	if err := lapsed.ValidAt(at.Add(2 * time.Hour)); !errors.Is(err, ErrCertificationEvidence) {
		t.Fatalf("lapsed ValidAt err=%v, want evidence refusal", err)
	}

	// Missing the exit dimension entirely.
	partial := app006Evidence(at)[:len(app006Evidence(at))-1]
	missing, err := Certify(version, partial, at)
	if !errors.Is(err, ErrCertificationEvidence) || missing.Status != StatusDecertified {
		t.Fatalf("missing dimension cert=%+v err=%v", missing, err)
	}
}

// TestTodo_APP_006_Fault proves faulted evidence — future assessment,
// duplicate dimension, blank ref — refuses before any status is granted.
func TestTodo_APP_006_Fault(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	version := app006Version(t)

	future := app006Evidence(at)
	future[0].AssessedAt = at.Add(48 * time.Hour)
	if _, err := Certify(version, future, at); !errors.Is(err, ErrInvalidCertification) {
		t.Fatalf("future assessment err=%v", err)
	}
	dup := append(app006Evidence(at), app006Evidence(at)[0])
	if _, err := Certify(version, dup, at); !errors.Is(err, ErrInvalidCertification) {
		t.Fatalf("duplicate dimension err=%v", err)
	}
	blank := app006Evidence(at)
	blank[1].EvidenceRef = "  "
	if _, err := Certify(version, blank, at); !errors.Is(err, ErrInvalidCertification) {
		t.Fatalf("blank ref err=%v", err)
	}
	var zero ApplicationVersion
	if _, err := Certify(zero, app006Evidence(at), at); !errors.Is(err, ErrInvalidCertification) {
		t.Fatalf("unpublished version err=%v", err)
	}
}

// TestTodo_APP_006_Security proves the assessor is never the requester and
// evidence refs stay opaque non-blank pins.
func TestTodo_APP_006_Security(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	version := app006Version(t)
	self := app006Evidence(at)
	for i := range self {
		self[i].Assessor = version.Requester
	}
	if _, err := Certify(version, self, at); !errors.Is(err, ErrInvalidCertification) {
		t.Fatalf("self-certification err=%v", err)
	}
	cert, err := Certify(version, app006Evidence(at), at)
	if err != nil {
		t.Fatal(err)
	}
	if cert.ApplicationID != version.ApplicationID || cert.VersionDigest != version.Digest {
		t.Fatalf("certification does not bind the version: %+v", cert)
	}
}

// TestTodo_APP_006_Conformance proves the certification is a deterministic
// function of the version digest plus evidence: replays agree, and a new
// version never inherits the old certification.
func TestTodo_APP_006_Conformance(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	version := app006Version(t)
	first, err := Certify(version, app006Evidence(at), at)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Certify(version, app006Evidence(at), at)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || first.ExpiresAt != second.ExpiresAt {
		t.Fatal("identical certification inputs diverge")
	}
	next, err := version.Deprecate("requester-1", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := Certify(next, app006Evidence(at), at)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Digest == first.Digest {
		t.Fatal("successor version inherited the prior certification digest")
	}
	if moved.VersionDigest != next.Digest {
		t.Fatal("successor certification does not bind the successor digest")
	}
}
