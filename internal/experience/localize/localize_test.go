package localize

import (
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"
)

func fixtureRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	if err := r.Register(Catalog{Locale: "en-US", Version: "v1", Messages: map[string]Message{
		"welcome": {Text: "Welcome, {name}!"},
		"items":   {Plural: map[string]string{"one": "{count} item", "other": "{count} items"}},
		"actor":   {Gender: map[string]string{"female": "She", "male": "He"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Catalog{Locale: "ar", Version: "v1", Messages: map[string]Message{"welcome": {Text: "مرحبا"}}}); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLocalizationResolutionReturnsVersionedTextFormattingAndDirection(t *testing.T) {
	r := fixtureRegistry(t)
	got, err := r.Resolve(Context{Locale: "en_US", TimeZone: "UTC", CatalogVersion: "v1"}, "welcome", ResolveOptions{Vars: map[string]string{"name": "Ada"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Welcome, Ada!" || got.CatalogVersion != "v1" || got.Direction != LTR {
		t.Fatalf("unexpected result: %+v", got)
	}
	if got.FormatProfile != "en-US/gregorian/UTC" {
		t.Fatalf("profile = %q", got.FormatProfile)
	}
}

func TestTodo_I18N_002_Property(t *testing.T) {
	r := fixtureRegistry(t)
	for _, n := range []string{"0", "1", "2", "1000000.50"} {
		rr, _ := new(big.Rat).SetString(n)
		got, err := r.Resolve(Context{Locale: "en-US", CatalogVersion: "v1"}, "items", ResolveOptions{Count: rr, Vars: map[string]string{"count": n}})
		if err != nil {
			t.Fatal(err)
		}
		if got.PluralCase == "" || got.Text == "" {
			t.Fatalf("unresolved %q: %+v", n, got)
		}
	}
}

func TestTodo_I18N_002_Golden(t *testing.T) {
	if got, _ := FormatNumber("en-US", "1234567.895", 2); got != "1,234,567.90" {
		t.Fatalf("number = %q", got)
	}
	if got, _ := FormatMoney("de-DE", "1234.5", "EUR", 2); got != "1.234,50\u00a0EUR" {
		t.Fatalf("money = %q", got)
	}
}

func TestTodo_I18N_002_Race(t *testing.T) {
	r := fixtureRegistry(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Resolve(Context{Locale: "en-US", CatalogVersion: "v1"}, "welcome", ResolveOptions{})
		}()
	}
	wg.Wait()
}

func TestRegistryConcurrentRegistrationAndResolution(t *testing.T) {
	r := fixtureRegistry(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(version int) {
			defer wg.Done()
			_ = r.Register(Catalog{
				Locale:  "de-DE",
				Version: fmt.Sprintf("concurrent-%d", version),
				Messages: map[string]Message{
					"welcome": {Text: "Willkommen"},
				},
			})
		}(i)
		go func() {
			defer wg.Done()
			got, err := r.Resolve(Context{Locale: "en-US", CatalogVersion: "v1"}, "welcome", ResolveOptions{})
			if err != nil || got.Text == "" {
				t.Errorf("concurrent resolve failed: %+v, %v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_I18N_002_Fault(t *testing.T) {
	r := fixtureRegistry(t)
	if _, err := r.Resolve(Context{Locale: "en-US", CatalogVersion: "v1"}, "missing", ResolveOptions{}); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := FormatNumber("en-US", "not-a-number", 2); err == nil {
		t.Fatal("invalid decimal accepted")
	}
	if _, err := FormatDate(Context{Locale: "en-US", TimeZone: "No/Such_Zone"}, time.Now()); err == nil {
		t.Fatal("invalid timezone accepted")
	}
}

func TestTodo_UXAUDIT_006_I18N_SharedDateFormatting(t *testing.T) {
	instant := time.Date(2026, time.September, 3, 14, 5, 0, 0, time.UTC)
	for _, tc := range []struct {
		locale string
		want   string
	}{
		{locale: "en-US", want: "09/03/2026"},
		{locale: "de-DE", want: "03.09.2026"},
		{locale: "ar", want: "٣ سبتمبر ٢٠٢٦"},
		{locale: "fr-FR", want: "2026-09-03"},
	} {
		got, err := FormatDate(Context{Locale: tc.locale, TimeZone: "UTC"}, instant)
		if err != nil || got != tc.want {
			t.Errorf("FormatDate(%q) = (%q, %v), want %q", tc.locale, got, err, tc.want)
		}
	}
}

func TestTodo_I18N_002_Security(t *testing.T) {
	r := fixtureRegistry(t)
	got, err := r.Resolve(Context{Locale: "en-US", CatalogVersion: "v1"}, "welcome", ResolveOptions{Vars: map[string]string{"name": "<script>alert(1)</script>"}})
	if err != nil || got.Text == "" {
		t.Fatal("interpolation failed")
	}
	// Values are data, not format strings: braces and percent signs survive.
	if got.Text != "Welcome, <script>alert(1)</script>!" {
		t.Fatalf("value changed: %q", got.Text)
	}
}

func TestTodo_I18N_002_Conformance(t *testing.T) {
	if DirectionFor("ar-SA") != RTL || DirectionFor("he") != RTL || DirectionFor("fr-FR") != LTR {
		t.Fatal("direction mismatch")
	}
	if got := FormatName("ja-JP", "太郎", "山田"); got != "山田 太郎" {
		t.Fatalf("name = %q", got)
	}
}

func TestTodo_I18N_002_Mutation(t *testing.T) {
	c := Catalog{Locale: "en-US", Version: "v1", Messages: map[string]Message{"x": {Text: "original", Plural: map[string]string{"one": "one"}}}}
	r := NewRegistry()
	if err := r.Register(c); err != nil {
		t.Fatal(err)
	}
	mutated := c.Messages["x"]
	mutated.Text = "changed"
	mutated.Plural["one"] = "changed"
	c.Messages["x"] = mutated
	got, err := r.Resolve(Context{Locale: "en-US", CatalogVersion: "v1"}, "x", ResolveOptions{})
	if err != nil || got.Text != "original" {
		t.Fatalf("registry mutated: %+v, %v", got, err)
	}
}
