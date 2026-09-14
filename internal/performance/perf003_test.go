package performance

import (
	"testing"
	"time"
)

func perf003Plan() SoakPlan {
	return SoakPlan{
		Tenant:                     "tenant-a",
		Cell:                       "cell-1",
		QuotaLimit:                 100,
		CapacityPerHour:            100,
		RetryAllowance:             60,
		DeclaredPeakCommandsPerSec: 5000,
		UnitCommandsPerSec:         25,
		Stages: []SoakStage{
			{Name: "baseline", VirtualHours: 8, OfferedUnits: 40},
			{Name: "ramp", VirtualHours: 4, OfferedUnits: 80},
			{Name: "peak", VirtualHours: 4, OfferedUnits: 120},
			{Name: "burst", VirtualHours: 2, OfferedUnits: 120, Burst: true},
			{Name: "restart", VirtualHours: 2, OfferedUnits: 60, Restart: true},
			{Name: "drain", VirtualHours: 4, OfferedUnits: 40},
		},
		P99Ceiling:   500 * time.Millisecond,
		MaxErrorRate: 0,
		// Ten large batched windows per hour: hiccups dilute away
		// while per-op means stay exact.
		OpsPerHour: 10 * soakBatchOps,
	}
}

// TestTodo_PERF_003 runs the staged 24-virtual-hour soak — baseline,
// ramp, peak, double burst, restart and drain — and proves bounded
// admission with no leaks, no duplicate effects and no deterioration.
func TestTodo_PERF_003(t *testing.T) {
	plan := perf003Plan()
	first, err := RunSoak(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Hours) != 24 {
		t.Fatalf("hours=%d", len(first.Hours))
	}
	if first.TotalEffects == 0 || first.DuplicateEffects != 0 {
		t.Fatalf("verdict=%+v", first)
	}
	if first.LeasesGranted != first.LeasesReleased {
		t.Fatalf("lease drift: granted=%d released=%d", first.LeasesGranted, first.LeasesReleased)
	}
	if first.TotalRetried > first.TotalRejected {
		t.Fatalf("retry amplification: retried=%d rejected=%d", first.TotalRetried, first.TotalRejected)
	}
	if first.HourP99(1) <= 0 || first.HourP99(24) <= 0 {
		t.Fatal("missing hourly p99 samples")
	}
	if first.BaselineP99 <= 0 || first.ClosingP99 <= 0 {
		t.Fatal("missing endpoint-window p99 medians")
	}
	if first.QueueDepth(24) != 0 {
		t.Fatalf("final queue depth=%d", first.QueueDepth(24))
	}
	second, err := RunSoak(plan)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("identical soak plans replayed to different digests")
	}
	// Seeded defect: an undersized peak model must reject with
	// PERF_003_REJECTED — never report pass.
	small := perf003Plan()
	small.DeclaredPeakCommandsPerSec = Peak.CommandsPerSecond - 1
	_, err = RunSoak(small)
	rejected, ok := AsSoakRejected(err)
	if !ok || rejected.Code != SoakRejectedCode {
		t.Fatalf("undersized peak error = %v", err)
	}
	if rejected.Field == "" || rejected.State == "" || rejected.Version == 0 {
		t.Fatalf("rejection names no field/state/version: %+v", rejected)
	}
}
