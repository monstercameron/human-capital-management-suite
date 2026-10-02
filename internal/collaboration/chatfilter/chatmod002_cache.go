package chatfilter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"time"
)

// The service reads a tenant's definitions and enablements at most once every
// filterCacheTTL on this instance, and CreateVersion and Enable on this
// instance drop the tenant's entry at once. Another instance's change is seen
// within filterCacheTTL. Compiled evaluators are shared by every tenant and
// channel whose active set is byte-for-byte the same; the key includes the
// tenant, so one tenant's rows are never served to another.
const (
	filterCacheTTL        = 3 * time.Second
	filterCacheTenants    = 1024
	filterCacheChannels   = 256
	filterCacheEvaluators = 64
)

type tenantRules struct {
	mu       sync.Mutex
	loaded   time.Time
	ready    bool
	defs     []Definition // latest version of every built-in and stored definition
	enabled  []Enablement
	channels map[string]*channelRules
}

// channelRules is the outcome of resolving one tenant and channel: the rules
// that are on (enforced or dry-run) and when each dry run ends.
type channelRules struct {
	active   []Definition
	dryUntil map[string]time.Time
	once     sync.Once
	compiled *Evaluator
	err      error
}

func (s *Service) cacheNow() time.Time {
	if s.cacheClock != nil {
		return s.cacheClock()
	}
	return time.Now()
}

func (s *Service) tenantEntry(tenant string) *tenantRules {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.tenants == nil {
		s.tenants = map[string]*tenantRules{}
	}
	t := s.tenants[tenant]
	if t == nil {
		if len(s.tenants) >= filterCacheTenants {
			for k := range s.tenants {
				delete(s.tenants, k)
				break
			}
		}
		t = &tenantRules{}
		s.tenants[tenant] = t
	}
	return t
}

// invalidate forgets what this instance cached for the tenant.
func (s *Service) invalidate(tenant string) {
	s.cacheMu.Lock()
	delete(s.tenants, tenant)
	s.cacheMu.Unlock()
}

// resolve answers which rules are on for the tenant and channel, from the cache
// when it is fresh. It reads the store at most once per window per tenant.
func (s *Service) resolve(ctx context.Context, tenant, channel string) (*channelRules, error) {
	t := s.tenantEntry(tenant)
	t.mu.Lock()
	defer t.mu.Unlock()
	if now := s.cacheNow(); !t.ready || now.Sub(t.loaded) >= filterCacheTTL || now.Before(t.loaded) {
		defs, err := s.Store.Definitions(ctx, tenant)
		if err != nil {
			return nil, err
		}
		enabled, err := s.Store.Enablements(ctx, tenant)
		if err != nil {
			return nil, err
		}
		t.defs = latest(append(Builtins(), defs...))
		t.enabled = append([]Enablement(nil), enabled...)
		t.channels = map[string]*channelRules{}
		t.loaded, t.ready = now, true
	}
	if cr := t.channels[channel]; cr != nil {
		return cr, nil
	}
	cr := resolveChannel(t.defs, t.enabled, channel)
	if len(t.channels) >= filterCacheChannels {
		t.channels = map[string]*channelRules{}
	}
	t.channels[channel] = cr
	return cr, nil
}

// resolveChannel applies the workspace setting of every definition and then the
// channel's own override (in either direction) where the rule allows one.
func resolveChannel(defs []Definition, enabled []Enablement, channel string) *channelRules {
	workspace := map[string]Enablement{}
	override := map[string]Enablement{}
	for _, e := range enabled {
		if e.Channel == "" {
			workspace[e.RuleID] = e
		}
		if channel != "" && e.Channel == channel {
			override[e.RuleID] = e
		}
	}
	cr := &channelRules{dryUntil: map[string]time.Time{}}
	for _, d := range defs {
		setting, found := workspace[d.ID]
		if e, ok := override[d.ID]; ok && (d.Product || len(d.Channels) == 1) {
			setting, found = e, true
		}
		if !found || !setting.Enabled {
			continue
		}
		if setting.Action != "" && d.Product {
			d.Action = setting.Action
		}
		cr.active = append(cr.active, d)
		cr.dryUntil[d.ID] = setting.DryRunUntil
	}
	return cr
}

func (cr *channelRules) dry(id string, at time.Time) bool {
	until, ok := cr.dryUntil[id]
	return ok && at.Before(until)
}

// evaluator compiles the active set once; equal sets share one evaluator.
func (s *Service) evaluator(tenant string, cr *channelRules) (*Evaluator, error) {
	cr.once.Do(func() {
		key := activeDigest(tenant, cr.active)
		s.cacheMu.Lock()
		e := s.evaluators[key]
		s.cacheMu.Unlock()
		if e != nil {
			cr.compiled = e
			return
		}
		e, err := s.Registry.Compile(cr.active)
		if err != nil {
			cr.err = err
			return
		}
		cr.compiled = e
		s.cacheMu.Lock()
		if s.evaluators == nil {
			s.evaluators = map[string]*Evaluator{}
		}
		if _, ok := s.evaluators[key]; !ok {
			if len(s.evalOrder) >= filterCacheEvaluators {
				delete(s.evaluators, s.evalOrder[0])
				s.evalOrder = s.evalOrder[1:]
			}
			s.evaluators[key] = e
			s.evalOrder = append(s.evalOrder, key)
		}
		s.cacheMu.Unlock()
	})
	return cr.compiled, cr.err
}

// activeDigest identifies a compiled set: the tenant and every field of every
// active definition (so the id, version and action, and also the terms).
func activeDigest(tenant string, active []Definition) string {
	h := sha256.New()
	put := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte{':'})
		h.Write([]byte(s))
	}
	list := func(xs []string) {
		put(strconv.Itoa(len(xs)))
		for _, x := range xs {
			put(x)
		}
	}
	put(tenant)
	for _, d := range active {
		for _, s := range []string{d.ID, d.Version, d.Action, d.Name, d.Language, d.Kind, d.Target} {
			put(s)
		}
		put(strconv.FormatBool(d.Hard) + strconv.FormatBool(d.Product))
		list(d.Match)
		list(d.Channels)
		list(d.ExemptRoles)
		list(d.ExemptAgents)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// HasActive reports whether at least one rule, built-in or custom, enforced or
// dry-run, is on for the tenant and channel, by the resolution Evaluate uses.
// It answers from the cache and compiles nothing.
func (s *Service) HasActive(ctx context.Context, tenant, channel string) (bool, error) {
	if s.Store == nil || s.Registry == nil || tenant == "" {
		return false, ErrUnavailable
	}
	cr, err := s.resolve(ctx, tenant, channel)
	if err != nil {
		return false, err
	}
	return len(cr.active) > 0, nil
}
