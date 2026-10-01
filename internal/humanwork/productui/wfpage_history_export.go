package productui

import (
	"context"
	"errors"
	"strings"
	"time"

	exportformat "github.com/monstercameron/human-capital-management-suite/internal/operations/export"
)

const WorkflowHistoryExportLimit = 10000

var ErrWorkflowHistoryExport = errors.New("workflow history export could not be written")

type WorkflowHistoryExportAudit struct {
	Filter         WorkflowRunHistoryFilter
	RowCount       int
	Truncated      bool
	ExportID       string
	TenantID       string
	SubjectID      string
	Scope          string
	Purpose        string
	ExpiresAt      time.Time
	ContentDigest  string
	ManifestDigest string
}

type WorkflowHistoryExportResult struct {
	CSV    []byte
	Audit  WorkflowHistoryExportAudit
	Notice string
}

// WorkflowHistoryExportRequest is the authorization and retention context
// required by the governed report-export path. The caller supplies the
// already-authorized rows; this package never widens the list while exporting.
type WorkflowHistoryExportRequest struct {
	Runs      []WorkflowHistoryRun
	Filter    WorkflowRunHistoryFilter
	TenantID  string
	SubjectID string
	Scope     string
	Purpose   string
	ExportID  string
	Locale    string
	Now       time.Time
}

// WorkflowHistoryExportRecorder persists the audit row after the complete
// artifact has been rendered. A recorder failure prevents the bytes from
// being returned, so callers cannot download an export with no audit trail.
type WorkflowHistoryExportRecorder func(context.Context, WorkflowHistoryExportAudit) error

// WorkflowHistoryReportExportService adapts workflow history to the existing
// governed report-export primitive. Retention and audit are applied here,
// rather than by a page-specific CSV writer.
type WorkflowHistoryReportExportService struct {
	Record    WorkflowHistoryExportRecorder
	Retention time.Duration
}

func (s WorkflowHistoryReportExportService) Export(ctx context.Context, request WorkflowHistoryExportRequest) (WorkflowHistoryExportResult, error) {
	filtered := FilterWorkflowHistoryRuns(request.Runs, request.Filter)
	truncated := len(filtered) > WorkflowHistoryExportLimit
	if truncated {
		filtered = filtered[:WorkflowHistoryExportLimit]
	}
	now := request.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	retention := s.Retention
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	tenant := strings.TrimSpace(request.TenantID)
	if tenant == "" {
		tenant = "workflow-history"
	}
	subject := strings.TrimSpace(request.SubjectID)
	if subject == "" {
		subject = "workflow-history-viewer"
	}
	scope := strings.TrimSpace(request.Scope)
	if scope == "" {
		scope = "workflow-history"
	}
	purpose := strings.TrimSpace(request.Purpose)
	if purpose == "" {
		purpose = "workflow-history-export"
	}
	exportID := strings.TrimSpace(request.ExportID)
	if exportID == "" {
		exportID = "workflow-history-export"
	}
	fields := []string{"workflow", "version", "subject", "requester", "participants", "stage", "status_group", "started_at", "updated_at"}
	records := make([]exportformat.Record, 0, len(filtered))
	for _, run := range filtered {
		records = append(records, exportformat.Record{ID: run.ID, Fields: []exportformat.Field{
			{Name: "workflow", Value: run.Workflow}, {Name: "version", Value: run.Version},
			{Name: "subject", Value: run.Subject}, {Name: "requester", Value: run.Requester},
			{Name: "participants", Value: run.Participants}, {Name: "stage", Value: run.Stage},
			{Name: "status_group", Value: run.StatusGroup}, {Name: "started_at", Value: run.StartedAt},
			{Name: "updated_at", Value: run.UpdatedAt},
		}})
	}
	artifact, err := exportformat.RenderHumanSpreadsheet(exportformat.Request{
		TenantID: tenant, SubjectID: subject, Scope: scope, Purpose: purpose,
		SchemaDigest: "hcmnext.workflow-history/v1", Classification: "CONFIDENTIAL_HR",
		DLPPolicyRef: "workflow-history-export/v1", AllowedFields: fields,
		ExpiresAt: now.Add(retention), Profile: exportformat.HumanSpreadsheet,
		Locale: request.Locale, Records: records,
	}, now)
	if err != nil {
		return WorkflowHistoryExportResult{}, ErrWorkflowHistoryExport
	}
	audit := WorkflowHistoryExportAudit{Filter: request.Filter, RowCount: len(filtered), Truncated: truncated,
		ExportID: exportID, TenantID: tenant, SubjectID: subject, Scope: scope, Purpose: purpose,
		ExpiresAt: artifact.Manifest.ExpiresAt, ContentDigest: artifact.Manifest.ContentDigest,
		ManifestDigest: artifact.Manifest.ManifestDigest}
	if s.Record != nil {
		if err := s.Record(ctx, audit); err != nil {
			return WorkflowHistoryExportResult{}, ErrWorkflowHistoryExport
		}
	}
	notice := ""
	if truncated {
		notice = "Export capped at 10,000 rows."
	}
	return WorkflowHistoryExportResult{CSV: artifact.Content, Audit: audit, Notice: notice}, nil
}

// ExportWorkflowHistoryCSV builds the entire authorized export in memory and
// returns bytes only after csv.Writer reports success. That makes an
// interrupted export impossible to expose as a partial file.
func ExportWorkflowHistoryCSV(runs []WorkflowHistoryRun, filter WorkflowRunHistoryFilter) (WorkflowHistoryExportResult, error) {
	return (WorkflowHistoryReportExportService{}).Export(context.Background(), WorkflowHistoryExportRequest{Runs: runs, Filter: filter, Locale: "en-US"})
}
