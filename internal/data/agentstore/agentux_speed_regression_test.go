package agentstore

import (
	"testing"
	"time"
)

func TestTodo_AGENTUX_SPEED_R9_ReissuedLeasePrecision(t *testing.T) {
	base := PersonaSecurityLease{TenantID: "tenant-a", AdmissionID: "admission-a", IssuedAt: time.Unix(100, 123), ExpiresAt: time.Unix(200, 500)}
	reissued := base
	reissued.IssuedAt = base.IssuedAt.Add(time.Second)
	reissued.ExpiresAt = base.ExpiresAt.Add(time.Microsecond - time.Nanosecond)
	if !samePersonaSecurityLease(base, reissued) {
		t.Fatal("lease reissue differing only in issue time and sub-microsecond expiry did not match")
	}
	reissued.ExpiresAt = base.ExpiresAt.Add(time.Microsecond)
	if samePersonaSecurityLease(base, reissued) {
		t.Fatal("one-microsecond expiry difference matched")
	}
	reissued = base
	reissued.AdmissionID = "admission-b"
	if samePersonaSecurityLease(base, reissued) {
		t.Fatal("different lease binding matched")
	}
}
