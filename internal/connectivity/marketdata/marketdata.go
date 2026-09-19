// Package marketdata is the egress-backed rewards.MarketRateSource.
//
// It is a skeleton, not a vendor integration: the wire shape below is the
// package's own vendor-neutral contract, and no composition constructs a
// Source unless a market-rate URL is explicitly configured. Until then the
// high-performer workflow resolves its anchor from the static stub table,
// so an unconfigured deployment cannot fail at fetch_market_rate for want
// of a vendor.
//
// Every byte leaves through the egress gateway: proxy, DNS, TLS and DLP
// enforcement from internal/connectivity/egress apply unchanged. The source
// holds no credentials of its own; vendor authentication arrives later as a
// credential lease on the gateway request.
package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/egress"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Purpose names the outbound call in gateway policy.
const Purpose = "promotion.market_rate.read"

// SourceVersion pins the wire contract this skeleton speaks.
const SourceVersion = "rewards.market.http/2026.1"

// moneyScale is the declared scale of every market leg on the wire.
const moneyScale int32 = 2

// Config binds the skeleton to its transport. BaseURL is mandatory: an
// empty URL is refused so "unconfigured" can never become a request to
// nowhere. Now stamps provenance only and defaults to the wall clock; tests
// pin it.
type Config struct {
	Gateway *egress.Gateway
	BaseURL string
	Now     func() time.Time
}

// Source is a rewards.MarketRateSource over one egress gateway and one base
// URL. The zero value is unusable: New is the only construction path, and a
// composition that has no market-rate URL keeps a nil *Source, which the
// governed lookup refuses loudly rather than treating as "no market".
type Source struct {
	gateway *egress.Gateway
	base    *url.URL
	now     func() time.Time
}

// New binds the skeleton to a gateway and a base URL.
func New(cfg Config) (*Source, error) {
	if cfg.Gateway == nil {
		return nil, fmt.Errorf("marketdata: no egress gateway")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("marketdata: no market-rate base URL")
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("marketdata: invalid market-rate base URL %q", cfg.BaseURL)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Source{gateway: cfg.Gateway, base: base, now: now}, nil
}

// wireAnchor is the vendor-neutral response shape. Legs are decimal strings
// at moneyScale so no JSON float ever becomes a compensation number.
type wireAnchor struct {
	P25           string `json:"p25"`
	P50           string `json:"p50"`
	P75           string `json:"p75"`
	Currency      string `json:"currency"`
	AsOf          string `json:"as_of"`
	SourceVersion string `json:"source_version"`
}

// LookupMarketRate implements rewards.MarketRateSource.
func (s *Source) LookupMarketRate(ctx context.Context, q rewards.MarketQuery) (rewards.MarketRecord, error) {
	if s == nil || s.gateway == nil || s.base == nil {
		return rewards.MarketRecord{}, fmt.Errorf("%w: no market-rate source is configured", rewards.ErrMarketQueryInvalid)
	}
	if err := q.Validate(); err != nil {
		return rewards.MarketRecord{}, err
	}
	target := *s.base
	target.Path += "/v1/market-rate"
	query := target.Query()
	query.Set("job_code", q.JobCode)
	query.Set("grade", q.Grade)
	query.Set("pay_zone", q.PayZone)
	query.Set("currency", q.Currency)
	query.Set("as_of", q.AsOf.String())
	target.RawQuery = query.Encode()

	result, err := s.gateway.Do(ctx, egress.Request{
		Method:    http.MethodGet,
		Target:    target.String(),
		Purpose:   Purpose,
		Principal: "hcmnext.workflows.promotion.high_performer/fetch_market_rate",
	})
	if err != nil {
		return rewards.MarketRecord{}, fmt.Errorf("%w: %w", rewards.ErrMarketSourceFailed, err)
	}
	defer result.Response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(result.Response.Body, 64<<10))
	if err != nil {
		return rewards.MarketRecord{}, fmt.Errorf("%w: %w", rewards.ErrMarketSourceFailed, err)
	}
	if result.Response.StatusCode == http.StatusNotFound {
		return rewards.MarketRecord{}, fmt.Errorf("%w: %s/%s/%s in %s",
			rewards.ErrMarketRateNotFound, q.JobCode, q.Grade, q.PayZone, q.Currency)
	}
	if result.Response.StatusCode != http.StatusOK {
		return rewards.MarketRecord{}, fmt.Errorf("%w: status %s", rewards.ErrMarketSourceFailed, result.Response.Status)
	}
	var wire wireAnchor
	if err := json.Unmarshal(body, &wire); err != nil {
		return rewards.MarketRecord{}, fmt.Errorf("%w: %w", rewards.ErrMarketSourceFailed, err)
	}
	p25, err := values.NewMoney(wire.P25, wire.Currency, moneyScale, values.RoundingHalfEven)
	if err != nil {
		return rewards.MarketRecord{}, fmt.Errorf("%w: p25: %w", rewards.ErrMarketAnchorInvalid, err)
	}
	p50, err := values.NewMoney(wire.P50, wire.Currency, moneyScale, values.RoundingHalfEven)
	if err != nil {
		return rewards.MarketRecord{}, fmt.Errorf("%w: p50: %w", rewards.ErrMarketAnchorInvalid, err)
	}
	p75, err := values.NewMoney(wire.P75, wire.Currency, moneyScale, values.RoundingHalfEven)
	if err != nil {
		return rewards.MarketRecord{}, fmt.Errorf("%w: p75: %w", rewards.ErrMarketAnchorInvalid, err)
	}
	asOf, err := values.ParseLocalDate(wire.AsOf)
	if err != nil {
		return rewards.MarketRecord{}, fmt.Errorf("%w: as-of: %w", rewards.ErrMarketAnchorInvalid, err)
	}
	version := wire.SourceVersion
	if version == "" {
		version = SourceVersion
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(s.now().UTC()))
	if err != nil {
		return rewards.MarketRecord{}, fmt.Errorf("%w: %w", rewards.ErrMarketSourceFailed, err)
	}
	record := rewards.MarketRecord{
		Scope:         q.Scope(),
		Anchor:        rewards.MarketAnchor{P25: p25, P50: p50, P75: p75, AsOf: asOf},
		SourceVersion: version,
		Authority: evidence.SourceAuthority{
			Kind:      evidence.AuthorityExternalObservation,
			System:    s.base.Host,
			PolicyRef: "rewards.market_source/2026.1",
		},
		Provenance: evidence.Provenance{
			Source:      s.base.Host,
			EvidenceRef: "evd_market_http_" + strings.ToLower(q.JobCode) + "_" + strings.ToLower(q.Grade),
			RecordedAt:  recorded,
		},
	}
	if err := record.Validate(q.Currency); err != nil {
		return rewards.MarketRecord{}, err
	}
	return record, nil
}
