package health

import (
	"strings"
	"testing"
	"time"
)

func TestTodo_INTG_017(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	r, err := Project(Input{TenantID: "tenant-a", ConnectionID: "conn-a", Now: now, Signals: []Signal{
		{Kind: Authentication, Status: Healthy, Watermark: now.Add(-time.Minute)},
		{Kind: Queue, Status: Degraded, Cause: "queue age exceeded budget", Watermark: now, Workflow: "Promotion", Capability: "promotion.execute", Deadline: now.Add(time.Hour)},
	}})
	if err != nil || r.Status != Degraded || len(r.Causes) != 1 || len(r.Impacts) != 1 {
		t.Fatalf("unexpected projection: %#v err=%v", r, err)
	}
	if r.Digest == "" || !strings.Contains(Explain(r), "DEGRADED") {
		t.Fatalf("projection is not explainable: %#v", r)
	}
}

func TestTodo_REV_013_02_Security(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	secrets := []string{
		"Bearer secret-token-value",
		"provider denied access_token=secret-value&scope=worker.read",
		"client_secret:secret-value",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJvcCJ9.signaturevalue123456",
		"-----BEGIN PRIVATE KEY-----",
	}
	for i, secret := range secrets {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			report, err := Project(Input{TenantID: "tenant-a", ConnectionID: "conn-a", Now: now, Signals: []Signal{{
				Kind: Authentication, Status: Incident, Cause: "provider check failed: " + secret,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(report.Causes[0].Detail, secret) || strings.Contains(report.Digest, secret) {
				t.Fatalf("report exposed credential material: %+v", report)
			}
			if report.Causes[0].Detail != "dependency reported sensitive diagnostic detail" {
				t.Fatalf("sensitive provider detail was not replaced: %q", report.Causes[0].Detail)
			}
		})
	}

	clean, err := Project(Input{TenantID: "tenant-a", ConnectionID: "conn-a", Now: now, Signals: []Signal{{
		Kind: Queue, Status: Degraded, Cause: "queue age exceeded budget",
	}}})
	if err != nil || clean.Causes[0].Detail != "queue age exceeded budget" {
		t.Fatalf("ordinary operational detail was changed: report=%+v err=%v", clean, err)
	}
}

func TestTodo_INTG_017_Golden(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	r, err := Project(Input{TenantID: "t", ConnectionID: "c", Now: now, Signals: []Signal{
		{Kind: Error, Status: Incident, Cause: "provider unavailable"},
		{Kind: Authentication, Status: Healthy},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Incident || r.Causes[0].Code != "INCIDENT_ERROR" || r.Causes[0].Detail != "provider unavailable" {
		t.Fatalf("golden mismatch: %#v", r)
	}
}

func TestTodo_INTG_017_Integration(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	report, err := Project(Input{TenantID: "tenant-integration", ConnectionID: "connection-integration", Now: now, Signals: []Signal{
		{Kind: Authentication, Status: Healthy, Watermark: now},
		{Kind: Permission, Status: Incident, Cause: "required scope was revoked", Workflow: "Payroll close", Capability: "payroll.submit"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != Incident || report.TenantID != "tenant-integration" || report.ConnectionID != "connection-integration" || len(report.Impacts) != 1 || report.Impacts[0].Workflow != "Payroll close" {
		t.Fatalf("permission incident did not project to its workflow: %+v", report)
	}
}

func TestTodo_INTG_017_Fault(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	r, err := Project(Input{TenantID: "t", ConnectionID: "c", Now: now, Signals: []Signal{{Kind: Sync, Status: Healthy, FreshUntil: now.Add(-time.Second)}}})
	if err != nil || r.Status != Unknown || r.Causes[0].Code != "UNKNOWN_SYNC" {
		t.Fatalf("stale signal was not unknown: %#v err=%v", r, err)
	}
}

func TestTodo_INTG_017_Race(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	in := Input{TenantID: "t", ConnectionID: "c", Now: now, Signals: []Signal{{Kind: Permission, Status: Degraded, Cause: "scope missing"}}}
	done := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		go func() { _, _ = Project(in); done <- struct{}{} }()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

func FuzzTodo_INTG_017(f *testing.F) {
	f.Add("provider unavailable")
	f.Fuzz(func(t *testing.T, cause string) {
		r, err := Project(Input{TenantID: "t", ConnectionID: "c", Now: time.Unix(10, 0), Signals: []Signal{{Kind: Error, Status: Incident, Cause: cause}}})
		if err != nil || r.Digest == "" || strings.Contains(r.Digest, cause) {
			t.Fatalf("invalid projection: %#v err=%v", r, err)
		}
	})
}

func BenchmarkTodo_INTG_017(b *testing.B) {
	in := Input{TenantID: "t", ConnectionID: "c", Now: time.Unix(10, 0), Signals: []Signal{{Kind: Queue, Status: Degraded, Workflow: "Promotion"}}}
	for i := 0; i < b.N; i++ {
		_, _ = Project(in)
	}
}
