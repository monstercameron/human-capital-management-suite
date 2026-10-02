package application

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/extcost"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type extcostAppJournal struct {
	lines []extcost.Line
	fault error
}

func (j *extcostAppJournal) Reserve(_ context.Context, r extcost.Reservation) (extcost.Reservation, error) {
	if j.fault != nil {
		return r, j.fault
	}
	for _, l := range j.lines {
		if l.Call.Key == r.Call.Key {
			r.Replay = &l
			return r, nil
		}
	}
	return r, nil
}
func (*extcostAppJournal) MarkSent(context.Context, extcost.Reservation) error { return nil }
func (j *extcostAppJournal) Settle(_ context.Context, _ extcost.Reservation, l extcost.Line) error {
	j.lines = append(j.lines, l)
	return nil
}
func (*extcostAppJournal) Release(context.Context, extcost.Reservation) error       { return nil }
func (*extcostAppJournal) ChangeBudget(context.Context, extcost.BudgetChange) error { return nil }
func (j *extcostAppJournal) Read(_ context.Context, _ string, _ extcost.Filter) (extcost.Report, error) {
	return extcost.Report{Lines: j.lines}, j.fault
}
func (*extcostAppJournal) Reconcile(context.Context, extcost.Reconciliation, *extcost.Line) error {
	return nil
}

type extcostAppAuthority struct{ denied bool }

