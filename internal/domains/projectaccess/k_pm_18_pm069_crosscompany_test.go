package projectaccess

import (
	"testing"
	"time"
)

func crossCompanyFixture(t *testing.T) CrossCompanyProject {
	t.Helper()
	project, err := AdmitCrossCompany(CrossCompanyRequest{ProjectID: "project-1", HostTenant: "host", HostOwner: "owner", GuestTenant: "guest", HostAccepted: true, GuestAccepted: true, Classification: 2, EgressClasses: []string{"INTERNAL"}, AdmittedAt: time.Unix(100, 0), ExpiresAt: time.Unix(1000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	project, err = project.Grant("owner", "guest", "guest-user", ExternalViewer, time.Unix(110, 0), time.Unix(900, 0))
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func TestTodo_PM_069(t *testing.T) {
	project := crossCompanyFixture(t)
	for _, surface := range []CrossCompanySurface{SurfaceProject, SurfaceTask, SurfaceLinkedDocument, CrossCompanySurfaceSearch, SurfaceRetainedStream} {
		if decision := project.Authorize("guest", "guest-user", surface, time.Unix(200, 0)); !decision.Allowed {
			t.Fatalf("delegated guest denied for %s: %+v", surface, decision)
		}
	}
	if decision := project.Authorize("host", "owner", SurfaceProject, time.Unix(200, 0)); !decision.Allowed || decision.Reason != "host_owner" {
		t.Fatalf("host owner decision = %+v", decision)
	}
	oneSided := CrossCompanyRequest{ProjectID: "project-1", HostTenant: "host", HostOwner: "owner", GuestTenant: "guest", HostAccepted: true, Classification: 2, EgressClasses: []string{"INTERNAL"}, AdmittedAt: time.Unix(100, 0), ExpiresAt: time.Unix(1000, 0)}
	if _, err := AdmitCrossCompany(oneSided); err == nil {
		t.Fatal("one-sided admission accepted")
	}
	sameTenant := oneSided
	sameTenant.GuestTenant = "host"
	sameTenant.GuestAccepted = true
	if _, err := AdmitCrossCompany(sameTenant); err == nil {
		t.Fatal("same-tenant admission accepted")
	}
	noEgress := oneSided
	noEgress.GuestAccepted = true
	noEgress.EgressClasses = nil
	if _, err := AdmitCrossCompany(noEgress); err == nil {
		t.Fatal("admission without egress agreement accepted")
	}
	if _, err := project.Grant("guest-user", "guest", "guest-intruder", ExternalViewer, time.Unix(200, 0), time.Unix(800, 0)); err == nil {
		t.Fatal("non-owner grant accepted")
	}
	if _, err := project.Grant("owner", "guest", "guest-late", ExternalViewer, time.Unix(200, 0), time.Unix(1001, 0)); err == nil {
		t.Fatal("grant beyond admission expiry accepted")
	}
	if _, err := project.Revoke("guest-user", time.Unix(300, 0), "takeover"); err == nil {
		t.Fatal("guest revocation accepted")
	}
	if _, err := project.Revoke("owner", time.Unix(300, 0), "  "); err == nil {
		t.Fatal("revocation without reason accepted")
	}
}

func TestTodo_PM_069_Conformance(t *testing.T) {
	project := crossCompanyFixture(t)
	if decision := project.CheckEgress("guest", "INTERNAL", time.Unix(200, 0)); !decision.Allowed {
		t.Fatalf("agreed egress denied: %+v", decision)
	}
	if decision := project.CheckEgress("guest", "MEDICAL", time.Unix(200, 0)); decision.Allowed {
		t.Fatal("classification outside bilateral egress agreement was allowed")
	}
	expired := project.Authorize("guest", "guest-user", SurfaceTask, time.Unix(1000, 0))
	if expired.Allowed || expired.Reason != "expired" {
		t.Fatalf("expired delegation = %+v", expired)
	}
	lapsed := project.Authorize("guest", "guest-user", SurfaceTask, time.Unix(950, 0))
	if lapsed.Allowed || lapsed.Reason != "no_active_delegation" {
		t.Fatalf("lapsed grant inside live admission = %+v", lapsed)
	}
}

func TestTodo_PM_069_Security(t *testing.T) {
	project := crossCompanyFixture(t)
	revoked, err := project.Revoke("owner", time.Unix(300, 0), "access review")
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range []CrossCompanySurface{SurfaceProject, SurfaceTask, SurfaceLinkedDocument, CrossCompanySurfaceSearch, SurfaceRetainedStream} {
		if decision := revoked.Authorize("guest", "guest-user", surface, time.Unix(301, 0)); decision.Allowed || decision.Reason != "revoked" {
			t.Fatalf("revoked access for %s = %+v", surface, decision)
		}
	}
	if decision := revoked.CheckEgress("guest", "INTERNAL", time.Unix(301, 0)); decision.Allowed {
		t.Fatal("revoked egress remained usable")
	}
	if decision := revoked.Authorize("other", "guest-user", CrossCompanySurfaceSearch, time.Unix(301, 0)); decision.Allowed {
		t.Fatal("foreign tenant gained search access")
	}
}
