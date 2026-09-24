package delivery

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type domainRepoFake struct {
	domains  []ActiveSendingDomain
	saved    []ActiveSendingDomain
	err      error
	saveErr  error
	beginErr error
}

func (f *domainRepoFake) ListActive(context.Context) ([]ActiveSendingDomain, error) {
	return append([]ActiveSendingDomain(nil), f.domains...), f.err
}
func (f *domainRepoFake) BeginCheck(_ context.Context, profile ActiveSendingDomain, at time.Time) error {
	profile.LastCheckedAt = at
	profile.Profile.Verified = false
	profile.Profile.SPFVerified = false
	profile.Profile.DKIMVerified = false
	profile.Profile.DMARCVerified = false
	if profile.AlertCycle == "" {
		profile.AlertCycle = at.Format(time.RFC3339Nano)
	}
	f.saved = append(f.saved, profile)
	if f.beginErr != nil {
		return f.beginErr
	}
	return f.err
}
func (f *domainRepoFake) SaveProfile(_ context.Context, profile ActiveSendingDomain) error {
	f.saved = append(f.saved, profile)
	if f.saveErr != nil {
		return f.saveErr
	}
	return f.err
}

type dnsResolverFake struct {
	records DNSRecords
	err     error
}

func (f dnsResolverFake) Resolve(context.Context, DomainProfile) (DNSRecords, error) {
	return f.records, f.err
}

type domainAlertFake struct {
	raised, resolved int
	reason           string
	err              error
}

type txtResolverFake struct {
	names    []string
	records  map[string][]string
	failName string
}

func (r *txtResolverFake) LookupTXT(_ context.Context, name string) ([]string, error) {
	r.names = append(r.names, name)
	if name == r.failName {
		return nil, errors.New("lookup failed")
	}
	return r.records[name], nil
}

func TestNetDNSResolverReadsAuthenticationRecords(t *testing.T) {
	fake := &txtResolverFake{records: map[string][]string{
		"mail.example.test":               {"v=spf1 include:mail.example.test -all"},
		"_dmarc.mail.example.test":        {"v=DMARC1; p=reject"},
		"s1._domainkey.mail.example.test": {"v=DKIM1; p=public-key"},
	}}
	got, err := (NetDNSResolver{Resolver: fake}).Resolve(context.Background(), DomainProfile{Domain: "mail.example.test", DKIMSelectors: []string{"s1"}})
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"mail.example.test", "_dmarc.mail.example.test", "s1._domainkey.mail.example.test"}
	if !reflect.DeepEqual(fake.names, wantNames) {
		t.Fatalf("DNS lookups=%v want %v", fake.names, wantNames)
	}
	if got.SPF != "v=spf1 include:mail.example.test -all" || got.DMARC != "v=DMARC1; p=reject" || got.DKIMSelectors["s1"] != "v=DKIM1; p=public-key" {
		t.Fatalf("DNS evidence=%+v", got)
	}
}

func TestNetDNSResolverFailsWhenAuthenticationRecordIsMissing(t *testing.T) {
	fake := &txtResolverFake{failName: "_dmarc.mail.example.test", records: map[string][]string{"mail.example.test": {"v=spf1 -all"}}}
	if _, err := (NetDNSResolver{Resolver: fake}).Resolve(context.Background(), DomainProfile{Domain: "mail.example.test", DKIMSelectors: []string{"s1"}}); err == nil {
		t.Fatal("missing DMARC TXT was accepted")
	}
}

func (f *domainAlertFake) Raise(_ context.Context, _ ActiveSendingDomain, reason string, _ time.Time) error {
	f.raised++
	f.reason = reason
	return f.err
}
func (f *domainAlertFake) Resolve(context.Context, ActiveSendingDomain, time.Time) error {
	f.resolved++
	return f.err
}

