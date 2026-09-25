package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/workorderservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderreport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
)

var errWorkOrderReportSourceUnavailable = errors.New("application: required work order report source unavailable")

// SnapshotWorkOrderReportEngine binds reports to the work order's immutable
// template pin and derives report facts only from its already-authorized,
// revision-pinned snapshot. It deliberately does not read notes, journal
// details, worker identities, or free-form request text into report output.
type SnapshotWorkOrderReportEngine struct {
	Templates workorderservice.Templates
	Orders    workorderservice.Repository
}

var _ workorderservice.ReportEngine = SnapshotWorkOrderReportEngine{}

// NewSnapshotWorkOrderReportEngine returns the report adapter used by the
// work order application service. Both dependencies are required; methods fail
// closed if either is absent.
func NewSnapshotWorkOrderReportEngine(templates workorderservice.Templates, orders workorderservice.Repository) workorderservice.ReportEngine {
	return SnapshotWorkOrderReportEngine{Templates: templates, Orders: orders}
}

func (e SnapshotWorkOrderReportEngine) ResolveDefinition(ctx context.Context, tenantID, projectID, orderID, definitionID string, definitionVersion uint32) (workorderreport.Definition, error) {
	if e.Templates == nil || e.Orders == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(orderID) == "" || strings.TrimSpace(definitionID) == "" || definitionVersion == 0 {
		return workorderreport.Definition{}, workorderservice.ErrUnavailable
	}
	order, err := e.Orders.Get(ctx, tenantID, orderID)
	if err != nil {
		return workorderreport.Definition{}, err
	}
	if order.TenantID != tenantID || order.ProjectID != projectID || order.ID != orderID || order.TemplateID == "" || order.TemplateVersion == "" || order.TemplateDigest == "" {
		return workorderreport.Definition{}, workorder.ErrNotFound
	}
	pub, err := e.Templates.ResolvePublished(ctx, tenantID, order.TemplateID, order.TemplateVersion)
	if err != nil {
		return workorderreport.Definition{}, err
	}
	def, err := definitionFromTemplate(pub, order.TemplateDigest, definitionID, definitionVersion)
	if err != nil {
		return workorderreport.Definition{}, err
	}
	return def, nil
}

func (e SnapshotWorkOrderReportEngine) BuildRequest(ctx context.Context, order workorder.Snapshot, definition workorderreport.Definition) (workorderreport.Request, error) {
	if e.Templates == nil || order.TenantID == "" || order.ProjectID == "" || order.ID == "" || order.TemplateID == "" || order.TemplateVersion == "" || order.TemplateDigest == "" || order.Revision == 0 {
		return workorderreport.Request{}, workorderservice.ErrInvalidRequest
	}
	pub, err := e.Templates.ResolvePublished(ctx, order.TenantID, order.TemplateID, order.TemplateVersion)
	if err != nil {
		return workorderreport.Request{}, err
	}
	expected, err := definitionFromTemplate(pub, order.TemplateDigest, definition.ID, definition.Version)
	if err != nil {
		return workorderreport.Request{}, err
	}
	if expected != definition {
		return workorderreport.Request{}, workorder.ErrConflict
	}
	sources, err := reportSources(order, definition.Kind)
	if err != nil {
		return workorderreport.Request{}, err
	}
	return workorderreport.Request{Sources: sources}, nil
}

func definitionFromTemplate(pub workordertemplate.Published, expectedTemplateDigest, policyID string, version uint32) (workorderreport.Definition, error) {
	if err := pub.Verify(); err != nil || pub.Digest() == "" || pub.Digest() != expectedTemplateDigest {
		return workorderreport.Definition{}, workorder.ErrConflict
	}
	draft := pub.Snapshot()
	for _, policy := range draft.ReportPolicies {
		if policy.ID != policyID {
			continue
		}
		parsed, err := strconv.ParseUint(policy.Version, 10, 32)
		if err != nil || parsed == 0 || strconv.FormatUint(parsed, 10) != policy.Version || uint32(parsed) != version || !supportedReportKind(string(policy.Kind)) {
			continue
		}
		return workorderreport.Definition{ID: policy.ID, Version: uint32(parsed), Digest: reportPolicyDigest(pub.Digest(), policy.ID, policy.Version, string(policy.Kind)), Kind: string(policy.Kind)}, nil
	}
	return workorderreport.Definition{}, fmt.Errorf("%w: report policy is absent from the pinned template", workorderservice.ErrInvalidRequest)
}

func supportedReportKind(kind string) bool {
	switch kind {
	case workorderreport.DailyField, workorderreport.Progress, workorderreport.Cost, workorderreport.OpenRequests, workorderreport.Closeout:
		return true
	default:
		return false
	}
}

