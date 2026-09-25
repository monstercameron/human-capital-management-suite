// Package workorderreport builds deterministic, authorization-bound work order
// report projections from bounded source snapshots supplied by domain ports.
package workorderreport

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/reportrender"
)

var (
	ErrInvalid      = errors.New("workorderreport: invalid input")
	ErrUnauthorized = errors.New("workorderreport: unauthorized")
)

const (
	DailyField     = "daily-field"
	Progress       = "progress"
	Cost           = "cost"
	OpenRequests   = "open-requests"
	Closeout       = "closeout"
	MaxRecords     = 10000
	StatusComplete = reportrender.Complete
	StatusPartial  = reportrender.Partial
	StatusStale    = reportrender.Stale
	StatusUnknown  = reportrender.Unknown
)

type Scope struct {
	TenantID    string `json:"tenant_id"`
	ProjectID   string `json:"project_id"`
	WorkOrderID string `json:"work_order_id"`
}

// Definition is copied into each report and pins the published report logic.
type Definition struct {
	ID      string `json:"id"`
	Version uint32 `json:"version"`
	Digest  string `json:"digest"`
	Kind    string `json:"kind"`
}

type Principal struct {
	ID      string `json:"id"`
	Tenant  string `json:"tenant"`
	Purpose string `json:"purpose"`
}

type Authorization struct {
	Principal Principal
	// Allow is evaluated for the exact tenant/project/order and report kind at
	// generation time. Nil fails closed; callers must re-evaluate on every run.
	Allow func(Principal, Scope, string) bool
}

type Watermark struct {
	Source string `json:"source"`
	Value  string `json:"value"`
}

// Entry is a bounded, tenant and work-order scoped source fact. Values are
// already authorization-filtered by the source port; the scope is checked
// again here before any projection is built.
type Entry struct {
	TenantID    string            `json:"tenant_id"`
	ProjectID   string            `json:"project_id"`
	WorkOrderID string            `json:"work_order_id"`
	Source      string            `json:"source"`
	ID          string            `json:"id"`
	Fields      map[string]string `json:"fields"`
}

type Source struct {
	Name      string    `json:"name"`
	Watermark Watermark `json:"watermark"`
	Complete  bool      `json:"complete"`
	Entries   []Entry   `json:"entries"`
}

type Request struct {
	Scope              Scope         `json:"scope"`
	Definition         Definition    `json:"definition"`
	Authorization      Authorization `json:"-"`
	Sources            []Source      `json:"sources"`
	ExpectedWatermarks []Watermark   `json:"expected_watermarks,omitempty"`
}

type Result struct {
	Kind           string                        `json:"kind"`
	Scope          Scope                         `json:"scope"`
	Definition     Definition                    `json:"definition"`
	Status         string                        `json:"status"`
	PrincipalID    string                        `json:"principal_id"`
	Watermarks     []Watermark                   `json:"watermarks"`
	MissingSources []string                      `json:"missing_sources,omitempty"`
	StaleSources   []string                      `json:"stale_sources,omitempty"`
	RenderInput    reportrender.ExecutionRequest `json:"-"`
}

var required = map[string][]string{
	DailyField:   {"work-log", "progress", "cost", "request"},
	Progress:     {"progress"},
	Cost:         {"cost"},
	OpenRequests: {"request"},
	Closeout:     {"progress", "cost", "request", "evidence", "inspection"},
}

