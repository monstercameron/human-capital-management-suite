package importdraft

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Limits bound every batch this package will stage. A staging path with no
// bound is a staging path that can be made to allocate without limit by
// whatever produced the file; every limit here is a hard rejection, never a
// silent truncation.
const (
	// MaxHeaderColumns bounds the width of a batch.
	MaxHeaderColumns = 512
	// MaxColumnNameLength bounds one column name.
	MaxColumnNameLength = 128
	// MaxCellBytes bounds one cell.
	MaxCellBytes = 32 * 1024
	// MaxRows bounds the number of rows a single batch may stage.
	MaxRows = 500_000
	// MaxSourceCSVBytes bounds the total input read by StageCSV. It is
	// enforced before a full parse, so an oversized file is rejected without
	// buffering it.
	MaxSourceCSVBytes = 64 * 1024 * 1024
)

// Batch errors. All are matchable with errors.Is.
var (
	ErrSourceInvalid     = errors.New("importing: source descriptor is invalid")
	ErrHeaderInvalid     = errors.New("importing: header is invalid")
	ErrHeaderTooWide     = errors.New("importing: header exceeds the maximum column count")
	ErrColumnNameInvalid = errors.New("importing: column name is empty, too long, or not valid UTF-8")
	ErrDuplicateColumn   = errors.New("importing: header names the same column twice")
	ErrCellTooLarge      = errors.New("importing: cell exceeds the maximum size")
	ErrCellEncoding      = errors.New("importing: cell is not valid UTF-8")
	ErrRowWidthMismatch  = errors.New("importing: row does not have one cell per header column")
	ErrTooManyRows       = errors.New("importing: batch exceeds the maximum row count")
	ErrSourceTooLarge    = errors.New("importing: source input exceeds the maximum byte budget")
	ErrRetrievedAtUnset  = errors.New("importing: retrieved-at instant is unset")
)

// SourceKind names the kind of origin a batch was staged from. The
// vocabulary is closed: this package parses CSV text and accepts pre-parsed
// in-memory records; it does not reach into a connector, an object store or a
// filesystem to fetch bytes for a caller.
type SourceKind uint8

// Source kinds.
const (
	// SourceKindUnspecified is the zero value and is never legal.
	SourceKindUnspecified SourceKind = iota
	// SourceKindCSV is delimited text parsed with encoding/csv.
	SourceKindCSV
	// SourceKindRecords is caller-supplied, already-parsed rows (for example,
	// a page decoded upstream from an API response).
	SourceKindRecords
)

var sourceKindWire = map[SourceKind]string{
	SourceKindCSV:     "CSV",
	SourceKindRecords: "RECORDS",
}

// String returns the stable wire token, or "SOURCE_KIND_UNSPECIFIED".
func (k SourceKind) String() string {
	if s, ok := sourceKindWire[k]; ok {
		return s
	}
	return "SOURCE_KIND_UNSPECIFIED"
}

// Valid reports whether k is a declared source kind.
func (k SourceKind) Valid() bool { _, ok := sourceKindWire[k]; return ok }

// SourceDescriptor names where a batch's bytes came from, without carrying
// the bytes themselves. URI is an opaque, caller-supplied locator (a
// filename, an upload reference, an API request id); this package never
// dereferences it.
type SourceDescriptor struct {
	Kind   SourceKind
	URI    string
	Tenant values.TenantId
}

// Validate reports whether the descriptor is complete and well formed.
func (s SourceDescriptor) Validate() error {
	if !s.Kind.Valid() {
		return fmt.Errorf("%w: source kind is unspecified", ErrSourceInvalid)
	}
	if s.URI == "" {
		return fmt.Errorf("%w: source uri is empty", ErrSourceInvalid)
	}
	if !utf8.ValidString(s.URI) {
		return fmt.Errorf("%w: source uri is not valid UTF-8", ErrSourceInvalid)
	}
	if err := s.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrSourceInvalid, err)
	}
	return nil
}

// canonical writes the descriptor's canonical bytes into w.
func (s SourceDescriptor) canonical(w *Canon) {
	w.Str(s.Kind.String()).Str(s.URI).Str(string(s.Tenant))
}

// Row is one staged, content-addressed record. Its identity is derived from
// its own content and the batch's header only - never from its position - so
// that reordering a batch never changes which rows are "the same" one.
//
// Row carries no exported field and no mutation method: the only way to
// obtain one is [StageBatch] or [StageCSV], and the only way to read one is
// through its accessors, each of which returns a defensive copy.
type Row struct {
	id    string
	cells []string
}