func (a extcostAppAuthority) AuthorizeExternalUsage(context.Context, string, string, string, extcost.Filter) error {
	if a.denied {
		return extcost.ErrDenied
	}
	return nil
}
func extcostAppDate() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
func extcostAppCall(key string) extcost.Call {
	return extcost.Call{Tenant: "tenant-a", LegalEntity: "entity", Feature: "answers", Provider: "fixture", Operation: "invoke", Model: "model", ModelVersion: "1", Purpose: "answers", DataClasses: []string{"internal"}, Key: key, Cause: "cause", RequestDigest: strings.Repeat("a", 64), Attribution: extcost.Attribution{Actor: "person"}}
}
func extcostAppPort(t *testing.T, j *extcostAppJournal, denied bool) *extcost.Gateway {
	t.Helper()
	s := extcost.Schedule{Version: "fixture-v1", Provider: "fixture", Operation: "invoke", Model: "model", ModelVersion: "1", Currency: "USD", Authority: "admin", Source: "fixture-only", EffectiveFrom: extcostAppDate().Add(-time.Hour), ReviewAt: extcostAppDate().Add(time.Hour), Rates: []extcost.Rate{{Unit: extcost.InputTokens, Tiers: []extcost.Tier{{Micros: 1, Per: 1}}}, {Unit: extcost.CachedTokens, Tiers: []extcost.Tier{{Micros: 1, Per: 1}}}, {Unit: extcost.OutputTokens, Tiers: []extcost.Tier{{Micros: 2, Per: 1}}}, {Unit: extcost.Requests, Tiers: []extcost.Tier{{Micros: 10, Per: 1}}}}}
	c, err := extcost.NewCatalog([]extcost.Schedule{s})
	if err != nil {
		t.Fatal(err)
	}
	g, err := extcost.NewGateway(j, c, extcostAppAuthority{denied}, []extcost.Operation{{Provider: "fixture", Name: "invoke", Model: "model", ModelVersion: "1"}}, extcostAppDate)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

type extcostAppModel struct {
	calls   int
	failure error
}

func (*extcostAppModel) Identity() agentmodel.ModelIdentity {
	return agentmodel.ModelIdentity{ProviderID: "fixture", ModelID: "model", Version: "1"}
}
func (*extcostAppModel) Capabilities() agentmodel.AdapterCapabilities {
	return agentmodel.AdapterCapabilities{MaxTools: 7}
}
func (m *extcostAppModel) Invoke(context.Context, agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	m.calls++
	return agentmodel.ModelResult{Usage: agentmodel.ModelUsage{InputTokens: 6, CachedInputTokens: 2, OutputTokens: 4, TotalTokens: 10, CostMicros: 14}}, m.failure
}

type extcostAppResults struct {
	values        map[string]agentmodel.ModelResult
	failure       error
	saved, loaded int
}

func (r *extcostAppResults) SaveExternalModelResult(_ context.Context, c extcost.Call, result agentmodel.ModelResult) (string, string, error) {
	if r.failure != nil {
		return "", "", r.failure
	}
	if r.values == nil {
		r.values = map[string]agentmodel.ModelResult{}
	}
	ref := "result-" + c.Key
	r.values[ref] = result
	r.saved++
	raw, _ := json.Marshal(result)
	digest := sha256.Sum256(raw)
	return ref, hex.EncodeToString(digest[:]), nil
}
func (r *extcostAppResults) LoadExternalModelResult(_ context.Context, _ extcost.Call, ref string) (agentmodel.ModelResult, error) {
	if r.failure != nil {
		return agentmodel.ModelResult{}, r.failure
	}
	result, ok := r.values[ref]
	if !ok {
		return agentmodel.ModelResult{}, extcost.ErrPending
	}
	r.loaded++
	return result, nil
}
func TestTodo_EXTCOST_002_Adapters(t *testing.T) {
	j := &extcostAppJournal{}
	port := extcostAppPort(t, j, false)
	inner := &extcostAppModel{}
	selection := agentmodel.ModelSelection{ProfileID: "profile", ProfileDigest: "digest", Identity: inner.Identity()}
	results := &extcostAppResults{}
	wrapped, err := ExtcostWrapModelAdapters(map[agentmodel.ModelSelection]agentmodel.ModelAdapter{selection: inner}, port, ExtcostModelCall, results)
	if err != nil {
		t.Fatal(err)
	}
	adapter := wrapped[selection]
	ctx := ExtcostWithCall(context.Background(), extcostAppCall("trace"))
	request := agentmodel.ModelRequest{TraceID: "trace", Limits: agentmodel.ModelLimits{MaxInputTokens: 10, MaxOutputTokens: 10}}
	result, err := adapter.Invoke(ctx, request)
	if err != nil || result.Usage.TotalTokens != 10 || len(j.lines) != 1 || j.lines[0].CostMicros != 14 || j.lines[0].Measurement.Units[extcost.CachedTokens] != 2 || inner.calls != 1 {
		t.Fatal("model accounting", result, err)
	}
	if adapter.Capabilities().MaxTools != 7 {
		t.Fatal("capabilities lost")
	}
	if replay, err := adapter.Invoke(ctx, request); err != nil || replay.Usage.CostMicros != 14 || replay.Usage.TotalTokens != 10 || inner.calls != 1 || results.saved != 1 || results.loaded != 1 {
		t.Fatal("model replay re-invoked")
	}
	if _, err := adapter.Invoke(context.Background(), request); !errors.Is(err, extcost.ErrInvalid) {
		t.Fatal("unbound model called")
	}
	if _, err := ExtcostWrapModelAdapters(nil, port, ExtcostModelCall); !errors.Is(err, extcost.ErrInvalid) {
		t.Fatal("empty catalog accepted")
	}
	typed := adapter.(*ExtcostModelAdapter)
	if typed.Identity() != inner.Identity() {
		t.Fatal("identity lost")
	}
	var nilAdapter *ExtcostModelAdapter
	if nilAdapter.Identity() != (agentmodel.ModelIdentity{}) || nilAdapter.Capabilities().MaxTools != 0 {
		t.Fatal("nil adapter")
	}
	if _, err := ExtcostWrapModelAdapters(map[agentmodel.ModelSelection]agentmodel.ModelAdapter{selection: inner}, port, ExtcostModelCall); !errors.Is(err, extcost.ErrInvalid) {
		t.Fatal("missing result journal accepted")
	}
	results.failure = errors.New("result journal unavailable")
	request.TraceID = "journal-failure"
	ctx = ExtcostWithCall(context.Background(), extcostAppCall(request.TraceID))
	if _, err := adapter.Invoke(ctx, request); !errors.Is(err, extcost.ErrPending) || inner.calls != 2 || len(j.lines) != 1 {
		t.Fatal("missing durable result was settled", err)
	}

}

type extcostAppHTTP struct {
	calls int
	body  *extcostAppBody
}

func (h *extcostAppHTTP) Do(*http.Request) (*http.Response, error) {
	h.calls++
	h.body = &extcostAppBody{Reader: strings.NewReader("fixture-response")}
	return &http.Response{StatusCode: 200, Body: h.body}, nil
}

type extcostAppBody struct {
	*strings.Reader
	closed bool
}

func (b *extcostAppBody) Close() error { b.closed = true; return nil }
func TestTodo_EXTCOST_002_HTTP(t *testing.T) {
	j := &extcostAppJournal{}
	port := extcostAppPort(t, j, false)
	inner := &extcostAppHTTP{}
	d := &ExtcostHTTPDoer{Port: port, Inner: inner, Bind: func(*http.Request) (extcost.Call, extcost.Units, error) {
		return extcostAppCall("http"), extcost.Units{extcost.Requests: 1}, nil
	}, Measure: func(*http.Request, *http.Response, error) extcost.Measurement {
		return extcost.Measurement{Units: extcost.Units{extcost.Requests: 1}, Billed: true, Outcome: "succeeded"}
	}}
	request := httptest.NewRequest(http.MethodPost, "https://provider.invalid/operation", nil)
	response, err := d.Do(request)
	if err != nil || response.StatusCode != 200 || len(j.lines) != 1 || j.lines[0].CostMicros != 10 {
		t.Fatal("HTTP usage", err)
	}
	_ = response.Body.Close()
	if _, err := d.Do(request); !errors.Is(err, extcost.ErrReplay) || inner.calls != 1 {
		t.Fatal("HTTP replay")
	}
	j.fault = extcost.ErrBudget
	if _, err := d.Do(request); !errors.Is(err, extcost.ErrBudget) || inner.calls != 1 {
		t.Fatal("budget dispatched HTTP")
	}
	reason, route := ExtcostBudgetRoute(errors.Join(errors.New("outer"), extcost.ErrBudget))
	if !route || !strings.Contains(reason, "spending limit") || strings.Contains(reason, "outer") {
		t.Fatal("budget route has no reason")
	}
	if reason, route := ExtcostBudgetRoute(errors.Join(errors.New("private provider detail"), extcost.ErrOverrun)); !route || strings.Contains(reason, "private") {
		t.Fatal("budget reason disclosed provider failure")
	}
	if _, route := ExtcostBudgetRoute(nil); route {
		t.Fatal("nil routed")
	}
	if _, err := (&ExtcostHTTPDoer{}).Do(request); !errors.Is(err, extcost.ErrInvalid) {
		t.Fatal("missing dependencies")
	}
	j.fault = nil
	d.Bind = func(*http.Request) (extcost.Call, extcost.Units, error) {
		return extcostAppCall("unpriced-response"), extcost.Units{extcost.Requests: 1}, nil
	}
	d.Measure = func(*http.Request, *http.Response, error) extcost.Measurement {
		return extcost.Measurement{Units: extcost.Units{extcost.Characters: 1}, Billed: true, Outcome: "succeeded"}
	}
	if _, err := d.Do(request); !errors.Is(err, extcost.ErrUnpriced) || inner.calls != 2 || !inner.body.closed || len(j.lines) != 1 {
		t.Fatal("accounting failure leaked response or settled unknown price", err)
	}

}
func TestTodo_EXTCOST_004_Application(t *testing.T) {
	identity := agentmodel.ModelIdentity{ProviderID: "fixture", ModelID: "model", Version: "1"}
	p, err := agentmodel.NewPricingSchedule(agentmodel.PricingSchedule{Version: "approved", Authority: "admin", Signature: "fixture-signed", Entries: []agentmodel.PricingEntry{{Identity: identity, InputMicrosPerMillionTokens: 1_000_000, OutputMicrosPerMillionTokens: 2_000_000, CachedInputMicrosPerMillionTokens: 500_000}}})
	if err != nil {
		t.Fatal(err)
	}
	schedules, err := ExtcostTokenSchedules(p, "USD", "invoke", "approved schedule", extcostAppDate().Add(-time.Hour), time.Time{}, extcostAppDate().Add(time.Hour))
	if err != nil || len(schedules) != 1 {
		t.Fatal("price conversion", err)
	}
	cost, err := extcost.Price(schedules[0], extcost.Units{extcost.InputTokens: 4, extcost.CachedTokens: 2, extcost.OutputTokens: 4})
	if err != nil || cost != 13 {
		t.Fatal("converted cost", cost, err)
	}
	if _, err := ExtcostTokenSchedules(nil, "USD", "invoke", "source", extcostAppDate(), time.Time{}, extcostAppDate()); !errors.Is(err, extcost.ErrUnpriced) {
		t.Fatal("nil pricing silently free")
	}
}
func TestTodo_EXTCOST_006_Application(t *testing.T) {
	j := &extcostAppJournal{lines: []extcost.Line{{Call: extcostAppCall("a"), Currency: "USD", CostMicros: 10, At: extcostAppDate(), Measurement: extcost.Measurement{Outcome: "succeeded"}}}}
	service := &ExtcostAdminService{Port: extcostAppPort(t, j, false)}
	view := productui.ApplyLocale(productui.NewView(productui.ExtcostPageID, "tenant-a", "admin", ""), productui.ResolveProductLocale("en-US"))
	rendered := false
	handler := &ExtcostAdminHandler{Service: service, Viewer: func(*http.Request) (productui.View, string, string, error) { return view, "tenant-a", "admin", nil }, Render: func(w http.ResponseWriter, _ *http.Request, p productui.ExtcostPageProps) {
		rendered = true
		if p.Allowed {
			_, _ = io.WriteString(w, "authorized page")
		}
	}}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, productui.ExtcostPagePath+"?provider=fixture", nil))
	if w.Code != 200 || !rendered || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("admin read", w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, productui.ExtcostPagePath+"?export=csv", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "cost_micros") || !strings.Contains(w.Body.String(), "fixture") {
		t.Fatal("export")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, productui.ExtcostPagePath, nil))
	if w.Code != 405 {
		t.Fatal("write method accepted")
	}
	j.lines[0].Call.Feature = "=SUM(1,1)"
	w = httptest.NewRecorder()
	if err := ExtcostExportCSV(w, extcost.Report{Lines: j.lines}); err != nil || !strings.Contains(w.Body.String(), "'=SUM") {
		t.Fatal("CSV formula injection")
	}
	for _, raw := range []string{"from=invalid", "from=2026-10-02&until=2026-10-01", "limit=10001", "search=%00"} {
		q, _ := url.ParseQuery(raw)
		if _, err := ExtcostFilter(q); !errors.Is(err, extcost.ErrInvalid) {
			t.Fatal("bad filter accepted", raw)
		}
	}
	f, err := ExtcostFilter(url.Values{"from": {"2026-10-01"}, "until": {"2026-11-01"}, "limit": {"20"}, "purpose": {"answers"}})
	if err != nil || f.Limit != 20 || f.Purpose != "answers" {
		t.Fatal("filter", err)
	}
	w = httptest.NewRecorder()
	if err := ExtcostExportCSV(w, extcost.Report{Pending: []extcost.Reservation{{Call: extcostAppCall("pending"), At: extcostAppDate(), Currency: "USD", MaximumMicros: 10, State: "sent"}}}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil || len(rows) != 2 || len(rows[1]) != 22 || rows[1][6] != "" || rows[1][7] != "estimated" || rows[1][20] != "10" || rows[1][21] != "sent" {
		t.Fatal("pending export claimed zero measured spend", rows, err)
	}

}
func TestTodo_EXTCOST_006_ApplicationSecurity(t *testing.T) {
	service := &ExtcostAdminService{Port: extcostAppPort(t, &extcostAppJournal{}, true)}
	p, err := service.Page(context.Background(), productui.View{}, "tenant-a", "person", extcost.Filter{})
	if !errors.Is(err, extcost.ErrDenied) || p.Allowed || len(p.Report.Lines) != 0 {
		t.Fatal("denied read exposed usage")
	}
}

