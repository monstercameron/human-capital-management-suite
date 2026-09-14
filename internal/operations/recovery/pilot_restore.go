package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Isolated pilot restore drill (RECOVERY-003) proves the pilot can return
// inside a fenced recovery cell. It layers runtime-state preservation —
// timers, signals, frontier nodes, outbox entries and idempotency keys —
// over the tenant data-plane acceptance ([RestoreTenant]): the drill is
// READY only when the data plane conforms, every runtime count is
// accounted for, tombstones and holds are applied, and the destination is
// isolated with zero production effects. RPO comes from the data plane;
// RTO is measured from drill start to restore completion.
//
// The drill is pure: it certifies the fenced restore a recovery operator
// must then execute, and touches no production destination itself.

// RestoredRuntimeState is the runtime inventory the backup must preserve.
type RestoredRuntimeState struct {
	Timers          int    `json:"timers"`
	Signals         int    `json:"signals"`
	FrontierNodes   int    `json:"frontier_nodes"`
	OutboxEntries   int    `json:"outbox_entries"`
	IdempotencyKeys int    `json:"idempotency_keys"`
	StateDigest     string `json:"state_digest"`
}

// PilotRestoreRequest is the complete drill envelope.
type PilotRestoreRequest struct {
	DrillID     string               `json:"drill_id"`
	Destination string               `json:"destination"`
	StartedAt   time.Time            `json:"started_at"`
	Tenant      TenantRestoreRequest `json:"tenant"`
	Runtime     RestoredRuntimeState `json:"runtime"`
}

// PilotRestoreFinding names one drill defect.
type PilotRestoreFinding struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// PilotRestoreReport is the deterministic drill receipt.
type PilotRestoreReport struct {
	DrillID           string                `json:"drill_id"`
	Destination       string                `json:"destination"`
	Status            RestoreStatus         `json:"status"`
	RPO               time.Duration         `json:"rpo_ns"`
	RTO               time.Duration         `json:"rto_ns"`
	RestoredCounts    RestoredRuntimeState  `json:"restored_counts"`
	TombstonesApplied int                   `json:"tombstones_applied"`
	HoldsApplied      int                   `json:"holds_applied"`
	ProductionEffects bool                  `json:"production_effects"`
	Findings          []PilotRestoreFinding `json:"findings"`
	DataDigest        string                `json:"data_digest"`
	Digest            string                `json:"digest"`
}

// Explain returns a bounded summary suitable for an operator log.
func (r PilotRestoreReport) Explain() string {
	return fmt.Sprintf("pilot restore drill v1 id=%s dest=%s status=%s rpo=%s rto=%s production_effects=%t digest=%s",
		r.DrillID, r.Destination, r.Status, r.RPO, r.RTO, r.ProductionEffects, r.Digest)
}

// DrillPilotRestore evaluates one isolated restore drill. Malformed
// envelopes and production destinations are errors; lost runtime state
// or unconforming data fences the drill with findings.
func DrillPilotRestore(req PilotRestoreRequest) (PilotRestoreReport, error) {
	if err := validatePilotRestore(req); err != nil {
		return PilotRestoreReport{}, err
	}
	receipt, err := RestoreTenant(req.Tenant)
	if err != nil {
		return PilotRestoreReport{}, err
	}
	rep := PilotRestoreReport{
		DrillID: req.DrillID, Destination: req.Destination, Status: RestoreReady,
		RPO: receipt.RPO, RTO: req.Tenant.RestoredAt.Sub(req.StartedAt),
		RestoredCounts: req.Runtime, TombstonesApplied: receipt.DeletedCount,
		HoldsApplied: receipt.HeldCount,
		DataDigest:   receipt.DataDigest,
	}
	if receipt.Status != RestoreReady {
		rep.Status = RestoreFenced
		rep.Findings = append(rep.Findings, PilotRestoreFinding{
			Code: "DATA_PLANE_UNCONFORMING", Detail: "tenant data plane did not conform; service stays fenced"})
	}
	for _, count := range []struct {
		name  string
		value int
	}{
		{"timers", req.Runtime.Timers}, {"signals", req.Runtime.Signals},
		{"frontier_nodes", req.Runtime.FrontierNodes}, {"outbox_entries", req.Runtime.OutboxEntries},
		{"idempotency_keys", req.Runtime.IdempotencyKeys},
	} {
		if count.value <= 0 {
			rep.Status = RestoreFenced
			rep.Findings = append(rep.Findings, PilotRestoreFinding{
				Code: "RUNTIME_STATE_LOST", Detail: count.name + " inventory is absent from the restore"})
		}
	}
	if strings.TrimSpace(req.Runtime.StateDigest) == "" {
		rep.Status = RestoreFenced
		rep.Findings = append(rep.Findings, PilotRestoreFinding{
			Code: "RUNTIME_HASHES_INVALID", Detail: "runtime state digest is absent"})
	}
	sort.Slice(rep.Findings, func(i, j int) bool {
		if rep.Findings[i].Code != rep.Findings[j].Code {
			return rep.Findings[i].Code < rep.Findings[j].Code
		}
		return rep.Findings[i].Detail < rep.Findings[j].Detail
	})
	rep.Digest = digestPilotRestore(rep)
	return rep, nil
}

func validatePilotRestore(req PilotRestoreRequest) error {
	switch {
	case strings.TrimSpace(req.DrillID) == "":
		return fmt.Errorf("%w: drill id is required", ErrInvalidTenantRestore)
	case strings.TrimSpace(req.Destination) == "":
		return fmt.Errorf("%w: recovery destination is required", ErrInvalidTenantRestore)
	case req.Destination != strings.TrimSpace(req.Destination):
		return fmt.Errorf("%w: recovery destination is not clean", ErrInvalidTenantRestore)
	case isProductionDestination(req.Destination):
		return fmt.Errorf("%w: destination %q can reach production", ErrInvalidTenantRestore, req.Destination)
	case req.StartedAt.IsZero():
		return fmt.Errorf("%w: drill start is required", ErrInvalidTenantRestore)
	case req.Tenant.RestoredAt.Before(req.StartedAt):
		return fmt.Errorf("%w: restore completed before the drill started", ErrInvalidTenantRestore)
	}
	return nil
}

// isProductionDestination refuses any destination that is not explicitly a
// recovery cell. Unknown destinations fail closed: only names beginning
// with the recovery prefix may receive a restore.
func isProductionDestination(dest string) bool {
	lower := strings.ToLower(strings.TrimSpace(dest))
	return !strings.HasPrefix(lower, "recovery-")
}

func digestPilotRestore(rep PilotRestoreReport) string {
	rep.Digest = ""
	raw := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%s\x00%s",
		rep.DrillID, rep.Destination, rep.Status, int64(rep.RPO), int64(rep.RTO),
		rep.RestoredCounts.Timers, rep.RestoredCounts.Signals, rep.RestoredCounts.FrontierNodes,
		rep.RestoredCounts.OutboxEntries, rep.RestoredCounts.IdempotencyKeys,
		rep.DataDigest, rep.RestoredCounts.StateDigest)
	codes := make([]string, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		codes = append(codes, f.Code+"\x00"+f.Detail)
	}
	sort.Strings(codes)
	for _, c := range codes {
		raw += "\x00" + c
	}
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}
