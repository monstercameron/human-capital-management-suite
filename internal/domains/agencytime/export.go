package agencytime

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// ApprovedLine is the minimal approved-time input this package needs. It is
// defined here, rather than imported from an approval or timecard package,
// so this domain never depends on an unfinished lane; a service layer maps
// its own approved-timecard record onto this shape.
type ApprovedLine struct {
	WorkerRef      string
	Date           string // ISO 8601 calendar date, "2006-01-02"
	Minutes        int
	Project        string
	CostCode       string
	RateCode       string
	RevisionDigest string
}

func (l ApprovedLine) validate(assignmentWorker string) error {
	if strings.TrimSpace(l.WorkerRef) == "" || l.WorkerRef != assignmentWorker {
		return reject("ApprovedLine.WorkerRef", l.WorkerRef, "approved line worker must match the assignment worker")
	}
	if strings.TrimSpace(l.Date) == "" {
		return reject("ApprovedLine.Date", l.Date, "approved line date is required")
	}
	if l.Minutes <= 0 {
		return reject("ApprovedLine.Minutes", strconv.Itoa(l.Minutes), "approved minutes must be positive")
	}
	if strings.TrimSpace(l.RevisionDigest) == "" {
		return reject("ApprovedLine.RevisionDigest", "", "approved line must carry its approval revision digest")
	}
	return nil
}

// ExportPayload is the generic canonical shape delivered to the agency or a
// vendor-management system (VMS). It is deliberately format-agnostic; format
// adapters (ToCSV, and any future adapter) render it for a specific target.
// Candidate VMS targets for a future connector binding: SAP Fieldglass,
// Beeline, Workday VNDLY. This package names them only in documentation; no
// per-target logic lives here.
type ExportPayload struct {
	Assignment Assignment
	Lines      []ApprovedLine
	Digest     string
}

// BuildExportRequest carries one assignment's approved lines to export.
type BuildExportRequest struct {
	Assignment Assignment
	Lines      []ApprovedLine
}

// BuildExport packages one assignment's approved time into the generic
// export payload. Every line must belong to the assignment's worker; a line
// for another worker is rejected rather than silently included.
func BuildExport(req BuildExportRequest) (ExportPayload, error) {
	if err := req.Assignment.Validate(); err != nil {
		return ExportPayload{}, err
	}
	if len(req.Lines) == 0 {
		return ExportPayload{}, reject("BuildExportRequest.Lines", "", "export needs at least one approved line")
	}
	seen := map[string]bool{}
	lines := make([]ApprovedLine, 0, len(req.Lines))
	for _, l := range req.Lines {
		if err := l.validate(req.Assignment.WorkerRef); err != nil {
			return ExportPayload{}, err
		}
		key := l.Date + "|" + l.RevisionDigest
		if seen[key] {
			return ExportPayload{}, reject("ApprovedLine", key, "duplicate approved line")
		}
		seen[key] = true
		lines = append(lines, l)
	}
	payload := ExportPayload{Assignment: req.Assignment, Lines: lines}
	payload.Digest = digestPayload(payload)
	return payload, nil
}

func digestPayload(p ExportPayload) string {
	fields := []string{p.Assignment.ID, p.Assignment.WorkerRef, p.Assignment.AgencyRef, p.Assignment.HostEntityRef,
		p.Assignment.WorkOrderRef, p.Assignment.WorksiteRef}
	for _, l := range p.Lines {
		fields = append(fields, l.WorkerRef, l.Date, strconv.Itoa(l.Minutes), l.Project, l.CostCode, l.RateCode, l.RevisionDigest)
	}
	b := strings.Builder{}
	for _, f := range fields {
		fmt.Fprintf(&b, "%d:%s", len(f), f)
	}
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:])
}

// Validate verifies the frozen export payload and its digest.
func (p ExportPayload) Validate() error {
	if err := p.Assignment.Validate(); err != nil {
		return err
	}
	if len(p.Lines) == 0 {
		return reject("ExportPayload.Lines", "", "export payload has no lines")
	}
	if p.Digest == "" || p.Digest != digestPayload(p) {
		return reject("ExportPayload.Digest", p.Digest, "export payload content digest mismatch")
	}
	return nil
}

// csvHeader is the generic column layout for the CSV format adapter. It maps
// 1:1 onto ApprovedLine plus the assignment identity columns, so a receiving
// VMS format adapter can be built by re-ordering or re-labeling columns
// without changing this package.
var csvHeader = []string{
	"assignment_id", "agency_ref", "host_entity_ref", "work_order_ref", "worksite_ref",
	"worker_ref", "date", "minutes", "project", "cost_code", "rate_code", "revision_digest",
}

// ToCSV renders the payload as the generic CSV layout. It never renders an
// invalid payload.
func ToCSV(p ExportPayload) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write(csvHeader); err != nil {
		return "", fmt.Errorf("agencytime: write csv header: %w", err)
	}
	for _, l := range p.Lines {
		row := []string{
			p.Assignment.ID, p.Assignment.AgencyRef, p.Assignment.HostEntityRef, p.Assignment.WorkOrderRef, p.Assignment.WorksiteRef,
			l.WorkerRef, l.Date, strconv.Itoa(l.Minutes), l.Project, l.CostCode, l.RateCode, l.RevisionDigest,
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("agencytime: write csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("agencytime: flush csv: %w", err)
	}
	return b.String(), nil
}

// FromCSV parses the generic CSV layout back into an export payload for
// round-trip verification. It rejects a header mismatch or a malformed row
// rather than skipping it.
func FromCSV(text string) (ExportPayload, error) {
	r := csv.NewReader(strings.NewReader(text))
	rows, err := r.ReadAll()
	if err != nil {
		return ExportPayload{}, reject("CSV", "parse", "malformed csv: "+err.Error())
	}
	if len(rows) == 0 {
		return ExportPayload{}, reject("CSV", "empty", "csv has no header row")
	}
	if !equalHeader(rows[0], csvHeader) {
		return ExportPayload{}, reject("CSV.Header", strings.Join(rows[0], ","), "csv header does not match the generic layout")
	}
	if len(rows) < 2 {
		return ExportPayload{}, reject("CSV", "no-rows", "csv has no data rows")
	}
	var assignment Assignment
	var lines []ApprovedLine
	for i, row := range rows[1:] {
		if len(row) != len(csvHeader) {
			return ExportPayload{}, reject("CSV.Row", strconv.Itoa(i), "row has the wrong number of columns")
		}
		minutes, err := strconv.Atoi(row[7])
		if err != nil {
			return ExportPayload{}, reject("CSV.Row.Minutes", row[7], "minutes column is not an integer")
		}
		if i == 0 {
			assignment = Assignment{ID: row[0], AgencyRef: row[1], HostEntityRef: row[2], WorkOrderRef: row[3], WorksiteRef: row[4], WorkerRef: row[5]}
		}
		lines = append(lines, ApprovedLine{
			WorkerRef: row[5], Date: row[6], Minutes: minutes, Project: row[8], CostCode: row[9], RateCode: row[10], RevisionDigest: row[11],
		})
	}
	return BuildExport(BuildExportRequest{Assignment: assignment, Lines: lines})
}

func equalHeader(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
