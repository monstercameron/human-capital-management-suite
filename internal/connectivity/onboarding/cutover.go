package onboarding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ONBOARD-005: source-freeze, delta and authority cutover.
//
// Moving authority for a domain from a source system to this platform is one
// signed epoch, and it is issued only when every gate holds in order: the
// source is frozen at a recorded version and has not changed since; the delta
// from the base snapshot through the frozen version committed completely;
// validation found no errors; the simulation was zero-effect and is the one
// the approval names; the approver is not the requester; replication lag is
// within its bound; reconciliation found no mismatch; no writer other than the
// cutover itself touched the target; and the prior epoch is still current. The
// first failing gate blocks the cutover and yields a safe abort that leaves
// authority with the source -- nothing half-cuts over.

// Cutover gates, in evaluation order.
const (
	GateFreeze         = "FREEZE"
	GateSourceStable   = "SOURCE_STABLE"
	GateDualWriter     = "DUAL_WRITER"
	GateEpoch          = "EPOCH_CURRENT"
	GateDelta          = "DELTA"
	GateValidation     = "VALIDATION"
	GateSimulation     = "SIMULATION"
	GateApproval       = "APPROVAL"
	GateLag            = "LAG"
	GateReconciliation = "RECONCILIATION"
)

// CutoverGates lists the gates in order.
func CutoverGates() []string {
	return []string{GateFreeze, GateSourceStable, GateDualWriter, GateEpoch, GateDelta, GateValidation, GateSimulation, GateApproval, GateLag, GateReconciliation}
}

// Cutover outcomes.
const (
	CutoverEpochSigned = "EPOCH_SIGNED"
	CutoverAborted     = "ABORTED"
)

// ErrCutoverSigning reports a signer failure.
var ErrCutoverSigning = errors.New("onboarding: cutover epoch signing failed")

// FreezeRecord is the source freeze.
type FreezeRecord struct {
	Token         string
	SourceVersion string
	FrozenAt      time.Time
}

// DeltaRecord is the committed delta.
type DeltaRecord struct {
	BaseVersion    string
	ThroughVersion string
	CommitDigest   string
	Complete       bool
	Failed         int
	Pending        int
}

// CutoverEvidence is everything the gates judge.
type CutoverEvidence struct {
	Tenant          string
	Domain          string
	SourceAuthority string
	TargetAuthority string

	Freeze                  FreezeRecord
	CurrentSourceVersion    string
	SourceWritesSinceFreeze int
	// TargetWriters are the writers observed on the target since the freeze;
	// only CutoverWriter may appear.
	TargetWriters []string
	CutoverWriter string

	PriorEpoch   uint64
	CurrentEpoch uint64

	Delta                    DeltaRecord
	ValidationErrors         int
	SimulationDigest         string
	SimulationZeroEffect     bool
	ApprovedSimulationDigest string
	ApprovedBy               string
	RequestedBy              string
	ReplicationLag           time.Duration
	MaxReplicationLag        time.Duration
	ReconciliationMismatches int
	DecidedAt                time.Time
}

// AuthorityEpoch is the signed transfer of authority.
type AuthorityEpoch struct {
	Tenant              string    `json:"tenant"`
	Domain              string    `json:"domain"`
	FromAuthority       string    `json:"from_authority"`
	ToAuthority         string    `json:"to_authority"`
	Epoch               uint64    `json:"epoch"`
	FreezeToken         string    `json:"freeze_token"`
	FrozenSourceVersion string    `json:"frozen_source_version"`
	DeltaCommitDigest   string    `json:"delta_commit_digest"`
	SimulationDigest    string    `json:"simulation_digest"`
	ApprovedBy          string    `json:"approved_by"`
	IssuedAt            time.Time `json:"issued_at"`
	Digest              string    `json:"digest"`
	Signature           string    `json:"signature"`
	SigningAuthority    string    `json:"signing_authority"`
}

// CutoverSigner signs epochs.
type CutoverSigner interface {
	Authority() string
	Sign(payload []byte) (string, error)
	Verify(payload []byte, signature string) error
}

// CutoverDecision is the result of evaluating a cutover.
type CutoverDecision struct {
	Outcome    string
	FailedGate string
	Reason     string
	Epoch      *AuthorityEpoch
	// Authority is who holds authority after the decision.
	Authority string
}

