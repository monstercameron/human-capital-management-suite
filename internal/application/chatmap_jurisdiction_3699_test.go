package application

import (
	"net/http"
	"testing"
)

// ISSUE-3699: the country form accepted codes that are not countries ("ZZ",
// "Narnia") and markup ("<b>x</b>"). Only "*" (the workspace default), an ISO
// 3166-1 alpha-2 code, or a country name is stored; anything else is refused
// with 400 before the settings are written.
func TestIssue3699_JurisdictionRefusesUnknownCountries(t *testing.T) {
	for _, country := range []string{"ZZ", "Narnia", "<b>x</b>"} {
		f := &chatmapSettingsFixture{}
		h := ChatmapHTTP{Surface: f}
		body := `{"TenantID":"chatmap-tenant","Country":"` + country + `","Enabled":true,"Basis":"qa test"}`
		w := chatmapPost(t, h, "setjurisdiction", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%q: status %d, want 400", country, w.Code)
		}
		if f.saved.Country != "" {
			t.Fatalf("%q was written as %q", country, f.saved.Country)
		}
	}
}

func TestIssue3699_JurisdictionAcceptsKnownCountries(t *testing.T) {
	for in, want := range map[string]string{
		"DE": "DE", "germany": "DE", "UK": "GB", "ng": "NG", "*": "*",
	} {
		f := &chatmapSettingsFixture{}
		h := ChatmapHTTP{Surface: f}
		body := `{"TenantID":"chatmap-tenant","Country":"` + in + `","Enabled":false,"Basis":"pending"}`
		w := chatmapPost(t, h, "setjurisdiction", body)
		if w.Code != http.StatusOK || f.saved.Country != want {
			t.Fatalf("%q: status %d stored %q, want 200 and %q", in, w.Code, f.saved.Country, want)
		}
	}
}
