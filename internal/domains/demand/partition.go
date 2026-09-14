// Demand/supply partition: DEMAND-003 compares required demand with
// assigned supply and partitions every signal into covered, short,
// excess or unknown by interval, dimension and source snapshot.
//
// Only qualified and available workers satisfy a requirement: their
// assigned quantities sum against the signal. Unknown-confidence
// signals resolve unknown rather than covered. The partition cites
// its source snapshot and digests deterministically.
package demand

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CellOutcome is the closed per-signal partition.
type CellOutcome string

// The cell outcomes.
const (
	CellCovered CellOutcome = "COVERED"
	CellShort   CellOutcome = "SHORT"
	CellExcess  CellOutcome = "EXCESS"
	CellUnknown CellOutcome = "UNKNOWN"
)

// SupplyAssignment binds one supply reference to a worker with its
// qualification and availability. Unqualified or unavailable workers
// contribute zero: they cannot satisfy a requirement.
type SupplyAssignment struct {
	Supply    SupplyReference
	WorkerRef string
	Qualified bool
	Available bool
}

// CoverageCell is one partitioned signal.
type CoverageCell struct {
	SignalID  string
	Interval  string
	Dimension string
	Outcome   CellOutcome
	Required  string
	Assigned  string
}

// DemandPartition is the deterministic snapshot-cited result.
type DemandPartition struct {
	RequirementID string
	Snapshot      string
	Cells         []CoverageCell
	Digest        string
}

// PartitionDemand compares one requirement against assigned supply.
func PartitionDemand(requirement CoverageRequirement, assignments []SupplyAssignment, snapshot string) (DemandPartition, error) {
	if err := requirement.Validate(); err != nil {
		return DemandPartition{}, err
	}
	if strings.TrimSpace(snapshot) == "" {
		return DemandPartition{}, errors.New("demand: source snapshot is required")
	}
	for _, assignment := range assignments {
		if err := assignment.Supply.Validate(); err != nil {
			return DemandPartition{}, err
		}
		if strings.TrimSpace(assignment.WorkerRef) == "" {
			return DemandPartition{}, errors.New("demand: assignment worker ref is required")
		}
	}
	partition := DemandPartition{RequirementID: requirement.RequirementID, Snapshot: snapshot}
	signals := append([]DemandSignal(nil), requirement.Signals...)
	sort.Slice(signals, func(i, j int) bool { return signals[i].SignalID < signals[j].SignalID })
	for _, signal := range signals {
		partition.Cells = append(partition.Cells, partitionSignal(signal, assignments))
	}
	partition.Digest = digestPartition(partition)
	return partition, nil
}

func partitionSignal(signal DemandSignal, assignments []SupplyAssignment) CoverageCell {
	cell := CoverageCell{
		SignalID:  signal.SignalID,
		Interval:  signal.Work.String(),
		Dimension: demandScope(signal.Location, signal.OrgUnit) + "\x00" + demandTarget(signal),
		Required:  signal.Quantity.String(),
	}
	if signal.ConfidenceClass == ConfidenceUnknown {
		cell.Outcome = CellUnknown
		return cell
	}
	// The accumulator matches the signal's declared scale: kernel
	// quantity arithmetic requires matching scales.
	total, err := values.NewQuantity("0", signal.Quantity.Unit(), signal.Quantity.Value().Scale(), values.RoundingHalfEven)
	if err != nil {
		cell.Outcome = CellUnknown
		return cell
	}
	for _, assignment := range assignments {
		if !supplyApplies(signal, assignment) {
			continue
		}
		supply := assignment.Supply
		total, err = total.Add(supply.Quantity)
		if err != nil {
			cell.Outcome = CellUnknown
			return cell
		}
	}
	cell.Assigned = total.String()
	switch cmp := total.Value().Cmp(signal.Quantity.Value()); {
	case cmp == 0:
		cell.Outcome = CellCovered
	case cmp < 0:
		cell.Outcome = CellShort
	default:
		cell.Outcome = CellExcess
	}
	return cell
}

func digestPartition(partition DemandPartition) string {
	parts := []string{"demand003-partition", partition.RequirementID, partition.Snapshot}
	for _, cell := range partition.Cells {
		parts = append(parts, strings.Join([]string{
			cell.SignalID, cell.Interval, cell.Dimension,
			string(cell.Outcome), cell.Required, cell.Assigned,
		}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
