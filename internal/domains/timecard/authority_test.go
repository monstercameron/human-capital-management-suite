package timecard

import (
	"errors"
	"testing"
	"time"
)

// TestTodo_WF_CAP_007_Authority is the RED test for the per-tenant
// authority decision: every tenant records NATIVE or DELEGATED timecard
// authority, a delegated tenant names its external source system, and a
// native tenant must not.
func TestTodo_WF_CAP_007_Authority(t *testing.T) {
	now := time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC)

	native, err := RecordAuthority("tenant-a", AuthorityNative, "", "operator-1", now)
	if err != nil {
		t.Fatalf("RecordAuthority native: %v", err)
	}
	if native.Tenant != "tenant-a" || native.Class != AuthorityNative || !native.Native() {
		t.Fatalf("native record = %+v, want tenant-a/NATIVE", native)
	}

	delegated, err := RecordAuthority("tenant-b", AuthorityDelegated, "acme-timekeeping", "operator-1", now)
	if err != nil {
		t.Fatalf("RecordAuthority delegated: %v", err)
	}
	if delegated.Tenant != "tenant-b" || delegated.Class != AuthorityDelegated || delegated.Native() {
		t.Fatalf("delegated record = %+v, want tenant-b/DELEGATED", delegated)
	}
	if delegated.SourceSystem != "acme-timekeeping" {
		t.Fatalf("delegated source = %q, want acme-timekeeping", delegated.SourceSystem)
	}

	cases := []struct {
		name   string
		tenant string
		class  AuthorityClass
		source string
		by     string
		at     time.Time
	}{
		{"blank tenant", "", AuthorityNative, "", "operator-1", now},
		{"invalid class", "tenant-a", "MAYBE", "", "operator-1", now},
		{"delegated without source", "tenant-b", AuthorityDelegated, "", "operator-1", now},
		{"native with source", "tenant-a", AuthorityNative, "acme-timekeeping", "operator-1", now},
		{"blank decider", "tenant-a", AuthorityNative, "", "", now},
		{"zero time", "tenant-a", AuthorityNative, "", "operator-1", time.Time{}},
	}
	for _, c := range cases {
		if _, err := RecordAuthority(c.tenant, c.class, c.source, c.by, c.at); !errors.Is(err, ErrTimecardRejected) {
			t.Fatalf("%s: got %v, want ErrTimecardRejected", c.name, err)
		}
	}
}
