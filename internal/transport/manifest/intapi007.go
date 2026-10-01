package manifest

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// HTTPRoute is the public, resource-shaped projection of one served RPC.
// The Connect procedure remains the canonical wire identity; ResourcePath is
// an additive alias and may therefore be introduced without changing the RPC
// contract.
type HTTPRoute struct {
	EndpointID   string
	Procedure    string
	Method       string
	ResourcePath string
}

// PublicHTTPRoutes returns the resource aliases described by the reviewed
// endpoint manifest. Refused and procedure-shaped rows are deliberately not
// returned: a public alias must never make a non-public operation callable.
func PublicHTTPRoutes(m *EndpointManifest) []HTTPRoute {
	if m == nil {
		return nil
	}
	out := make([]HTTPRoute, 0, len(m.Endpoints))
	for _, endpoint := range m.Endpoints {
		if endpoint.Disposition != DispositionServed || endpoint.HTTPPathTemplate == "" || endpoint.HTTPPathTemplate == endpoint.GRPCProcedure {
			continue
		}
		out = append(out, HTTPRoute{
			EndpointID: endpoint.EndpointID, Procedure: endpoint.GRPCProcedure,
			Method: endpoint.HTTPMethod, ResourcePath: endpoint.HTTPPathTemplate,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ResourcePath != out[j].ResourcePath {
			return out[i].ResourcePath < out[j].ResourcePath
		}
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Procedure < out[j].Procedure
	})
	return out
}

// DeprecationPolicy is the reviewable lifecycle record for an HTTP operation.
// A published notice must precede deprecation by NoticePeriod, and Sunset is
// the earliest point at which removal may be considered.
type DeprecationPolicy struct {
	DeprecatedAt      time.Time     `json:"deprecated_at"`
	SunsetAt          time.Time     `json:"sunset_at"`
	NoticePublishedAt time.Time     `json:"notice_published_at"`
	NoticePeriod      time.Duration `json:"notice_period"`
	NoticeURL         string        `json:"notice_url"`
}

var (
	ErrDeprecationPolicyInvalid = errors.New("manifest: invalid deprecation policy")
	ErrRemovalNoticeIncomplete  = errors.New("manifest: removal notice period is incomplete")
)

// Validate enforces the public compatibility rule independently of a server
// clock, which makes policy review deterministic and suitable for CI.
func (p DeprecationPolicy) Validate() error {
	if p.DeprecatedAt.IsZero() || p.SunsetAt.IsZero() || p.NoticePublishedAt.IsZero() || p.NoticePeriod <= 0 || strings.TrimSpace(p.NoticeURL) == "" {
		return ErrDeprecationPolicyInvalid
	}
	if !p.DeprecatedAt.Before(p.SunsetAt) || p.NoticePublishedAt.After(p.DeprecatedAt) || p.DeprecatedAt.Sub(p.NoticePublishedAt) < p.NoticePeriod {
		return ErrDeprecationPolicyInvalid
	}
	return nil
}

// Headers returns the RFC 9745/RFC 8594 response headers once deprecation is
// effective. Before that date the operation is still active and no warning is
// emitted.
func (p DeprecationPolicy) Headers(now time.Time) (http.Header, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if now.Before(p.DeprecatedAt) {
		return make(http.Header), nil
	}
	h := make(http.Header)
	h.Set("Deprecation", "@"+fmt.Sprintf("%d", p.DeprecatedAt.Unix()))
	h.Set("Sunset", p.SunsetAt.UTC().Format(http.TimeFormat))
	return h, nil
}

// RemovalAllowed reports whether a route may be removed. The notice must be
// valid and the sunset must have passed; callers still need to publish the
// actual removal separately.
func (p DeprecationPolicy) RemovalAllowed(now time.Time) bool {
	return p.Validate() == nil && !now.Before(p.SunsetAt)
}
