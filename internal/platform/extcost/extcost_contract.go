// Package extcost owns the external-call accounting port. Journal entries
// contain identifiers, units and digests, never prompts or provider content.
package extcost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strings"
	"time"
	"unicode"
)

var (
	ErrWarning       = errors.New("external usage: budget warning could not be delivered; ask an administrator to restore owner notifications")
	ErrInvalid       = errors.New("external usage: invalid accounting metadata")
	ErrUnpriced      = errors.New("external usage: no approved price is available; ask an administrator to configure pricing")
	ErrBudget        = errors.New("external usage: the spending limit would be exceeded; ask an administrator to raise the budget")
	ErrBudgetMissing = errors.New("external usage: spending limits are missing; ask an administrator to configure budgets")
	ErrPending       = errors.New("external usage: this attempt may have reached the provider; reconcile it before retrying")
	ErrReplay        = errors.New("external usage: this attempt is already recorded; use its stored result")
	ErrConflict      = errors.New("external usage: idempotency key belongs to a different call")
	ErrDenied        = errors.New("external usage: administrator permission is required")
	ErrOverflow      = errors.New("external usage: cost is not representable")
	ErrOverrun       = errors.New("external usage: provider exceeded the reserved maximum; further calls are stopped")
)

type Unit string

const (
	InputTokens      Unit = "input_tokens"
	CachedTokens     Unit = "cached_input_tokens"
	OutputTokens     Unit = "output_tokens"
	Seconds          Unit = "seconds"
	Characters       Unit = "characters"
	Requests         Unit = "requests"
	Messages         Unit = "messages"
	ByteMonths       Unit = "byte_months"
	BytesTransferred Unit = "bytes_transferred"
	MinutesRelayed   Unit = "minutes_relayed"
)

type Units map[Unit]int64
type Attribution struct {
	Actor              string `json:"actor"`
	WorkflowDefinition string `json:"workflow_definition,omitempty"`
	WorkflowRun        string `json:"workflow_run,omitempty"`
	Node               string `json:"node,omitempty"`
	Agent              string `json:"agent,omitempty"`
	AgentRun           string `json:"agent_run,omitempty"`
	Step               string `json:"step,omitempty"`
	Conversation       string `json:"conversation,omitempty"`
	Document           string `json:"document,omitempty"`
	IndexJob           string `json:"index_job,omitempty"`
	ScheduledJob       string `json:"scheduled_job,omitempty"`
}

