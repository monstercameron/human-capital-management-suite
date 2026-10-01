package bilateral

import (
	"errors"
	"testing"
	"time"
)

var bilateralNow = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func TestTodo_AGENT_037(t *testing.T) {
	req := installRequest(t)
	installed, err := AuthorizeInstall(req)
	if err != nil {
		t.Fatalf("AuthorizeInstall = %v", err)
	}
	if installed.MembershipRevision != req.MembershipRevision || len(installed.Grants) != 2 || installed.Grants[0].ConsumerTenant != "home-a" || installed.Grants[1].ConsumerTenant != "home-b" {
		t.Fatalf("installation binding = %+v", installed)
	}
	if err := AuthorizeOutput(outputRequest(installed, req.Grants)); err != nil {
		t.Fatalf("current audience denied: %v", err)
	}
	first := req.Grants[0].Terms
	first.Egress = []string{"chat", "agent-output"}
	d1, err := TermsDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	first.Egress = []string{"agent-output", "chat"}
	d2, err := TermsDigest(first)
	if err != nil || d1 != d2 {
		t.Fatalf("set ordering changed the terms digest: %q != %q (%v)", d1, d2, err)
	}
}

func TestTodo_AGENT_037_Security(t *testing.T) {
	base := installRequest(t)
	cases := []struct {
		name   string
		change func(*InstallRequest)
	}{
		{name: "host approval missing", change: func(r *InstallRequest) { r.Grants[0].HostConsent = Consent{} }},
		{name: "home approval missing", change: func(r *InstallRequest) { r.Grants[0].HomeConsent = Consent{} }},
		{name: "grant revision stale", change: func(r *InstallRequest) { r.Grants[0].Version++ }},
		{name: "installation expanded", change: func(r *InstallRequest) { r.Grants[0].Terms.InstallationID = "another-install" }},
		{name: "agent version expanded", change: func(r *InstallRequest) { r.Grants[0].Terms.AgentVersion = "v2" }},
		{name: "egress expanded", change: func(r *InstallRequest) {
			r.Grants[0].Terms.Egress = append(r.Grants[0].Terms.Egress, "external-export")
		}},
		{name: "grant revoked", change: func(r *InstallRequest) { r.Grants[0].RevokedAt = bilateralNow }},
		{name: "consent from wrong tenant", change: func(r *InstallRequest) { r.Grants[0].HomeConsent.TenantID = "host" }},
		{name: "membership tenant lacks consent", change: func(r *InstallRequest) { r.MemberTenants = append(r.MemberTenants, "home-c") }},
		{name: "unilateral install", change: func(r *InstallRequest) { r.Grants[1] = Grant{} }},
		{name: "expired grant", change: func(r *InstallRequest) { r.Now = r.ExpiresAt }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := cloneInstallRequest(base)
			tc.change(&req)
			if _, err := AuthorizeInstall(req); !errors.Is(err, ErrDenied) && !errors.Is(err, ErrInvalid) {
				t.Fatalf("AuthorizeInstall accepted unsafe state: %v", err)
			}
		})
	}
}

func TestTodo_AGENT_037_Integration(t *testing.T) {
	req := installRequest(t)
	installed, err := AuthorizeInstall(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*OutputRequest)
	}{
		{name: "membership changed", change: func(r *OutputRequest) { r.CurrentMembershipRevision++ }},
		{name: "new external audience tenant", change: func(r *OutputRequest) { r.AudienceTenants = append(r.AudienceTenants, "home-c") }},
		{name: "old private output audience omitted", change: func(r *OutputRequest) { r.AudienceTenants = []string{"host", "home-a"} }},
		{name: "grant revision changed", change: func(r *OutputRequest) { r.CurrentGrants[0].Version++ }},
		{name: "purpose changed", change: func(r *OutputRequest) { r.CurrentGrants[0].Terms.Purpose = "payroll.export" }},
		{name: "approval revoked", change: func(r *OutputRequest) { r.CurrentGrants[0].RevokedAt = bilateralNow }},
		{name: "consumer consent withdrawn", change: func(r *OutputRequest) { r.CurrentGrants[0].HomeConsent = Consent{} }},
		{name: "missing grant snapshot", change: func(r *OutputRequest) { r.CurrentGrants = r.CurrentGrants[:1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := outputRequest(installed, req.Grants)
			tc.change(&out)
			if err := AuthorizeOutput(out); !errors.Is(err, ErrDenied) {
				t.Fatalf("AuthorizeOutput = %v, want ErrDenied", err)
			}
		})
	}
}