func TestTodo_EXTCOST_002_Binding(t *testing.T) {
	identity := (&extcostAppModel{}).Identity()
	request := AgentModelGatewayRequest{TenantID: "tenant-a", Dispatch: agentegress.ProviderDispatchRequest{Model: agentmodel.ModelRequest{TraceID: "trace"}, Outbound: agentegress.OutboundRequest{Tenant: "tenant-a", Principal: "person", TaskID: "run", Purpose: "answers", Fields: []agentegress.Field{{Name: "content", Value: "private payload", Class: trustdlp.DataClass("internal")}}}}}
	call, err := ExtcostGatewayCall(request, identity, "entity")
	if err != nil || call.Tenant != "tenant-a" || call.Attribution.Actor != "person" || call.Attribution.AgentRun != "run" || call.Key != "trace" || call.Cause != "run" || call.Operation != "model.invoke" || len(call.DataClasses) != 1 || call.DataClasses[0] != "internal" {
		t.Fatal("trusted metadata lost", call, err)
	}
	raw, _ := json.Marshal(call)
	if strings.Contains(string(raw), "private payload") || len(call.RequestDigest) != 64 {
		t.Fatal("request content entered ledger")
	}
	bound, err := ExtcostModelCall(ExtcostWithCall(context.Background(), call), request.Dispatch.Model)
	if err != nil || bound.RequestDigest != call.RequestDigest {
		t.Fatal("binding lost", err)
	}
	request.TenantID = "other"
	if _, err := ExtcostGatewayCall(request, identity, "entity"); !errors.Is(err, extcost.ErrInvalid) {
		t.Fatal("cross tenant binding accepted")
	}
	if _, err := ExtcostModelCall(nil, request.Dispatch.Model); !errors.Is(err, extcost.ErrInvalid) {
		t.Fatal("nil context accepted")
	}
}

