package productui

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// SMBPublishedAnnouncement is a projection of the existing documentation
// hub deployment and audience records. It carries the exact deployed version;
// an announcement cannot silently follow a later document deployment.
type SMBPublishedAnnouncement struct {
	TenantID          string
	AnnouncementID    string
	PublisherID       string
	AudienceID        string
	Message           string
	DocumentVersionID string
	DocumentDigest    string
	DueAt             time.Time
	PublishedAt       time.Time
	Deployed          bool
}

func (a SMBPublishedAnnouncement) Validate() error {
	for name, value := range map[string]string{
		"tenant": a.TenantID, "announcement": a.AnnouncementID, "publisher": a.PublisherID,
		"audience": a.AudienceID, "message": a.Message, "document version": a.DocumentVersionID,
		"document digest": a.DocumentDigest,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("announcement: %s is required", name)
		}
	}
	if a.PublishedAt.IsZero() || !a.Deployed {
		return fmt.Errorf("announcement: only a deployed immutable version may be published")
	}
	return nil
}

// ProjectSMBAnnouncements applies the server's already-authorized audience
// projection at the final UI boundary. It also refuses undeployed records so
// a draft or a later redeployment cannot change the acknowledgement target.
func ProjectSMBAnnouncements(items []SMBPublishedAnnouncement, tenantID, audienceID string) []SMBPublishedAnnouncement {
	result := make([]SMBPublishedAnnouncement, 0, len(items))
	for _, item := range items {
		if item.TenantID != tenantID || item.AudienceID != audienceID || !item.Deployed || item.Validate() != nil {
			continue
		}
		result = append(result, item)
	}
	return result
}

type SMBAnnouncementReceipt struct {
	TenantID          string
	AnnouncementID    string
	RecipientID       string
	DocumentVersionID string
	DocumentDigest    string
	AcknowledgedAt    time.Time
}

func (r SMBAnnouncementReceipt) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" || strings.TrimSpace(r.AnnouncementID) == "" || strings.TrimSpace(r.RecipientID) == "" || strings.TrimSpace(r.DocumentVersionID) == "" || strings.TrimSpace(r.DocumentDigest) == "" || r.AcknowledgedAt.IsZero() {
		return fmt.Errorf("announcement receipt: all receipt fields are required")
	}
	return nil
}

func AcknowledgeSMBAnnouncement(announcement SMBPublishedAnnouncement, tenantID, recipientID, versionID, digest string, at time.Time) (SMBAnnouncementReceipt, error) {
	if err := announcement.Validate(); err != nil {
		return SMBAnnouncementReceipt{}, err
	}
	if announcement.TenantID != tenantID || strings.TrimSpace(recipientID) == "" || versionID != announcement.DocumentVersionID || digest != announcement.DocumentDigest || at.IsZero() {
		return SMBAnnouncementReceipt{}, fmt.Errorf("announcement receipt: recipient or immutable version does not match")
	}
	return SMBAnnouncementReceipt{TenantID: tenantID, AnnouncementID: announcement.AnnouncementID, RecipientID: recipientID, DocumentVersionID: versionID, DocumentDigest: digest, AcknowledgedAt: at.UTC()}, nil
}

func SMBAnnouncementReminderDue(announcement SMBPublishedAnnouncement, acknowledged bool, now time.Time) bool {
	return !acknowledged && !announcement.DueAt.IsZero() && !now.Before(announcement.DueAt)
}

// SMBAnnouncementCard is the small Help-hub/chat card. Its copy explicitly
// distinguishes receipt from signature or comprehension.
func SMBAnnouncementCard(announcement SMBPublishedAnnouncement, receipt *SMBAnnouncementReceipt, now time.Time) ui.Node {
	status := "Acknowledgement due"
	if receipt != nil && receipt.DocumentVersionID == announcement.DocumentVersionID && receipt.DocumentDigest == announcement.DocumentDigest {
		status = "Acknowledged exact document version " + receipt.DocumentVersionID
	} else if !SMBAnnouncementReminderDue(announcement, false, now) {
		status = "Receipt not yet recorded"
	}
	return html.Section(html.Props{Class: "smb-announcement-card", Raw: map[string]any{"data-announcement-id": announcement.AnnouncementID, "data-document-version": announcement.DocumentVersionID}},
		html.H2(html.Props{Class: "smb-announcement-title"}, ui.Text(announcement.Message)),
		html.P(html.Props{Class: "smb-announcement-document"}, ui.Text("Document version "+announcement.DocumentVersionID+" ("+announcement.DocumentDigest+")")),
		html.P(html.Props{Class: "smb-announcement-status", Role: "status"}, ui.Text(status)),
		html.P(html.Props{Class: "smb-announcement-receipt-note"}, ui.Text("This records receipt of the exact version; it is not a signature or proof of comprehension.")),
	)
}
