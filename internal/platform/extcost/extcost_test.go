package extcost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type extcostAuthority struct{ denied bool }

func (a extcostAuthority) AuthorizeExternalUsage(_ context.Context, t, u, action string, _ Filter) error {
	if a.denied || t == "" || u == "" {
		return ErrDenied
	}
	return nil
}

type extcostMemory struct {
	warnings []BudgetStatus
	mu       sync.Mutex
	lines    []Line
	pending  map[string]Reservation
	sent     map[string]bool
	fault    string
	changes  []BudgetChange
	reports  []Reconciliation
}

func (m *extcostMemory) Reserve(_ context.Context, r Reservation) (Reservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fault == "reserve" {
		return Reservation{}, ErrBudget
	}
	if m.pending == nil {
		m.pending = map[string]Reservation{}
		m.sent = map[string]bool{}
	}
	k := r.Call.Tenant + ":" + r.Call.Key
	if p, ok := m.pending[k]; ok {
		if p.Fingerprint != r.Fingerprint {
			return Reservation{}, ErrConflict
		}
		for _, l := range m.lines {
			if l.Call.Key == r.Call.Key && l.Call.Tenant == r.Call.Tenant {
				r.Replay = &l
				return r, nil
			}
		}
		return Reservation{}, ErrPending
	}
	r.Budgets = m.warnings
	m.pending[k] = r
	return r, nil
}
func (m *extcostMemory) MarkSent(_ context.Context, r Reservation) error {
	if m.fault == "sent" {
		return ErrPending
	}
	m.sent[r.Call.Tenant+":"+r.Call.Key] = true
	return nil
}
func (m *extcostMemory) Settle(_ context.Context, r Reservation, l Line) error {
	if m.fault == "settle" {
		return ErrPending
	}
	if !m.sent[r.Call.Tenant+":"+r.Call.Key] {
		return ErrPending
	}
	for _, p := range m.lines {
		if p.Call.Key == l.Call.Key && p.Call.Tenant == l.Call.Tenant {
			return nil
		}
	}
	m.lines = append(m.lines, l)
	return nil
}
func (m *extcostMemory) Release(_ context.Context, r Reservation) error {
	delete(m.pending, r.Call.Tenant+":"+r.Call.Key)
	return nil
}
func (m *extcostMemory) ChangeBudget(_ context.Context, c BudgetChange) error {
	m.changes = append(m.changes, c)
	return nil
}
func (m *extcostMemory) Read(_ context.Context, t string, _ Filter) (Report, error) {
	var r Report
	for _, l := range m.lines {
		if l.Call.Tenant == t {
			r.Lines = append(r.Lines, l)
		}
	}
	return r, nil
}
func (m *extcostMemory) Reconcile(_ context.Context, r Reconciliation, _ *Line) error {
	m.reports = append(m.reports, r)
	return nil
}
func extcostDate() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
func extcostSchedule() Schedule {
	return Schedule{Version: "fixture-v1", Provider: "simulator", Operation: "request", Currency: "USD", Authority: "admin", Source: "fixture-only", EffectiveFrom: extcostDate().Add(-time.Hour), ReviewAt: extcostDate().Add(time.Hour), Rates: []Rate{{Requests, []Tier{{0, 10, 1}}}}}
}
func extcostCall(k string) Call {
	return Call{Tenant: "tenant-a", LegalEntity: "entity", Feature: "answers", Provider: "simulator", Operation: "request", Purpose: "answers", DataClasses: []string{"internal"}, Key: k, Cause: "cause", RequestDigest: strings.Repeat("a", 64), Attribution: Attribution{Actor: "person", Agent: "agent", AgentRun: "run", Step: "step", Conversation: "room"}}
}
func extcostGateway(t *testing.T, m *extcostMemory) *Gateway {
	t.Helper()
	c, err := NewCatalog([]Schedule{extcostSchedule()})
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewGateway(m, c, extcostAuthority{}, []Operation{{"simulator", "request", "", ""}}, extcostDate)
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func extcostInvoke(n int64) Invoke {
	return func(context.Context) (Measurement, error) {
		return Measurement{Units: Units{Requests: n}, Outcome: "succeeded", Billed: true, ResultRef: "result-id"}, nil
	}
}
func TestTodo_EXTCOST_002(t *testing.T) {
	m := &extcostMemory{}
	g := extcostGateway(t, m)
	calls := 0
	invoke := func(context.Context) (Measurement, error) {
		calls++
		return Measurement{Units: Units{Requests: 2}, Outcome: "failed", Billed: true, ResultRef: "result"}, errors.New("provider-private-response")
	}
	l, err := g.Run(context.Background(), extcostCall("a"), Units{Requests: 3}, invoke)
	if err == nil || l.CostMicros != 20 || l.ScheduleVersion != "fixture-v1" || len(m.lines) != 1 {
		t.Fatalf("billed failure %+v %v", l, err)
	}
	_, err = g.Run(context.Background(), extcostCall("a"), Units{Requests: 3}, invoke)
	if !errors.Is(err, ErrReplay) || calls != 1 {
		t.Fatal("replay called provider")
	}
	_, err = g.Run(context.Background(), extcostCall("b"), Units{Requests: 3}, invoke)
	if err == nil || calls != 2 || len(m.lines) != 2 {
		t.Fatal("real retry not recorded separately")
	}
	raw, _ := json.Marshal(m.lines)
	if strings.Contains(string(raw), "provider-private-response") {
		t.Fatal("journal retains content")
	}
}
func TestTodo_EXTCOST_002_Property(t *testing.T) {
	for _, keys := range [][]string{{"a", "a", "b", "c", "b"}, {"b", "c", "a", "a", "c"}} {
		m := &extcostMemory{}
		g := extcostGateway(t, m)
		for _, k := range keys {
			_, err := g.Run(context.Background(), extcostCall(k), Units{Requests: 2}, extcostInvoke(1))
			if err != nil && !errors.Is(err, ErrReplay) {
				t.Fatal(err)
			}
		}
		total := int64(0)
		for _, l := range m.lines {
			total += l.CostMicros
		}
		if total != 30 || len(m.lines) != 3 {
			t.Fatal("retry order changes cause sum")
		}
	}
}
func TestTodo_EXTCOST_002_Security(t *testing.T) {
	m := &extcostMemory{}
	g := extcostGateway(t, m)
	g.authority = extcostAuthority{true}
	called := false
	_, err := g.Run(context.Background(), extcostCall("a"), Units{Requests: 1}, func(context.Context) (Measurement, error) { called = true; return Measurement{}, nil })
	if !errors.Is(err, ErrDenied) || called || len(m.pending) != 0 {
		t.Fatal("authorization after effect")
	}
	c := extcostCall("a")
	c.Tenant = ""
	if ValidCall(c) {
		t.Fatal("tenant missing")
	}
	c = extcostCall("a")
	c.Key = "bad\nkey"
	if ValidCall(c) {
		t.Fatal("content accepted as identifier")
	}
}
func TestTodo_EXTCOST_002_Fault(t *testing.T) {
	for _, fault := range []string{"reserve", "sent", "settle", "unknown"} {
		t.Run(fault, func(t *testing.T) {
			m := &extcostMemory{fault: fault}
			g := extcostGateway(t, m)
			calls := 0
			_, err := g.Run(context.Background(), extcostCall("a"), Units{Requests: 3}, func(ctx context.Context) (Measurement, error) {
				calls++
				if fault == "unknown" {
					return Measurement{}, errors.New("lost response")
				}
				return extcostInvoke(1)(ctx)
			})
			if err == nil || len(m.lines) != 0 {
				t.Fatal("fault not preserved")
			}
			if fault == "reserve" || fault == "sent" {
				if calls != 0 {
					t.Fatal("dispatch before admission")
				}
			} else {
				m.fault = ""
				r := m.pending["tenant-a:a"]
				l, err := g.Recover(context.Background(), "admin", r, Measurement{Units: Units{Requests: 1}, Billed: true, Outcome: "succeeded"})
				if err != nil || l.CostMicros != 10 || len(m.lines) != 1 || calls != 1 {
					t.Fatal("resume not resolved from evidence")
				}
			}
		})
	}
}
func TestTodo_EXTCOST_002_Performance(t *testing.T) {
	m := &extcostMemory{}
	g := extcostGateway(t, m)
	start := time.Now()
	for i := 0; i < 100; i++ {
		if _, err := g.Run(context.Background(), extcostCall(fmt.Sprint(i)), Units{Requests: 1}, extcostInvoke(1)); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("gateway mean with deterministic in-memory journal: %s (not PostgreSQL overhead)", time.Since(start)/100)
}
func TestTodo_EXTCOST_004(t *testing.T) {
	s := extcostSchedule()
	s.Rates = []Rate{{Requests, []Tier{{10, 1, 3}, {0, 2, 3}}}, {Characters, []Tier{{0, 1, 3}}}}
	s.MinimumMicros = 2
	c, err := NewCatalog([]Schedule{s})
	if err != nil {
		t.Fatal(err)
	}
	q, err := c.Quote(extcostCall("a"), Units{Requests: 11, Characters: 1}, extcostDate())
	if err != nil || q.CostMicros != 5 {
		t.Fatalf("rational tiers %+v %v", q, err)
	}
	s.Rates[0].Tiers[0].Micros = 100
	q, _ = c.Quote(extcostCall("a"), Units{Requests: 11, Characters: 1}, extcostDate())
	if q.CostMicros != 5 {
		t.Fatal("catalog not immutable")
	}
	if _, err := c.Quote(extcostCall("a"), Units{Seconds: 1}, extcostDate()); !errors.Is(err, ErrUnpriced) {
		t.Fatal("unpriced silently erased")
	}
	if _, err := c.Quote(extcostCall("a"), Units{Requests: 1}, s.EffectiveFrom.Add(-time.Second)); !errors.Is(err, ErrUnpriced) {
		t.Fatal("effective date ignored")
	}
	cost, err := Price(extcostSchedule(), Units{Requests: 1})
	if err != nil || cost != 10 {
		t.Fatal("price", cost, err)
	}
}
func TestTodo_EXTCOST_004_Property(t *testing.T) {
	s := extcostSchedule()
	s.Rates = []Rate{{InputTokens, []Tier{{0, 3, 10}}}, {OutputTokens, []Tier{{0, 7, 10}}}}
	for i := int64(0); i < 100; i++ {
		a, err := Price(s, Units{InputTokens: i, OutputTokens: 100 - i})
		if err != nil {
			t.Fatal(err)
		}
		s.Rates[0], s.Rates[1] = s.Rates[1], s.Rates[0]
		b, err := Price(s, Units{OutputTokens: 100 - i, InputTokens: i})
		if err != nil || a != b {
			t.Fatal("evaluation order changed cost")
		}
	}
	s.Rates = []Rate{{Requests, []Tier{{0, math.MaxInt64, 1}}}}
	if _, err := Price(s, Units{Requests: 2}); !errors.Is(err, ErrOverflow) {
		t.Fatal("overflow accepted")
	}
}
func TestTodo_EXTCOST_004_Golden(t *testing.T) {
	s := extcostSchedule()
	if ScheduleDigest(s) != "e62a0935124fc30f8008fc644ebe722c4d8e0b50df4d133135402e9ee73272ad" {
		t.Fatalf("fixture digest changed: %s", ScheduleDigest(s))
	}
	cost, err := Price(s, Units{Requests: 7})
	if err != nil || cost != 70 {
		t.Fatal("golden cost changed")
	}
	raw, _ := json.Marshal(Units{Requests: 7})
	if string(raw) != `{"requests":7}` {
		t.Fatalf("golden units %s", raw)
	}
}
func TestTodo_EXTCOST_005(t *testing.T) {
	m := &extcostMemory{fault: "reserve"}
	g := extcostGateway(t, m)
	called := false
	_, err := g.Run(context.Background(), extcostCall("a"), Units{Requests: 3}, func(context.Context) (Measurement, error) { called = true; return Measurement{}, nil })
	if !errors.Is(err, ErrBudget) || called {
		t.Fatal("hard stop dispatched")
	}
	m.fault = ""
	err = g.ChangeBudget(context.Background(), BudgetChange{Budget: Budget{Tenant: "tenant-a", Scope: Scope{"tenant", "tenant-a", "month"}, Currency: "USD", LimitMicros: 100, WarningBasisPoints: 8000}, Actor: "admin", Reason: "approved"})
	if err != nil || len(m.changes) != 1 || m.changes[0].At.IsZero() {
		t.Fatal("budget audit missing")
	}
	if len(Scopes(extcostCall("a"))) != 5 || PeriodStart(extcostDate(), "month").Day() != 1 || PeriodStart(extcostDate(), "run") != time.Unix(0, 0).UTC() {
		t.Fatal("nested scopes or period")
	}
}
func TestTodo_EXTCOST_005_Security(t *testing.T) {
	m := &extcostMemory{}
	g := extcostGateway(t, m)
	g.authority = extcostAuthority{true}
	if err := g.ChangeBudget(context.Background(), BudgetChange{Budget: Budget{Tenant: "tenant-a"}, Actor: "agent", Reason: "increase myself"}); !errors.Is(err, ErrDenied) || len(m.changes) != 0 {
		t.Fatal("agent raised budget")
	}
}
func TestTodo_EXTCOST_006_Security(t *testing.T) {
	g := extcostGateway(t, &extcostMemory{})
	g.authority = extcostAuthority{true}
	if _, err := g.Read(context.Background(), "tenant-a", "person", Filter{}); !errors.Is(err, ErrDenied) {
		t.Fatal("unrestricted read")
	}
}
func TestTodo_EXTCOST_004_ProviderDifference(t *testing.T) {
	m := &extcostMemory{}
	g := extcostGateway(t, m)
	reported := int64(9)
	l, err := g.Run(context.Background(), extcostCall("a"), Units{Requests: 1}, func(context.Context) (Measurement, error) {
		return Measurement{Units: Units{Requests: 1}, Billed: true, ProviderCostMicros: &reported, ProviderCurrency: "USD"}, nil
	})
	if err != nil || l.Finding != "provider_price_difference" || l.Measurement.ProviderCostMicros == nil {
		t.Fatal("provider discrepancy lost")
	}
	r := Reconciliation{Tenant: "tenant-a", Provider: "simulator", Operation: "request", Currency: "USD", ReportID: "day-1", Day: extcostDate(), LedgerMicros: 10, ProviderMicros: 9}
	if err := g.Reconcile(context.Background(), "admin", r, nil); err != nil || len(m.reports) != 1 || m.reports[0].DifferenceMicros != -1 {
		t.Fatal("reconcile metadata")
	}
}
func TestTodo_EXTCOST_003_Conformance(t *testing.T) {
	s := extcostSchedule()
	free := s
	free.Operation = "discover"
	free.Rates = nil
	free.NoChargeReason = "Included in the identity contract"
	c, err := NewCatalog([]Schedule{s, free})
	if err != nil {
		t.Fatal(err)
	}
	m := &extcostMemory{}
	g, err := NewGateway(m, c, extcostAuthority{}, []Operation{{"simulator", "request", "", ""}, {"simulator", "discover", "", ""}}, extcostDate)
	if err != nil {
		t.Fatal(err)
	}
	for op := range g.operations {
		call := extcostCall(op.Name)
		call.Operation = op.Name
		if _, err := g.Run(context.Background(), call, Units{Requests: 1}, extcostInvoke(1)); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.lines) != len(g.operations) {
		t.Fatal("operation missing ledger line")
	}
	for _, l := range m.lines {
		if !reflect.DeepEqual(l.Measurement.Units, Units{Requests: 1}) {
			t.Fatal("units changed")
		}
		if l.Call.Operation == "discover" && (l.CostMicros != 0 || l.NoChargeReason == "") {
			t.Fatal("no-charge not explicitly recorded")
		}
	}
}
func TestTodo_EXTCOST_003(t *testing.T) {
	raw, err := os.ReadFile("extcost_contract.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckGatewaySource(raw); err != nil {
		t.Fatal(err)
	}
	c, _ := NewCatalog([]Schedule{extcostSchedule()})
	if _, err := NewGateway(&extcostMemory{}, c, extcostAuthority{}, []Operation{{"new", "unpriced", "", ""}}, extcostDate); !errors.Is(err, ErrUnpriced) {
		t.Fatal("unpriced registration accepted")
	}
}
func TestTodo_EXTCOST_003_Mutation(t *testing.T) {
	raw, err := os.ReadFile("extcost_contract.go")
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(raw), "g.journal.Settle(settleCtx, r, line)", "nil", 1)
	if mutated == string(raw) || CheckGatewaySource([]byte(mutated)) == nil {
		t.Fatal("removing write escaped gate")
	}
	root := t.TempDir()
	for _, d := range []string{"internal/newcaller", "cmd", "tools"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0700); err != nil {
			t.Fatal(err)
		}
	}
	source := `package newcaller;import h "net/http";func call(){_=new(h.Client);h.Get("https://provider.invalid")}`
	if err := os.WriteFile(filepath.Join(root, "internal/newcaller/client.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	sites, err := ScanNetwork(root)
	if err != nil || len(sites) != 2 {
		t.Fatalf("aliases not detected %+v %v", sites, err)
	}
	if _, err := CheckInventory(root, Inventory{}); err == nil {
		t.Fatal("raw client escaped inventory gate")
	}
}
func TestTodo_EXTCOST_001(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "internal/platform/extcost/extcost_inventory.json")
	inventory, err := LoadInventory(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("EXTCOST_UPDATE_INVENTORY") == "1" {
		sites, err := ScanNetwork(root)
		if err != nil {
			t.Fatal(err)
		}
		inventory.Allowed = nil
		for _, site := range sites {
			reason := "Same-origin browser or RPC client"
			defect := false
			switch {
			case strings.Contains(site.File, "connectivity/egress/"):
				reason = "Central outbound enforcement transport"
			case strings.Contains(site.File, "data/pgtest/"):
				reason = "Test PostgreSQL archive support"
			case strings.Contains(site.File, "journeywasm/") || strings.Contains(site.File, "chatui/") || strings.Contains(site.File, "journeyclient/"):
			default:
				reason = "Existing outbound client: requires common-ledger composition and egress review"
				defect = true
			}
			inventory.Allowed = append(inventory.Allowed, AllowedSite{site, reason, defect})
		}
		raw, _ := json.MarshalIndent(inventory, "", "  ")
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := CheckInventory(root, inventory)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Operations) < 20 || len(findings) == 0 {
		t.Fatal("incomplete inventory")
	}
	for _, row := range inventory.Operations {
		if !row.CommonLedger {
			finding := row.Package + ":" + row.Operation + ": no external usage record"
			found := false
			for _, f := range findings {
				if f == finding {
					found = true
				}
			}
			if !found {
				t.Fatal("unrecorded operation lacks a finding", row.Provider, row.Operation)
			}
		}
	}
	t.Logf("%d operations; %d network constructions; %d unresolved findings", len(inventory.Operations), len(inventory.Allowed), len(findings))
}
func TestTodo_EXTCOST_001_Golden(t *testing.T) {
	i, err := LoadInventory("extcost_inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range i.Operations {
		k := r.Package + ":" + r.Provider + ":" + r.Operation
		if seen[k] {
			t.Fatal("duplicate operation", k)
		}
		seen[k] = true
	}
	for _, provider := range []string{"openai", "anthropic", "private", "s3", "oidc", "smtp", "giphy"} {
		found := false
		for _, r := range i.Operations {
			if r.Provider == provider {
				found = true
			}
		}
		if !found {
			t.Fatal("missing provider", provider)
		}
	}
}
func TestTodo_EXTCOST_001_Security(t *testing.T) {
	i, err := LoadInventory("extcost_inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range i.Allowed {
		if strings.Contains(a.Site.File, "application/documentembed/") && a.Defect {
			found = true
		}
	}
	if !found {
		t.Fatal("embedding gateway bypass not declared as defect")
	}
}

type extcostWarnings struct {
	delivered int
	failure   error
}

func (w *extcostWarnings) NotifyExternalBudgetWarning(_ context.Context, c Call, b BudgetStatus) error {
	if !b.Warning || b.Budget.Tenant != c.Tenant {
		return ErrInvalid
	}
	w.delivered++
	return w.failure
}
func TestTodo_EXTCOST_005_Warnings(t *testing.T) {
	catalog, err := NewCatalog([]Schedule{extcostSchedule()})
	if err != nil {
		t.Fatal(err)
	}
	j := &extcostMemory{warnings: []BudgetStatus{{Budget: Budget{Tenant: "tenant-a", Scope: Scope{"feature", "answers", "month"}, Currency: "USD", LimitMicros: 100, WarningBasisPoints: 8000}, ReservedMicros: 80, Warning: true}}}
	sink := &extcostWarnings{}
	g, err := NewGatewayWithWarnings(j, catalog, extcostAuthority{}, []Operation{{"simulator", "request", "", ""}}, extcostDate, sink)
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Run(context.Background(), extcostCall("warn"), Units{Requests: 8}, func(ctx context.Context) (Measurement, error) {
		if sink.delivered != 1 {
			t.Fatal("owner warning followed provider dispatch")
		}
		return extcostInvoke(8)(ctx)
	})
	if err != nil || sink.delivered != 1 {
		t.Fatal("owner warning", err)
	}
	sink.failure = errors.New("owner inbox unavailable")
	called := false
	_, err = g.Run(context.Background(), extcostCall("warn-failure"), Units{Requests: 8}, func(context.Context) (Measurement, error) { called = true; return Measurement{}, nil })
	if err == nil || called {
		t.Fatal("warning loss permitted dispatch")
	}
	if _, ok := j.pending["tenant-a:warn-failure"]; ok {
		t.Fatal("warning failure leaked reservation")
	}
	if _, err := NewGatewayWithWarnings(j, catalog, extcostAuthority{}, []Operation{{"simulator", "request", "", ""}}, extcostDate, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing owner sink accepted")
	}
	g, err = NewGateway(j, catalog, extcostAuthority{}, []Operation{{"simulator", "request", "", ""}}, extcostDate)
	if err != nil {
		t.Fatal(err)
	}
	called = false
	_, err = g.Run(context.Background(), extcostCall("missing-inbox"), Units{Requests: 8}, func(context.Context) (Measurement, error) { called = true; return Measurement{}, nil })
	if !errors.Is(err, ErrWarning) || called {
		t.Fatal("missing inbox silently lost the warning", err)
	}
	if _, ok := j.pending["tenant-a:missing-inbox"]; ok {
		t.Fatal("missing inbox leaked reservation")
	}

}

type extcostCancelJournal struct {
	*extcostMemory
	cancel context.CancelFunc
}

func (j extcostCancelJournal) Reserve(ctx context.Context, r Reservation) (Reservation, error) {
	r, err := j.extcostMemory.Reserve(ctx, r)
	j.cancel()
	return r, err
}
func TestTodo_EXTCOST_005_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	j := &extcostMemory{}
	catalog, _ := NewCatalog([]Schedule{extcostSchedule()})
	g, err := NewGateway(extcostCancelJournal{j, cancel}, catalog, extcostAuthority{}, []Operation{{"simulator", "request", "", ""}}, extcostDate)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = g.Run(ctx, extcostCall("cancel"), Units{Requests: 1}, func(context.Context) (Measurement, error) { called = true; return Measurement{}, nil })
	if !errors.Is(err, context.Canceled) || called || len(j.pending) != 0 {
		t.Fatal("cancellation leaked or dispatched")
	}
	g = extcostGateway(t, j)
	if err := g.ReleaseUnsent(context.Background(), "admin", Reservation{Call: extcostCall("a")}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_EXTCOST_004_Versions(t *testing.T) {
	old := extcostSchedule()
	next := old
	next.Version = "fixture-v2"
	next.EffectiveFrom = extcostDate().Add(time.Hour)
	next.ReviewAt = next.EffectiveFrom.Add(time.Hour)
	next.Rates = []Rate{{Requests, []Tier{{0, 20, 1}}}}
	c, err := NewCatalog([]Schedule{old, next})
	if err != nil {
		t.Fatal("append effective-dated price", err)
	}
	before, err := c.Quote(extcostCall("a"), Units{Requests: 1}, extcostDate())
	if err != nil || before.CostMicros != 10 || before.Schedule.Version != "fixture-v1" {
		t.Fatal("past price changed", err)
	}
	after, err := c.Quote(extcostCall("a"), Units{Requests: 1}, next.EffectiveFrom)
	if err != nil || after.CostMicros != 20 || after.Schedule.Version != "fixture-v2" {
		t.Fatal("new price not selected", err)
	}
	j := &extcostMemory{}
	g := extcostGateway(t, j)
	_, err = g.Run(context.Background(), extcostCall("lost"), Units{Requests: 2}, func(context.Context) (Measurement, error) { return Measurement{}, errors.New("reply lost") })
	if !errors.Is(err, ErrPending) {
		t.Fatal(err)
	}
	g.catalog = c
	g.now = func() time.Time { return next.EffectiveFrom }
	l, err := g.Recover(context.Background(), "admin", j.pending["tenant-a:lost"], Measurement{Units: Units{Requests: 1}, Billed: true, Outcome: "succeeded"})
	if err != nil || l.CostMicros != 10 || l.ScheduleVersion != "fixture-v1" {
		t.Fatal("resume repriced past call", err)
	}
}

func TestTodo_EXTCOST_002_PriceReplay(t *testing.T) {
	call := extcostCall("stable")
	a := Quote{CostMicros: 10, Schedule: Schedule{Version: "old", Currency: "USD"}, Digest: "old-digest"}
	b := Quote{CostMicros: 20, Schedule: Schedule{Version: "new", Currency: "USD"}, Digest: "new-digest"}
	if Fingerprint(call, a) != Fingerprint(call, b) {
		t.Fatal("new price invalidated request replay")
	}
	changed := call
	changed.RequestDigest = strings.Repeat("b", 64)
	if Fingerprint(call, a) == Fingerprint(changed, a) {
		t.Fatal("changed request reused replay identity")
	}
	g := extcostGateway(t, &extcostMemory{})
	called := false
	_, err := g.Run(context.Background(), call, Units{Requests: 0}, func(context.Context) (Measurement, error) { called = true; return Measurement{}, nil })
	if !errors.Is(err, ErrUnpriced) || called {
		t.Fatal("zero reservation dispatched a priced call", err)
	}

}

type extcostScopeAuthority struct{ seen Filter }

func (a *extcostScopeAuthority) AuthorizeExternalUsage(_ context.Context, _, _, action string, f Filter) error {
	if action != "budget" {
		return ErrDenied
	}
	a.seen = f
	return nil
}
func TestTodo_EXTCOST_005_ScopeAuthorization(t *testing.T) {
	g := extcostGateway(t, &extcostMemory{})
	authority := &extcostScopeAuthority{}
	g.authority = authority
	for _, kind := range []string{"tenant", "feature", "agent", "workflow", "run", "node", "step"} {
		scope := Scope{Kind: kind, ID: "scope-id", Period: "month"}
		err := g.ChangeBudget(context.Background(), BudgetChange{Budget: Budget{Tenant: "tenant-a", Scope: scope, Currency: "USD", LimitMicros: 100, WarningBasisPoints: 8000}, Actor: "admin", Reason: "authorized review"})
		if err != nil || authority.seen.BudgetScope != scope {
			t.Fatal("budget authority was given the wrong scope", kind, err)
		}
		if kind == "feature" && authority.seen.Feature != scope.ID || kind == "agent" && authority.seen.Agent != scope.ID || kind == "workflow" && authority.seen.Workflow != scope.ID {
			t.Fatal("wrong administrative dimension", kind)
		}
		if kind != "feature" && authority.seen.Feature != "" {
			t.Fatal("budget scope was misrepresented as a feature", kind)
		}
	}
}
