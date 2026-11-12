package importing

import (
	"io"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/importdraft"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The value types the browser client must link without the import pipeline
// live in the draft leaf package. The aliases below keep every caller of this
// package unchanged: a name here is the same type, constant, variable or
// function as its draft counterpart, so errors.Is and type identity hold.

// Staged batch and source types.
type (
	SourceKind       = importdraft.SourceKind
	SourceDescriptor = importdraft.SourceDescriptor
	Row              = importdraft.Row
	Batch            = importdraft.Batch
)

// Draft and validation value types.
type (
	DraftWrite          = importdraft.DraftWrite
	SimulationConflict  = importdraft.SimulationConflict
	BusinessIntentDraft = importdraft.BusinessIntentDraft
	ValidationSeverity  = importdraft.ValidationSeverity
	ValidationError     = importdraft.ValidationError
)

// Commit and checkpoint value types.
type (
	ItemRequest       = importdraft.ItemRequest
	ItemLineage       = importdraft.ItemLineage
	ItemCommitter     = importdraft.ItemCommitter
	ItemState         = importdraft.ItemState
	Checkpoint        = importdraft.Checkpoint
	CheckpointStore   = importdraft.CheckpointStore
	MemoryCheckpoints = importdraft.MemoryCheckpoints
)

// Batch limits.
const (
	MaxHeaderColumns    = importdraft.MaxHeaderColumns
	MaxColumnNameLength = importdraft.MaxColumnNameLength
	MaxCellBytes        = importdraft.MaxCellBytes
	MaxRows             = importdraft.MaxRows
	MaxSourceCSVBytes   = importdraft.MaxSourceCSVBytes
)

// Source kinds.
const (
	SourceKindUnspecified = importdraft.SourceKindUnspecified
	SourceKindCSV         = importdraft.SourceKindCSV
	SourceKindRecords     = importdraft.SourceKindRecords
)

// Severities.
const (
	ValidationSeverityError   = importdraft.ValidationSeverityError
	ValidationSeverityWarning = importdraft.ValidationSeverityWarning
)

// Item outcome and reconciliation statuses.
const (
	ItemCommitted          = importdraft.ItemCommitted
	ItemFailed             = importdraft.ItemFailed
	ItemNoOp               = importdraft.ItemNoOp
	ItemError              = importdraft.ItemError
	ItemConflict           = importdraft.ItemConflict
	ReconciliationMatched  = importdraft.ReconciliationMatched
	ReconciliationMismatch = importdraft.ReconciliationMismatch
)

// Batch and checkpoint errors.
var (
	ErrSourceInvalid      = importdraft.ErrSourceInvalid
	ErrHeaderInvalid      = importdraft.ErrHeaderInvalid
	ErrHeaderTooWide      = importdraft.ErrHeaderTooWide
	ErrColumnNameInvalid  = importdraft.ErrColumnNameInvalid
	ErrDuplicateColumn    = importdraft.ErrDuplicateColumn
	ErrCellTooLarge       = importdraft.ErrCellTooLarge
	ErrCellEncoding       = importdraft.ErrCellEncoding
	ErrRowWidthMismatch   = importdraft.ErrRowWidthMismatch
	ErrTooManyRows        = importdraft.ErrTooManyRows
	ErrSourceTooLarge     = importdraft.ErrSourceTooLarge
	ErrRetrievedAtUnset   = importdraft.ErrRetrievedAtUnset
	ErrCheckpointConflict = importdraft.ErrCheckpointConflict
)

// StageBatch stages pre-parsed records; see [importdraft.StageBatch].
func StageBatch(source SourceDescriptor, retrievedAt values.Instant, header []string, records [][]string) (Batch, error) {
	return importdraft.StageBatch(source, retrievedAt, header, records)
}

// StageCSV stages delimited text; see [importdraft.StageCSV].
func StageCSV(source SourceDescriptor, retrievedAt values.Instant, r io.Reader) (Batch, error) {
	return importdraft.StageCSV(source, retrievedAt, r)
}

// IdempotencyKey is the item key a commit presents for a row of a simulation.
func IdempotencyKey(simulationDigest, rowID string) string {
	return importdraft.IdempotencyKey(simulationDigest, rowID)
}

// NewMemoryCheckpoints returns an empty in-process [CheckpointStore].
func NewMemoryCheckpoints() *MemoryCheckpoints { return importdraft.NewMemoryCheckpoints() }
