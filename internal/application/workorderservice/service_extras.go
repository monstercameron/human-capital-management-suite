package workorderservice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderreport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrArtifactUnavailable = errors.New("workorderservice: artifact service unavailable")
	ErrArtifactNotFound    = errors.New("workorderservice: artifact not found")
	ErrInvalidPage         = errors.New("workorderservice: invalid page")
	ErrInvalidCursor       = errors.New("workorderservice: invalid page cursor")
)

// ListRequest is bounded by the repository and always scoped to one project.
type ListRequest struct {
	ProjectID, Status, Cursor string
	Limit                     int
}

type ListResult struct {
	Orders     []workorder.Snapshot
	NextCursor string
}

// WorkEntryRequest records a labor or production fact against the current
// work-order revision. The authenticated actor is supplied by the service.
type WorkEntryRequest struct {
	ScopedRequest
	ExpectedRevision uint64
	Input            workorder.WorkEntryInput
}

type WorkEntryResult struct {
	Order workorder.Snapshot
	Entry workorder.WorkEntry
}

type ReportRequest struct {
	ScopedRequest
	ExpectedRevision   uint64
	Kind, DefinitionID string
	DefinitionVersion  uint32
}

type ReportResult struct {
	Report   workorderreport.Result
	Artifact ArtifactRecord
}

type BillingDraftRequest struct {
	ScopedRequest
	ExpectedRevision       uint64
	ContractVersion        string
	PeriodStart, PeriodEnd time.Time
}

type BillingDraftResult struct {
	Draft          workorderbilling.BillingDraft
	SourceRevision uint64
	Artifact       ArtifactRecord
}

// ArtifactRepository persists generated immutable artifacts with the source
// order revision, command digest and idempotency receipt in one transaction.
type ArtifactRepository interface {
	GetArtifact(context.Context, string, string, string, string, string) (ArtifactRecord, error)
	RecordArtifact(context.Context, string, string, string, string, string, uint64, string, string, []byte) (ArtifactRecord, error)
}

type ArtifactRecord struct {
	ID, TenantID, WorkOrderID, ProjectID, Kind, ActorID, IdempotencyKey, CommandDigest string
	SourceRevision                                                                     uint64
	Payload                                                                            []byte
	CreatedAt                                                                          time.Time
}

// WorkerActorAuthorizer binds a labor entry to its real worker identity or a
// current project role that may record entries on another worker's behalf.
type WorkerActorAuthorizer interface {
	AuthorizeWorkEntry(context.Context, *trust.Principal, string, string, string) error
}

type WorkOrderWorkerResolver interface {
	ResolveWorker(context.Context, string, string) (string, bool, error)
}

type ReportEngine interface {
	ResolveDefinition(context.Context, string, string, string, string, uint32) (workorderreport.Definition, error)
	BuildRequest(context.Context, workorder.Snapshot, workorderreport.Definition) (workorderreport.Request, error)
}

type PricingPolicies interface {
	ResolvePricing(context.Context, string, string, string, string) (workorderbilling.PricingPolicy, error)
}

type BillingSourceProvider interface {
	Sources(context.Context, workorder.Snapshot, time.Time, time.Time) ([]workorderbilling.Source, map[string]string, error)
}

type reportArtifact struct {
	Result      workorderreport.Result `json:"result"`
	RenderInput any                    `json:"render_input"`
}

type listCursor struct {
	Tenant  string `json:"t"`
	Project string `json:"p"`
	Status  string `json:"s"`
	Store   string `json:"c"`
}

func (s Service) signCursor(c listCursor) (string, error) {
	if len(s.CursorKey) < 32 || c.Store == "" {
		return "", ErrUnavailable
	}
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.CursorKey)
	_, _ = mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s Service) verifyCursor(token, tenantID, projectID, status string) (string, error) {
	if token == "" {
		return "", nil
	}
	if len(s.CursorKey) < 32 || len(token) > 4096 {
		return "", ErrInvalidCursor
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", ErrInvalidCursor
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(body) > 2048 {
		return "", ErrInvalidCursor
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrInvalidCursor
	}
	mac := hmac.New(sha256.New, s.CursorKey)
	_, _ = mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", ErrInvalidCursor
	}
	var c listCursor
	if err := json.Unmarshal(body, &c); err != nil || c.Tenant != tenantID || c.Project != projectID || c.Status != status || c.Store == "" {
		return "", ErrInvalidCursor
	}
	return c.Store, nil
}