func Generate(req Request) (Result, error) {
	p, s, d := req.Authorization.Principal, req.Scope, req.Definition
	if p.ID == "" || p.Tenant == "" || p.Purpose == "" || p.Tenant != s.TenantID || s.TenantID == "" || s.ProjectID == "" || s.WorkOrderID == "" || d.ID == "" || d.Version == 0 || d.Digest == "" || len(required[d.Kind]) == 0 {
		return Result{}, ErrInvalid
	}
	if req.Authorization.Allow == nil || !req.Authorization.Allow(p, s, d.Kind) {
		return Result{}, ErrUnauthorized
	}
	if len(req.Sources) > 32 || len(req.ExpectedWatermarks) > 32 {
		return Result{}, ErrInvalid
	}
	seenSource := map[string]bool{}
	seenIDs := map[string]bool{}
	count := 0
	var rows []reportrender.Row
	marks := make([]Watermark, 0, len(req.Sources))
	complete := map[string]bool{}
	for _, src := range req.Sources {
		if src.Name == "" || src.Watermark.Source != src.Name || src.Watermark.Value == "" || seenSource[src.Name] {
			return Result{}, ErrInvalid
		}
		seenSource[src.Name], complete[src.Name] = true, src.Complete
		marks = append(marks, Watermark{src.Name, src.Watermark.Value})
		count += len(src.Entries)
		if count > MaxRecords {
			return Result{}, ErrInvalid
		}
		for _, e := range src.Entries {
			if e.TenantID != s.TenantID || e.ProjectID != s.ProjectID || e.WorkOrderID != s.WorkOrderID || e.Source != src.Name || e.ID == "" {
				return Result{}, ErrUnauthorized
			}
			key := src.Name + "/" + e.ID
			if seenIDs[key] {
				return Result{}, ErrInvalid
			}
			seenIDs[key] = true
			fields := map[string]string{"kind": d.Kind, "source": src.Name, "id": e.ID}
			for k, v := range e.Fields {
				if k == "" || k == "kind" || k == "source" || k == "id" {
					return Result{}, ErrInvalid
				}
				fields[k] = v
			}
			rows = append(rows, fields)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i]["source"] == rows[j]["source"] {
			return rows[i]["id"] < rows[j]["id"]
		}
		return rows[i]["source"] < rows[j]["source"]
	})
	sort.Slice(marks, func(i, j int) bool { return marks[i].Source < marks[j].Source })
	expected := map[string]string{}
	for _, w := range req.ExpectedWatermarks {
		if w.Source == "" || w.Value == "" || expected[w.Source] != "" {
			return Result{}, ErrInvalid
		}
		expected[w.Source] = w.Value
	}
	missing, stale := make([]string, 0), make([]string, 0)
	for _, source := range required[d.Kind] {
		if !seenSource[source] || !complete[source] {
			missing = append(missing, source)
		}
	}
	for name, value := range expected {
		found := false
		for _, w := range marks {
			if w.Source == name {
				found = true
				if w.Value != value {
					stale = append(stale, name)
				}
				break
			}
		}
		if !found {
			stale = append(stale, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	status := StatusComplete
	if len(missing) > 0 {
		status = StatusPartial
	}
	if len(stale) > 0 {
		status = StatusStale
	}
	if len(marks) == 0 {
		status = StatusUnknown
	}
	fields := []string{"kind", "source", "id"}
	for _, row := range rows {
		for k := range row {
			if k != "kind" && k != "source" && k != "id" {
				fields = append(fields, k)
			}
		}
	}
	sort.Strings(fields)
	fields = unique(fields)
	definition := reportrender.ReportDefinition{ID: d.ID + "/" + d.Kind, Version: d.Version, Population: scopeKey(s), Fields: fields}
	definition.Digest = d.Digest
	population := scopeKey(s)
	plan := reportrender.ExecutionRequest{Definition: definition, Authorization: reportrender.Authorization{Principal: reportrender.Principal{ID: p.ID, Tenant: p.Tenant, Purpose: p.Purpose}, Scope: reportrender.Scope{Population: population, Fields: fields}, Allow: func(reportrender.Principal, reportrender.Scope) bool { return req.Authorization.Allow(p, s, d.Kind) }}, Sources: []reportrender.Source{{Name: "work-order-" + d.Kind, Watermark: reportrender.Watermark{Source: "work-order-" + d.Kind, Value: watermarkText(marks)}, Complete: status == StatusComplete, Rows: rows}}}
	return Result{Kind: d.Kind, Scope: s, Definition: d, Status: status, PrincipalID: p.ID, Watermarks: marks, MissingSources: missing, StaleSources: stale, RenderInput: plan}, nil
}

func scopeKey(s Scope) string {
	return fmt.Sprintf("tenant/%s/project/%s/work-order/%s", s.TenantID, s.ProjectID, s.WorkOrderID)
}
func watermarkText(ws []Watermark) string {
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = w.Source + "=" + w.Value
	}
	return strings.Join(parts, ",")
}
func unique(xs []string) []string {
	if len(xs) == 0 {
		return xs
	}
	out := xs[:1]
	for _, x := range xs[1:] {
		if out[len(out)-1] != x {
			out = append(out, x)
		}
	}
	return out
}
