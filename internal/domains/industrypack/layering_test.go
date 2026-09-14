package industrypack

import (
	"errors"
	"strings"
	"testing"
)

func layerSpec() LayerSpec {
	return LayerSpec{
		Packs: []LayerPack{
			{Layer: LayerCustomer, PackID: "acme", Version: 4, Settings: []LayerSetting{
				{Family: "session", Key: "idle_minutes", Values: []string{"10"}},
				{Family: "retention", Key: "document_classes", Values: []string{"acme-badge-photos"}},
				{Family: "branding", Key: "theme", Values: []string{"acme-dark"}},
				{Family: "approval", Key: "promotion_chain", Values: []string{"manager", "hrbp"}},
			}},
			{Layer: LayerPlatform, PackID: "platform", Version: 12, Settings: []LayerSetting{
				{Family: "session", Key: "idle_minutes", Values: []string{"30"}, Mandatory: true, Class: ClassSecurity},
				{Family: "branding", Key: "theme", Values: []string{"default"}},
				{Family: "locale", Key: "fallback", Values: []string{"en-US"}},
			}},
			{Layer: LayerCountry, PackID: "us-country", Version: 2026, Settings: []LayerSetting{
				{Family: "retention", Key: "document_classes", Values: []string{"i9", "w4"}, Mandatory: true, Class: ClassLegal},
				{Family: "approval", Key: "promotion_chain", Values: []string{"manager"}, Mandatory: true, Class: ClassLegal},
			}},
			{Layer: LayerIndustry, PackID: "healthcare-base", Version: 2, Settings: []LayerSetting{
				{Family: "retention", Key: "document_classes", Values: []string{"license-verification"}},
				{Family: "session", Key: "idle_minutes", Values: []string{"15"}},
			}},
		},
		Policies: []FamilyPolicy{
			{Family: "session", Strategy: StrategyRestrictive, LowerIsStricter: true},
			{Family: "retention", Strategy: StrategyAdditive},
			{Family: "branding", Strategy: StrategyPrecedence},
			{Family: "approval", Strategy: StrategyCustom, Resolver: "chain-union"},
		},
		Resolvers: map[string]CustomResolver{"chain-union": func(_ string, cs []Contribution) ([]string, error) {
			var all []string
			for _, c := range cs {
				all = append(all, c.Values...)
			}
			return all, nil
		}},
	}
}

func setting(t *testing.T, cfg LayeredConfig, key string) ResolvedSetting {
	t.Helper()
	for _, s := range cfg.Settings {
		if s.Family+"."+s.Key == key {
			return s
		}
	}
	t.Fatalf("setting %s missing", key)
	return ResolvedSetting{}
}

func wantLayerRefusal(t *testing.T, err error, field, state, version string) {
	t.Helper()
	var r *LayerRefusal
	if !errors.As(err, &r) || !errors.Is(err, ErrLayerComposition) || r.Code != LayerRejectionCode || r.Field != field || r.State != state || r.Version != version ||
		!strings.Contains(r.Error(), LayerRejectionCode) {
		t.Fatalf("err = %v, want %s %s@%s", err, field, state, version)
	}
}

// TestTodo_PACK_006 proves each explicit strategy resolves its family across
// platform, country, industry and customer layers with provenance.
func TestTodo_PACK_006(t *testing.T) {
	cfg, err := ComposeLayers(layerSpec())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Layers, ","); got != "PLATFORM:platform@12,COUNTRY:us-country@2026,INDUSTRY:healthcare-base@2,CUSTOMER:acme@4" {
		t.Fatalf("layers = %s", got)
	}
	for key, want := range map[string]string{
		"session.idle_minutes":       "10",
		"retention.document_classes": "acme-badge-photos,i9,license-verification,w4",
		"branding.theme":             "acme-dark",
		"approval.promotion_chain":   "hrbp,manager",
		"locale.fallback":            "en-US",
	} {
		if got := strings.Join(setting(t, cfg, key).Values, ","); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
	if s := setting(t, cfg, "session.idle_minutes"); !s.Mandatory || s.Class != ClassSecurity || len(s.Provenance) != 3 {
		t.Fatalf("session provenance = %+v", s)
	}
	if !strings.HasPrefix(cfg.Digest, "sha256:") {
		t.Fatal("no digest")
	}
}

// TestTodo_PACK_006_Golden pins the composed digest and its independence
// from input pack order.
func TestTodo_PACK_006_Golden(t *testing.T) {
	cfg, err := ComposeLayers(layerSpec())
	if err != nil {
		t.Fatal(err)
	}
	spec := layerSpec()
	spec.Packs[0], spec.Packs[3] = spec.Packs[3], spec.Packs[0]
	again, err := ComposeLayers(spec)
	if err != nil || again.Digest != cfg.Digest {
		t.Fatalf("order-dependent digest: %s vs %s (%v)", again.Digest, cfg.Digest, err)
	}
	const golden = "sha256:570213745ad8c77cc1e71e393a0384cf8b5f3600542032ae83787d8f122e3cf1"
	if cfg.Digest != golden {
		t.Fatalf("golden digest = %s, want %s", cfg.Digest, golden)
	}
}

