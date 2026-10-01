package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// UsageDimension is the published, tenant-plan vocabulary. Files are measured
// in bytes, search in queries, and AI spend in micros of the plan currency.
type UsageDimension string

const (
	UsageProjects      UsageDimension = "projects"
	UsageTasks         UsageDimension = "tasks"
	UsageFields        UsageDimension = "fields"
	UsageFiles         UsageDimension = "files"
	UsageEvents        UsageDimension = "events"
	UsageSearch        UsageDimension = "search"
	UsageAITokens      UsageDimension = "ai_tokens"
	UsageAISpendMicros UsageDimension = "ai_spend_micros"
)

var publishedUsageDimensions = []UsageDimension{
	UsageProjects, UsageTasks, UsageFields, UsageFiles,
	UsageEvents, UsageSearch, UsageAITokens, UsageAISpendMicros,
}

var (
	ErrInvalidUsagePlan  = errors.New("project: invalid usage plan")
	ErrInvalidUsage      = errors.New("project: invalid usage request")
	ErrUsageLimitReached = errors.New("project: tenant usage limit reached")
	ErrUsageNotFound     = errors.New("project: usage tenant not found")
)

// Quota is a published hard limit and its warning threshold. Both boundaries
// are inclusive: a request reaching WarningAt is admitted with a warning;
// a request exceeding Limit is rejected atomically.
type Quota struct {
	Limit     int64 `json:"limit"`
	WarningAt int64 `json:"warning_at"`
}

// UsagePlan is immutable after construction. Keeping the version on every
// export makes a plan change auditable rather than silently changing meaning.
type UsagePlan struct {
	ID      string                   `json:"id"`
	Version string                   `json:"version"`
	Quotas  map[UsageDimension]Quota `json:"quotas"`
}

func PublishedUsagePlan() (UsagePlan, error) {
	return NewUsagePlan("project-standard", "v1", map[UsageDimension]Quota{
		UsageProjects:      {Limit: 100, WarningAt: 80},
		UsageTasks:         {Limit: 10000, WarningAt: 8000},
		UsageFields:        {Limit: 2500, WarningAt: 2000},
		UsageFiles:         {Limit: 50 * 1024 * 1024 * 1024, WarningAt: 40 * 1024 * 1024 * 1024},
		UsageEvents:        {Limit: 100000, WarningAt: 80000},
		UsageSearch:        {Limit: 100000, WarningAt: 80000},
		UsageAITokens:      {Limit: 1000000, WarningAt: 800000},
		UsageAISpendMicros: {Limit: 100000000, WarningAt: 80000000},
	})
}

func NewUsagePlan(id, version string, quotas map[UsageDimension]Quota) (UsagePlan, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(version) == "" || len(quotas) != len(publishedUsageDimensions) {
		return UsagePlan{}, ErrInvalidUsagePlan
	}
	copyQuotas := make(map[UsageDimension]Quota, len(quotas))
	for _, dimension := range publishedUsageDimensions {
		quota, ok := quotas[dimension]
		if !ok || quota.Limit <= 0 || quota.WarningAt <= 0 || quota.WarningAt > quota.Limit {
			return UsagePlan{}, fmt.Errorf("%w: %s quota must have 0 < warning_at <= limit", ErrInvalidUsagePlan, dimension)
		}
		copyQuotas[dimension] = quota
	}
	return UsagePlan{ID: id, Version: version, Quotas: copyQuotas}, nil
}

// UsageDelta is one atomic operation. A rejected operation contributes no
// amount to any dimension or project attribution.
type UsageDelta map[UsageDimension]int64

func (d UsageDelta) validate() error {
	if len(d) == 0 {
		return ErrInvalidUsage
	}
	known := make(map[UsageDimension]struct{}, len(publishedUsageDimensions))
	for _, dimension := range publishedUsageDimensions {
		known[dimension] = struct{}{}
	}
	for dimension, amount := range d {
		if _, ok := known[dimension]; !ok || amount <= 0 {
			return fmt.Errorf("%w: %s must be a positive published dimension", ErrInvalidUsage, dimension)
		}
	}
	return nil
}

type UsageRequest struct {
	TenantID   string
	ProjectID  string
	Operation  string
	Delta      UsageDelta
	CostMicros int64
}

type AdmissionStatus string

const (
	UsageAdmitted AdmissionStatus = "ADMITTED"
	UsageWarning  AdmissionStatus = "ADMITTED_WITH_WARNING"
	UsageRejected AdmissionStatus = "REJECTED"
)

type UsageAdmission struct {
	Status    AdmissionStatus
	TenantID  string
	ProjectID string
	Warnings  []UsageDimension
	Rejected  []UsageDimension
	Totals    UsageDelta
	Reason    string
}

type CostAttribution struct {
	TenantID   string         `json:"tenant_id"`
	ProjectID  string         `json:"project_id"`
	Operation  string         `json:"operation"`
	Dimension  UsageDimension `json:"dimension"`
	Quantity   int64          `json:"quantity"`
	CostMicros int64          `json:"cost_micros"`
}

type ProjectUsage struct {
	ProjectID string     `json:"project_id"`
	Totals    UsageDelta `json:"totals"`
}

type UsageExport struct {
	TenantID     string            `json:"tenant_id"`
	PlanID       string            `json:"plan_id"`
	PlanVersion  string            `json:"plan_version"`
	Totals       UsageDelta        `json:"totals"`
	Projects     []ProjectUsage    `json:"projects"`
	Attributions []CostAttribution `json:"attributions"`
	Digest       string            `json:"digest"`
}

