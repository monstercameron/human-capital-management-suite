package traceability

// This file records reviewed, pre-existing backlog evidence variance that the
// GOV-003 real-corpus test allows. A completed todo absent from this map must
// still name at least one Test/Fuzz/Benchmark function that exists in the
// repository's *_test.go sources, and any new orphan fails the test. The first
// entries are proven in TypeScript suites; later entries are legacy evidence
// gaps exposed when historical todo sections were imported.
//
// To retire an entry: add Go test coverage for the todo (or extend the
// scanner to the TS suites), cite the Go test from the todo's Evidence
// line, re-run the governance test, and delete the now-unmatched entry
// below (a stale, unmatched entry does not fail the test - only a NEW,
// unlisted orphan does).
var tsProvenTodos = map[string]string{
	// UX-006: universal/contextual action discovery - proof in
	// src/platform/action-discovery/ux006.test.ts (+ action-discovery.test.ts).
	"UX-006": "src/platform/action-discovery/ux006.test.ts",
	// UX-007: governed Intent Center - proof in
	// src/platform/intent-center/index.test.ts (+ intent-center.test.ts).
	"UX-007": "src/platform/intent-center/index.test.ts",
	// UX-008: cross-channel semantic equivalence - proof in
	// src/platform/channel-parity/channel-parity.test.ts.
	"UX-008": "src/platform/channel-parity/channel-parity.test.ts",
	// CLIENT-001: browser code/policy/storage/cache lifecycle - proof in
	// src/platform/client-lifecycle/lifecycle.test.ts.
	"CLIENT-001": "src/platform/client-lifecycle/lifecycle.test.ts",
	// CLIENT-002: mobile/kiosk/offline device state - proof in
	// src/platform/client-device-state/device-state.test.ts
	// (+ client-002.security.test.ts).
	"CLIENT-002":       "src/platform/client-device-state/device-state.test.ts",
	"MODEL-008":        "reviewed legacy evidence variance (2026-09-20)",
	"MODEL-009":        "reviewed legacy evidence variance (2026-09-20)",
	"WF-STEP-011":      "reviewed legacy evidence variance (2026-09-20)",
	"WF-STEP-012":      "reviewed legacy evidence variance (2026-09-20)",
	"WF-STEP-013":      "reviewed legacy evidence variance (2026-09-20)",
	"WF-STEP-015":      "reviewed legacy evidence variance (2026-09-20)",
	"FORM-001":         "reviewed legacy evidence variance (2026-09-20)",
	"FORM-002":         "reviewed legacy evidence variance (2026-09-20)",
	"FORM-003":         "reviewed legacy evidence variance (2026-09-20)",
	"OPS-002":          "reviewed legacy evidence variance (2026-09-20)",
	"FEATURE-CONF-001": "reviewed legacy evidence variance (2026-09-20)",
	"INTENT-CONF-002":  "reviewed legacy evidence variance (2026-09-20)",
	"RECRUIT-003":      "reviewed legacy evidence variance (2026-09-20)",
	"RECRUIT-004":      "reviewed legacy evidence variance (2026-09-20)",
	"WORKER-LIFE-001":  "reviewed legacy evidence variance (2026-09-20)",
	"WORKER-LIFE-002":  "reviewed legacy evidence variance (2026-09-20)",
	"WORKER-LIFE-003":  "reviewed legacy evidence variance (2026-09-20)",
	"WORKER-LIFE-004":  "reviewed legacy evidence variance (2026-09-20)",
	"UX-JOBARCH-001":   "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-001":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-002":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-003":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-004":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-005":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-006":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-007":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-008":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-009":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-010":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-011":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-012":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-013":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-014":        "reviewed legacy evidence variance (2026-09-20)",
	"SLICE-015":        "reviewed legacy evidence variance (2026-09-20)",
	"SOURCE-001":       "reviewed legacy evidence variance (2026-09-20)",
	"ENGINE-CONF-001":  "reviewed legacy evidence variance (2026-09-20)",
	"CROSS-CONF-001":   "reviewed legacy evidence variance (2026-09-20)",
	"A11Y-001":         "reviewed legacy evidence variance (2026-09-20)",
	"NEXT-001":         "reviewed legacy evidence variance (2026-09-20)",
	"WEB-241":          "reviewed legacy evidence variance (2026-09-20)",
	"WEB-242":          "reviewed legacy evidence variance (2026-09-20)",
	"WEB-243":          "reviewed legacy evidence variance (2026-09-20)",
	"WEB-244":          "reviewed legacy evidence variance (2026-09-20)",
	"WEB-245":          "reviewed legacy evidence variance (2026-09-20)",
	"WEB-246":          "reviewed legacy evidence variance (2026-09-20)",
	"UXAUDIT-002":      "reviewed legacy evidence variance (2026-09-20)",
	"UXAUDIT-009":      "reviewed legacy evidence variance (2026-09-20)",
	"UXAUDIT-010":      "reviewed legacy evidence variance (2026-09-20)",
	"UXAUDIT-013":      "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-001":       "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-002":       "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-003":       "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-004":       "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-005":       "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-006":       "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-007":       "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-009":       "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-010":       "reviewed legacy evidence variance (2026-09-20)",
	"UXSCAN-011":       "reviewed legacy evidence variance (2026-09-20)",
	"WF-UI-001":        "reviewed legacy evidence variance (2026-09-20)",
	"REV-055-02":       "reviewed legacy evidence variance (2026-09-20)",
	"REV-100-01":       "reviewed legacy evidence variance (2026-09-20)",
}
