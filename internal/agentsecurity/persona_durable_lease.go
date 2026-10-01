package agentsecurity

import (
	"errors"
	"strings"
	"time"
)

var ErrPersonaLeaseBinding = errors.New("agentsecurity: persona lease binding is invalid")

// DurablePersonaLease is the security-relevant projection of an agentstore
// lease. It contains evidence read from the committed admission and current
// durable scope epochs, not a caller-created authority resolver result.
type DurablePersonaLease struct {
	TenantID, LeaseID, AdmissionID, InvocationID, RunID    string
	IssuerID, AuthorityRef, PolicyDigest                   string
	PrincipalID, PersonaID, PersonaVersion, InstallationID string
	Mode, AdmissionDecision                                string
	TenantEpoch, PrincipalEpoch, PersonaEpoch              int64
	VersionEpoch, InstallationEpoch, RunEpoch              int64
	IssuedAt, ExpiresAt, AdmissionDeadline                 time.Time
}

// CheckDurablePersonaLease refuses a lease unless the durable admission,
// independent authority reference, and all captured revocation scopes agree.
func CheckDurablePersonaLease(lease DurablePersonaLease, current map[string]int64, at time.Time) error {
	if strings.TrimSpace(lease.TenantID) == "" || strings.TrimSpace(lease.LeaseID) == "" ||
		strings.TrimSpace(lease.AdmissionID) == "" || strings.TrimSpace(lease.RunID) == "" ||
		strings.TrimSpace(lease.InvocationID) == "" ||
		strings.TrimSpace(lease.IssuerID) == "" || strings.TrimSpace(lease.AuthorityRef) == "" ||
		lease.IssuerID == lease.PrincipalID || !validDigest(lease.PolicyDigest) ||
		strings.TrimSpace(lease.PrincipalID) == "" || strings.TrimSpace(lease.PersonaID) == "" ||
		strings.TrimSpace(lease.PersonaVersion) == "" || strings.TrimSpace(lease.InstallationID) == "" ||
		lease.Mode != "ON_BEHALF_OF" || lease.AdmissionDecision != "ACCEPTED" ||
		lease.IssuedAt.IsZero() || lease.ExpiresAt.IsZero() || lease.AdmissionDeadline.IsZero() ||
		!lease.ExpiresAt.After(lease.IssuedAt) || lease.ExpiresAt.After(lease.AdmissionDeadline) ||
		at.IsZero() || at.Before(lease.IssuedAt) || !at.Before(lease.ExpiresAt) {
		return ErrPersonaLeaseBinding
	}
	expected := map[string]int64{
		"TENANT:" + lease.TenantID:                                lease.TenantEpoch,
		"PRINCIPAL:" + lease.PrincipalID:                          lease.PrincipalEpoch,
		"PERSONA:" + lease.PersonaID:                              lease.PersonaEpoch,
		"VERSION:" + lease.PersonaID + ":" + lease.PersonaVersion: lease.VersionEpoch,
		"INSTALLATION:" + lease.InstallationID:                    lease.InstallationEpoch,
		"RUN:" + lease.RunID:                                      lease.RunEpoch,
	}
	for key, epoch := range expected {
		if epoch <= 0 || current[key] != epoch {
			return ErrPersonaLeaseBinding
		}
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
