package journey

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
)

func TestCustomerThemeRPCPreservesOrganizationColorTokens(t *testing.T) {
	want := preferences.TenantTheme{Version: 7, Theme: preferences.Theme{
		Palette: "custom", TokenOverrides: map[string]string{
			"color.brand.primary": "#4d1f78", "color.canvas": "#fbf9fd",
		}, DarkTokenOverrides: map[string]string{"color.brand.primary": "#ba9ce7", "color.canvas": "#101019"},
	}}
	got := fromCustomerTheme(toCustomerTheme(want))
	if got.Version != want.Version || got.Palette != want.Palette || !reflect.DeepEqual(got.TokenOverrides, want.TokenOverrides) || !reflect.DeepEqual(got.DarkTokenOverrides, want.DarkTokenOverrides) {
		t.Fatalf("custom token round trip = %+v, want %+v", got, want)
	}
}
