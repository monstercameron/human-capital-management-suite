package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/extcost"
	"net/http"
	"sort"
	"time"
)

var ErrExtcostRecordedFailure = errors.New("external usage: the recorded model attempt failed")

type extcostCallContextKey struct{}

// ExtcostWithCall binds metadata after the caller's tenant authorization.
func ExtcostWithCall(ctx context.Context, c extcost.Call) context.Context {
	return context.WithValue(ctx, extcostCallContextKey{}, c)
}
func ExtcostModelCall(ctx context.Context, r agentmodel.ModelRequest) (extcost.Call, error) {
	if ctx == nil {
		return extcost.Call{}, extcost.ErrInvalid
	}
	c, ok := ctx.Value(extcostCallContextKey{}).(extcost.Call)
	if !ok || !extcost.ValidCall(c) || c.Key != r.TraceID {
		return extcost.Call{}, extcost.ErrInvalid
	}
	return c, nil
}

// ExtcostGatewayCall uses trusted dispatch metadata; additional conversation,
// document and agent identity can be filled by the owning caller before binding.
func ExtcostGatewayCall(r AgentModelGatewayRequest, identity agentmodel.ModelIdentity, legalEntity string) (extcost.Call, error) {
	outbound := r.Dispatch.Outbound
	classes := map[string]bool{}
	for _, f := range outbound.Fields {
		classes[string(f.Class)] = true
	}
	dataClasses := make([]string, 0, len(classes))
	for c := range classes {
		dataClasses = append(dataClasses, c)
	}
	sort.Strings(dataClasses)
	raw, err := json.Marshal(r.Dispatch.Model)
	if err != nil {
		return extcost.Call{}, err
	}
	digest := sha256.Sum256(raw)
	c := extcost.Call{Tenant: r.TenantID, LegalEntity: legalEntity, Feature: outbound.Purpose, Provider: identity.ProviderID, Operation: "model.invoke", Model: identity.ModelID, ModelVersion: identity.Version, Purpose: outbound.Purpose, DataClasses: dataClasses, Key: r.Dispatch.Model.TraceID, Cause: outbound.TaskID, RequestDigest: hex.EncodeToString(digest[:]), Attribution: extcost.Attribution{Actor: outbound.Principal, AgentRun: outbound.TaskID, Step: r.Dispatch.Model.TraceID}}
	if c.Tenant != outbound.Tenant || !extcost.ValidCall(c) {
		return extcost.Call{}, extcost.ErrInvalid
	}
	return c, nil
}

// ExtcostModelResults journals normalized results outside the content-free cost
// ledger. Save must return a durable reference and digest; replay reads it.
type ExtcostModelResults interface {
	SaveExternalModelResult(context.Context, extcost.Call, agentmodel.ModelResult) (string, string, error)
	LoadExternalModelResult(context.Context, extcost.Call, string) (agentmodel.ModelResult, error)
}
type ExtcostModelBinding func(context.Context, agentmodel.ModelRequest) (extcost.Call, error)
type ExtcostModelAdapter struct {
	Port    *extcost.Gateway
	Inner   agentmodel.ModelAdapter
	Model   agentmodel.ModelIdentity
	Bind    ExtcostModelBinding
	Results ExtcostModelResults
}