func TestTodo_AGENT_037_Mutation(t *testing.T) {
	base := installRequest(t)
	mutations := map[string]func(*Terms){
		"conversation":     func(terms *Terms) { terms.ConversationID = "other" },
		"host":             func(terms *Terms) { terms.HostTenant = "other-host" },
		"consumer":         func(terms *Terms) { terms.ConsumerTenant = "other-home" },
		"purpose":          func(terms *Terms) { terms.Purpose = "payroll.export" },
		"region":           func(terms *Terms) { terms.ProcessingRegion = "EU" },
		"retention":        func(terms *Terms) { terms.Retention = "forever" },
		"egress":           func(terms *Terms) { terms.Egress = []string{"external-export"} },
		"incident contact": func(terms *Terms) { terms.IncidentContact = "other-team" },
		"exit behavior":    func(terms *Terms) { terms.ExitBehavior = "retain-access" },
		"expiry":           func(terms *Terms) { terms.ExpiresAt = terms.ExpiresAt.Add(time.Hour) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			req := cloneInstallRequest(base)
			mutate(&req.Grants[0].Terms)
			if _, err := AuthorizeInstall(req); !errors.Is(err, ErrDenied) {
				t.Fatalf("term mutation accepted: %v", err)
			}
		})
	}
}

func TestTodo_AGENT_037_Golden(t *testing.T) {
	req := installRequest(t)
	digest, err := TermsDigest(req.Grants[0].Terms)
	if err != nil {
		t.Fatal(err)
	}
	const want = "d71b1f34ac77cada883ba83cd08ab10ce4eaca7e278086adb071eee05ec9625c"
	if digest != want {
		t.Fatalf("canonical bilateral terms digest = %s, want %s", digest, want)
	}
}

func installRequest(t *testing.T) InstallRequest {
	t.Helper()
	r := InstallRequest{
		ConversationID: "conversation-1", HostTenant: "host", AgentID: "agent-4", AgentVersion: "v7", InstallationID: "install-5",
		Purpose: "benefits.summary", ProcessingRegion: "US-East", Retention: "30-days", Egress: []string{"chat", "agent-output"},
		IncidentContact: "host-security", ExitBehavior: "purge-derived-output", ExpiresAt: bilateralNow.Add(24 * time.Hour),
		MemberTenants: []string{"home-b", "host", "home-a"}, MembershipRevision: 12, Now: bilateralNow,
	}
	r.Grants = []Grant{approvedGrant(t, r, "home-a", "grant-a"), approvedGrant(t, r, "home-b", "grant-b")}
	return r
}

func approvedGrant(t *testing.T, req InstallRequest, consumer, id string) Grant {
	t.Helper()
	terms := termsFor(req, consumer)
	digest, err := TermsDigest(terms)
	if err != nil {
		t.Fatal(err)
	}
	return Grant{ID: id, Version: 3, Terms: terms,
		HostConsent: Consent{TenantID: req.HostTenant, GrantVersion: 3, TermsDigest: digest, AcceptedAt: bilateralNow.Add(-time.Hour)},
		HomeConsent: Consent{TenantID: consumer, GrantVersion: 3, TermsDigest: digest, AcceptedAt: bilateralNow.Add(-time.Hour)}}
}

func outputRequest(installed Installation, grants []Grant) OutputRequest {
	return OutputRequest{Installation: installed, AudienceTenants: []string{"host", "home-a", "home-b"}, CurrentMembershipRevision: installed.MembershipRevision, CurrentGrants: cloneGrants(grants), Now: bilateralNow}
}

func cloneInstallRequest(in InstallRequest) InstallRequest {
	in.MemberTenants = append([]string(nil), in.MemberTenants...)
	in.Egress = append([]string(nil), in.Egress...)
	in.Grants = cloneGrants(in.Grants)
	return in
}

func cloneGrants(in []Grant) []Grant {
	out := append([]Grant(nil), in...)
	for i := range out {
		out[i].Terms.Egress = append([]string(nil), out[i].Terms.Egress...)
	}
	return out
}