// DecideCutover evaluates the gates in order and signs the epoch when all
// hold.
func DecideCutover(ev CutoverEvidence, signer CutoverSigner) (CutoverDecision, error) {
	if gate, reason := firstFailedGate(ev); gate != "" {
		return CutoverDecision{Outcome: CutoverAborted, FailedGate: gate, Reason: reason, Authority: ev.SourceAuthority}, nil
	}
	if signer == nil || signer.Authority() == "" {
		return CutoverDecision{}, fmt.Errorf("%w: no signing authority", ErrCutoverSigning)
	}
	epoch := AuthorityEpoch{
		Tenant: ev.Tenant, Domain: ev.Domain, FromAuthority: ev.SourceAuthority, ToAuthority: ev.TargetAuthority,
		Epoch: ev.PriorEpoch + 1, FreezeToken: ev.Freeze.Token, FrozenSourceVersion: ev.Freeze.SourceVersion,
		DeltaCommitDigest: ev.Delta.CommitDigest, SimulationDigest: ev.SimulationDigest, ApprovedBy: ev.ApprovedBy,
		IssuedAt: ev.DecidedAt.UTC(), SigningAuthority: signer.Authority(),
	}
	payload := epochPayload(epoch)
	sum := sha256.Sum256(payload)
	epoch.Digest = "sha256:" + hex.EncodeToString(sum[:])
	sig, err := signer.Sign(payload)
	if err != nil || sig == "" {
		return CutoverDecision{}, fmt.Errorf("%w: %v", ErrCutoverSigning, err)
	}
	if err := signer.Verify(payload, sig); err != nil {
		return CutoverDecision{}, fmt.Errorf("%w: signature does not verify: %v", ErrCutoverSigning, err)
	}
	epoch.Signature = sig
	return CutoverDecision{Outcome: CutoverEpochSigned, Epoch: &epoch, Authority: ev.TargetAuthority}, nil
}

// VerifyEpoch checks an epoch's digest and signature.
func VerifyEpoch(e AuthorityEpoch, signer CutoverSigner) error {
	if signer == nil {
		return fmt.Errorf("%w: no verifier", ErrCutoverSigning)
	}
	payload := epochPayload(e)
	sum := sha256.Sum256(payload)
	if e.Digest != "sha256:"+hex.EncodeToString(sum[:]) || e.SigningAuthority != signer.Authority() {
		return fmt.Errorf("%w: epoch digest or authority mismatch", ErrCutoverSigning)
	}
	return signer.Verify(payload, e.Signature)
}

func epochPayload(e AuthorityEpoch) []byte {
	e.Digest, e.Signature = "", ""
	b, _ := json.Marshal(e)
	return b
}

func firstFailedGate(ev CutoverEvidence) (string, string) {
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	switch {
	case blank(ev.Tenant) || blank(ev.Domain) || blank(ev.SourceAuthority) || blank(ev.TargetAuthority) || ev.SourceAuthority == ev.TargetAuthority ||
		blank(ev.Freeze.Token) || blank(ev.Freeze.SourceVersion) || ev.Freeze.FrozenAt.IsZero() || ev.DecidedAt.IsZero() || ev.DecidedAt.Before(ev.Freeze.FrozenAt):
		return GateFreeze, "no complete source freeze precedes the decision"
	case ev.CurrentSourceVersion != ev.Freeze.SourceVersion || ev.SourceWritesSinceFreeze != 0:
		return GateSourceStable, "the source changed after it was frozen"
	case dualWriter(ev):
		return GateDualWriter, "a writer other than the cutover wrote to the target"
	case ev.PriorEpoch != ev.CurrentEpoch:
		return GateEpoch, "the authority epoch moved since the cutover was prepared"
	case !ev.Delta.Complete || ev.Delta.Failed != 0 || ev.Delta.Pending != 0 || blank(ev.Delta.CommitDigest) || ev.Delta.ThroughVersion != ev.Freeze.SourceVersion || blank(ev.Delta.BaseVersion):
		return GateDelta, "the delta through the frozen version is not completely committed"
	case ev.ValidationErrors != 0:
		return GateValidation, "validation reported errors"
	case blank(ev.SimulationDigest) || !ev.SimulationZeroEffect:
		return GateSimulation, "no zero-effect simulation"
	case ev.ApprovedSimulationDigest != ev.SimulationDigest || blank(ev.ApprovedBy) || strings.EqualFold(ev.ApprovedBy, ev.RequestedBy):
		return GateApproval, "the approval does not bind this simulation or is self-approved"
	case ev.MaxReplicationLag <= 0 || ev.ReplicationLag < 0 || ev.ReplicationLag > ev.MaxReplicationLag:
		return GateLag, "replication lag is unbounded or exceeds its limit"
	case ev.ReconciliationMismatches != 0:
		return GateReconciliation, "reconciliation found mismatches"
	}
	return "", ""
}

func dualWriter(ev CutoverEvidence) bool {
	for _, w := range ev.TargetWriters {
		if w != ev.CutoverWriter {
			return true
		}
	}
	return strings.TrimSpace(ev.CutoverWriter) == ""
}
