// Package promotionhiperf defines the high-performer promotion variant
// (owner: workflow-runtime; phase: Gate C; HIPERF-002).
//
// # The variant plan
//
// [Definition] is hcmnext.workflows.promotion.high_performer v1: the exact
// execute node set from [promotionexec.SharedGraph] plus one
// fetch_market_rate capability node invoking
// hcmnext.rewards.market_rate/v1. The fetch runs between snapshot_worker
// and simulate_compensation; its anchor digest feeds simulate_compensation
// and the resulting market floor feeds raise_threshold. Everything else --
// approvals, waits, revalidation, the commit, the observations, the
// terminals -- is the execute graph unchanged, so the execute definition,
// digest and promotionexec tests are untouched by the variant's existence.
//
// # What this deliberately is not
//
// This package defines the plan; it never executes it. Execution belongs to
// internal/workflow/execute composed by internal/platform/execution, the
// market anchor's meaning stays with internal/domains/rewards, and routing
// subjects to this plan stays with internal/intent/app.
package promotionhiperf
