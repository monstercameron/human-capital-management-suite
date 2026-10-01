package clockservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type rosterDeviceFixture struct {
	DeviceStore
	device     DeviceRecord
	err        error
	calls      int
	tenant, id string
}

func (f *rosterDeviceFixture) GetDevice(_ context.Context, tenant, id string) (DeviceRecord, error) {
	f.calls++
	f.tenant, f.id = tenant, id
	return f.device, f.err
}

type rosterSourceFixture struct {
	delta                         RosterDelta
	err                           error
	calls                         int
	tenant, site, profile, cursor string
	now                           time.Time
}

func (f *rosterSourceFixture) Delta(_ context.Context, tenant, site, profile, cursor string, now time.Time) (RosterDelta, error) {
	f.calls++
	f.tenant, f.site, f.profile, f.cursor, f.now = tenant, site, profile, cursor, now
	return f.delta, f.err
}

func rosterPrincipal(t *testing.T, kind trust.SubjectKind, client string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("ironridge"), Subject: "clock-machine", ClientID: client,
		SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "device-session", CredentialDigest: "verified-credential",
		IssuedAt:  time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func rosterServiceFixture() (Service, *rosterDeviceFixture, *rosterSourceFixture) {
	d := &rosterDeviceFixture{device: DeviceRecord{
		TenantID: "ironridge", ID: "kiosk-1", SiteID: "riverside", ProfileID: "managed-kiosk/v1",
		State: "ACTIVE", Timezone: "America/New_York", Revision: 1, PublicKey: make([]byte, 32),
	}}
	r := &rosterSourceFixture{delta: RosterDelta{
		SnapshotRevision: "revision-7", MaxOfflineAge: time.Hour, PunchPolicyVersion: 2,
		Workers:   []RosterWorker{{WorkerRef: "worker-1", DisplayName: "Sam", PINHash: make([]byte, 32), PINSalt: make([]byte, 16)}},
		JobCodes:  []JobCode{{Code: "job-1", Name: "Site work"}},
		Strings:   map[string]string{"clock.in": "Clock in"},
		Questions: punchpolicy.QuestionSet{ID: "out", Version: 1, Jurisdiction: "US-CA", Questions: []punchpolicy.Question{{ID: "meal", Kind: punchpolicy.QuestionBreakProvided, TextKey: "meal", Options: []string{"yes", "no"}}}},
	}}
	s := Service{Devices: d, Roster: r, Clock: func() time.Time { return time.Date(2026, 9, 28, 11, 0, 0, 0, time.UTC) }}
	return s, d, r
}

func TestTodo_TCLOCK_004(t *testing.T) {
	s, d, r := rosterServiceFixture()
	got, err := s.SyncRoster(context.Background(), rosterPrincipal(t, trust.SubjectKindService, "kiosk-1"), "kiosk-1", "opaque-cursor")
	if err != nil {
		t.Fatal(err)
	}
	if d.tenant != "ironridge" || r.tenant != "ironridge" || r.site != "riverside" || r.profile != "managed-kiosk/v1" || r.cursor != "opaque-cursor" || r.now != s.now() {
		t.Fatalf("scope did not come from verified device: device=%+v source=%+v", d, r)
	}
	if got.SnapshotRevision != "revision-7" || got.MaxOfflineAge != time.Hour || got.PunchPolicyVersion != 2 {
		t.Fatalf("missing policy metadata: %+v", got)
	}
	got.Workers[0].PINHash[0] = 9
	got.Workers[0].PINSalt[0] = 9
	got.JobCodes[0].Name = "changed"
	got.Strings["clock.in"] = "changed"
	got.Questions.Questions[0].Options[0] = "changed"
	if r.delta.Workers[0].PINHash[0] != 0 || r.delta.Workers[0].PINSalt[0] != 0 || r.delta.JobCodes[0].Name != "Site work" || r.delta.Strings["clock.in"] != "Clock in" || r.delta.Questions.Questions[0].Options[0] != "yes" {
		t.Fatal("returned roster aliases source state")
	}
}

func TestTodo_TCLOCK_004_Security(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Service, *rosterDeviceFixture)
		kind   trust.SubjectKind
		client string
		want   error
	}{
		{name: "human", kind: trust.SubjectKindHuman, client: "kiosk-1", want: ErrDeviceNotEligible},
		{name: "wrong machine", kind: trust.SubjectKindService, client: "kiosk-2", want: ErrDeviceNotEligible},
		{name: "cross tenant row", kind: trust.SubjectKindService, client: "kiosk-1", change: func(_ *Service, d *rosterDeviceFixture) { d.device.TenantID = "other" }, want: ErrDeviceNotEligible},
		{name: "revoked", kind: trust.SubjectKindService, client: "kiosk-1", change: func(_ *Service, d *rosterDeviceFixture) { d.device.State = "REVOKED" }, want: ErrDeviceNotEligible},
		{name: "suspended", kind: trust.SubjectKindService, client: "kiosk-1", change: func(_ *Service, d *rosterDeviceFixture) { d.device.State = "SUSPENDED" }, want: ErrDeviceNotEligible},
		{name: "bad zone", kind: trust.SubjectKindService, client: "kiosk-1", change: func(_ *Service, d *rosterDeviceFixture) { d.device.Timezone = "not-a-zone" }, want: ErrDeviceNotEligible},
		{name: "expired principal", kind: trust.SubjectKindService, client: "kiosk-1", change: func(s *Service, _ *rosterDeviceFixture) {
			s.Clock = func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }
		}, want: ErrInvalidPrincipal},
		{name: "clock missing", kind: trust.SubjectKindService, client: "kiosk-1", change: func(s *Service, _ *rosterDeviceFixture) { s.Clock = nil }, want: ErrUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, d, r := rosterServiceFixture()
			if tc.change != nil {
				tc.change(&s, d)
			}
			_, err := s.SyncRoster(context.Background(), rosterPrincipal(t, tc.kind, tc.client), "kiosk-1", "")
			if !errors.Is(err, tc.want) || r.calls != 0 {
				t.Fatalf("err=%v source calls=%d", err, r.calls)
			}
		})
	}
}