type usageLedger struct {
	totals       UsageDelta
	projects     map[string]UsageDelta
	attributions []CostAttribution
}

// UsageMeter owns mutable counters. It is safe for concurrent admission and
// makes the limit decision and counter update one critical section.
type UsageMeter struct {
	mu      sync.Mutex
	plan    UsagePlan
	tenants map[string]*usageLedger
}

func NewUsageMeter(plan UsagePlan) (*UsageMeter, error) {
	validated, err := NewUsagePlan(plan.ID, plan.Version, plan.Quotas)
	if err != nil {
		return nil, err
	}
	return &UsageMeter{plan: validated, tenants: make(map[string]*usageLedger)}, nil
}

func (m *UsageMeter) Admit(req UsageRequest) (UsageAdmission, error) {
	if m == nil {
		return UsageAdmission{}, ErrInvalidUsage
	}
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.Operation) == "" || req.CostMicros < 0 || req.Delta.validate() != nil {
		return UsageAdmission{}, ErrInvalidUsage
	}
	if spend := req.Delta[UsageAISpendMicros]; req.CostMicros > 0 && spend != req.CostMicros {
		return UsageAdmission{}, fmt.Errorf("%w: cost_micros must equal ai_spend_micros delta", ErrInvalidUsage)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	ledger := m.tenants[req.TenantID]
	if ledger == nil {
		ledger = &usageLedger{totals: UsageDelta{}, projects: make(map[string]UsageDelta)}
	}
	projectTotals := ledger.projects[req.ProjectID]
	projectTotals = cloneUsage(projectTotals)
	projectTotals.add(req.Delta)
	prospective := cloneUsage(ledger.totals)
	prospective.add(req.Delta)
	warnings := make([]UsageDimension, 0)
	rejected := make([]UsageDimension, 0)
	for _, dimension := range publishedUsageDimensions {
		amount := prospective[dimension]
		quota := m.plan.Quotas[dimension]
		if amount > quota.Limit {
			rejected = append(rejected, dimension)
		} else if amount >= quota.WarningAt {
			warnings = append(warnings, dimension)
		}
	}
	if len(rejected) > 0 {
		return UsageAdmission{Status: UsageRejected, TenantID: req.TenantID, ProjectID: req.ProjectID, Warnings: warnings, Rejected: rejected, Totals: prospective, Reason: ErrUsageLimitReached.Error()}, ErrUsageLimitReached
	}
	m.tenants[req.TenantID] = ledger
	ledger.totals = prospective
	ledger.projects[req.ProjectID] = projectTotals
	for _, dimension := range sortedDeltaDimensions(req.Delta) {
		quantity := req.Delta[dimension]
		cost := int64(0)
		if dimension == UsageAISpendMicros {
			cost = quantity
		}
		ledger.attributions = append(ledger.attributions, CostAttribution{TenantID: req.TenantID, ProjectID: req.ProjectID, Operation: req.Operation, Dimension: dimension, Quantity: quantity, CostMicros: cost})
	}
	status := UsageAdmitted
	if len(warnings) > 0 {
		status = UsageWarning
	}
	return UsageAdmission{Status: status, TenantID: req.TenantID, ProjectID: req.ProjectID, Warnings: warnings, Totals: prospective, Reason: "usage admitted within published tenant plan"}, nil
}

func (m *UsageMeter) Export(tenantID string) (UsageExport, error) {
	if m == nil || strings.TrimSpace(tenantID) == "" {
		return UsageExport{}, ErrInvalidUsage
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ledger, ok := m.tenants[tenantID]
	if !ok {
		return UsageExport{}, ErrUsageNotFound
	}
	result := UsageExport{TenantID: tenantID, PlanID: m.plan.ID, PlanVersion: m.plan.Version, Totals: cloneUsage(ledger.totals), Attributions: append([]CostAttribution(nil), ledger.attributions...)}
	for projectID, totals := range ledger.projects {
		result.Projects = append(result.Projects, ProjectUsage{ProjectID: projectID, Totals: cloneUsage(totals)})
	}
	sort.Slice(result.Projects, func(i, j int) bool { return result.Projects[i].ProjectID < result.Projects[j].ProjectID })
	sort.Slice(result.Attributions, func(i, j int) bool {
		if result.Attributions[i].ProjectID != result.Attributions[j].ProjectID {
			return result.Attributions[i].ProjectID < result.Attributions[j].ProjectID
		}
		if result.Attributions[i].Operation != result.Attributions[j].Operation {
			return result.Attributions[i].Operation < result.Attributions[j].Operation
		}
		return result.Attributions[i].Dimension < result.Attributions[j].Dimension
	})
	result.Digest = usageExportDigest(result)
	return result, nil
}

func cloneUsage(in UsageDelta) UsageDelta {
	out := make(UsageDelta, len(in))
	for dimension, amount := range in {
		out[dimension] = amount
	}
	return out
}

func (d UsageDelta) add(other UsageDelta) {
	for dimension, amount := range other {
		d[dimension] += amount
	}
}

func sortedDeltaDimensions(delta UsageDelta) []UsageDimension {
	out := make([]UsageDimension, 0, len(delta))
	for dimension := range delta {
		out = append(out, dimension)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func usageExportDigest(export UsageExport) string {
	copy := export
	copy.Digest = ""
	raw, _ := json.Marshal(copy)
	sum := sha256.Sum256(append([]byte("hcmnext.project.usage/v1\x00"), raw...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
