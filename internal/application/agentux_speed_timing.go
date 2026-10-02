package application

import (
	"context"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

// agentUXRunTiming is deliberately content-free. It records only named
// execution stages and infrastructure operations for one persona run.
type agentUXRunTiming struct {
	mu        sync.Mutex
	started   time.Time
	runID     string
	stages    map[string]agentUXTimingMetric
	events    map[string]agentUXTimingMetric
	authority *agentUXAuthorityCache
	immutable map[string]agentUXImmutableModelWork
	toolPins  map[string][]agentskills.SkillPin
}

type agentUXTimingMetric struct {
	Count    int
	Duration time.Duration
}

type agentUXAuthorityCache struct {
	request  agentrun.Request
	snapshot agentrun.AuthoritySnapshot
}

type agentUXImmutableModelWork struct {
	profile  agentpersona.PersonaProfile
	manifest agentmanifest.Manifest
	route    PersonaRunModelRoute
	policy   PersonaRunEffectivePolicy
}

type agentUXRunTimingKey struct{}

func withAgentUXRunTiming(ctx context.Context) (context.Context, *agentUXRunTiming) {
	if timing := agentUXRunTimingFromContext(ctx); timing != nil {
		return ctx, timing
	}
	timing := &agentUXRunTiming{started: time.Now(), stages: make(map[string]agentUXTimingMetric), events: make(map[string]agentUXTimingMetric), immutable: make(map[string]agentUXImmutableModelWork), toolPins: make(map[string][]agentskills.SkillPin)}
	return context.WithValue(ctx, agentUXRunTimingKey{}, timing), timing
}

func agentUXSpeedImmutableModelWork(ctx context.Context, runID string) (agentUXImmutableModelWork, bool) {
	timing := agentUXRunTimingFromContext(ctx)
	if timing == nil || runID == "" {
		return agentUXImmutableModelWork{}, false
	}
	timing.mu.Lock()
	defer timing.mu.Unlock()
	value, ok := timing.immutable[runID]
	return value, ok
}

func agentUXSpeedCacheImmutableModelWork(ctx context.Context, runID string, value agentUXImmutableModelWork) {
	if timing := agentUXRunTimingFromContext(ctx); timing != nil && runID != "" {
		timing.mu.Lock()
		if _, exists := timing.immutable[runID]; !exists {
			timing.immutable[runID] = value
		}
		timing.mu.Unlock()
	}
}

func agentUXRunTimingFromContext(ctx context.Context) *agentUXRunTiming {
	if ctx == nil {
		return nil
	}
	timing, _ := ctx.Value(agentUXRunTimingKey{}).(*agentUXRunTiming)
	return timing
}

func agentUXSpeedStage(ctx context.Context, name string) func() {
	return agentUXSpeedMeasure(ctx, true, name)
}

func agentUXSpeedEvent(ctx context.Context, name string) func() {
	return agentUXSpeedMeasure(ctx, false, name)
}

func agentUXSpeedMeasure(ctx context.Context, stage bool, name string) func() {
	timing := agentUXRunTimingFromContext(ctx)
	if timing == nil || strings.TrimSpace(name) == "" {
		return func() {}
	}
	started := time.Now()
	return func() {
		timing.mu.Lock()
		target := timing.events
		if stage {
			target = timing.stages
		}
		metric := target[name]
		metric.Count++
		metric.Duration += time.Since(started)
		target[name] = metric
		timing.mu.Unlock()
	}
}

func agentUXSpeedSetRunID(ctx context.Context, runID string) {
	if timing := agentUXRunTimingFromContext(ctx); timing != nil && runID != "" {
		timing.mu.Lock()
		if timing.runID == "" {
			timing.runID = runID
		}
		timing.mu.Unlock()
	}
}

func agentUXSpeedInvalidateAuthority(ctx context.Context) {
	if timing := agentUXRunTimingFromContext(ctx); timing != nil {
		timing.mu.Lock()
		timing.authority = nil
		clear(timing.toolPins)
		timing.mu.Unlock()
	}
}

func agentUXSpeedCacheToolPins(ctx context.Context, invocationID string, pins []agentskills.SkillPin) {
	if timing := agentUXRunTimingFromContext(ctx); timing != nil && invocationID != "" {
		timing.mu.Lock()
		timing.toolPins[invocationID] = append([]agentskills.SkillPin(nil), pins...)
		timing.mu.Unlock()
	}
}

func agentUXSpeedToolPins(ctx context.Context, invocationID string) ([]agentskills.SkillPin, bool) {
	timing := agentUXRunTimingFromContext(ctx)
	if timing == nil || invocationID == "" {
		return nil, false
	}
	timing.mu.Lock()
	defer timing.mu.Unlock()
	pins, ok := timing.toolPins[invocationID]
	return append([]agentskills.SkillPin(nil), pins...), ok
}

func agentUXSpeedCachedAuthority(ctx context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, bool) {
	timing := agentUXRunTimingFromContext(ctx)
	if timing == nil {
		return agentrun.AuthoritySnapshot{}, false
	}
	timing.mu.Lock()
	defer timing.mu.Unlock()
	if timing.authority == nil || !reflect.DeepEqual(timing.authority.request, request) {
		return agentrun.AuthoritySnapshot{}, false
	}
	return timing.authority.snapshot, true
}

func agentUXSpeedCacheAuthority(ctx context.Context, request agentrun.Request, snapshot agentrun.AuthoritySnapshot) {
	if timing := agentUXRunTimingFromContext(ctx); timing != nil {
		timing.mu.Lock()
		timing.authority = &agentUXAuthorityCache{request: request, snapshot: snapshot}
		timing.mu.Unlock()
	}
}

func agentUXSpeedEmit(ctx context.Context, failed bool) {
	timing := agentUXRunTimingFromContext(ctx)
	if timing == nil {
		return
	}
	timing.mu.Lock()
	runID, total := timing.runID, time.Since(timing.started)
	stages, events := cloneAgentUXMetrics(timing.stages), cloneAgentUXMetrics(timing.events)
	timing.mu.Unlock()
	attrs := []any{"run_id", runID, "total_ms", total.Milliseconds(), "failed", failed}
	attrs = append(attrs, agentUXMetricAttrs("stage", stages)...)
	attrs = append(attrs, agentUXMetricAttrs("event", events)...)
	slog.InfoContext(ctx, "hcmnext.persona_run_timing", attrs...)
}

func cloneAgentUXMetrics(in map[string]agentUXTimingMetric) map[string]agentUXTimingMetric {
	out := make(map[string]agentUXTimingMetric, len(in))
	for name, metric := range in {
		out[name] = metric
	}
	return out
}

func agentUXMetricAttrs(prefix string, metrics map[string]agentUXTimingMetric) []any {
	names := make([]string, 0, len(metrics))
	for name := range metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	attrs := make([]any, 0, len(names)*4)
	for _, name := range names {
		key := strings.NewReplacer(".", "_", "-", "_").Replace(name)
		attrs = append(attrs, prefix+"_"+key+"_count", metrics[name].Count, prefix+"_"+key+"_ms", metrics[name].Duration.Milliseconds())
	}
	return attrs
}