func (s Service) List(ctx context.Context, p *trust.Principal, req ListRequest) (ListResult, error) {
	if err := validPrincipal(p); err != nil {
		return ListResult{}, err
	}
	if s.Auth == nil || s.Orders == nil {
		return ListResult{}, ErrUnavailable
	}
	if strings.TrimSpace(req.ProjectID) == "" || req.Limit < 1 || req.Limit > 200 || len(req.Cursor) > 4096 {
		return ListResult{}, ErrInvalidPage
	}
	if len(s.CursorKey) < 32 {
		return ListResult{}, ErrUnavailable
	}
	if req.Status != "" && req.Status != "DRAFT" && req.Status != "ACTIVE" && req.Status != "BLOCKED" && req.Status != "ACCEPTED" && req.Status != "CLOSED" && req.Status != "CANCELLED" {
		return ListResult{}, ErrInvalidPage
	}
	if err := s.Auth.AuthorizeList(ctx, p, req.ProjectID); err != nil {
		return ListResult{}, err
	}
	lister, ok := s.Orders.(interface {
		List(context.Context, string, string, string, string, int) ([]workorder.Snapshot, string, error)
	})
	if !ok {
		return ListResult{}, ErrUnavailable
	}
	storeCursor, err := s.verifyCursor(req.Cursor, tenant(p), req.ProjectID, req.Status)
	if err != nil {
		return ListResult{}, err
	}
	orders, next, err := lister.List(ctx, tenant(p), req.ProjectID, req.Status, storeCursor, req.Limit)
	if err != nil {
		return ListResult{}, err
	}
	visible := make([]workorder.Snapshot, 0, len(orders))
	for _, order := range orders {
		if order.TenantID != tenant(p) || order.ProjectID != req.ProjectID {
			return ListResult{}, ErrScopeMismatch
		}
		validated, validateErr := workorder.Restore(order)
		if validateErr != nil {
			return ListResult{}, validateErr
		}
		order = validated.Snapshot()
		if err := s.Auth.Authorize(ctx, p, req.ProjectID, order.ID, workorderaccess.Read); err != nil {
			// Do not reveal the existence or position of inaccessible orders. The
			// repository cursor must not be returned if filtering changed the page.
			return ListResult{}, err
		}
		projected, err := s.projectSnapshot(ctx, p, order)
		if err != nil {
			return ListResult{}, err
		}
		visible = append(visible, projected)
	}
	nextCursor := ""
	if next != "" {
		nextCursor, err = s.signCursor(listCursor{Tenant: tenant(p), Project: req.ProjectID, Status: req.Status, Store: next})
		if err != nil {
			return ListResult{}, err
		}
	}
	return ListResult{Orders: visible, NextCursor: nextCursor}, nil
}

