package productui

import (
	"strings"
	"testing"
)

func TestChatRetentionAdminPageLinksToDedicatedChatSettings(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageAdmin), []string{RoleHCMAdmin})
	view.ChatRetentionConfigured = true
	view.ChatRetentionPolicy = ChatRetentionPolicy{Mode: "BEFORE_DATE", BeforeDate: "2026-01-15", Revision: 4}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Chat settings", "/workspace/app/admin/chat-settings", "Manage tenant-wide chat retention settings."} {
		if !strings.Contains(doc, want) {
			t.Errorf("admin chat settings link missing %q", want)
		}
	}
	if strings.Contains(doc, `data-chat-retention="settings"`) {
		t.Fatal("admin overview still renders chat retention controls inline")
	}
}

func TestDedicatedChatSettingsPageShowsSavedPolicyAndPendingEnforcement(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageChatSettings), []string{RoleHCMAdmin})
	view.ChatRetentionConfigured = true
	view.ChatRetentionPolicy = ChatRetentionPolicy{Mode: "BEFORE_DATE", BeforeDate: "2026-01-15", Revision: 4}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Chat settings", "Chat retention", "Before a date: 2026-01-15", "Enforcement is not active yet", "Save policy"} {
		if !strings.Contains(doc, want) {
			t.Errorf("dedicated chat settings page missing %q", want)
		}
	}
	if count := strings.Count(doc, "Manage tenant-wide chat retention settings."); count != 1 {
		t.Fatalf("chat settings subtitle appears %d times, want exactly once", count)
	}
}

func TestChatRetentionAdminPageRemainsBehindAdminRouteAuthorization(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageChatSettings), []string{"worker_self"})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `data-chat-retention="settings"`) {
		t.Fatal("non-admin render exposed chat retention settings")
	}
}

func TestDedicatedChatSettingsPageUsesArabicRTLCopy(t *testing.T) {
	view := ApplyRoleVisibility(testView(PageChatSettings), []string{RoleHCMAdmin})
	view.Locale = ResolveProductLocale("ar")
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"إعدادات الدردشة", "أدر إعدادات الاحتفاظ برسائل الدردشة على مستوى المستأجر.", `dir="rtl"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("Arabic chat settings page missing %q", want)
		}
	}
}
