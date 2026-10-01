package projectaccess

import (
	"errors"
	"testing"
	"time"
)

func pm17Policy(tenant TenantID, admin UserID, consent bool) ExternalProjectPolicy {
	return ExternalProjectPolicy{
		TenantID: tenant, PolicyAdministrator: admin, Consent: consent,
		ClassificationCeiling: 2, EgressProfile: "project-safe/us", RetentionClass: "project-90d",
		ExportAllowed: true, GuestRoleCeiling: ExternalGuestContributor, RevocationEpoch: 3,
	}
}

func pm17Project(t *testing.T) BilateralProjectAdmission {
	t.Helper()
	project, err := AdmitBilateralProject("project-cross", "host-co", "host-owner", 1, "home-co", pm17Policy("host-co", "host-admin", true), pm17Policy("home-co", "home-admin", true))
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func TestTodo_PM_064(t *testing.T) {
	project := pm17Project(t)
	if !project.Admission.Active || project.Admission.HostTenant != "host-co" || project.Admission.HomeTenant != "home-co" || project.Admission.EgressProfile != "project-safe/us" || project.Admission.RetentionClass != "project-90d" {
		t.Fatalf("admission did not bind bilateral project policy: %+v", project.Admission)
	}
	if _, err := project.InviteExternalGuest("host-co", "host-owner", "guest-1", "guest", "home-co", ExternalGuestViewer, time.Now().Add(time.Hour), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PM_064_Security(t *testing.T) {
	host := pm17Policy("host-co", "host-admin", true)
	withoutHomeConsent := pm17Policy("home-co", "home-admin", false)
	if _, err := AdmitBilateralProject("project-cross", "host-co", "host-owner", 1, "home-co", host, withoutHomeConsent); !errors.Is(err, ErrInvalidExternalPolicy) {
		t.Fatalf("unilateral admission error = %v, want bilateral policy denial", err)
	}
	if _, err := AdmitBilateralProject("project-cross", "host-co", "host-owner", 1, "home-co", host, pm17Policy("home-co", "home-admin", true)); err != nil {
		t.Fatal(err)
	}
	// A caller that only has a chat relationship has no project admission
	// record, so it cannot manufacture a guest read grant.
	project := pm17Project(t)
	if _, err := project.AuthorizeExternalGuestRead("chat-member", "chat-user", "home-co", time.Now(), project.Admission.RevocationEpoch, 1); !errors.Is(err, ErrExternalGuestAccess) {
		t.Fatalf("chat-only principal received project access: %v", err)
	}
}

func TestTodo_PM_064_Integration(t *testing.T) {
	project := pm17Project(t)
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	project, err := project.InviteExternalGuest("host-co", "host-owner", "guest-1", "guest", "home-co", ExternalGuestContributor, now.Add(2*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	project, err = project.AcceptExternalGuest("guest-1", "guest", "home-co", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	grant, err := project.AuthorizeExternalGuestRead("guest-1", "guest", "home-co", now.Add(time.Minute), project.Admission.RevocationEpoch, 1)
	if err != nil || grant.CursorEpoch == 0 || !grant.ExportPermitted || grant.HomeTenant != "home-co" {
		t.Fatalf("authorized guest read grant = %+v, err=%v", grant, err)
	}
}

func TestTodo_PM_064_Golden(t *testing.T) {
	project := pm17Project(t)
	if len(project.Evidence) != 0 || project.Admission.Revision != 1 || project.Admission.RevocationEpoch != 1 {
		t.Fatalf("initial admission unexpectedly has mutable evidence or epoch: %+v", project)
	}
	if project.Admission.HostPolicy.TenantID != project.Admission.HostTenant || project.Admission.HomePolicy.TenantID != project.Admission.HomeTenant {
		t.Fatalf("admission policy tenants are not preserved: %+v", project.Admission)
	}
}

func TestTodo_PM_065(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	project := pm17Project(t)
	project, err := project.InviteExternalGuest("host-co", "host-owner", "guest-1", "guest", "home-co", ExternalGuestViewer, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	project, err = project.AcceptExternalGuest("guest-1", "guest", "home-co", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	project, err = project.ChangeExternalGuestRole("host-co", "host-owner", "guest-1", ExternalGuestContributor, now.Add(2*time.Minute))
	if err != nil || project.Guests[0].Role != ExternalGuestContributor {
		t.Fatalf("delegated guest role = %+v, err=%v", project.Guests, err)
	}
	if _, err := project.AuthorizeExternalGuestRead("guest-1", "guest", "home-co", now.Add(2*time.Minute), project.Admission.RevocationEpoch, 1); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PM_065_Security(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	project := pm17Project(t)
	project, err := project.InviteExternalGuest("host-co", "host-owner", "guest-1", "guest", "home-co", ExternalGuestViewer, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	project, err = project.AcceptExternalGuest("guest-1", "guest", "home-co", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	project, err = project.OffboardExternalGuest("home-co", "guest-1", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.AuthorizeExternalGuestRead("guest-1", "guest", "home-co", now.Add(2*time.Minute), project.Admission.RevocationEpoch, 1); !errors.Is(err, ErrExternalGuestAccess) {
		t.Fatalf("offboarded guest retained read/cursor access: %v", err)
	}
	if project.Guests[0].State != ExternalGuestRevoked || project.Guests[0].SessionEpoch < 2 {
		t.Fatalf("home offboarding did not invalidate guest session: %+v", project.Guests[0])
	}
}

func TestTodo_PM_065_Recovery(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	project := pm17Project(t)
	project, err := project.InviteExternalGuest("host-co", "host-owner", "guest-expired", "guest", "home-co", ExternalGuestViewer, now.Add(time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.AcceptExternalGuest("guest-expired", "guest", "home-co", now.Add(time.Minute)); !errors.Is(err, ErrExternalGuestAccess) {
		t.Fatalf("expired invite accepted: %v", err)
	}
	if project.Guests[0].State != ExternalGuestInvited {
		t.Fatalf("failed expired acceptance mutated caller state: %+v", project.Guests[0])
	}
}

func TestTodo_PM_065_Golden(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	project := pm17Project(t)
	project, err := project.InviteExternalGuest("host-co", "host-owner", "guest-1", "guest", "home-co", ExternalGuestViewer, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Evidence) != 1 || project.Evidence[0].Event != "GUEST_INVITED" || project.Evidence[0].ActorTenant != "host-co" || project.Evidence[0].RecordedAt.Location() != time.UTC {
		t.Fatalf("guest evidence = %+v", project.Evidence)
	}
}