// ID returns the row's content-addressed identifier.
func (r Row) ID() string { return r.id }

// Cells returns a copy of the row's cell values, aligned with the batch's
// header.
func (r Row) Cells() []string { return append([]string(nil), r.cells...) }

// Cell returns the cell at the given header index and whether it exists.
func (r Row) Cell(index int) (string, bool) {
	if index < 0 || index >= len(r.cells) {
		return "", false
	}
	return r.cells[index], true
}

// rowDigest computes the content address of one row: the header digest binds
// the row to the exact schema it was read under, and the cells are written in
// order (order is the record's own field order, which is meaningful and must
// not be sorted away).
func rowDigest(headerDigest string, cells []string) string {
	w := NewCanon("hcmnext.dataops.importing.Row", 1)
	w.Str(headerDigest)
	w.Strings(cells)
	return w.DigestHex()
}

// Batch is an immutable, content-addressed set of staged rows. Staging a
// batch never writes to any domain: the type holds no store, no connector,
// and exposes no method that could mutate a row or append to the row set
// after construction.
type Batch struct {
	source      SourceDescriptor
	retrievedAt values.Instant
	header      []string
	rows        []Row
	digest      string
}

// Source returns the batch's source descriptor.
func (b Batch) Source() SourceDescriptor { return b.source }

// RetrievedAt returns when the batch's bytes were retrieved.
func (b Batch) RetrievedAt() values.Instant { return b.retrievedAt }

// Header returns a copy of the column names, in file order.
func (b Batch) Header() []string { return append([]string(nil), b.header...) }

// RowCount returns the number of staged rows.
func (b Batch) RowCount() int { return len(b.rows) }

// Rows returns a copy of the staged rows, in file order.
func (b Batch) Rows() []Row { return append([]Row(nil), b.rows...) }

// Row returns the row at the given position and whether it exists. Position
// is a read convenience only; row identity is [Row.ID], not this index.
func (b Batch) Row(i int) (Row, bool) {
	if i < 0 || i >= len(b.rows) {
		return Row{}, false
	}
	return b.rows[i], true
}

// ColumnIndex returns the header position of a column name and whether it was
// found.
func (b Batch) ColumnIndex(column string) (int, bool) {
	for i, c := range b.header {
		if c == column {
			return i, true
		}
	}
	return -1, false
}

// Digest is the batch's content address: source, schema, retrieval time and
// every row's own content address, in file order. Two stagings of
// byte-identical input at the same retrieval instant always produce the same
// digest; changing the source, the header, any cell, or the row order always
// changes it.
func (b Batch) Digest() string { return b.digest }

// Validate reports whether the batch is internally coherent. StageBatch and
// StageCSV never return a batch that fails this check.
func (b Batch) Validate() error {
	if err := b.source.Validate(); err != nil {
		return err
	}
	if !b.retrievedAt.IsSet() {
		return ErrRetrievedAtUnset
	}
	if len(b.header) == 0 {
		return fmt.Errorf("%w: no columns", ErrHeaderInvalid)
	}
	for _, r := range b.rows {
		if len(r.cells) != len(b.header) {
			return fmt.Errorf("%w: row %s has %d cells, header has %d",
				ErrRowWidthMismatch, r.id, len(r.cells), len(b.header))
		}
	}
	return nil
}

