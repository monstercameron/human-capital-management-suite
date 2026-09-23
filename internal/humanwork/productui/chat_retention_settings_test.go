package productui

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestChatRetentionSettingsShowsUnsetPolicyAndSeparateDeletionBoundary(t *testing.T) {
	node := ChatRetentionSettings(ChatRetentionSettingsProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Editable: true})
	doc, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No policy is configured. All messages are retained.", "Before a date", "Storage limit", "Enforcement is not active yet", `id="chat-retention-date"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("retention UI missing %q", want)
		}
	}
	if strings.Contains(doc, "Delete now") || strings.Contains(doc, "Purge") {
		t.Fatalf("policy editor exposes a deletion action: %s", doc)
	}
}

func TestChatRetentionStylesUseSharedDesignTokens(t *testing.T) {
	css := ChatRetentionStylesheet()
	for _, want := range []string{
		"var(--hcm-space-3)", "var(--hcm-density)", "var(--hcm-font-size-small)",
		"var(--hcm-radius-control)", "var(--hcm-shadow-raised)",
		"color-mix(in srgb,var(--ink) 42%,transparent)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("chat retention stylesheet missing shared design token %q", want)
		}
	}
	for _, literal := range []string{"rgb(8 14 24 / 52%)", "0 20px 64px", "var(--radius)"} {
		if strings.Contains(css, literal) {
			t.Errorf("chat retention stylesheet retains avoidable literal %q", literal)
		}
	}
}

func TestProductStylesheetIncludesChatRetentionDesignTokens(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{".chat-retention-form{", ".chat-settings-page{", "var(--hcm-space-3)", "var(--hcm-shadow-raised)"} {
		if !strings.Contains(css, want) {
			t.Errorf("product stylesheet missing chat settings rule %q", want)
		}
	}
}

func TestChatRetentionConfirmationUsesAccessibleModalKeyboardLifecycle(t *testing.T) {
	source, err := os.ReadFile("chat_retention_settings.go")
	if err != nil {
		t.Fatalf("read retention settings source: %v", err)
	}
	body := string(source)
	for _, want := range []string{
		`useDrawerFocusTrap("chat-retention-confirm-dialog", "chat-retention-save", draft.Confirm)`,
		`ID: "chat-retention-confirm-dialog"`,
		`"role": "alertdialog"`,
		`"aria-modal": "true"`,
		`OnKeyDown: ui.UseEvent(func(event ui.KeyboardEvent)`,
		`drawerEscapeCloses(event.GetKey())`,
		`closeConfirm()`,
		`ui.Text(copy.cancel)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("retention confirmation is missing keyboard dialog behavior %q", want)
		}
	}
}

func TestChatRetentionSettingsRequiresExplicitConfirmationForReduction(t *testing.T) {
	date := chatRetentionDraft{Configured: true, Source: ChatRetentionPolicy{Mode: "BEFORE_DATE", BeforeDate: "2026-01-01"}, Mode: "BEFORE_DATE", BeforeDate: "2026-02-01"}
	if !retentionDraftReducesRetention(date) {
		t.Fatal("later date did not require confirmation")
	}
	budget := chatRetentionDraft{Configured: true, Source: ChatRetentionPolicy{Mode: "SIZE_BUDGET", BudgetBytes: 512 * (1 << 20)}, Mode: "SIZE_BUDGET", BudgetMiB: "256"}
	if !retentionDraftReducesRetention(budget) {
		t.Fatal("smaller disk budget did not require confirmation")
	}
	if retentionDraftReducesRetention(chatRetentionDraft{Configured: false, Mode: "BEFORE_DATE", BeforeDate: "2026-02-01"}) {
		t.Fatal("first policy configuration incorrectly required reduction confirmation")
	}
}

func TestChatRetentionSettingsRendersArabicDirectionAndLoadingState(t *testing.T) {
	node := ChatRetentionSettings(ChatRetentionSettingsProps{I18nProps: I18nProps{Locale: ResolveProductLocale("ar")}, Loading: true})
	doc, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `dir="rtl"`) || !strings.Contains(doc, "جارٍ تحميل سياسة الاحتفاظ") || !strings.Contains(doc, `role="status"`) {
		t.Fatalf("Arabic loading state is incomplete: %s", doc)
	}
}

func TestRetentionBudgetBytesRejectsInvalidOrOverflowingValues(t *testing.T) {
	for _, raw := range []string{"", "0", "1.5", "-1", "18446744073709551615", "8796093022208"} {
		if value, ok := RetentionBudgetBytes(raw); ok || value != 0 {
			t.Errorf("budget %q = %d, %t, want rejected", raw, value, ok)
		}
	}
	if value, ok := RetentionBudgetBytes("128"); !ok || value != 128*(1<<20) {
		t.Fatalf("valid MiB conversion = %d, %t", value, ok)
	}
}
