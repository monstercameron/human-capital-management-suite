package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_007_SelfServiceBoundaryCopy(t *testing.T) {
	for _, item := range []struct {
		locale string
		title  string
		detail string
		badge  string
	}{
		{"en-US", "Request changes through a workflow", "Your records are view-only. Start an available workflow to request a change; each step is reviewed and recorded.", "View only"},
		{"de-DE", "Änderungen beantragen", "Ihre Daten sind schreibgeschützt. Starten Sie einen verfügbaren Ablauf, um eine Änderung zu beantragen. Jeder Schritt wird geprüft und dokumentiert.", "Nur ansehen"},
		{"ar", "اطلب التغييرات عبر سير العمل", "سجلاتك للعرض فقط. ابدأ سير عمل متاحًا لطلب تغيير؛ تُراجَع كل خطوة وتُسجَّل.", "للعرض فقط"},
	} {
		t.Run(item.locale, func(t *testing.T) {
			locale := ResolveProductLocale(item.locale)
			if got := locale.Text("myself.read_only_detail"); got != item.detail {
				t.Fatalf("detail catalog mismatch: got %q, want %q", got, item.detail)
			}
			markup, err := ui.RenderToString(SelfServiceBoundary(SelfServiceBoundaryProps{I18nProps: I18nProps{Locale: locale}}))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{item.title, item.badge, item.detail} {
				if !strings.Contains(markup, want) {
					t.Errorf("boundary omitted localized task copy %q", want)
				}
			}
			if strings.Contains(markup, "governed workflows") ||
				(item.locale != "en-US" && strings.Contains(markup, "Your records are view-only")) {
				t.Fatal("boundary leaked old or English fallback copy")
			}
		})
	}
}