func (a *ExtcostModelAdapter) Identity() agentmodel.ModelIdentity {
	if a == nil {
		return agentmodel.ModelIdentity{}
	}
	return a.Model
}
func (a *ExtcostModelAdapter) Capabilities() agentmodel.AdapterCapabilities {
	if a == nil || a.Inner == nil {
		return agentmodel.AdapterCapabilities{}
	}
	return a.Inner.Capabilities()
}
func (a *ExtcostModelAdapter) Invoke(ctx context.Context, r agentmodel.ModelRequest) (agentmodel.ModelResult, error) {
	if a == nil || ctx == nil || a.Port == nil || a.Inner == nil || a.Bind == nil || a.Results == nil {
		return agentmodel.ModelResult{}, extcost.ErrInvalid
	}
	c, err := a.Bind(ctx, r)
	if err != nil {
		return agentmodel.ModelResult{}, err
	}
	if c.Provider != a.Model.ProviderID || c.Model != a.Model.ModelID || c.ModelVersion != a.Model.Version || c.Key != r.TraceID {
		return agentmodel.ModelResult{}, extcost.ErrInvalid
	}
	var result agentmodel.ModelResult
	maximum := extcost.Units{extcost.InputTokens: r.Limits.MaxInputTokens, extcost.OutputTokens: r.Limits.MaxOutputTokens}
	line, err := a.Port.Run(ctx, c, maximum, func(ctx context.Context) (extcost.Measurement, error) {
		var callErr error
		result, callErr = a.Inner.Invoke(ctx, r)
		u := result.Usage
		if u.CachedInputTokens < 0 || u.CachedInputTokens > u.InputTokens || u.InputTokens < 0 || u.OutputTokens < 0 || u.InputTokens > u.TotalTokens || u.OutputTokens != u.TotalTokens-u.InputTokens {
			return extcost.Measurement{}, extcost.ErrInvalid
		}
		if callErr != nil && result.Failure == nil {
			result.Failure = &agentmodel.ModelFailure{Code: agentmodel.FailureInternal}
		}
		resultCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		ref, digest, saveErr := a.Results.SaveExternalModelResult(resultCtx, c, result)
		if saveErr != nil || ref == "" || len(digest) != 64 {
			return extcost.Measurement{}, errors.Join(extcost.ErrPending, callErr, saveErr)
		}
		outcome := "succeeded"
		if callErr != nil || result.Failure != nil {
			outcome = "failed"
		}
		if result.Refusal != nil {
			outcome = "refused"
		}
		if u.TotalTokens == 0 && (callErr != nil || result.Failure != nil) {
			return extcost.Measurement{Outcome: outcome}, callErr
		}
		return extcost.Measurement{Units: extcost.Units{extcost.InputTokens: u.InputTokens - u.CachedInputTokens, extcost.CachedTokens: u.CachedInputTokens, extcost.OutputTokens: u.OutputTokens}, Outcome: outcome, Billed: u.TotalTokens > 0, ResultRef: ref, ResultDigest: digest}, callErr
	})
	if errors.Is(err, extcost.ErrReplay) {
		result, err = a.Results.LoadExternalModelResult(ctx, c, line.Measurement.ResultRef)
		if err != nil {
			return agentmodel.ModelResult{}, errors.Join(extcost.ErrPending, err)
		}
		raw, marshalErr := json.Marshal(result)
		digest := sha256.Sum256(raw)
		if marshalErr != nil || hex.EncodeToString(digest[:]) != line.Measurement.ResultDigest {
			return agentmodel.ModelResult{}, extcost.ErrConflict
		}
		if result.Failure != nil {
			err = ErrExtcostRecordedFailure
		}
	}
	if line.ScheduleVersion != "" {
		result.Usage.CostMicros = line.CostMicros
	}
	return result, err
}

// ExtcostWrapModelAdapters is installed once below the model gateway, covering
// answers, announcements, writing style, translation, tidy and decision calls.
func ExtcostWrapModelAdapters(adapters map[agentmodel.ModelSelection]agentmodel.ModelAdapter, port *extcost.Gateway, bind ExtcostModelBinding, results ...ExtcostModelResults) (map[agentmodel.ModelSelection]agentmodel.ModelAdapter, error) {
	if len(adapters) == 0 || port == nil || bind == nil || len(results) != 1 || results[0] == nil {
		return nil, extcost.ErrInvalid
	}
	out := make(map[agentmodel.ModelSelection]agentmodel.ModelAdapter, len(adapters))
	for selection, inner := range adapters {
		identified, ok := inner.(interface {
			Identity() agentmodel.ModelIdentity
		})
		if inner == nil || !ok || identified.Identity() != selection.Identity {
			return nil, extcost.ErrInvalid
		}
		out[selection] = &ExtcostModelAdapter{Port: port, Inner: inner, Model: selection.Identity, Bind: bind, Results: results[0]}
	}
	return out, nil
}

