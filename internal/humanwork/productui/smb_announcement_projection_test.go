package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func smbAnnouncementFixture() SMBPublishedAnnouncement {
	return SMBPublishedAnnouncement{TenantID: "tenant-a", AnnouncementID: "ann-1", PublisherID: "publisher-1", AudienceID: "team-support", Message: "Updated travel policy", DocumentVersionID: "policy-v4", DocumentDigest: "sha256:policy-v4", PublishedAt: time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC), DueAt: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC), Deployed: true}
}

func TestTodo_SMB_002(t *testing.T) {
	announcement := smbAnnouncementFixture()
	items := ProjectSMBAnnouncements([]SMBPublishedAnnouncement{announcement}, "tenant-a", "team-support")
	if len(items) != 1 || items[0].DocumentVersionID != "policy-v4" {
		t.Fatalf("targeted projection = %+v", items)
	}
	receipt, err := AcknowledgeSMBAnnouncement(announcement, "tenant-a", "employee-1", "policy-v4", "sha256:policy-v4", time.Now())
	if err != nil || receipt.DocumentVersionID != announcement.DocumentVersionID || receipt.DocumentDigest != announcement.DocumentDigest {
		t.Fatalf("exact-version receipt = %+v, err=%v", receipt, err)
	}
	if SMBAnnouncementReminderDue(announcement, true, announcement.DueAt.Add(time.Hour)) {
		t.Fatal("acknowledged recipient still marked reminder-due")
	}
}

func TestTodo_SMB_002_Browser(t *testing.T) {
	announcement := smbAnnouncementFixture()
	markup, err := ui.RenderToString(SMBAnnouncementCard(announcement, nil, announcement.DueAt.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Updated travel policy", "policy-v4", "sha256:policy-v4", "Acknowledgement due", "not a signature or proof of comprehension"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("announcement card missing %q: %s", want, markup)
		}
	}
}

func TestTodo_SMB_002_Security(t *testing.T) {
	announcement := smbAnnouncementFixture()
	if got := ProjectSMBAnnouncements([]SMBPublishedAnnouncement{announcement}, "tenant-b", "team-support"); len(got) != 0 {
		t.Fatalf("cross-tenant announcement projection = %+v", got)
	}
	if got := ProjectSMBAnnouncements([]SMBPublishedAnnouncement{announcement}, "tenant-a", "other-team"); len(got) != 0 {
		t.Fatalf("wrong-audience announcement projection = %+v", got)
	}
	if _, err := AcknowledgeSMBAnnouncement(announcement, "tenant-a", "employee-1", "policy-v3", "sha256:policy-v3", time.Now()); err == nil {
		t.Fatal("acknowledgement accepted a stale document version")
	}
	announcement.Deployed = false
	if got := ProjectSMBAnnouncements([]SMBPublishedAnnouncement{announcement}, "tenant-a", "team-support"); len(got) != 0 {
		t.Fatalf("undeployed announcement projection = %+v", got)
	}
}