func (s Service) RecordWorkEntry(ctx context.Context, p *trust.Principal, req WorkEntryRequest) (WorkEntryResult, error) {
	if err := validPrincipal(p); err != nil {
		return WorkEntryResult{}, err
	}
	if s.Workers == nil || s.Orders == nil {
		return WorkEntryResult{}, ErrUnavailable
	}
	if err := validScope(req.ScopedRequest); err != nil {
		return WorkEntryResult{}, err
	}
	if req.ExpectedRevision == 0 || req.IdempotencyKey == "" {
		return WorkEntryResult{}, ErrInvalidRequest
	}
	input := req.Input
	if input.WorkerID == "" || input.Kind == "" || input.WorkDate == "" || input.TimeZone == "" || (input.Kind == "LABOR" && input.DurationMinutes == 0) {
		return WorkEntryResult{}, ErrInvalidRequest
	}
	current, err := s.Orders.Get(ctx, tenant(p), req.WorkOrderID)
	if err != nil {
		return WorkEntryResult{}, err
	}
	if current.TenantID != tenant(p) || current.ID != req.WorkOrderID || (req.ProjectID != "" && current.ProjectID != req.ProjectID) {
		return WorkEntryResult{}, workorder.ErrNotFound
	}
	projectID := current.ProjectID
	if s.Auth == nil {
		return WorkEntryResult{}, ErrUnavailable
	}
	if err := s.Auth.Authorize(ctx, p, projectID, req.WorkOrderID, workorderaccess.Request); err != nil {
		return WorkEntryResult{}, err
	}
	resolver, ok := s.Workers.(WorkOrderWorkerResolver)
	if !ok {
		return WorkEntryResult{}, ErrUnavailable
	}
	canonicalWorkerID, found, err := resolver.ResolveWorker(ctx, tenant(p), input.WorkerID)
	if err != nil {
		return WorkEntryResult{}, err
	}
	if !found || strings.TrimSpace(canonicalWorkerID) == "" {
		return WorkEntryResult{}, ErrWorkerIneligible
	}
	input.WorkerID = canonicalWorkerID
	assigned := false
	workDay, parseErr := time.Parse("2006-01-02", input.WorkDate)
	if parseErr != nil {
		return WorkEntryResult{}, ErrInvalidRequest
	}
	workLocation, parseErr := time.LoadLocation(input.TimeZone)
	if parseErr != nil {
		return WorkEntryResult{}, ErrInvalidRequest
	}
	localStart := time.Date(workDay.Year(), workDay.Month(), workDay.Day(), 0, 0, 0, 0, workLocation)
	localEnd := localStart.AddDate(0, 0, 1)
	for _, assignment := range current.Assignments {
		startsBeforeDayEnds := assignment.Start.IsZero() || assignment.Start.Before(localEnd)
		endsAfterDayStarts := assignment.End.IsZero() || assignment.End.After(localStart)
		if assignment.WorkerID == input.WorkerID && startsBeforeDayEnds && endsAfterDayStarts {
			assigned = true
			break
		}
	}
	if !assigned {
		return WorkEntryResult{}, ErrWorkerIneligible
	}
	workerAuth, ok := s.Auth.(WorkerActorAuthorizer)
	if !ok {
		return WorkEntryResult{}, ErrUnavailable
	}
	if err := workerAuth.AuthorizeWorkEntry(ctx, p, projectID, req.WorkOrderID, input.WorkerID); err != nil {
		return WorkEntryResult{}, err
	}
	eligible, err := s.Workers.ResolveEligible(ctx, tenant(p), input.WorkerID)
	if err != nil {
		return WorkEntryResult{}, err
	}
	if !eligible {
		return WorkEntryResult{}, ErrWorkerIneligible
	}
	entryID := input.ID
	if entryID == "" {
		entryID = stableID(tenant(p), p.Subject(), "workorder.work-entry/"+req.WorkOrderID, req.IdempotencyKey)
	}
	in := input
	in.ID, in.ActorID = entryID, p.Subject()
	in.ExpectedRevision = req.ExpectedRevision
	in.IdempotencyKey = req.IdempotencyKey
	scope := req.ScopedRequest
	scope.ProjectID = projectID
	order, err := s.mutate(ctx, p, scope, workorderaccess.Request, req.ExpectedRevision, req.IdempotencyKey, in, func(a *workorder.WorkOrder, digest string) error {
		entry := in
		entry.CommandDigest = digest
		entry.Now = s.now()
		return a.RecordWorkEntry(entry)
	})
	if err != nil {
		return WorkEntryResult{}, err
	}
	for _, entry := range order.WorkEntries {
		if entry.ID == entryID {
			projected, projectErr := s.projectSnapshot(ctx, p, order)
			if projectErr != nil {
				return WorkEntryResult{}, projectErr
			}
			for _, projectedEntry := range projected.WorkEntries {
				if projectedEntry.ID == entryID {
					return WorkEntryResult{Order: projected, Entry: projectedEntry}, nil
				}
			}
			return WorkEntryResult{}, ErrScopeMismatch
		}
	}
	return WorkEntryResult{}, fmt.Errorf("%w: committed work entry absent from snapshot", ErrScopeMismatch)
}

