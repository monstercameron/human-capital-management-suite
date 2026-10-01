package governance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

type AuthZInput struct {
	Effect       string
	Reason       string
	Restrictions []string
	Digest       string
	ValidUntil   time.Time
	EvaluatedAt  time.Time
}

type LegalInput struct {
	Effect      string
	Reason      string
	Obligations []string
	Digest      string
	ValidUntil  time.Time
	EvaluatedAt time.Time
}

type PurposeInput struct {
	Allowed bool
	Reason  string
}

type RiskInput struct {
	Level       string
	Obligations []string
}

type ComposeRequest struct {
	AuthZ   AuthZInput
	Legal   LegalInput
	Purpose PurposeInput
	Risk    RiskInput
	Now     time.Time
}

type ComposeResult struct {
	Decision     string
	Restrictions []string
	Obligations  []string
	Explanation  string
	Digest       string
	ValidUntil   time.Time
	EvaluatedAt  time.Time
	Privacy      PrivacyRuntime `json:"-"`
}

// PrivacyRuntime returns the composed privacy boundary without adding
// implementation details to serialized governance projections.
func (r ComposeResult) PrivacyRuntime() PrivacyRuntime { return r.Privacy }

func composeDigest(r ComposeRequest) string {
	h := sha256.New()
	w := func(label, v string) { fmt.Fprintf(h, "%s=%d:%s;", label, len(v), v) }
	w("authz.effect", r.AuthZ.Effect)
	w("authz.reason", r.AuthZ.Reason)
	w("authz.digest", r.AuthZ.Digest)
	aR := append([]string(nil), r.AuthZ.Restrictions...)
	sort.Strings(aR)
	for _, v := range aR {
		w("authz.restriction", v)
	}
	w("legal.effect", r.Legal.Effect)
	w("legal.reason", r.Legal.Reason)
	w("legal.digest", r.Legal.Digest)
	lO := append([]string(nil), r.Legal.Obligations...)
	sort.Strings(lO)
	for _, v := range lO {
		w("legal.obligation", v)
	}
	if r.Purpose.Allowed {
		w("purpose", "allow")
	} else {
		w("purpose", "deny")
	}
	w("purpose.reason", r.Purpose.Reason)
	w("risk.level", r.Risk.Level)
	rO := append([]string(nil), r.Risk.Obligations...)
	sort.Strings(rO)
	for _, v := range rO {
		w("risk.obligation", v)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func intersect(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	if len(a) == 0 {
		out := append([]string(nil), b...)
		sort.Strings(out)
		return out
	}
	if len(b) == 0 {
		out := append([]string(nil), a...)
		sort.Strings(out)
		return out
	}
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	var out []string
	for _, v := range b {
		if set[v] {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func unionDedup(lists ...[]string) []string {
	m := map[string]bool{}
	for _, l := range lists {
		for _, v := range l {
			if v != "" {
				m[v] = true
			}
		}
	}
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func Compose(r ComposeRequest) ComposeResult {
	digest := composeDigest(r)
	privacyRuntime := NewPrivacyRuntime()
	now := r.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	validUntil := r.AuthZ.ValidUntil
	if r.Legal.ValidUntil.Before(validUntil) {
		validUntil = r.Legal.ValidUntil
	}
	evaluatedAt := r.AuthZ.EvaluatedAt
	if r.Legal.EvaluatedAt.After(evaluatedAt) {
		evaluatedAt = r.Legal.EvaluatedAt
	}
	stale := !now.Before(validUntil)
	if stale {
		return ComposeResult{
			Decision:     "UNKNOWN",
			Restrictions: nil,
			Obligations:  nil,
			Explanation:  "stale decision permits nothing: authz or legal validity expired",
			Digest:       digest,
			ValidUntil:   validUntil,
			EvaluatedAt:  evaluatedAt,
			Privacy:      privacyRuntime,
		}
	}
	if r.AuthZ.Effect == "DENY" {
		obs := unionDedup(r.Legal.Obligations, r.Risk.Obligations)
		return ComposeResult{
			Decision:     "DENY",
			Restrictions: intersect(r.AuthZ.Restrictions, r.Legal.Obligations),
			Obligations:  obs,
			Explanation:  fmt.Sprintf("DENY authz=%s legal=%s purpose=%v", r.AuthZ.Reason, r.Legal.Reason, r.Purpose.Allowed),
			Digest:       digest,
			ValidUntil:   validUntil,
			EvaluatedAt:  evaluatedAt,
			Privacy:      privacyRuntime,
		}
	}
	if r.Legal.Effect == "NON_COMPLIANT" {
		obs := unionDedup(r.Legal.Obligations, r.Risk.Obligations)
		return ComposeResult{
			Decision:     "DENY",
			Restrictions: intersect(r.AuthZ.Restrictions, r.Legal.Obligations),
			Obligations:  obs,
			Explanation:  fmt.Sprintf("DENY legal=%s authz=%s", r.Legal.Reason, r.AuthZ.Reason),
			Digest:       digest,
			ValidUntil:   validUntil,
			EvaluatedAt:  evaluatedAt,
			Privacy:      privacyRuntime,
		}
	}
	if !r.Purpose.Allowed {
		return ComposeResult{
			Decision:     "DENY",
			Restrictions: r.AuthZ.Restrictions,
			Obligations:  unionDedup(r.Legal.Obligations, r.Risk.Obligations),
			Explanation:  fmt.Sprintf("DENY purpose=%s", r.Purpose.Reason),
			Digest:       digest,
			ValidUntil:   validUntil,
			EvaluatedAt:  evaluatedAt,
			Privacy:      privacyRuntime,
		}
	}
	if r.AuthZ.Effect == "UNKNOWN" || r.Legal.Effect == "UNKNOWN" || r.Risk.Level == "UNKNOWN" {
		obs := unionDedup(r.Legal.Obligations, r.Risk.Obligations)
		return ComposeResult{
			Decision:     "UNKNOWN",
			Restrictions: intersect(r.AuthZ.Restrictions, nil),
			Obligations:  obs,
			Explanation:  "UNKNOWN obligation or coverage missing",
			Digest:       digest,
			ValidUntil:   validUntil,
			EvaluatedAt:  evaluatedAt,
			Privacy:      privacyRuntime,
		}
	}
	restrictions := intersect(r.AuthZ.Restrictions, r.Legal.Obligations)
	if len(r.AuthZ.Restrictions) > 0 && len(r.Legal.Obligations) == 0 {
		restrictions = append([]string(nil), r.AuthZ.Restrictions...)
		sort.Strings(restrictions)
	}
	obligations := unionDedup(r.Legal.Obligations, r.Risk.Obligations)
	if len(obligations) > 0 {
		return ComposeResult{
			Decision:     "OBLIGATIONS",
			Restrictions: restrictions,
			Obligations:  obligations,
			Explanation:  fmt.Sprintf("OBLIGATIONS authz=%s legal=%s risk=%s", r.AuthZ.Reason, r.Legal.Reason, r.Risk.Level),
			Digest:       digest,
			ValidUntil:   validUntil,
			EvaluatedAt:  evaluatedAt,
			Privacy:      privacyRuntime,
		}
	}
	return ComposeResult{
		Decision:     "ALLOW",
		Restrictions: restrictions,
		Obligations:  nil,
		Explanation:  fmt.Sprintf("ALLOW authz=%s legal=%s", r.AuthZ.Reason, r.Legal.Reason),
		Digest:       digest,
		ValidUntil:   validUntil,
		EvaluatedAt:  evaluatedAt,
		Privacy:      privacyRuntime,
	}
}
