package workspace

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatperfShellConfig is the island a signed-in person's Chat page carries.
func chatperfShellConfig() JourneyConfig {
	return JourneyConfig{
		TunnelURL: "ws://cell.test" + PathTunnel, Bearer: "token",
		Tenant: "ironridge-demo", Subject: "hc-001-walt-brennan", Purpose: "compensation_review",
		Roles: []string{"hcm_admin"},
	}
}

// BenchmarkTodo_CHATBUG_014_ShellDocument measures what the server does to
// produce the workspace document for Chat, with nothing read from a store:
//
//	go test -run '^$' -bench Todo_CHATBUG_014_ShellDocument -benchtime 20x ./internal/humanwork/workspace/
func BenchmarkTodo_CHATBUG_014_ShellDocument(b *testing.B) {
	config := chatperfShellConfig()
	locale := productui.ResolveProductLocale("en-US")
	theme := productui.DefaultCustomerTheme()
	stylesheet, err := productStylesheetForTheme(theme)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, err := productShellDocumentForRouteStateWithPreferencesAndNavigation(config, true, locale, productui.PageChat, "", "", theme, productui.DefaultAccessibilityPreferences(), stylesheet, nil, nil)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(doc)))
	}
}