func (s Service) RequestReport(ctx context.Context, p *trust.Principal, req ReportRequest) (ReportResult, error) {
	if err := validPrincipal(p); err != nil {
		return ReportResult{}, err
	}
	if s.Auth == nil || s.Orders == nil || s.Templates == nil || s.Reports == nil || s.Artifacts == nil {
		return ReportResult{}, ErrArtifactUnavailable
	}
	if strings.TrimSpace(req.WorkOrderID) == "" {
		return ReportResult{}, ErrInvalidRequest
	}
	if req.IdempotencyKey == "" || req.ExpectedRevision == 0 || req.DefinitionVersion == 0 || req.DefinitionID == "" || req.Kind == "" {
		return ReportResult{}, ErrInvalidRequest
	}
	order, err := s.Orders.Get(ctx, tenant(p), req.WorkOrderID)
	if err != nil {
		return ReportResult{}, err
	}
	if order.TenantID != tenant(p) || order.ID != req.WorkOrderID || (req.ProjectID != "" && order.ProjectID != req.ProjectID) {
		return ReportResult{}, workorder.ErrNotFound
	}
	projectID := order.ProjectID
	if err := s.Auth.Authorize(ctx, p, projectID, req.WorkOrderID, workorderaccess.Inspect); err != nil {
		return ReportResult{}, err
	}
	if req.Kind == workorderreport.Cost || req.Kind == workorderreport.DailyField || req.Kind == workorderreport.Closeout {
		if err := s.Auth.Authorize(ctx, p, projectID, req.WorkOrderID, workorderaccess.ViewCost); err != nil {
			return ReportResult{}, err
		}
	}
	command, err := json.Marshal(struct {
		Tenant, Project, Order, Kind, DefinitionID string
		DefinitionVersion                          uint32
		ExpectedRevision                           uint64
	}{tenant(p), projectID, req.WorkOrderID, req.Kind, req.DefinitionID, req.DefinitionVersion, req.ExpectedRevision})
	if err != nil {
		return ReportResult{}, err
	}
	digest := workorder.DigestCommandJSON(command)
	if prior, getErr := s.Artifacts.GetArtifact(ctx, tenant(p), req.WorkOrderID, p.Subject(), req.IdempotencyKey, "REPORT"); getErr == nil {
		if prior.CommandDigest != digest {
			return ReportResult{}, workorder.ErrConflict
		}
		var stored reportArtifact
		if err := json.Unmarshal(prior.Payload, &stored); err != nil {
			return ReportResult{}, ErrArtifactUnavailable
		}
		return ReportResult{Report: stored.Result, Artifact: prior}, nil
	} else if !errors.Is(getErr, ErrArtifactNotFound) {
		return ReportResult{}, getErr
	}
	published, err := s.Templates.ResolvePublished(ctx, tenant(p), order.TemplateID, order.TemplateVersion)
	if err != nil {
		return ReportResult{}, err
	}
	if err := published.Verify(); err != nil || published.Digest() != order.TemplateDigest {
		return ReportResult{}, ErrInvalidRequest
	}
	policyBound := false
	for _, policy := range published.Snapshot().ReportPolicies {
		if policy.ID == req.DefinitionID && policy.Version == fmt.Sprint(req.DefinitionVersion) && string(policy.Kind) == req.Kind {
			policyBound = true
			break
		}
	}
	if !policyBound {
		return ReportResult{}, ErrInvalidRequest
	}
	definition, err := s.Reports.ResolveDefinition(ctx, tenant(p), projectID, req.WorkOrderID, req.DefinitionID, req.DefinitionVersion)
	if err != nil {
		return ReportResult{}, err
	}
	if definition.ID != req.DefinitionID || definition.Version != req.DefinitionVersion || definition.Kind != req.Kind || definition.Digest == "" {
		return ReportResult{}, ErrInvalidRequest
	}
	input, err := s.Reports.BuildRequest(ctx, order, definition)
	if err != nil {
		return ReportResult{}, err
	}
	if containsPayRateFields(input.Sources) {
		if err := s.Auth.Authorize(ctx, p, projectID, req.WorkOrderID, workorderaccess.ViewPay); err != nil {
			return ReportResult{}, err
		}
	}
	input.Scope = workorderreport.Scope{TenantID: tenant(p), ProjectID: projectID, WorkOrderID: req.WorkOrderID}
	input.Definition = definition
	input.Authorization = workorderreport.Authorization{Principal: workorderreport.Principal{ID: p.Subject(), Tenant: tenant(p), Purpose: "work-order-report"}, Allow: func(actor workorderreport.Principal, scope workorderreport.Scope, kind string) bool {
		return actor.ID == p.Subject() && scope.TenantID == tenant(p) && scope.ProjectID == projectID && scope.WorkOrderID == req.WorkOrderID && kind == req.Kind
	}}
	result, err := workorderreport.Generate(input)
	if err != nil {
		return ReportResult{}, err
	}
	// Persist the complete generated projection, including its renderer input,
	// before returning a success response. Report generation never mutates the
	// work order revision; artifact persistence pins that source revision.
	payload, err := canonicalJSON(reportArtifact{Result: result, RenderInput: result.RenderInput})
	if err != nil {
		return ReportResult{}, err
	}
	artifact, err := s.Artifacts.RecordArtifact(ctx, tenant(p), req.WorkOrderID, projectID, p.Subject(), req.IdempotencyKey, req.ExpectedRevision, "REPORT", digest, payload)
	if err != nil {
		return ReportResult{}, err
	}
	var stored reportArtifact
	if err := json.Unmarshal(artifact.Payload, &stored); err != nil {
		return ReportResult{}, ErrArtifactUnavailable
	}
	return ReportResult{Report: stored.Result, Artifact: artifact}, nil
}