// A genuine provider retry uses a new Key under the same Cause. Replaying
// one attempt reuses its Key and never dispatches a second provider call.
type Call struct {
	Tenant        string      `json:"tenant"`
	LegalEntity   string      `json:"legal_entity"`
	Feature       string      `json:"feature"`
	Provider      string      `json:"provider"`
	Operation     string      `json:"operation"`
	Model         string      `json:"model,omitempty"`
	ModelVersion  string      `json:"model_version,omitempty"`
	Purpose       string      `json:"purpose"`
	DataClasses   []string    `json:"data_classes"`
	Key           string      `json:"key"`
	Cause         string      `json:"cause"`
	RequestDigest string      `json:"request_digest"`
	Attribution   Attribution `json:"attribution"`
}
type Measurement struct {
	Units              Units  `json:"units"`
	Estimated          bool   `json:"estimated"`
	Outcome            string `json:"outcome"`
	Billed             bool   `json:"billed"`
	ResultDigest       string `json:"result_digest,omitempty"`
	ResultRef          string `json:"result_ref,omitempty"`
	ProviderCostMicros *int64 `json:"provider_cost_micros,omitempty"`
	ProviderCurrency   string `json:"provider_currency,omitempty"`
}
type Line struct {
	Call            Call        `json:"call"`
	Measurement     Measurement `json:"measurement"`
	ScheduleVersion string      `json:"schedule_version"`
	ScheduleDigest  string      `json:"schedule_digest"`
	Currency        string      `json:"currency"`
	CostMicros      int64       `json:"cost_micros"`
	NoChargeReason  string      `json:"no_charge_reason,omitempty"`
	At              time.Time   `json:"at"`
	BudgetAt        time.Time   `json:"budget_at"`
	Finding         string      `json:"finding,omitempty"`
	CorrectionOf    string      `json:"correction_of,omitempty"`
}
type Scope struct{ Kind, ID, Period string }
type Budget struct {
	Tenant                          string
	Scope                           Scope
	Currency                        string
	LimitMicros, WarningBasisPoints int64
}
type BudgetChange struct {
	Budget        Budget
	Actor, Reason string
	At            time.Time
}
type BudgetStatus struct {
	Budget                      Budget
	Start                       time.Time
	SpentMicros, ReservedMicros int64
	Warning                     bool
}
type Reservation struct {
	State                                     string
	Call                                      Call
	Fingerprint                               string
	MaximumMicros                             int64
	Currency, ScheduleVersion, ScheduleDigest string
	At                                        time.Time
	Budgets                                   []BudgetStatus
	Replay                                    *Line
}
type Filter struct {
	BudgetScope                                                                        Scope
	From, Until                                                                        time.Time
	Provider, Operation, Feature, Purpose, Agent, Workflow, Person, Cause, Key, Search string
	Limit                                                                              int
}
type Group struct {
	Kind, Name, Currency         string
	CostMicros, Calls, Estimated int64
}
type Statistic struct {
	Workflow, Node, Currency      string
	Runs, MedianMicros, P95Micros int64
}
type Report struct {
	ReconciledAt    map[string]time.Time
	Pending         []Reservation
	Lines           []Line
	Groups          []Group
	Statistics      []Statistic
	Budgets         []BudgetStatus
	Reconciliations []Reconciliation
	Truncated       bool
}
type Reconciliation struct {
	Tenant, Provider, Operation, Currency, ReportID, ScheduleVersion string
	Day, At                                                          time.Time
	LedgerMicros, ProviderMicros, DifferenceMicros                   int64
	CorrectionKey                                                    string
}
type Journal interface {
	Reserve(context.Context, Reservation) (Reservation, error)
	MarkSent(context.Context, Reservation) error
	Settle(context.Context, Reservation, Line) error
	Release(context.Context, Reservation) error
	ChangeBudget(context.Context, BudgetChange) error
	Read(context.Context, string, Filter) (Report, error)
	Reconcile(context.Context, Reconciliation, *Line) error
}
type Authorizer interface {
	AuthorizeExternalUsage(context.Context, string, string, string, Filter) error
}
type Tier struct{ UpTo, Micros, Per int64 }
type Rate struct {
	Unit  Unit
	Tiers []Tier
}
type Schedule struct {
	Version, Provider, Operation, Model, ModelVersion, Currency string
	Authority, Source, NoChargeReason                           string
	EffectiveFrom, EffectiveUntil, ReviewAt                     time.Time
	Rates                                                       []Rate
	MinimumMicros, ToleranceMicros                              int64
}
type Quote struct {
	Schedule      Schedule
	Digest        string
	CostMicros    int64
	ReviewOverdue bool
}
type Catalog struct{ schedules []Schedule }
type Operation struct{ Provider, Name, Model, ModelVersion string }
type WarningSink interface {
	NotifyExternalBudgetWarning(context.Context, Call, BudgetStatus) error
}

type Gateway struct {
	warnings   WarningSink
	journal    Journal
	catalog    *Catalog
	authority  Authorizer
	now        func() time.Time
	operations map[Operation]bool
}
type Invoke func(context.Context) (Measurement, error)