// TestTodo_PACK_006_Security proves mandatory security and legal constraints
// cannot be weakened by any higher layer under any strategy.
func TestTodo_PACK_006_Security(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LayerSpec)
		field  string
		pack   string
	}{
		{"customer relaxes idle timeout", func(s *LayerSpec) { s.Packs[0].Settings[0].Values = []string{"45"} }, "settings[session.idle_minutes]", "acme@4"},
		{"customer removes legal retention", func(s *LayerSpec) {
			s.Packs[0].Settings[1] = LayerSetting{Family: "retention", Key: "document_classes", Values: []string{"i9"}, Remove: true}
		}, "settings[retention.document_classes]", "acme@4"},
		{"customer overrides mandatory precedence", func(s *LayerSpec) {
			s.Packs[1].Settings[1].Mandatory, s.Packs[1].Settings[1].Class = true, ClassSecurity
		}, "settings[branding.theme]", "acme@4"},
		{"custom resolver drops legal approver", func(s *LayerSpec) {
			s.Resolvers["chain-union"] = func(string, []Contribution) ([]string, error) { return []string{"hrbp"}, nil }
		}, "settings[approval.promotion_chain]", "acme@4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := layerSpec()
			tc.mutate(&spec)
			_, err := ComposeLayers(spec)
			wantLayerRefusal(t, err, tc.field, LayerWeakenedMandatory, tc.pack)
		})
	}
	// A non-mandatory removal and a stricter customer value are allowed.
	spec := layerSpec()
	spec.Packs[3].Settings[0] = LayerSetting{Family: "retention", Key: "document_classes", Values: []string{"acme-badge-photos"}, Remove: true}
	spec.Packs[0].Settings[1] = LayerSetting{Family: "retention", Key: "document_classes", Values: []string{"license-verification"}, Remove: true}
	cfg, err := ComposeLayers(spec)
	if err != nil || strings.Join(setting(t, cfg, "retention.document_classes").Values, ",") != "i9,w4" {
		t.Fatalf("non-mandatory removal = %+v, %v", cfg, err)
	}
}

// TestTodo_PACK_006_Mutation proves every blocking ambiguity and invalid
// layer is refused with the offending pack version.
func TestTodo_PACK_006_Mutation(t *testing.T) {
	cases := []struct {
		name                  string
		mutate                func(*LayerSpec)
		field, state, version string
	}{
		{"no strategy for conflict", func(s *LayerSpec) { s.Policies = s.Policies[1:] }, "settings[session.idle_minutes]", LayerBlockingAmbiguity, "acme@4"},
		{"no strategy for removal", func(s *LayerSpec) {
			s.Packs[1].Settings[2].Values = []string{"en-US"}
			s.Packs[0].Settings = append(s.Packs[0].Settings, LayerSetting{Family: "locale", Key: "fallback", Values: []string{"en-US"}, Remove: true})
		}, "settings[locale.fallback]", LayerBlockingAmbiguity, "acme@4"},
		{"two packs one layer", func(s *LayerSpec) {
			s.Packs = append(s.Packs, LayerPack{Layer: LayerCustomer, PackID: "acme-eu", Version: 1})
		}, "packs", LayerBlockingAmbiguity, "acme-eu@1"},
		{"unknown layer", func(s *LayerSpec) { s.Packs[0].Layer = 9 }, "packs", LayerInvalid, "acme@4"},
		{"no identity", func(s *LayerSpec) { s.Packs[0].Version = 0 }, "packs", LayerInvalid, "acme@0"},
		{"no platform", func(s *LayerSpec) { s.Packs = append(s.Packs[:1], s.Packs[2:]...) }, "packs", LayerInvalid, ""},
		{"precedence same-layer conflict", func(s *LayerSpec) {
			s.Packs[0].Settings = append(s.Packs[0].Settings, LayerSetting{Family: "branding", Key: "theme", Values: []string{"acme-light"}})
		}, "settings[branding.theme]", LayerBlockingAmbiguity, "acme@4"},
		{"restrictive multi value", func(s *LayerSpec) { s.Packs[0].Settings[0].Values = []string{"5", "6"} }, "settings[session.idle_minutes]", LayerBlockingAmbiguity, "acme@4"},
		{"restrictive non numeric", func(s *LayerSpec) { s.Packs[0].Settings[0].Values = []string{"soon"} }, "settings[session.idle_minutes]", LayerBlockingAmbiguity, "acme@4"},
		{"resolver missing", func(s *LayerSpec) { s.Resolvers = nil }, "settings[approval.promotion_chain]", LayerBlockingAmbiguity, "acme@4"},
		{"resolver fails", func(s *LayerSpec) {
			s.Resolvers["chain-union"] = func(string, []Contribution) ([]string, error) { return nil, errors.New("cycle") }
		}, "settings[approval.promotion_chain]", LayerBlockingAmbiguity, "acme@4"},
		{"unknown strategy", func(s *LayerSpec) { s.Policies[2].Strategy = "MERGE" }, "settings[branding.theme]", LayerBlockingAmbiguity, "acme@4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := layerSpec()
			tc.mutate(&spec)
			_, err := ComposeLayers(spec)
			wantLayerRefusal(t, err, tc.field, tc.state, tc.version)
		})
	}
	// HIGHER_IS_STRICTER picks the largest value.
	spec := layerSpec()
	spec.Policies[0].LowerIsStricter = false
	spec.Packs[1].Settings[0].Mandatory = false
	cfg, err := ComposeLayers(spec)
	if err != nil || setting(t, cfg, "session.idle_minutes").Values[0] != "30" {
		t.Fatalf("higher-is-stricter = %+v, %v", cfg, err)
	}
	for l, want := range map[Layer]string{LayerPlatform: "PLATFORM", LayerCountry: "COUNTRY", LayerIndustry: "INDUSTRY", LayerCustomer: "CUSTOMER", 0: "UNKNOWN"} {
		if l.String() != want {
			t.Errorf("Layer(%d) = %s", l, l.String())
		}
	}
}