func containsPayRateFields(sources []workorderreport.Source) bool {
	for _, source := range sources {
		for _, entry := range source.Entries {
			for name := range entry.Fields {
				key := strings.ToLower(name)
				key = strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(key)
				rateSensitive := strings.Contains(key, "compensation") || strings.Contains(key, "pay") && strings.Contains(key, "rate") || strings.Contains(key, "wage") && strings.Contains(key, "rate") || strings.Contains(key, "hour") && strings.Contains(key, "rate") || strings.Contains(key, "labor") && strings.Contains(key, "rate") || strings.Contains(key, "worker") && strings.Contains(key, "rate") || strings.Contains(key, "employee") && strings.Contains(key, "rate")
				if rateSensitive {
					return true
				}
			}
		}
	}
	return false
}

func (s Service) RequestBillingDraft(ctx context.Context, p *trust.Principal, req BillingDraftRequest) (BillingDraftResult, error) {
	if err := validPrincipal(p); err != nil {
		return BillingDraftResult{}, err
	}
	if s.Auth == nil || s.Orders == nil || s.Templates == nil || s.Pricing == nil || s.BillingSources == nil || s.Artifacts == nil {
		return BillingDraftResult{}, ErrArtifactUnavailable
	}
	if strings.TrimSpace(req.WorkOrderID) == "" {
		return BillingDraftResult{}, ErrInvalidRequest
	}
	if req.IdempotencyKey == "" || req.ExpectedRevision == 0 || req.ContractVersion == "" || req.PeriodStart.IsZero() || req.PeriodEnd.IsZero() || !req.PeriodEnd.After(req.PeriodStart) {
		return BillingDraftResult{}, ErrInvalidRequest
	}
	order, err := s.Orders.Get(ctx, tenant(p), req.WorkOrderID)
	if err != nil {
		return BillingDraftResult{}, err
	}
	if order.TenantID != tenant(p) || order.ID != req.WorkOrderID || (req.ProjectID != "" && order.ProjectID != req.ProjectID) {
		return BillingDraftResult{}, workorder.ErrNotFound
	}
	projectID := order.ProjectID
	if err := s.Auth.Authorize(ctx, p, projectID, req.WorkOrderID, workorderaccess.Bill); err != nil {
		return BillingDraftResult{}, err
	}
	command, err := json.Marshal(struct {
		Tenant, Project, Order, ContractVersion string
		PeriodStart, PeriodEnd                  time.Time
		ExpectedRevision                        uint64
	}{tenant(p), projectID, req.WorkOrderID, req.ContractVersion, req.PeriodStart.UTC(), req.PeriodEnd.UTC(), req.ExpectedRevision})
	if err != nil {
		return BillingDraftResult{}, err
	}
	digest := workorder.DigestCommandJSON(command)
	if prior, getErr := s.Artifacts.GetArtifact(ctx, tenant(p), req.WorkOrderID, p.Subject(), req.IdempotencyKey, "BILLING"); getErr == nil {
		if prior.CommandDigest != digest {
			return BillingDraftResult{}, workorder.ErrConflict
		}
		var stored struct{ Draft workorderbilling.BillingDraft }
		if err := json.Unmarshal(prior.Payload, &stored); err != nil {
			return BillingDraftResult{}, ErrArtifactUnavailable
		}
		return BillingDraftResult{Draft: stored.Draft, SourceRevision: prior.SourceRevision, Artifact: prior}, nil
	} else if !errors.Is(getErr, ErrArtifactNotFound) {
		return BillingDraftResult{}, getErr
	}
	published, err := s.Templates.ResolvePublished(ctx, tenant(p), order.TemplateID, order.TemplateVersion)
	if err != nil {
		return BillingDraftResult{}, err
	}
	if err := published.Verify(); err != nil || published.Digest() != order.TemplateDigest {
		return BillingDraftResult{}, ErrInvalidRequest
	}
	pinnedBilling := published.Snapshot().Billing
	if pinnedBilling == nil || pinnedBilling.Version != req.ContractVersion {
		return BillingDraftResult{}, ErrInvalidRequest
	}
	policy, err := s.Pricing.ResolvePricing(ctx, tenant(p), projectID, req.WorkOrderID, req.ContractVersion)
	if err != nil {
		return BillingDraftResult{}, err
	}
	if policy.Version != req.ContractVersion || policy.ID != pinnedBilling.ID || !billingModeMatches(policy.Mode, pinnedBilling.Mode) {
		return BillingDraftResult{}, ErrInvalidRequest
	}
	sources, descriptions, err := s.BillingSources.Sources(ctx, order, req.PeriodStart.UTC(), req.PeriodEnd.UTC())
	if err != nil {
		return BillingDraftResult{}, err
	}
	id := stableID(tenant(p), p.Subject(), "workorder.billing-draft", req.IdempotencyKey)
	draft, err := workorderbilling.Build(workorderbilling.BuildRequest{ID: id, WorkOrderID: order.ID, Policy: policy, Sources: sources, DescriptionBySource: descriptions})
	if err != nil {
		return BillingDraftResult{}, err
	}
	if err = draft.Validate(); err != nil {
		return BillingDraftResult{}, err
	}
	payload, err := canonicalJSON(struct {
		PeriodStart, PeriodEnd time.Time
		SourceRevision         uint64
		Draft                  workorderbilling.BillingDraft
	}{req.PeriodStart.UTC(), req.PeriodEnd.UTC(), order.Revision, draft})
	if err != nil {
		return BillingDraftResult{}, err
	}
	artifact, err := s.Artifacts.RecordArtifact(ctx, tenant(p), req.WorkOrderID, projectID, p.Subject(), req.IdempotencyKey, req.ExpectedRevision, "BILLING", digest, payload)
	if err != nil {
		return BillingDraftResult{}, err
	}
	var stored struct {
		PeriodStart, PeriodEnd time.Time
		SourceRevision         uint64
		Draft                  workorderbilling.BillingDraft
	}
	if err := json.Unmarshal(artifact.Payload, &stored); err != nil {
		return BillingDraftResult{}, ErrArtifactUnavailable
	}
	return BillingDraftResult{Draft: stored.Draft, SourceRevision: stored.SourceRevision, Artifact: artifact}, nil
}

func billingModeMatches(mode workorderbilling.PricingMode, templateMode string) bool {
	switch strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(templateMode, "-", "_"), " ", "_")) {
	case "UNIT_PRICE":
		return mode == workorderbilling.PricingUnitPrice
	case "TIME_AND_MATERIAL", "TIME_MATERIAL":
		return mode == workorderbilling.PricingTimeMaterial
	case "FIXED_MILESTONE":
		return mode == workorderbilling.PricingFixedMilestone
	default:
		return false
	}
}