type extcostAppRoundTripper struct{ http *extcostAppHTTP }

func (r extcostAppRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return r.http.Do(request)
}
func TestTodo_EXTCOST_002_RoundTrip(t *testing.T) {
	j := &extcostAppJournal{}
	inner := &extcostAppHTTP{}
	transport := &ExtcostRoundTripper{Port: extcostAppPort(t, j, false), Inner: extcostAppRoundTripper{inner}, Bind: func(*http.Request) (extcost.Call, extcost.Units, error) {
		return extcostAppCall("transport"), extcost.Units{extcost.Requests: 1}, nil
	}, Measure: func(*http.Request, *http.Response, error) extcost.Measurement {
		return extcost.Measurement{Units: extcost.Units{extcost.Requests: 1}, Outcome: "succeeded", Billed: true}
	}}
	req := httptest.NewRequest(http.MethodGet, "https://provider.invalid/operation", nil)
	response, err := transport.RoundTrip(req)
	if err != nil || response == nil || inner.calls != 1 || len(j.lines) != 1 || j.lines[0].CostMicros != 10 {
		t.Fatal("transport accounting", err)
	}
	_ = response.Body.Close()
	if _, err := transport.RoundTrip(req); !errors.Is(err, extcost.ErrReplay) || inner.calls != 1 {
		t.Fatal("transport replay dispatched")
	}
	var missing *ExtcostRoundTripper
	if _, err := missing.RoundTrip(req); !errors.Is(err, extcost.ErrInvalid) {
		t.Fatal("nil transport accepted")
	}
}

