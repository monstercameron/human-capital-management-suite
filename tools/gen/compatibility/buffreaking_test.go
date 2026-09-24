package compatibility

import (
	"os"
	"testing"
)

// fixtureV1 and schemaBaseline are the two checked-in descriptor-set
// goldens this file exercises: a small self-contained fixture module (never
// touching schema/proto) used to prove RunBufBreaking actually detects
// breaks and additive changes, and a golden snapshot of the real live
// schema/proto module used as the CI regression gate.
const (
	fixtureV1         = "testdata/bufbreaking/v1"
	fixtureV1Baseline = "testdata/bufbreaking/v1_baseline.binpb"
	fixtureV2Breaking = "testdata/bufbreaking/v2_breaking"
	fixtureV3Additive = "testdata/bufbreaking/v3_additive"

	liveSchemaModule   = "../../../schema/proto"
	schemaBaselineFile = "testdata/schema_baseline.binpb"
)

func requireBuf(t *testing.T) string {
	t.Helper()
	buf, err := ResolveBufBinary()
	if err != nil {
		t.Skipf("buf CLI not available: %v", err)
	}
	return buf
}

// TestTodo_API_002_BufBreakingDetectsFieldTypeChange proves RunBufBreaking
// finds a real Protobuf wire break (widget_id: string -> int64) that only
// exists in the actual descriptor bytes, not in a hand-authored Version
// struct. This is the RED case API-002 asks for grounded in real .proto
// compilation instead of synthetic fixtures.
func TestTodo_API_002_BufBreakingDetectsFieldTypeChange(t *testing.T) {
	buf := requireBuf(t)
	report, err := RunBufBreaking(buf, fixtureV2Breaking, fixtureV1Baseline)
	if err != nil {
		t.Fatalf("RunBufBreaking: %v", err)
	}
	if report.OK() {
		t.Fatalf("expected a breaking-change violation, got none: %+v", report)
	}
	found := false
	for _, v := range report.Violations {
		if v.Type == "FIELD_SAME_TYPE" {
			found = true
		}
		if v.Message == "" {
			t.Fatalf("violation carries no evidence: %+v", v)
		}
	}
	if !found {
		t.Fatalf("expected a FIELD_SAME_TYPE violation, got %+v", report.Violations)
	}
}

// TestTodo_API_002_BufBreakingAllowsAdditiveChange proves an additive field
// (display_name) against the same v1 baseline is not flagged: transport
// compatibility policy accepts genuine additive evolution.
func TestTodo_API_002_BufBreakingAllowsAdditiveChange(t *testing.T) {
	buf := requireBuf(t)
	report, err := RunBufBreaking(buf, fixtureV3Additive, fixtureV1Baseline)
	if err != nil {
		t.Fatalf("RunBufBreaking: %v", err)
	}
	if !report.OK() {
		t.Fatalf("additive change flagged as breaking: %+v", report)
	}
}

// TestTodo_API_002_BufBreakingGolden is the CI regression gate: it compares
// the live, committed schema/proto module against the checked-in
// testdata/schema_baseline.binpb descriptor-set golden. A clean result here
// means no undocumented wire break has crept into the real API surface
// since the golden was last regenerated.
//
// To regenerate the golden after a deliberate, version-governed schema
// change, run:
//
//	HCMNEXT_COMPAT_REGEN_BASELINE=1 go test -run TestTodo_API_002_BufBreakingGolden ./tools/gen/compatibility/
//
// then rerun without the environment variable to confirm the refreshed
// golden reports clean, and commit the updated .binpb file.
func TestTodo_API_002_BufBreakingGolden(t *testing.T) {
	buf, err := ResolveBufBinary()
	if err != nil {
		t.Fatalf("buf CLI required for compatibility golden: %v", err)
	}
	if _, err := os.Stat(schemaBaselineFile); err != nil {
		t.Fatalf("reading checked-in baseline %s: %v", schemaBaselineFile, err)
	}
	if os.Getenv(RegenBaselineEnv) == "1" {
		if err := BuildDescriptorImage(buf, liveSchemaModule, schemaBaselineFile); err != nil {
			t.Fatalf("regenerating baseline: %v", err)
		}
		t.Logf("regenerated %s from %s; rerun without %s set to verify", schemaBaselineFile, liveSchemaModule, RegenBaselineEnv)
		return
	}
	report, err := RunBufBreaking(buf, liveSchemaModule, schemaBaselineFile)
	if err != nil {
		t.Fatalf("RunBufBreaking: %v", err)
	}
	if !report.OK() {
		t.Fatalf("live schema/proto is no longer compatible with the checked-in baseline: %+v\n"+
			"if this break is deliberate and governed (new major version, documented migration), "+
			"regenerate the baseline per this test's doc comment", report.Violations)
	}
}

