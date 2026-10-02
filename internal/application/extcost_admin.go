package application

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/extcost"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ExtcostAdminService struct {
	Port  *extcost.Gateway
	Names func(context.Context, extcost.Report) map[string]string
}

func ExtcostFilter(q url.Values) (extcost.Filter, error) {
	f := extcost.Filter{Provider: q.Get("provider"), Operation: q.Get("operation"), Feature: q.Get("feature"), Purpose: q.Get("purpose"), Agent: q.Get("agent"), Workflow: q.Get("workflow"), Person: q.Get("person"), Cause: q.Get("cause"), Key: q.Get("key"), Search: q.Get("search")}
	for _, s := range []string{f.Provider, f.Operation, f.Feature, f.Purpose, f.Agent, f.Workflow, f.Person, f.Cause, f.Key, f.Search} {
		if len(s) > 256 || strings.ContainsAny(s, "\r\n\x00") {
			return f, extcost.ErrInvalid
		}
	}
	for k, p := range map[string]*time.Time{"from": &f.From, "until": &f.Until} {
		if raw := q.Get(k); raw != "" {
			at, err := time.Parse("2006-01-02", raw)
			if err != nil {
				return f, extcost.ErrInvalid
			}
			*p = at
		}
	}
	if !f.From.IsZero() && !f.Until.IsZero() && !f.Until.After(f.From) {
		return f, extcost.ErrInvalid
	}
	if q.Get("limit") != "" {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > 10000 {
			return f, extcost.ErrInvalid
		}
		f.Limit = n
	}
	return f, nil
}
func (s *ExtcostAdminService) Page(ctx context.Context, v productui.View, tenant, actor string, f extcost.Filter) (productui.ExtcostPageProps, error) {
	p := productui.ExtcostPageProps{View: v, Filter: f}
	if s == nil || s.Port == nil {
		p.Error = true
		return p, extcost.ErrInvalid
	}
	report, err := s.Port.Read(ctx, tenant, actor, f)
	if err != nil {
		p.Allowed = !errors.Is(err, extcost.ErrDenied)
		p.Error = p.Allowed
		return p, err
	}
	p.Allowed = true
	p.Report = report
	if s.Names != nil {
		p.Names = s.Names(ctx, report)
	}
	return p, nil
}

type ExtcostAdminViewer func(*http.Request) (productui.View, string, string, error)
type ExtcostAdminRender func(http.ResponseWriter, *http.Request, productui.ExtcostPageProps)
type ExtcostAdminHandler struct {
	Service *ExtcostAdminService
	Viewer  ExtcostAdminViewer
	Render  ExtcostAdminRender
}

// ServeHTTP is a thin, authenticated read/export boundary. The composition
// supplies the existing product-shell renderer; no second shell is invented.
func (h *ExtcostAdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if h == nil || h.Service == nil || h.Viewer == nil || h.Render == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	view, tenant, actor, err := h.Viewer(r)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	f, err := ExtcostFilter(r.URL.Query())
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		h.Render(w, r, productui.ExtcostPageProps{View: view, Allowed: true, Error: true})
		return
	}
	export := r.URL.Query().Get("export") == "csv"
	if export {
		f.Limit = 10000
	}
	p, err := h.Service.Page(r.Context(), view, tenant, actor, f)
	if err != nil {
		if errors.Is(err, extcost.ErrDenied) {
			w.WriteHeader(http.StatusForbidden)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		h.Render(w, r, p)
		return
	}
	if export {
		if p.Report.Truncated {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			h.Render(w, r, productui.ExtcostPageProps{View: view, Allowed: true, Error: true})
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="external-usage.csv"`)
		if err := ExtcostExportCSV(w, p.Report); err != nil {
			return
		}
		return
	}
	h.Render(w, r, p)
}
func extcostCSVCell(s string) string {
	if strings.ContainsAny(strings.TrimLeft(s, " \t"), "\r\n") {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
	}
	trimmed := strings.TrimLeft(s, " \t")
	if len(trimmed) > 0 && strings.ContainsAny(trimmed[:1], "=+-@") {
		return "'" + s
	}
	return s
}
func ExtcostExportCSV(w http.ResponseWriter, r extcost.Report) error {
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"time_utc", "feature", "provider", "operation", "model", "currency", "cost_micros", "measurement", "outcome", "actor", "agent", "workflow", "cause", "attempt", "schedule_version", "finding", "legal_entity", "units", "provider_cost_micros", "provider_currency", "reserved_maximum_micros", "state"}); err != nil {
		return err
	}
	for _, l := range r.Lines {
		quality := "measured"
		if l.Measurement.Estimated {
			quality = "estimated"
		}
		c := l.Call
		row := []string{l.At.UTC().Format(time.RFC3339), c.Feature, c.Provider, c.Operation, c.Model, l.Currency, fmt.Sprint(l.CostMicros), quality, l.Measurement.Outcome, c.Attribution.Actor, c.Attribution.Agent, c.Attribution.WorkflowDefinition, c.Cause, c.Key, l.ScheduleVersion, l.Finding}
		units, _ := json.Marshal(l.Measurement.Units)
		providerCost := ""
		if l.Measurement.ProviderCostMicros != nil {
			providerCost = fmt.Sprint(*l.Measurement.ProviderCostMicros)
		}
		row = append(row, c.LegalEntity, string(units), providerCost, l.Measurement.ProviderCurrency, "", "settled")
		for i := range row {
			row[i] = extcostCSVCell(row[i])
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	for _, pending := range r.Pending {
		c := pending.Call
		row := []string{pending.At.UTC().Format(time.RFC3339), c.Feature, c.Provider, c.Operation, c.Model, pending.Currency, "", "estimated", "unknown", c.Attribution.Actor, c.Attribution.Agent, c.Attribution.WorkflowDefinition, c.Cause, c.Key, pending.ScheduleVersion, "pending", c.LegalEntity, "", "", "", fmt.Sprint(pending.MaximumMicros), pending.State}
		for i := range row {
			row[i] = extcostCSVCell(row[i])
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