func TestTodo_EXTCOST_002_ResultJournal(t *testing.T) {
	j := &extcostAppJournal{}
	inner := &extcostAppModel{failure: errors.New("private provider failure")}
	results := &extcostAppResults{}
	selection := agentmodel.ModelSelection{ProfileID: "profile", ProfileDigest: "digest", Identity: inner.Identity()}
	wrapped, err := ExtcostWrapModelAdapters(map[agentmodel.ModelSelection]agentmodel.ModelAdapter{selection: inner}, extcostAppPort(t, j, false), ExtcostModelCall, results)
	if err != nil {
		t.Fatal(err)
	}
	adapter := wrapped[selection]
	req := agentmodel.ModelRequest{TraceID: "failed", Limits: agentmodel.ModelLimits{MaxInputTokens: 10, MaxOutputTokens: 10}}
	ctx := ExtcostWithCall(context.Background(), extcostAppCall(req.TraceID))
	result, err := adapter.Invoke(ctx, req)
	if err == nil || result.Failure == nil || result.Failure.Code != agentmodel.FailureInternal || len(j.lines) != 1 || j.lines[0].Measurement.Outcome != "failed" || j.lines[0].CostMicros != 14 {
		t.Fatal("billed failure was not durably journaled", err)
	}
	replay, err := adapter.Invoke(ctx, req)
	if !errors.Is(err, ErrExtcostRecordedFailure) || replay.Failure == nil || inner.calls != 1 || results.saved != 1 || results.loaded != 1 || strings.Contains(err.Error(), "private") {
		t.Fatal("replay changed failed outcome or dispatched", err)
	}
	corrupted := results.values["result-failed"]
	corrupted.Usage.InputTokens++
	results.values["result-failed"] = corrupted
	if _, err := adapter.Invoke(ctx, req); !errors.Is(err, extcost.ErrConflict) || inner.calls != 1 {
		t.Fatal("result digest mismatch accepted", err)
	}
}