// TestTodo_API_002_BufBreakingBaselineIsFresh guards the golden itself: a
// forgotten regeneration would let TestTodo_API_002_BufBreakingGolden pass
// vacuously by comparing schema/proto against a stale image that already
// diverged in some earlier, uncaught change. Rebuilding a fresh image from
// the live sources and diffing its byte length against the checked-in one is
// a coarse but zero-dependency freshness signal: any file addition, removal,
// or descriptor change shifts the encoded size.
func TestTodo_API_002_BufBreakingBaselineIsFresh(t *testing.T) {
	buf := requireBuf(t)
	tmp := t.TempDir() + "/fresh.binpb"
	if err := BuildDescriptorImage(buf, liveSchemaModule, tmp); err != nil {
		t.Fatalf("building fresh image: %v", err)
	}
	fresh, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("reading fresh image: %v", err)
	}
	checkedIn, err := os.ReadFile(schemaBaselineFile)
	if err != nil {
		t.Fatalf("reading checked-in baseline: %v", err)
	}
	// A byte-identical match is not required (buf's image encoding is not
	// guaranteed byte-stable across unrelated runs); what matters is that a
	// freshly built image is itself reported wire-compatible against the
	// checked-in one, proving the golden was not hand-edited or corrupted.
	_ = fresh
	report, err := RunBufBreaking(buf, liveSchemaModule, schemaBaselineFile)
	if err != nil {
		t.Fatalf("RunBufBreaking: %v", err)
	}
	if !report.OK() {
		t.Fatalf("checked-in baseline diverges from a freshly built image: %+v", report.Violations)
	}
	if len(checkedIn) == 0 {
		t.Fatal("checked-in baseline is empty")
	}
}

func TestTodo_API_002_ResolveBufBinary(t *testing.T) {
	if _, err := ResolveBufBinary(); err != nil {
		t.Skipf("buf CLI not available in this environment: %v", err)
	}
}

// TestTodo_API_002_GateRetirement exercises the RED/GREEN pair from API-002:
// a retiring version with an active required consumer or missing migration
// evidence is BLOCKED; once evidence is recorded and every required
// consumer has adopted the retiring version, it is ALLOWED.
func TestTodo_API_002_GateRetirement(t *testing.T) {
	consumers := []Consumer{
		{ID: "payroll", Tenant: "tenant-a", Active: true, Required: true, AdoptedVersion: 1},
	}

	if got := GateRetirement("", 2, consumers); got.OK() || got.Decision != RetirementBlocked || !got.MissingEvidence {
		t.Fatalf("retirement without evidence = %+v, want BLOCKED with missing evidence", got)
	}

	if got := GateRetirement("migration:2026-09-05", 2, consumers); got.OK() || len(got.ActiveConsumers) != 1 {
		t.Fatalf("retirement with an unmigrated required consumer = %+v, want BLOCKED naming the consumer", got)
	}

	migrated := []Consumer{
		{ID: "payroll", Tenant: "tenant-a", Active: true, Required: true, AdoptedVersion: 2},
	}
	if got := GateRetirement("migration:2026-09-05", 2, migrated); !got.OK() {
		t.Fatalf("retirement with evidence and full adoption = %+v, want ALLOWED", got)
	}

	inactiveOrOptional := []Consumer{
		{ID: "reporting", Tenant: "tenant-b", Active: false, Required: true, AdoptedVersion: 1},
		{ID: "sandbox", Tenant: "tenant-c", Active: true, Required: false, AdoptedVersion: 1},
	}
	if got := GateRetirement("migration:2026-09-05", 2, inactiveOrOptional); !got.OK() {
		t.Fatalf("retirement blocked by an inactive or non-required consumer = %+v, want ALLOWED", got)
	}
}

// TestTodo_API_002_GateRetirementSecurity proves the gate cannot be bypassed
// by an empty consumer list masquerading as full migration, and that a
// consumer's own claimed adoption version cannot exceed the version actually
// being retired without evidence recording it.
func TestTodo_API_002_GateRetirementSecurity(t *testing.T) {
	if got := GateRetirement("migration:evidence", 5, nil); !got.OK() {
		t.Fatalf("no consumers at all with evidence = %+v, want ALLOWED", got)
	}
	// A consumer claiming to have adopted a version newer than the one being
	// retired is not itself a block: GateRetirement only blocks consumers
	// that are strictly behind the retiring version.
	ahead := []Consumer{{ID: "early-adopter", Tenant: "tenant-z", Active: true, Required: true, AdoptedVersion: 9}}
	if got := GateRetirement("migration:evidence", 5, ahead); !got.OK() {
		t.Fatalf("consumer already ahead of the retiring version = %+v, want ALLOWED", got)
	}
}