func TestTodo_TCLOCK_004_Property(t *testing.T) {
	for size := 0; size < 32; size++ {
		t.Run(strings.Repeat("x", size)+"short verifier", func(t *testing.T) {
			s, _, r := rosterServiceFixture()
			r.delta.Workers[0].PINHash = make([]byte, size)
			if _, err := s.SyncRoster(context.Background(), rosterPrincipal(t, trust.SubjectKindService, "kiosk-1"), "kiosk-1", ""); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("hash size %d accepted: %v", size, err)
			}
		})
	}
	mutations := []struct {
		name   string
		change func(*RosterDelta)
	}{
		{"no offline expiry", func(d *RosterDelta) { d.MaxOfflineAge = 0 }},
		{"duplicate worker", func(d *RosterDelta) { d.Workers = append(d.Workers, d.Workers[0]) }},
		{"terminated verifier", func(d *RosterDelta) { d.Workers[0].Terminated = true }},
		{"urgent removal still active", func(d *RosterDelta) { d.UrgentRemovals = []string{"worker-1"} }},
		{"missing pagination cursor", func(d *RosterDelta) { d.HasMore = true }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			s, _, r := rosterServiceFixture()
			tc.change(&r.delta)
			if _, err := s.SyncRoster(context.Background(), rosterPrincipal(t, trust.SubjectKindService, "kiosk-1"), "kiosk-1", ""); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("invalid source accepted: %v", err)
			}
		})
	}
}

func TestTodo_TCLOCK_004_Recovery(t *testing.T) {
	s, d, r := rosterServiceFixture()
	p := rosterPrincipal(t, trust.SubjectKindService, "kiosk-1")
	if _, err := s.SyncRoster(context.Background(), p, "kiosk-1", ""); err != nil {
		t.Fatal(err)
	}
	d.device.SiteID = "new-site"
	d.device.Revision++
	r.delta.Workers = []RosterWorker{{WorkerRef: "worker-1", Terminated: true}}
	r.delta.UrgentRemovals = []string{"worker-1"}
	got, err := s.SyncRoster(context.Background(), p, "kiosk-1", "saved-cursor")
	if err != nil {
		t.Fatal(err)
	}
	if r.site != "new-site" || len(got.UrgentRemovals) != 1 || !got.Workers[0].Terminated {
		t.Fatalf("stale site or termination: %+v", got)
	}
	d.device.State = "REVOKED"
	if _, err := s.SyncRoster(context.Background(), p, "kiosk-1", got.NextCursor); !errors.Is(err, ErrDeviceNotEligible) {
		t.Fatalf("revoked device sync: %v", err)
	}
}