// validateHeader rejects an empty, oversized, duplicate or malformed set of
// column names.
func validateHeader(header []string) error {
	if len(header) == 0 {
		return fmt.Errorf("%w: no columns", ErrHeaderInvalid)
	}
	if len(header) > MaxHeaderColumns {
		return fmt.Errorf("%w: %d columns exceeds %d", ErrHeaderTooWide, len(header), MaxHeaderColumns)
	}
	seen := make(map[string]struct{}, len(header))
	for _, name := range header {
		if name == "" || len(name) > MaxColumnNameLength {
			return fmt.Errorf("%w: %q", ErrColumnNameInvalid, name)
		}
		if !utf8.ValidString(name) {
			return fmt.Errorf("%w: column name is not valid UTF-8", ErrColumnNameInvalid)
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("%w: %q", ErrDuplicateColumn, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// headerDigest computes the content address of a header/schema.
func headerDigest(header []string) string {
	w := NewCanon("hcmnext.dataops.importing.Header", 1)
	w.Strings(header)
	return w.DigestHex()
}

// validateRecord rejects a record whose width does not match the header, or
// whose cells are oversized or not valid UTF-8.
func validateRecord(header []string, record []string, rowIndex int) error {
	if len(record) != len(header) {
		return fmt.Errorf("%w: record %d has %d cells, header has %d",
			ErrRowWidthMismatch, rowIndex, len(record), len(header))
	}
	for i, cell := range record {
		if len(cell) > MaxCellBytes {
			return fmt.Errorf("%w: record %d column %q is %d bytes, max is %d",
				ErrCellTooLarge, rowIndex, header[i], len(cell), MaxCellBytes)
		}
		if !utf8.ValidString(cell) {
			return fmt.Errorf("%w: record %d column %q", ErrCellEncoding, rowIndex, header[i])
		}
	}
	return nil
}

// StageBatch builds an immutable batch from already-split records. It never
// mutates records or header, and the returned Batch shares no backing array
// with either.
//
// GREEN: staging never performs a domain write - the return type carries no
// store and no write method - and returns a StagedImport-equivalent
// (row_count, source, schema/header) with domain-write count fixed at 0 by
// construction.
func StageBatch(source SourceDescriptor, retrievedAt values.Instant, header []string, records [][]string) (Batch, error) {
	if err := source.Validate(); err != nil {
		return Batch{}, err
	}
	if !retrievedAt.IsSet() {
		return Batch{}, ErrRetrievedAtUnset
	}
	if err := validateHeader(header); err != nil {
		return Batch{}, err
	}
	if len(records) > MaxRows {
		return Batch{}, fmt.Errorf("%w: %d rows exceeds %d", ErrTooManyRows, len(records), MaxRows)
	}
	hdr := append([]string(nil), header...)
	hDigest := headerDigest(hdr)

	rows := make([]Row, 0, len(records))
	for i, record := range records {
		if err := validateRecord(hdr, record, i); err != nil {
			return Batch{}, err
		}
		cells := append([]string(nil), record...)
		rows = append(rows, Row{id: rowDigest(hDigest, cells), cells: cells})
	}

	w := NewCanon("hcmnext.dataops.importing.Batch", 1)
	source.canonical(w)
	sec, nsec := retrievedAt.Unix()
	w.I64(sec).I64(int64(nsec))
	w.Str(hDigest)
	w.U64(uint64(len(rows)))
	for _, r := range rows {
		w.Str(r.id)
	}

	b := Batch{
		source:      source,
		retrievedAt: retrievedAt,
		header:      hdr,
		rows:        rows,
		digest:      w.DigestHex(),
	}
	if err := b.Validate(); err != nil {
		return Batch{}, err
	}
	return b, nil
}

// StageCSV parses r as RFC 4180 CSV and stages the result. The first record
// is the header. Input is capped at MaxSourceCSVBytes: the reader is wrapped
// in a limiter one byte past the cap, so a file at or under the cap parses
// exactly as encoding/csv would parse it, and a file over the cap is always
// rejected with ErrSourceTooLarge rather than surfacing whatever parse error
// happened to occur at the truncation point.
func StageCSV(source SourceDescriptor, retrievedAt values.Instant, r io.Reader) (Batch, error) {
	limited := &io.LimitedReader{R: r, N: MaxSourceCSVBytes + 1}
	cr := csv.NewReader(limited)
	cr.FieldsPerRecord = -1 // widths are checked explicitly against the header, with a stable error.
	cr.ReuseRecord = false

	header, headerErr := cr.Read()
	if headerErr != nil && headerErr != io.EOF {
		if limited.N <= 0 {
			return Batch{}, fmt.Errorf("%w: input exceeds %d bytes", ErrSourceTooLarge, MaxSourceCSVBytes)
		}
		return Batch{}, fmt.Errorf("%w: %w", ErrHeaderInvalid, headerErr)
	}
	if headerErr == io.EOF {
		return Batch{}, fmt.Errorf("%w: input has no header row", ErrHeaderInvalid)
	}

	var records [][]string
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if limited.N <= 0 {
				return Batch{}, fmt.Errorf("%w: input exceeds %d bytes", ErrSourceTooLarge, MaxSourceCSVBytes)
			}
			return Batch{}, fmt.Errorf("importing: csv parse: %w", err)
		}
		if len(records) >= MaxRows {
			return Batch{}, fmt.Errorf("%w: exceeds %d rows", ErrTooManyRows, MaxRows)
		}
		records = append(records, record)
	}
	if limited.N <= 0 {
		return Batch{}, fmt.Errorf("%w: input exceeds %d bytes", ErrSourceTooLarge, MaxSourceCSVBytes)
	}
	return StageBatch(source, retrievedAt, header, records)
}