// ExtcostTokenSchedules converts approved pricing data; no provider rates are
// embedded in any caller. Old schedules stay available to recovery.
func ExtcostTokenSchedules(p *agentmodel.PricingSchedule, currency, operation, source string, from, until, review time.Time) ([]extcost.Schedule, error) {
	if p == nil || p.Digest == "" {
		return nil, extcost.ErrUnpriced
	}
	var out []extcost.Schedule
	for _, e := range p.Entries {
		input, output, cached, per := e.InputMicrosPerToken, e.OutputMicrosPerToken, e.InputMicrosPerToken, int64(1)
		if e.InputMicrosPerMillionTokens > 0 {
			input, output, cached, per = e.InputMicrosPerMillionTokens, e.OutputMicrosPerMillionTokens, e.CachedInputMicrosPerMillionTokens, 1_000_000
			if cached == 0 {
				cached = input
			}
		}
		rates := []extcost.Rate{{Unit: extcost.InputTokens, Tiers: []extcost.Tier{{Micros: input, Per: per}}}, {Unit: extcost.CachedTokens, Tiers: []extcost.Tier{{Micros: cached, Per: per}}}, {Unit: extcost.OutputTokens, Tiers: []extcost.Tier{{Micros: output, Per: per}}}}
		out = append(out, extcost.Schedule{Version: p.Version, Provider: e.Identity.ProviderID, Operation: operation, Model: e.Identity.ModelID, ModelVersion: e.Identity.Version, Currency: currency, Authority: p.Authority, Source: source, EffectiveFrom: from, EffectiveUntil: until, ReviewAt: review, Rates: rates})
	}
	if _, err := extcost.NewCatalog(out); err != nil {
		return nil, err
	}
	return out, nil
}

type ExtcostHTTPTransport interface {
	Do(*http.Request) (*http.Response, error)
}
type ExtcostHTTPBinding func(*http.Request) (extcost.Call, extcost.Units, error)
type ExtcostHTTPMeasurement func(*http.Request, *http.Response, error) extcost.Measurement
type ExtcostHTTPDoer struct {
	Port    *extcost.Gateway
	Inner   ExtcostHTTPTransport
	Bind    ExtcostHTTPBinding
	Measure ExtcostHTTPMeasurement
}

// Do journals a provider request with an enforcing egress.HTTPDoer underneath.
// Use the normalized model adapter instead for model token accounting.
func (d *ExtcostHTTPDoer) Do(r *http.Request) (*http.Response, error) {
	if d == nil || d.Port == nil || d.Inner == nil || d.Bind == nil || d.Measure == nil || r == nil {
		return nil, extcost.ErrInvalid
	}
	c, maximum, err := d.Bind(r)
	if err != nil {
		return nil, err
	}
	var response *http.Response
	_, err = d.Port.Run(r.Context(), c, maximum, func(ctx context.Context) (extcost.Measurement, error) {
		var callErr error
		response, callErr = d.Inner.Do(r.WithContext(ctx))
		return d.Measure(r, response, callErr), callErr
	})
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, err
	}
	return response, nil
}
func ExtcostBudgetRoute(err error) (string, bool) {
	for _, reason := range []error{extcost.ErrBudget, extcost.ErrBudgetMissing, extcost.ErrOverrun, extcost.ErrWarning} {
		if errors.Is(err, reason) {
			return reason.Error(), true
		}
	}
	return "", false
}

// ExtcostRoundTripper meters clients whose constructors take *http.Client.
// Bind and Measure use the same attempt metadata as ExtcostHTTPDoer; Inner
// must be the deployment's already-enforcing transport.
type ExtcostRoundTripper struct {
	Port    *extcost.Gateway
	Inner   http.RoundTripper
	Bind    ExtcostHTTPBinding
	Measure ExtcostHTTPMeasurement
}
type extcostRoundTripDoer struct{ inner http.RoundTripper }

func (d extcostRoundTripDoer) Do(r *http.Request) (*http.Response, error) {
	return d.inner.RoundTrip(r)
}
func (t *ExtcostRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	if t == nil || t.Inner == nil {
		return nil, extcost.ErrInvalid
	}
	d := ExtcostHTTPDoer{Port: t.Port, Inner: extcostRoundTripDoer{t.Inner}, Bind: t.Bind, Measure: t.Measure}
	return d.Do(r)
}