func ValidUnits(units Units) bool {
	for u, n := range units {
		if n < 0 {
			return false
		}
		switch u {
		case InputTokens, CachedTokens, OutputTokens, Seconds, Characters, Requests, Messages, ByteMonths, BytesTransferred, MinutesRelayed:
		default:
			return false
		}
	}
	return true
}
func identifier(s string) bool {
	return s != "" && strings.TrimSpace(s) == s && len(s) <= 256 && !strings.ContainsFunc(s, unicode.IsControl)
}
func ValidCall(c Call) bool {
	for _, s := range []string{c.Tenant, c.LegalEntity, c.Feature, c.Provider, c.Operation, c.Purpose, c.Key, c.Cause, c.Attribution.Actor} {
		if !identifier(s) {
			return false
		}
	}
	if len(c.RequestDigest) != 64 {
		return false
	}
	if _, err := hex.DecodeString(c.RequestDigest); err != nil {
		return false
	}
	if len(c.DataClasses) == 0 {
		return false
	}
	for _, s := range c.DataClasses {
		if !identifier(s) {
			return false
		}
	}
	for _, s := range []string{c.Model, c.ModelVersion, c.Attribution.Agent, c.Attribution.AgentRun, c.Attribution.Step, c.Attribution.WorkflowDefinition, c.Attribution.WorkflowRun, c.Attribution.Node, c.Attribution.Conversation, c.Attribution.Document, c.Attribution.IndexJob, c.Attribution.ScheduledJob} {
		if s != "" && !identifier(s) {
			return false
		}
	}
	if c.Attribution.Step != "" && c.Attribution.AgentRun == "" || c.Attribution.Node != "" && c.Attribution.WorkflowRun == "" {
		return false
	}
	return true
}
func PeriodStart(at time.Time, period string) time.Time {
	at = at.UTC()
	switch period {
	case "month":
		return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	case "day":
		return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	default:
		return time.Unix(0, 0).UTC()
	}
}
func Scopes(c Call) []Scope {
	out := []Scope{{"tenant", c.Tenant, "month"}, {"feature", c.Feature, "month"}}
	a := c.Attribution
	for _, s := range []Scope{{"agent", a.Agent, "month"}, {"workflow", a.WorkflowDefinition, "month"}, {"run", a.AgentRun, "run"}, {"run", a.WorkflowRun, "run"}} {
		if s.ID != "" {
			out = append(out, s)
		}
	}
	if a.AgentRun != "" && a.Step != "" {
		out = append(out, Scope{"step", a.AgentRun + ":" + a.Step, "run"})
	}
	if a.WorkflowRun != "" && a.Node != "" {
		out = append(out, Scope{"node", a.WorkflowRun + ":" + a.Node, "run"})
	}
	return out
}
func scheduleKey(s Schedule) string {
	return s.Provider + "\x00" + s.Operation + "\x00" + s.Model + "\x00" + s.ModelVersion
}
func NewCatalog(input []Schedule) (*Catalog, error) {
	if len(input) == 0 {
		return nil, ErrUnpriced
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, ErrInvalid
	}
	var entries []Schedule
	if json.Unmarshal(raw, &entries) != nil {
		return nil, ErrInvalid
	}
	versions := map[string]bool{}
	for i, s := range entries {
		key := scheduleKey(s)
		if !identifier(s.Version) || !identifier(s.Provider) || !identifier(s.Operation) || len(s.Currency) != 3 || s.Currency != strings.ToUpper(s.Currency) || s.Authority == "" || s.Source == "" || s.EffectiveFrom.IsZero() || s.ReviewAt.Before(s.EffectiveFrom) || (!s.EffectiveUntil.IsZero() && !s.EffectiveUntil.After(s.EffectiveFrom)) || s.MinimumMicros < 0 || s.ToleranceMicros < 0 || versions[key+"\x00"+s.Version] {
			return nil, ErrInvalid
		}
		versions[key+"\x00"+s.Version] = true
		if (s.NoChargeReason == "") == (len(s.Rates) == 0) || (s.NoChargeReason != "" && s.MinimumMicros != 0) {
			return nil, ErrInvalid
		}
		seen := map[Unit]bool{}
		for _, r := range s.Rates {
			if !ValidUnits(Units{r.Unit: 0}) || seen[r.Unit] || len(r.Tiers) == 0 {
				return nil, ErrInvalid
			}
			seen[r.Unit] = true
			last := int64(0)
			for j, t := range r.Tiers {
				if t.Micros <= 0 || t.Per <= 0 || t.UpTo < 0 || (t.UpTo != 0 && t.UpTo <= last) || (t.UpTo == 0 && j != len(r.Tiers)-1) {
					return nil, ErrInvalid
				}
				last = t.UpTo
			}
			if last != 0 {
				return nil, ErrInvalid
			}
		}
		for j := 0; j < i; j++ {
			p := entries[j]
			if scheduleKey(p) == key && s.EffectiveFrom.Equal(p.EffectiveFrom) {
				return nil, ErrInvalid
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].EffectiveFrom.After(entries[j].EffectiveFrom) })
	return &Catalog{entries}, nil
}
func ScheduleDigest(s Schedule) string {
	rates := append([]Rate(nil), s.Rates...)
	sort.Slice(rates, func(i, j int) bool { return rates[i].Unit < rates[j].Unit })
	s.Rates = rates
	raw, _ := json.Marshal(s)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (c *Catalog) Quote(call Call, units Units, at time.Time) (Quote, error) {
	if c == nil || !ValidUnits(units) || at.IsZero() {
		return Quote{}, ErrInvalid
	}
	for _, s := range c.schedules {
		if s.Provider == call.Provider && s.Operation == call.Operation && s.Model == call.Model && s.ModelVersion == call.ModelVersion && !at.Before(s.EffectiveFrom) && (s.EffectiveUntil.IsZero() || at.Before(s.EffectiveUntil)) {
			cost, err := Price(s, units)
			if err != nil {
				return Quote{}, err
			}
			return Quote{s, ScheduleDigest(s), cost, at.After(s.ReviewAt)}, nil
		}
	}
	return Quote{}, ErrUnpriced
}

// Price rounds the sum of rational prices up once, including tiers/minimums.
func Price(s Schedule, units Units) (int64, error) {
	if !ValidUnits(units) {
		return 0, ErrInvalid
	}
	if s.NoChargeReason != "" {
		return 0, nil
	}
	if len(s.Rates) == 0 {
		return 0, ErrUnpriced
	}
	rates := map[Unit]Rate{}
	for _, r := range s.Rates {
		rates[r.Unit] = r
	}
	total := new(big.Rat)
	for unit, n := range units {
		r, ok := rates[unit]
		if !ok && n != 0 {
			return 0, ErrUnpriced
		}
		previous := int64(0)
		for _, tier := range r.Tiers {
			if tier.Per <= 0 || tier.Micros <= 0 {
				return 0, ErrInvalid
			}
			count := n - previous
			if count <= 0 {
				break
			}
			if tier.UpTo != 0 && count > tier.UpTo-previous {
				count = tier.UpTo - previous
			}
			if count < 0 {
				return 0, ErrInvalid
			}
			value := new(big.Int).Mul(big.NewInt(count), big.NewInt(tier.Micros))
			total.Add(total, new(big.Rat).SetFrac(value, big.NewInt(tier.Per)))
			if tier.UpTo == 0 {
				break
			}
			previous = tier.UpTo
		}
	}
	result, remainder := new(big.Int), new(big.Int)
	result.QuoRem(total.Num(), total.Denom(), remainder)
	if remainder.Sign() != 0 {
		result.Add(result, big.NewInt(1))
	}
	if !result.IsInt64() {
		return 0, ErrOverflow
	}
	cost := result.Int64()
	if cost < s.MinimumMicros {
		cost = s.MinimumMicros
	}
	return cost, nil
}

// Fingerprint binds a replay to the same request and attribution. Prices belong
// to the persisted reservation; a later schedule cannot invalidate its replay.
func Fingerprint(c Call, _ Quote) string {
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func NewGateway(j Journal, c *Catalog, a Authorizer, operations []Operation, now func() time.Time) (*Gateway, error) {
	if j == nil || c == nil || a == nil || now == nil || len(operations) == 0 {
		return nil, ErrInvalid
	}
	registered := map[Operation]bool{}
	for _, op := range operations {
		if op.Provider == "" || op.Name == "" || registered[op] {
			return nil, ErrInvalid
		}
		found := false
		for _, s := range c.schedules {
			if s.Provider == op.Provider && s.Operation == op.Name && s.Model == op.Model && s.ModelVersion == op.ModelVersion {
				found = true
			}
		}
		if !found {
			return nil, ErrUnpriced
		}
		registered[op] = true
	}
	return &Gateway{journal: j, catalog: c, authority: a, now: now, operations: registered}, nil
}
func (g *Gateway) Run(ctx context.Context, c Call, maximum Units, invoke Invoke) (Line, error) {
	if g == nil || ctx == nil || invoke == nil || !ValidCall(c) || !ValidUnits(maximum) {
		return Line{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Line{}, err
	}
	if err := g.authority.AuthorizeExternalUsage(ctx, c.Tenant, c.Attribution.Actor, "call", Filter{Feature: c.Feature, Agent: c.Attribution.Agent, Workflow: c.Attribution.WorkflowDefinition, Person: c.Attribution.Actor}); err != nil {
		return Line{}, errors.Join(ErrDenied, err)
	}
	if !g.operations[Operation{c.Provider, c.Operation, c.Model, c.ModelVersion}] {
		return Line{}, ErrUnpriced
	}
	at := g.now().UTC()
	q, err := g.catalog.Quote(c, maximum, at)
	if err != nil {
		return Line{}, err
	}
	if q.CostMicros == 0 && q.Schedule.NoChargeReason == "" {
		return Line{}, ErrUnpriced
	}
	r, err := g.journal.Reserve(ctx, Reservation{Call: c, Fingerprint: Fingerprint(c, q), MaximumMicros: q.CostMicros, Currency: q.Schedule.Currency, ScheduleVersion: q.Schedule.Version, ScheduleDigest: q.Digest, At: at})
	if err != nil {
		return Line{}, err
	}
	if r.Replay != nil {
		return *r.Replay, ErrReplay
	}
	if g.warnings == nil {
		for _, b := range r.Budgets {
			if b.Warning {
				return Line{}, errors.Join(ErrWarning, g.journal.Release(context.WithoutCancel(ctx), r))
			}
		}
	}
	if g.warnings != nil {
		for _, b := range r.Budgets {
			if b.Warning {
				if err := g.warnings.NotifyExternalBudgetWarning(ctx, c, b); err != nil {
					return Line{}, errors.Join(err, g.journal.Release(context.WithoutCancel(ctx), r))
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Line{}, errors.Join(err, g.journal.Release(context.WithoutCancel(ctx), r))
	}
	if err := g.journal.MarkSent(ctx, r); err != nil {
		return Line{}, err
	}
	m, callErr := invoke(ctx)
	if m.Outcome == "" {
		if callErr == nil {
			m.Outcome = "succeeded"
		} else {
			m.Outcome = "failed"
		}
	}
	if len(m.Units) == 0 && q.Schedule.NoChargeReason == "" {
		return Line{}, errors.Join(ErrPending, callErr)
	}
	line, err := g.makeLine(r, q, m)
	if err != nil {
		return Line{}, errors.Join(err, callErr)
	}
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	err = g.journal.Settle(settleCtx, r, line)
	return line, errors.Join(callErr, err)
}
func (g *Gateway) makeLine(r Reservation, q Quote, m Measurement) (Line, error) {
	if !ValidUnits(m.Units) || (m.Outcome != "succeeded" && m.Outcome != "failed" && m.Outcome != "refused" && m.Outcome != "unknown") || (m.ProviderCostMicros != nil && (*m.ProviderCostMicros < 0 || m.ProviderCurrency == "")) {
		return Line{}, ErrInvalid
	}
	if m.ResultRef != "" && !identifier(m.ResultRef) {
		return Line{}, ErrInvalid
	}
	if m.ResultDigest != "" {
		if len(m.ResultDigest) != 64 {
			return Line{}, ErrInvalid
		}
		if _, err := hex.DecodeString(m.ResultDigest); err != nil {
			return Line{}, ErrInvalid
		}
	}
	cost, err := Price(q.Schedule, m.Units)
	if err != nil {
		return Line{}, err
	}
	if !m.Billed {
		cost = 0
	}
	line := Line{Call: r.Call, Measurement: m, ScheduleVersion: q.Schedule.Version, ScheduleDigest: q.Digest, Currency: q.Schedule.Currency, CostMicros: cost, NoChargeReason: q.Schedule.NoChargeReason, At: g.now().UTC(), BudgetAt: r.At}
	if q.ReviewOverdue {
		line.Finding = "schedule_review_overdue"
	}
	if m.ProviderCostMicros != nil {
		d := new(big.Int).Sub(big.NewInt(*m.ProviderCostMicros), big.NewInt(cost))
		d.Abs(d)
		if m.ProviderCurrency != line.Currency || d.Cmp(big.NewInt(q.Schedule.ToleranceMicros)) > 0 {
			line.Finding = "provider_price_difference"
		}
	}
	if cost > r.MaximumMicros {
		line.Finding = "reserved_maximum_exceeded"
	}
	return line, nil
}
func (g *Gateway) Recover(ctx context.Context, actor string, r Reservation, m Measurement) (Line, error) {
	if g == nil || ctx == nil || !ValidCall(r.Call) {
		return Line{}, ErrInvalid
	}
	if err := g.authority.AuthorizeExternalUsage(ctx, r.Call.Tenant, actor, "reconcile", Filter{Key: r.Call.Key}); err != nil {
		return Line{}, errors.Join(ErrDenied, err)
	}
	var q Quote
	found := false
	for _, s := range g.catalog.schedules {
		if s.Provider == r.Call.Provider && s.Operation == r.Call.Operation && s.Model == r.Call.Model && s.ModelVersion == r.Call.ModelVersion && s.Version == r.ScheduleVersion && ScheduleDigest(s) == r.ScheduleDigest {
			cost, err := Price(s, m.Units)
			if err != nil {
				return Line{}, err
			}
			q = Quote{s, ScheduleDigest(s), cost, g.now().After(s.ReviewAt)}
			found = true
			break
		}
	}
	if !found {
		return Line{}, ErrUnpriced
	}
	if q.Schedule.Version != r.ScheduleVersion || q.Digest != r.ScheduleDigest || q.Schedule.Currency != r.Currency {
		return Line{}, ErrConflict
	}
	line, err := g.makeLine(r, q, m)
	if err != nil {
		return Line{}, err
	}
	return line, g.journal.Settle(ctx, r, line)
}
func (g *Gateway) ChangeBudget(ctx context.Context, c BudgetChange) error {
	if g == nil || ctx == nil || !identifier(c.Actor) || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 1000 {
		return ErrInvalid
	}
	f := Filter{BudgetScope: c.Budget.Scope}
	switch c.Budget.Scope.Kind {
	case "feature":
		f.Feature = c.Budget.Scope.ID
	case "agent":
		f.Agent = c.Budget.Scope.ID
	case "workflow":
		f.Workflow = c.Budget.Scope.ID
	}
	if err := g.authority.AuthorizeExternalUsage(ctx, c.Budget.Tenant, c.Actor, "budget", f); err != nil {
		return errors.Join(ErrDenied, err)
	}
	c.At = g.now().UTC()
	return g.journal.ChangeBudget(ctx, c)
}
func (g *Gateway) Read(ctx context.Context, tenant, actor string, f Filter) (Report, error) {
	if g == nil || ctx == nil || !identifier(tenant) || !identifier(actor) {
		return Report{}, ErrInvalid
	}
	if err := g.authority.AuthorizeExternalUsage(ctx, tenant, actor, "read", f); err != nil {
		return Report{}, errors.Join(ErrDenied, err)
	}
	return g.journal.Read(ctx, tenant, f)
}
func (g *Gateway) Reconcile(ctx context.Context, actor string, r Reconciliation, correction *Line) error {
	if g == nil || ctx == nil || r.Tenant == "" || r.ReportID == "" || r.Provider == "" || r.Operation == "" || len(r.Currency) != 3 || r.Day.IsZero() || r.ProviderMicros < 0 || r.LedgerMicros < 0 {
		return ErrInvalid
	}
	if err := g.authority.AuthorizeExternalUsage(ctx, r.Tenant, actor, "reconcile", Filter{Provider: r.Provider, Operation: r.Operation}); err != nil {
		return errors.Join(ErrDenied, err)
	}
	r.At = g.now().UTC()
	r.Day = PeriodStart(r.Day, "day")
	r.DifferenceMicros = r.ProviderMicros - r.LedgerMicros
	if correction != nil && (correction.Call.Tenant != r.Tenant || correction.Call.Provider != r.Provider || correction.Call.Operation != r.Operation || correction.Currency != r.Currency || correction.CorrectionOf == "" || correction.Call.Key != r.CorrectionKey) {
		return ErrInvalid
	}
	return g.journal.Reconcile(ctx, r, correction)
}

// NewGatewayWithWarnings requires an idempotent, durable in-product inbox sink.
// Warning delivery precedes dispatch and carries no request content.
func NewGatewayWithWarnings(j Journal, c *Catalog, a Authorizer, ops []Operation, now func() time.Time, w WarningSink) (*Gateway, error) {
	if w == nil {
		return nil, ErrInvalid
	}
	g, err := NewGateway(j, c, a, ops, now)
	if err != nil {
		return nil, err
	}
	g.warnings = w
	return g, nil
}

// ReleaseUnsent resolves a crash before dispatch. The store refuses sent calls.
func (g *Gateway) ReleaseUnsent(ctx context.Context, actor string, r Reservation) error {
	if g == nil || ctx == nil {
		return ErrInvalid
	}
	if err := g.authority.AuthorizeExternalUsage(ctx, r.Call.Tenant, actor, "reconcile", Filter{Key: r.Call.Key}); err != nil {
		return errors.Join(ErrDenied, err)
	}
	return g.journal.Release(ctx, r)
}