type extcostAppMalformedModel struct{ extcostAppModel }

func (m *extcostAppMalformedModel) Invoke(ctx context.Context, request agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	result, err := m.extcostAppModel.Invoke(ctx, request)
	result.Usage.TotalTokens++
	return result, err
}
func TestTodo_EXTCOST_002_UnknownUnits(t *testing.T) {
	j := &extcostAppJournal{}
	inner := &extcostAppMalformedModel{}
	results := &extcostAppResults{}
	selection := agentmodel.ModelSelection{ProfileID: "profile", ProfileDigest: "digest", Identity: inner.Identity()}
	wrapped, err := ExtcostWrapModelAdapters(map[agentmodel.ModelSelection]agentmodel.ModelAdapter{selection: inner}, extcostAppPort(t, j, false), ExtcostModelCall, results)
	if err != nil {
		t.Fatal(err)
	}
	request := agentmodel.ModelRequest{TraceID: "unknown-units", Limits: agentmodel.ModelLimits{MaxInputTokens: 10, MaxOutputTokens: 10}}
	ctx := ExtcostWithCall(context.Background(), extcostAppCall(request.TraceID))
	_, err = wrapped[selection].Invoke(ctx, request)
	if !errors.Is(err, extcost.ErrInvalid) || !errors.Is(err, extcost.ErrPending) || len(j.lines) != 0 || results.saved != 0 || inner.calls != 1 {
		t.Fatal("unexplained usage was silently undercounted", err)
	}
}