func TestTodo_REV_058_01(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	profile, err := VerifyDomain("mail.example.test", DNSRecords{SPF: "v=spf1 include:mail.example.test -all", DKIMSelectors: map[string]string{"s1": "v=DKIM1; p=key-one"}, DMARC: "v=DMARC1; p=reject"}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	repo := &domainRepoFake{domains: []ActiveSendingDomain{{TenantID: "tenant-1", Profile: profile, Owner: "mail-ops", AlertCycle: now.Add(-time.Minute).Format(time.RFC3339Nano)}}}
	alerts := &domainAlertFake{}
	service := DomainReverifier{Profiles: repo, DNS: dnsResolverFake{records: DNSRecords{SPF: "v=spf1 include:mail.example.test -all", DKIMSelectors: map[string]string{"s1": "v=DKIM1; p=key-rotated"}, DMARC: "v=DMARC1; p=reject"}}, Alerts: alerts, Now: func() time.Time { return now }}
	if err := service.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.saved) != 3 {
		t.Fatalf("saved profiles=%d, expected permit revocation, verification and alert-cycle cleanup", len(repo.saved))
	}
	updated := repo.saved[1].Profile
	if !updated.Verified || updated.Version != profile.Version+1 || updated.PreviousDigest != profile.Digest || updated.RecordDigest == profile.RecordDigest {
		t.Fatalf("rotation not persisted: before=%+v after=%+v", profile, updated)
	}
	dns := DNSRecords{SPF: "v=spf1 include:mail.example.test -all", DKIMSelectors: map[string]string{"s1": "v=DKIM1; p=key-rotated"}, DMARC: "v=DMARC1; p=reject"}
	wantRotation, err := profile.Rotate(dns, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated, wantRotation) {
		t.Fatalf("DNS drift did not use DomainProfile.Rotate: got=%+v want=%+v", updated, wantRotation)
	}
	if alerts.resolved != 1 || alerts.raised != 0 {
		t.Fatalf("alert lifecycle raised=%d resolved=%d", alerts.raised, alerts.resolved)
	}
	if repo.saved[len(repo.saved)-1].AlertCycle != "" {
		t.Fatalf("resolved alert cycle remained active: %+v", repo.saved[len(repo.saved)-1])
	}
}

func TestTodo_REV_058_01_Fault(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	profile, err := VerifyDomain("mail.example.test", DNSRecords{SPF: "v=spf1 -all", DKIMSelectors: map[string]string{"s1": "key"}, DMARC: "v=DMARC1; p=reject"}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	repo := &domainRepoFake{domains: []ActiveSendingDomain{{TenantID: "tenant-1", Profile: profile, Owner: "mail-ops"}}, saveErr: errors.New("transient profile write failure")}
	alerts := &domainAlertFake{}
	service := DomainReverifier{Profiles: repo, DNS: dnsResolverFake{err: errors.New("resolver timeout")}, Alerts: alerts, Now: func() time.Time { return now }}
	err = service.RunOnce(context.Background())
	if !errors.Is(err, ErrDomainUnverified) || len(repo.saved) != 2 || repo.saved[0].Profile.Verified || repo.saved[0].Profile.SPFVerified || repo.saved[0].Profile.DKIMVerified || repo.saved[0].Profile.DMARCVerified || repo.saved[1].Profile.Verified || repo.saved[1].AlertCycle == "" || alerts.raised != 1 || alerts.reason == "" {
		t.Fatalf("DNS fault did not close send gate and raise owned alert: err=%v saved=%+v alerts=%+v", err, repo.saved, alerts)
	}
}

func TestParseDKIMTXTRejectsSpoofedAndMalformedRecords(t *testing.T) {
	for _, record := range []string{
		"random text with p=spoofed",
		"v=DKIM2; p=spoofed",
		"p=key; v=DKIM1",
		"v=DKIM1; p=",
		"v=DKIM1; p=key; p=spoofed",
		"v=DKIM1; malformed",
	} {
		t.Run(record, func(t *testing.T) {
			if key, err := parseDKIMTXT(record); err == nil {
				t.Fatalf("malformed DKIM TXT accepted as %q", key)
			}
		})
	}
	if key, err := parseDKIMTXT("v=DKIM1; p=actual-key; k=rsa"); err != nil || key == "" {
		t.Fatalf("valid DKIM TXT rejected: key=%q err=%v", key, err)
	}
}
