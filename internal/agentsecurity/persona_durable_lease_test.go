package agentsecurity

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckDurablePersonaLeaseRequiresAdmissionAuthorityAndCurrentEpochs(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	lease := DurablePersonaLease{
		TenantID: "tenant-a", LeaseID: "lease-a", AdmissionID: "admission-a", InvocationID: "invocation-a", RunID: "run-a",
		IssuerID: "security-issuer", AuthorityRef: "grant-a", PolicyDigest: "sha256:" + strings.Repeat("a", 64),
		PrincipalID: "invoker-a", PersonaID: "persona-a", PersonaVersion: "3", InstallationID: "room-a",
		Mode: "ON_BEHALF_OF", AdmissionDecision: "ACCEPTED", TenantEpoch: 1, PrincipalEpoch: 2,
		PersonaEpoch: 3, VersionEpoch: 4, InstallationEpoch: 5, RunEpoch: 6,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), AdmissionDeadline: now.Add(2 * time.Minute),
	}
	current := map[string]int64{
		"TENANT:tenant-a": 1, "PRINCIPAL:invoker-a": 2, "PERSONA:persona-a": 3,
		"VERSION:persona-a:3": 4, "INSTALLATION:room-a": 5, "RUN:run-a": 6,
	}
	if err := CheckDurablePersonaLease(lease, current, now); err != nil {
		t.Fatalf("valid durable lease: %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*DurablePersonaLease, map[string]int64)
		at     time.Time
	}{
		{name: "refused admission", mutate: func(v *DurablePersonaLease, _ map[string]int64) { v.AdmissionDecision = "REFUSED" }},
		{name: "missing invocation", mutate: func(v *DurablePersonaLease, _ map[string]int64) { v.InvocationID = " " }},
		{name: "not yet issued", at: now.Add(-time.Minute - time.Nanosecond)},
		{name: "sponsored principal", mutate: func(v *DurablePersonaLease, _ map[string]int64) { v.Mode = "SPONSORED" }},
		{name: "issuer impersonates invoker", mutate: func(v *DurablePersonaLease, _ map[string]int64) { v.IssuerID = v.PrincipalID }},
		{name: "missing independent authority", mutate: func(v *DurablePersonaLease, _ map[string]int64) { v.AuthorityRef = " " }},
		{name: "bad policy digest", mutate: func(v *DurablePersonaLease, _ map[string]int64) { v.PolicyDigest = "sha256:bad" }},
		{name: "epoch changed", mutate: func(_ *DurablePersonaLease, epochs map[string]int64) { epochs["INSTALLATION:room-a"]++ }},
		{name: "scope omitted", mutate: func(_ *DurablePersonaLease, epochs map[string]int64) { delete(epochs, "RUN:run-a") }},
		{name: "expired", at: now.Add(time.Minute)},
		{name: "outlives admission", mutate: func(v *DurablePersonaLease, _ map[string]int64) { v.ExpiresAt = v.AdmissionDeadline.Add(time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := lease
			epochs := make(map[string]int64, len(current))
			for key, epoch := range current {
				epochs[key] = epoch
			}
			if tc.mutate != nil {
				tc.mutate(&candidate, epochs)
			}
			at := tc.at
			if at.IsZero() {
				at = now
			}
			if err := CheckDurablePersonaLease(candidate, epochs, at); !errors.Is(err, ErrPersonaLeaseBinding) {
				t.Fatalf("lease check error = %v, want ErrPersonaLeaseBinding", err)
			}
		})
	}
}