func reportPolicyDigest(templateDigest, id, version, kind string) string {
	sum := sha256.Sum256([]byte("hcmnext.workorder.report-policy/v1\x00" + templateDigest + "\x00" + id + "\x00" + version + "\x00" + kind))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func reportSources(order workorder.Snapshot, kind string) ([]workorderreport.Source, error) {
	// These four datasets are fully represented by the authorized aggregate
	// snapshot, including empty collections. Their watermark pins each source
	// to the same durable work order revision.
	workLog := source(order, "work-log", true)
	progress := source(order, "progress", true)
	cost := source(order, "cost", true)
	requests := source(order, "request", true)
	for _, entry := range order.WorkEntries {
		fields := map[string]string{
			"entry_kind": entry.Kind, "work_date": entry.WorkDate, "duration_minutes": strconv.FormatUint(uint64(entry.DurationMinutes), 10),
			"line_id": entry.LineID, "unit": entry.Unit,
		}
		if entry.CorrectionOfEntryID != "" {
			fields["corrects_record_id"] = entry.CorrectionOfEntryID
		}
		if entry.Quantity.Validate() == nil {
			fields["quantity"] = entry.Quantity.String()
		} else {
			fields["quantity_status"] = "NOT_RECORDED"
		}
		workLog.Entries = append(workLog.Entries, entryRow(order, "work-log", entry.ID, fields))
	}
	for _, item := range order.Progress {
		progress.Entries = append(progress.Entries, entryRow(order, "progress", item.ID, map[string]string{
			"line_id": item.LineID, "unit": item.Unit, "quantity": decimalText(item.Quantity), "recorded_at": reportTime(item.At),
		}))
	}
	for _, item := range order.Spending {
		cost.Entries = append(cost.Entries, entryRow(order, "cost", item.ID, map[string]string{
			"category": item.Category, "disposition": string(item.Disposition), "amount": decimalText(item.Amount), "currency": item.Currency, "recorded_at": reportTime(item.At),
		}))
	}
	for _, item := range order.Requests {
		requests.Entries = append(requests.Entries, entryRow(order, "request", item.ID, map[string]string{
			"request_kind": string(item.Kind), "status": string(item.Status), "definition_id": item.DefinitionID,
			"created_at": reportTime(item.CreatedAt), "decided_at": reportTime(item.DecidedAt),
		}))
	}
	correctionSources := map[string]string{}
	for _, item := range order.WorkEntries {
		correctionSources[item.ID] = "work-log"
	}
	for _, item := range order.Progress {
		correctionSources[item.ID] = "progress"
	}
	for _, item := range order.Spending {
		correctionSources[item.ID] = "cost"
	}
	for _, correction := range order.Corrections {
		name, exists := correctionSources[correction.TargetRecordID]
		if !exists || correction.ID == "" || correction.At.IsZero() {
			return nil, fmt.Errorf("%w: correction source is absent from the work order snapshot", workorderservice.ErrInvalidRequest)
		}
		fields := map[string]string{"record_type": "CORRECTION", "corrects_record_id": correction.TargetRecordID, "recorded_at": reportTime(correction.At)}
		if correction.ReplacementAmount != nil {
			if correction.ReplacementAmount.Validate() != nil {
				return nil, fmt.Errorf("%w: malformed correction amount", workorderservice.ErrInvalidRequest)
			}
			fields["replacement_amount"] = correction.ReplacementAmount.String()
		}
		switch name {
		case "work-log":
			markReportEntryCorrected(&workLog, correction.TargetRecordID)
		case "progress":
			markReportEntryCorrected(&progress, correction.TargetRecordID)
		case "cost":
			markReportEntryCorrected(&cost, correction.TargetRecordID)
		}
		correctionEntry := entryRow(order, name, correction.ID, fields)
		switch name {
		case "work-log":
			workLog.Entries = append(workLog.Entries, correctionEntry)
		case "progress":
			progress.Entries = append(progress.Entries, correctionEntry)
		case "cost":
			cost.Entries = append(cost.Entries, correctionEntry)
		}
	}
	switch kind {
	case workorderreport.DailyField:
		return []workorderreport.Source{workLog, progress, cost, requests}, nil
	case workorderreport.Progress:
		return []workorderreport.Source{progress}, nil
	case workorderreport.Cost:
		return []workorderreport.Source{cost}, nil
	case workorderreport.OpenRequests:
		return []workorderreport.Source{requests}, nil
	case workorderreport.Closeout:
		// The aggregate does not contain authoritative evidence manifests or
		// inspection results. Do not synthesize either from note text, event
		// details, or request metadata; closeout must wait for those source ports.
		return nil, fmt.Errorf("%w: closeout requires evidence and inspection source adapters", errWorkOrderReportSourceUnavailable)
	default:
		return nil, fmt.Errorf("%w: unsupported report kind", workorderservice.ErrInvalidRequest)
	}
}

func markReportEntryCorrected(src *workorderreport.Source, id string) {
	for i := range src.Entries {
		if src.Entries[i].ID == id {
			src.Entries[i].Fields["corrected"] = "true"
			return
		}
	}
}

func source(order workorder.Snapshot, name string, complete bool) workorderreport.Source {
	value := "revision:" + strconv.FormatUint(order.Revision, 10)
	return workorderreport.Source{Name: name, Watermark: workorderreport.Watermark{Source: name, Value: value}, Complete: complete, Entries: []workorderreport.Entry{}}
}

func entryRow(order workorder.Snapshot, source, id string, fields map[string]string) workorderreport.Entry {
	return workorderreport.Entry{TenantID: order.TenantID, ProjectID: order.ProjectID, WorkOrderID: order.ID, Source: source, ID: id, Fields: fields}
}

func decimalText(v interface{ String() string }) string {
	return v.String()
}

func reportTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
